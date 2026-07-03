package opnsense

import (
	"github.com/maxfield-allison/dnsweaver/pkg/httputil"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// Factory returns a provider.Factory for creating OPNsense provider instances.
// The framework calls this once per configured DNSWEAVER_INSTANCES entry of
// type "opnsense".
//
// TLS is configured framework-wide via cfg.HTTP.TLS — this provider does not
// re-interpret TLS settings. httputil.NewClient itself emits the WARN when
// verification is disabled, so no duplicate warning here.
func Factory() provider.Factory {
	return func(cfg provider.FactoryConfig) (provider.Provider, error) {
		providerCfg, err := LoadConfigFromMap(cfg.Name, cfg.ProviderConfig)
		if err != nil {
			return nil, err
		}

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
