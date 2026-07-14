package provider

import (
	"log/slog"
	"strings"

	"github.com/maxfield-allison/dnsweaver/pkg/httputil"
)

// TLS-related keys lifted out of ProviderInstanceConfig.ProviderConfig and
// passed through HTTPConfig.TLS. Centralizing the key names here keeps the
// loader (internal/config) and the registry in sync. The loader writes these
// keys when DNSWEAVER_{NAME}_TLS_* env vars are set; the legacy
// INSECURE_SKIP_VERIFY key is also honored as a back-compat alias for
// TLS_SKIP_VERIFY.
const (
	tlsKeyCAFile     = "TLS_CA_FILE"
	tlsKeyCertFile   = "TLS_CERT_FILE"
	tlsKeyKeyFile    = "TLS_KEY_FILE"
	tlsKeyServerName = "TLS_SERVER_NAME"
	tlsKeySkipVerify = "TLS_SKIP_VERIFY"
	tlsKeyMinVersion = "TLS_MIN_VERSION"

	// TLSLegacyKeySkipVerify is the deprecated alias retained for one release.
	// The loader rewrites it into TLS_SKIP_VERIFY before the map ever reaches
	// the registry, so this constant is exported informationally for callers
	// that need to detect or migrate legacy configs.
	TLSLegacyKeySkipVerify = "INSECURE_SKIP_VERIFY"
)

// extractTLSConfig pulls TLS_* entries out of the per-instance provider
// config map and returns a *httputil.TLSConfig. Returns nil when no TLS
// settings are configured (so the resulting client uses stdlib defaults).
//
// Parsing errors (e.g. unsupported MIN_VERSION) are logged at WARN level
// against the named instance and the offending field is ignored — the
// instance still starts. A misconfigured CA path is NOT caught here; that
// surfaces inside httputil.NewClient → TLSConfig.Build at request time.
func extractTLSConfig(cfgMap map[string]string, logger *slog.Logger, instance string) *httputil.TLSConfig {
	if len(cfgMap) == 0 {
		return nil
	}

	tls := httputil.TLSConfig{
		CAFile:     cfgMap[tlsKeyCAFile],
		CertFile:   cfgMap[tlsKeyCertFile],
		KeyFile:    cfgMap[tlsKeyKeyFile],
		ServerName: cfgMap[tlsKeyServerName],
	}

	if v, ok := cfgMap[tlsKeySkipVerify]; ok && v != "" {
		tls.InsecureSkip = parseBoolish(v)
	}

	if v, ok := cfgMap[tlsKeyMinVersion]; ok && v != "" {
		parsed, err := httputil.ParseTLSMinVersion(v)
		if err != nil {
			if logger != nil {
				logger.Warn("ignoring invalid TLS min version, falling back to default",
					slog.String("instance", instance),
					slog.String("value", v),
					slog.String("error", err.Error()),
				)
			}
		} else {
			tls.MinVersion = parsed
		}
	}

	if tls.IsZero() {
		return nil
	}
	return &tls
}

// parseBoolish accepts the same truthy values as internal/config.parseBool so
// the registry does not need to import internal packages.
func parseBoolish(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}
