// Package incus certificate enrollment via trust tokens.
//
// Incus authenticates remote clients with a TLS client certificate. Rather than
// requiring operators to pre-provision a certificate, Incus can issue a
// one-time trust token that a client uses to register its own certificate into
// the server's trust store (see
// https://linuxcontainers.org/incus/docs/main/authentication/#adding-client-certificates-using-tokens).
//
// Because trust tokens are single-use, the generated keypair must be persisted
// and reused on subsequent starts. This file implements that flow with the
// standard library only (no Incus SDK):
//
//  1. If a persisted certificate exists in the cert store, use it (the token,
//     if any, is ignored — enrollment is idempotent).
//  2. Otherwise, if a trust token is configured, generate an ECDSA P-384
//     keypair + self-signed client certificate, POST it to /1.0/certificates
//     with the token, and persist the keypair on success.
//  3. Otherwise, fall back to any pre-provisioned cert/key files.
package incus

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maxfield-allison/dnsweaver/pkg/httputil"
)

// Cert store filenames for the persisted client keypair and pinned server
// fingerprint.
const (
	certFileName        = "client.crt"
	keyFileName         = "client.key"
	fingerprintFileName = "server.fingerprint"

	// defaultCertName is the certificate name registered in the Incus trust
	// store, and the CommonName of the generated certificate, when unset.
	defaultCertName = "dnsweaver"
)

// EnrollConfig configures trust-token enrollment.
type EnrollConfig struct {
	// BaseURL is the remote Incus API base URL (https://host:8443). Required.
	BaseURL string

	// Token is the one-time Incus trust token. When empty, no enrollment is
	// attempted (the persisted cert, or the fallback cert/key, is used).
	Token string

	// CertStore is the writable directory where the enrolled keypair is
	// persisted (client.crt / client.key). Required when Token is set.
	CertStore string

	// Name is the certificate name registered in the Incus trust store.
	// Defaults to "dnsweaver" when empty.
	Name string

	// Projects, when non-empty, registers the certificate as restricted to
	// those projects. Empty registers an unrestricted certificate.
	Projects []string

	// TLS is the TLS config used to reach the server during enrollment. Its
	// CAFile/ServerName/InsecureSkip settings apply to server verification.
	// CertFile/KeyFile are ignored (enrollment presents no client cert).
	TLS *httputil.TLSConfig

	// Logger defaults to slog.Default() if nil.
	Logger *slog.Logger
}

// ClientCert is the resolved certificate/key pair the Incus client should use.
type ClientCert struct {
	CertFile string
	KeyFile  string

	// PinnedSHA256 is the server leaf certificate fingerprint (hex) the client
	// should pin for ongoing connections. Populated from the trust token during
	// enrollment and from the persisted store on reuse. Empty when no pin is
	// available (e.g. fallback cert/key files, or a token without a fingerprint).
	PinnedSHA256 string
}

// certificatesPost is the /1.0/certificates request body. Mirrors the subset of
// the Incus API we need. The certificate itself is presented in the TLS
// handshake (mTLS), not in the body: an untrusted client registering via a
// trust token authenticates by presenting the very certificate it wants added,
// and Incus reads it from the connection's peer certificate. The server adds it
// to the trust store when the trust token is valid.
type certificatesPost struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	TrustToken string   `json:"trust_token"`
	Restricted bool     `json:"restricted,omitempty"`
	Projects   []string `json:"projects,omitempty"`
}

// EnsureClientCert resolves the client certificate to use for the Incus API.
//
// Precedence:
//  1. A persisted keypair in CertStore (idempotent reuse; token ignored).
//  2. Fresh enrollment with Token (generates + persists a keypair).
//  3. The fallback CertFile/KeyFile (returned when no token is configured).
//
// The returned ClientCert has empty fields when neither a persisted cert, a
// token, nor fallback files are available; the caller then proceeds without a
// client certificate.
func EnsureClientCert(ctx context.Context, cfg EnrollConfig, fallbackCert, fallbackKey string) (ClientCert, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// 1. Reuse a persisted keypair if present.
	if cfg.CertStore != "" {
		certPath := filepath.Join(cfg.CertStore, certFileName)
		keyPath := filepath.Join(cfg.CertStore, keyFileName)
		if fileExists(certPath) && fileExists(keyPath) {
			logger.Info("using persisted incus client certificate",
				slog.String("cert_store", cfg.CertStore),
			)
			return ClientCert{
				CertFile:     certPath,
				KeyFile:      keyPath,
				PinnedSHA256: loadPersistedFingerprint(cfg.CertStore),
			}, nil
		}
	}

	// 2. Enroll with a trust token.
	if cfg.Token != "" {
		if cfg.CertStore == "" {
			return ClientCert{}, fmt.Errorf("incus: DNSWEAVER_INCUS_CERT_STORE is required when a trust token is set (tokens are one-time use and the enrolled certificate must be persisted)")
		}
		cc, err := enroll(ctx, cfg, logger)
		if err != nil {
			return ClientCert{}, err
		}
		return cc, nil
	}

	// 3. Fall back to pre-provisioned cert/key files.
	return ClientCert{CertFile: fallbackCert, KeyFile: fallbackKey}, nil
}

// enroll generates a keypair, registers it with the trust token, and persists
// it to the cert store.
//
// The generated certificate is presented as the TLS client certificate during
// the POST: Incus reads the enrolling certificate from the mTLS handshake, not
// the request body. Because the certificate is brand-new and not yet in the
// trust store, the connection is inherently "untrusted" until the token is
// validated server-side; the trust token in the body is what authorizes it.
func enroll(ctx context.Context, cfg EnrollConfig, logger *slog.Logger) (ClientCert, error) {
	certPEM, keyPEM, _, err := generateClientCert(cfg.Name)
	if err != nil {
		return ClientCert{}, fmt.Errorf("incus: generating client certificate: %w", err)
	}

	clientCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return ClientCert{}, fmt.Errorf("incus: loading generated keypair: %w", err)
	}

	// The trust token embeds the server's certificate fingerprint. Pinning to
	// it lets enrollment (and later connections) verify the server without a CA
	// file and without a blanket InsecureSkip, even though Incus's default
	// certificate carries only loopback SANs and so fails hostname verification
	// against a remote address.
	fingerprint := tokenFingerprint(cfg.Token)

	name := cfg.Name
	if name == "" {
		name = defaultCertName
	}
	body := certificatesPost{
		Name:       name,
		Type:       "client",
		TrustToken: cfg.Token,
		Restricted: len(cfg.Projects) > 0,
		Projects:   cfg.Projects,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return ClientCert{}, fmt.Errorf("incus: encoding certificate request: %w", err)
	}

	client, err := enrollmentClient(cfg.TLS, clientCert, fingerprint)
	if err != nil {
		return ClientCert{}, fmt.Errorf("incus: building enrollment client: %w", err)
	}

	url := strings.TrimRight(cfg.BaseURL, "/") + "/1.0/certificates"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return ClientCert{}, fmt.Errorf("incus: creating certificate request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return ClientCert{}, fmt.Errorf("incus: registering certificate with token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return ClientCert{}, fmt.Errorf("incus: certificate registration failed: %s", decodeAPIError(resp))
	}

	// Persist only after a successful registration so a failed attempt is
	// retried on the next start.
	if err := os.MkdirAll(cfg.CertStore, 0o700); err != nil {
		return ClientCert{}, fmt.Errorf("incus: creating cert store %q: %w", cfg.CertStore, err)
	}
	certPath := filepath.Join(cfg.CertStore, certFileName)
	keyPath := filepath.Join(cfg.CertStore, keyFileName)
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return ClientCert{}, fmt.Errorf("incus: writing key to cert store: %w", err)
	}
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		return ClientCert{}, fmt.Errorf("incus: writing certificate to cert store: %w", err)
	}
	// Persist the pinned fingerprint so restarts (when the one-time token is
	// already consumed) can still verify the server without a CA file.
	if fingerprint != "" {
		fpPath := filepath.Join(cfg.CertStore, fingerprintFileName)
		if err := os.WriteFile(fpPath, []byte(fingerprint+"\n"), 0o600); err != nil {
			return ClientCert{}, fmt.Errorf("incus: writing server fingerprint to cert store: %w", err)
		}
	}

	logger.Info("enrolled and persisted incus client certificate via trust token",
		slog.String("name", name),
		slog.String("cert_store", cfg.CertStore),
		slog.Bool("restricted", len(cfg.Projects) > 0),
		slog.Bool("server_pinned", fingerprint != ""),
	)
	return ClientCert{CertFile: certPath, KeyFile: keyPath, PinnedSHA256: fingerprint}, nil
}

// generateClientCert returns a fresh ECDSA P-384 keypair and self-signed X.509
// client certificate, PEM-encoded, plus the raw certificate DER.
func generateClientCert(commonName string) (certPEM, keyPEM, certDER []byte, err error) {
	if commonName == "" {
		commonName = defaultCertName
	}
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("ecdsa key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("serial: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("creating cert: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshaling key: %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, der, nil
}

// enrollmentClient builds an HTTP client that presents clientCert in the TLS
// handshake. Server verification honors the operator's TLS settings (CAFile,
// ServerName, InsecureSkip) from cfg; any CertFile/KeyFile on cfg is ignored
// because the enrollment cert is the freshly generated one, not a persisted
// pair. When pin is non-empty it is applied as a leaf-certificate fingerprint
// pin so the server verifies without a CA file. When no TLS config is supplied
// a default is used.
func enrollmentClient(cfg *httputil.TLSConfig, clientCert tls.Certificate, pin string) (*http.Client, error) {
	var serverConf httputil.TLSConfig
	if cfg != nil {
		serverConf = *cfg
	}
	// The generated keypair is presented explicitly below; drop any
	// file-based client cert so Build() doesn't try to load one.
	serverConf.CertFile = ""
	serverConf.KeyFile = ""
	if pin != "" {
		serverConf.PinnedSHA256 = pin
	}

	tlsConf, err := serverConf.Build()
	if err != nil {
		return nil, err
	}
	if tlsConf == nil {
		tlsConf = &tls.Config{MinVersion: httputil.DefaultTLSMinVersion} //nolint:gosec // DefaultTLSMinVersion is TLS 1.2
	}
	tlsConf.Certificates = []tls.Certificate{clientCert}

	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsConf,
		},
	}, nil
}

// tokenFingerprint decodes an Incus trust token and returns the embedded server
// certificate fingerprint (hex SHA-256). Incus trust tokens are base64-encoded
// JSON that includes a "fingerprint" field. Returns "" when the token cannot be
// decoded or carries no fingerprint, in which case enrollment falls back to the
// operator's CA/ServerName/InsecureSkip settings.
func tokenFingerprint(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return ""
	}
	var payload struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Fingerprint)
}

// loadPersistedFingerprint reads a previously persisted server fingerprint from
// the cert store. Returns "" when absent or unreadable.
func loadPersistedFingerprint(certStore string) string {
	data, err := os.ReadFile(filepath.Join(certStore, fingerprintFileName))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// decodeAPIError extracts a useful message from an Incus error response.
func decodeAPIError(resp *http.Response) string {
	var env struct {
		Error string `json:"error"`
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&env); err == nil && env.Error != "" {
		return fmt.Sprintf("status %d: %s", resp.StatusCode, env.Error)
	}
	return fmt.Sprintf("status %d", resp.StatusCode)
}

// fileExists reports whether path exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
