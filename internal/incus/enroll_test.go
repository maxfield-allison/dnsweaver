package incus

import (
	"context"
	gotls "crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/httputil"
)

// tlsSkip returns a TLS config that skips server verification, for the
// self-signed cert used by httptest.NewTLSServer.
func tlsSkip() *httputil.TLSConfig {
	return &httputil.TLSConfig{InsecureSkip: true}
}

func TestGenerateClientCert(t *testing.T) {
	certPEM, keyPEM, der, err := generateClientCert("dnsweaver-test")
	if err != nil {
		t.Fatalf("generateClientCert: %v", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatal("cert PEM did not decode")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	if cert.Subject.CommonName != "dnsweaver-test" {
		t.Errorf("CN = %q, want dnsweaver-test", cert.Subject.CommonName)
	}
	if len(der) == 0 {
		t.Error("empty DER")
	}
	if kb, _ := pem.Decode(keyPEM); kb == nil || kb.Type != "PRIVATE KEY" {
		t.Error("key PEM did not decode")
	}
}

func TestEnsureClientCert_PersistedReuse(t *testing.T) {
	store := t.TempDir()
	certPath := filepath.Join(store, certFileName)
	keyPath := filepath.Join(store, keyFileName)
	if err := os.WriteFile(certPath, []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A token is set, but the persisted cert must win (idempotent) and no HTTP
	// call should occur (BaseURL is unreachable).
	cc, err := EnsureClientCert(context.Background(), EnrollConfig{
		BaseURL:   "https://127.0.0.1:1",
		Token:     "some-token",
		CertStore: store,
	}, "", "")
	if err != nil {
		t.Fatalf("EnsureClientCert: %v", err)
	}
	if cc.CertFile != certPath || cc.KeyFile != keyPath {
		t.Errorf("got %+v, want persisted paths", cc)
	}
}

func TestEnsureClientCert_Fallback(t *testing.T) {
	cc, err := EnsureClientCert(context.Background(), EnrollConfig{
		BaseURL: "https://incus:8443",
	}, "/etc/fallback.crt", "/etc/fallback.key")
	if err != nil {
		t.Fatalf("EnsureClientCert: %v", err)
	}
	if cc.CertFile != "/etc/fallback.crt" || cc.KeyFile != "/etc/fallback.key" {
		t.Errorf("got %+v, want fallback paths", cc)
	}
}

func TestEnsureClientCert_TokenRequiresStore(t *testing.T) {
	_, err := EnsureClientCert(context.Background(), EnrollConfig{
		BaseURL: "https://incus:8443",
		Token:   "tok",
	}, "", "")
	if err == nil {
		t.Fatal("expected error when token set without cert store")
	}
}

func TestEnsureClientCert_FreshEnrollment(t *testing.T) {
	var gotBody certificatesPost
	var peerCommonName string
	var peerPresented bool
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/1.0/certificates" || r.Method != http.MethodPost {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		// Incus reads the enrolling certificate from the TLS handshake, not
		// the body. Verify dnsweaver presented it there.
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			peerPresented = true
			peerCommonName = r.TLS.PeerCertificates[0].Subject.CommonName
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"type":"sync","status_code":200,"metadata":{}}`))
	}))
	// Request (but do not verify) a client certificate so the handshake
	// exposes the enrolling cert as a peer certificate.
	srv.TLS = &gotls.Config{ClientAuth: gotls.RequireAnyClientCert} //nolint:gosec // test server
	srv.StartTLS()
	defer srv.Close()

	store := t.TempDir()
	// The httptest TLS server uses a self-signed cert; skip verification.
	tls := tlsSkip()
	cc, err := EnsureClientCert(context.Background(), EnrollConfig{
		BaseURL:   srv.URL,
		Token:     "enroll-token",
		CertStore: store,
		Name:      "dnsweaver",
		Projects:  []string{"prod"},
		TLS:       tls,
	}, "", "")
	if err != nil {
		t.Fatalf("EnsureClientCert: %v", err)
	}

	// Cert + key persisted.
	if !fileExists(cc.CertFile) || !fileExists(cc.KeyFile) {
		t.Fatalf("keypair not persisted: %+v", cc)
	}
	// Request carried the token, type, and restricted projects.
	if gotBody.TrustToken != "enroll-token" {
		t.Errorf("TrustToken = %q", gotBody.TrustToken)
	}
	if gotBody.Type != "client" {
		t.Errorf("Type = %q, want client", gotBody.Type)
	}
	if !gotBody.Restricted || len(gotBody.Projects) != 1 || gotBody.Projects[0] != "prod" {
		t.Errorf("restriction not sent: restricted=%v projects=%v", gotBody.Restricted, gotBody.Projects)
	}
	// The generated certificate was presented in the TLS handshake.
	if !peerPresented {
		t.Error("client certificate was not presented in the TLS handshake")
	}
	if peerCommonName != "dnsweaver" {
		t.Errorf("peer certificate CommonName = %q, want dnsweaver", peerCommonName)
	}

	// A second call reuses the persisted cert (idempotent) without re-enrolling.
	cc2, err := EnsureClientCert(context.Background(), EnrollConfig{
		BaseURL:   srv.URL,
		Token:     "enroll-token",
		CertStore: store,
		TLS:       tls,
	}, "", "")
	if err != nil {
		t.Fatalf("second EnsureClientCert: %v", err)
	}
	if cc2.CertFile != cc.CertFile {
		t.Errorf("reuse path mismatch: %q vs %q", cc2.CertFile, cc.CertFile)
	}
}

func TestEnsureClientCert_EnrollmentFailureNotPersisted(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"invalid token"}`))
	}))
	defer srv.Close()

	store := t.TempDir()
	_, err := EnsureClientCert(context.Background(), EnrollConfig{
		BaseURL:   srv.URL,
		Token:     "bad-token",
		CertStore: store,
		TLS:       tlsSkip(),
	}, "", "")
	if err == nil {
		t.Fatal("expected enrollment error")
	}
	if fileExists(filepath.Join(store, certFileName)) {
		t.Error("certificate must not be persisted on failed enrollment")
	}
}
