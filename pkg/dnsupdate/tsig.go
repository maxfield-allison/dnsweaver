package dnsupdate

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/miekg/dns"
)

// TSIG represents a Transaction Signature key for RFC 2845 authentication.
type TSIG struct {
	// Name is the key name (must end with a dot, e.g., "dnsweaver.").
	Name string

	// Secret is the base64-encoded shared secret.
	Secret string

	// Algorithm is the TSIG algorithm (e.g., dns.HmacSHA256).
	Algorithm string
}

// NewTSIG creates a TSIG configuration from the given parameters.
// The secret should be base64-encoded.
func NewTSIG(name, secret, algorithm string) (*TSIG, error) {
	// Ensure name ends with a dot
	if !strings.HasSuffix(name, ".") {
		name += "."
	}

	// Validate secret is valid base64
	if _, err := base64.StdEncoding.DecodeString(secret); err != nil {
		return nil, fmt.Errorf("tsig secret is not valid base64: %w", err)
	}

	// Normalize algorithm
	alg := normalizeAlgorithm(algorithm)
	if !isValidAlgorithm(alg) {
		return nil, fmt.Errorf("unsupported tsig algorithm: %s", algorithm)
	}

	return &TSIG{
		Name:      name,
		Secret:    secret,
		Algorithm: alg,
	}, nil
}

// TSIGFromConfig creates a TSIG configuration from a Config.
// Returns nil if TSIG is not configured.
func TSIGFromConfig(config *Config) (*TSIG, error) {
	if !config.HasTSIG() {
		return nil, nil //nolint:nilnil // nil TSIG is valid (no auth)
	}

	return NewTSIG(config.TSIGKeyName, config.TSIGSecret, config.GetTSIGAlgorithm())
}

// ApplyToClient applies the TSIG configuration to a dns.Client.
func (t *TSIG) ApplyToClient(client *dns.Client) {
	if t == nil {
		return
	}
	client.TsigSecret = map[string]string{t.Name: t.Secret}
}

// ApplyToMessage applies the TSIG signature to a DNS message.
// This should be called after the message is fully constructed.
func (t *TSIG) ApplyToMessage(msg *dns.Msg) {
	if t == nil {
		return
	}
	msg.SetTsig(t.Name, t.Algorithm, 300, 0)
}

// normalizeAlgorithm normalizes algorithm strings to miekg/dns format.
func normalizeAlgorithm(alg string) string {
	if alg == "" {
		return DefaultTSIGAlgorithm
	}

	// Normalize the algorithm string
	normalized := strings.ToLower(strings.TrimSpace(alg))

	switch normalized {
	case "hmac-md5", "md5":
		return dns.HmacMD5
	case "hmac-sha256", "sha256":
		return dns.HmacSHA256
	case "hmac-sha512", "sha512":
		return dns.HmacSHA512
	default:
		// Return as-is for already normalized values or unknown
		return alg
	}
}

// isValidAlgorithm checks if the algorithm is supported.
func isValidAlgorithm(alg string) bool {
	switch alg {
	case dns.HmacMD5, dns.HmacSHA256, dns.HmacSHA512:
		return true
	default:
		return false
	}
}

// AlgorithmName returns a human-readable name for an algorithm.
func AlgorithmName(alg string) string {
	switch alg {
	case dns.HmacMD5:
		return "HMAC-MD5"
	case dns.HmacSHA256:
		return "HMAC-SHA256"
	case dns.HmacSHA512:
		return "HMAC-SHA512"
	default:
		return alg
	}
}

// SupportedAlgorithms returns a list of supported TSIG algorithms.
func SupportedAlgorithms() []string {
	return []string{
		"hmac-sha256 (recommended)",
		"hmac-sha512",
		"hmac-md5 (legacy)",
	}
}
