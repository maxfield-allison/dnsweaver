package webhook

import (
	"log/slog"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/httputil"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

// Factory returns a provider.Factory for creating Webhook provider instances.
// This is the recommended way to register the Webhook provider with the registry.
func Factory() provider.Factory {
	return func(cfg provider.FactoryConfig) (provider.Provider, error) {
		// Parse provider-specific configuration from the map
		providerCfg, err := LoadConfigFromMap(cfg.Name, cfg.ProviderConfig)
		if err != nil {
			return nil, err
		}

		// Create HTTP client with the factory's HTTP configuration
		// Note: Webhook provider has its own timeout handling via config.Timeout,
		// but we use the factory's HTTP config for TLS, user-agent, and logging
		httpClient := httputil.NewClient(&httputil.ClientConfig{
			Timeout:       cfg.HTTP.Timeout,
			TLSSkipVerify: cfg.HTTP.TLSSkipVerify,
			UserAgent:     cfg.HTTP.UserAgent,
			Logger:        cfg.HTTP.Logger,
		})

		// Log warning if TLS verification is disabled
		if cfg.HTTP.TLSSkipVerify && cfg.HTTP.Logger != nil {
			cfg.HTTP.Logger.Warn("TLS certificate verification disabled for Webhook provider",
				slog.String("provider", cfg.Name),
				slog.String("url", providerCfg.URL),
			)
		}

		// Create the provider with the pre-configured HTTP client
		return New(cfg.Name, providerCfg,
			WithProviderHTTPClient(httpClient),
			WithProviderLogger(cfg.HTTP.Logger),
		)
	}
}
