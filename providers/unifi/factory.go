package unifi

import (
	"context"
	"time"

	"github.com/maxfield-allison/dnsweaver/pkg/httputil"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// siteResolveTimeout bounds the site lookup Factory performs, matching the
// provider manager's connectivity check.
const siteResolveTimeout = 10 * time.Second

// Factory returns a provider.Factory for creating UniFi provider instances.
func Factory() provider.Factory {
	return func(cfg provider.FactoryConfig) (provider.Provider, error) {
		providerCfg, err := LoadConfigFromMap(cfg.Name, cfg.ProviderConfig)
		if err != nil {
			return nil, err
		}

		// TLS settings (custom CA, mTLS, SNI, skip-verify) are configured
		// framework-wide via cfg.HTTP.TLS. httputil.NewClient itself logs
		// a WARN when verification is skipped. UniFi consoles present a
		// self-signed certificate by default, so operators typically set
		// TLS_CA_FILE or TLS_SKIP_VERIFY here.
		httpClient := httputil.NewClient(&httputil.ClientConfig{
			Timeout:   cfg.HTTP.Timeout,
			TLS:       cfg.HTTP.TLS,
			UserAgent: cfg.HTTP.UserAgent,
			Logger:    cfg.HTTP.Logger,
		})

		p, err := New(cfg.Name, providerCfg,
			WithProviderHTTPClient(httpClient),
			WithProviderLogger(cfg.HTTP.Logger),
		)
		if err != nil {
			return nil, err
		}

		// The registry records Identity as soon as the factory returns, so the
		// site must be resolved to its UUID now for "default" and that UUID to
		// group as one backend. An unreachable console fails the factory and
		// the provider manager retries it with backoff.
		ctx, cancel := context.WithTimeout(context.Background(), siteResolveTimeout)
		defer cancel()
		if _, err := p.resolveSiteID(ctx); err != nil {
			return nil, err
		}

		return p, nil
	}
}
