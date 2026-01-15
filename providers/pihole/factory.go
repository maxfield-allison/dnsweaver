package pihole

import (
	"log/slog"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/httputil"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

// Factory returns a provider.Factory for creating Pi-hole provider instances.
// This is the recommended way to register the Pi-hole provider with the registry.
func Factory() provider.Factory {
	return func(cfg provider.FactoryConfig) (provider.Provider, error) {
		// Parse provider-specific configuration from the map
		providerCfg, err := LoadConfigFromMap(cfg.Name, cfg.ProviderConfig)
		if err != nil {
			return nil, err
		}

		// Build provider options
		opts := []ProviderOption{
			WithProviderLogger(cfg.HTTP.Logger),
		}

		// Only create HTTP client for API mode
		if providerCfg.Mode == ModeAPI {
			httpClient := httputil.NewClient(&httputil.ClientConfig{
				Timeout:       cfg.HTTP.Timeout,
				TLSSkipVerify: cfg.HTTP.TLSSkipVerify,
				UserAgent:     cfg.HTTP.UserAgent,
				Logger:        cfg.HTTP.Logger,
			})

			// Log warning if TLS verification is disabled
			if cfg.HTTP.TLSSkipVerify && cfg.HTTP.Logger != nil {
				cfg.HTTP.Logger.Warn("TLS certificate verification disabled for Pi-hole provider",
					slog.String("provider", cfg.Name),
					slog.String("url", providerCfg.URL),
				)
			}

			opts = append(opts, WithProviderHTTPClient(httpClient))
		}

		// Create the provider
		return New(cfg.Name, providerCfg, opts...)
	}
}
