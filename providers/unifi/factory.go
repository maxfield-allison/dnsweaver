package unifi

import (
	"github.com/maxfield-allison/dnsweaver/pkg/httputil"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

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

		return New(cfg.Name, providerCfg,
			WithProviderHTTPClient(httpClient),
			WithProviderLogger(cfg.HTTP.Logger),
		)
	}
}
