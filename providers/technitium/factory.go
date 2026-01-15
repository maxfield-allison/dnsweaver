package technitium

import (
	"fmt"
	"log/slog"
	"net/http"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/httputil"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

// Factory returns a provider.Factory for creating Technitium provider instances.
// This is the recommended way to register the Technitium provider with the registry.
func Factory() provider.Factory {
	return func(cfg provider.FactoryConfig) (provider.Provider, error) {
		// Parse provider-specific configuration from the map
		providerCfg, err := LoadConfigFromMap(cfg.Name, cfg.ProviderConfig)
		if err != nil {
			return nil, err
		}

		// Merge TLS skip verify: HTTP config from registry (global/per-instance) OR legacy per-provider setting
		tlsSkipVerify := cfg.HTTP.TLSSkipVerify || providerCfg.InsecureSkipVerify

		// Create HTTP client with the merged HTTP configuration
		httpClient := httputil.NewClient(&httputil.ClientConfig{
			Timeout:       cfg.HTTP.Timeout,
			TLSSkipVerify: tlsSkipVerify,
			UserAgent:     cfg.HTTP.UserAgent,
			Logger:        cfg.HTTP.Logger,
		})

		// Log warning if TLS verification is disabled
		if tlsSkipVerify && cfg.HTTP.Logger != nil {
			cfg.HTTP.Logger.Warn("TLS certificate verification disabled for Technitium provider",
				slog.String("provider", cfg.Name),
				slog.String("url", providerCfg.URL),
			)
		}

		// Create the provider with the HTTP client
		return NewWithHTTPClient(cfg.Name, providerCfg, httpClient, cfg.HTTP.Logger)
	}
}

// NewWithHTTPClient creates a new Technitium provider with a pre-configured HTTP client.
// This allows the factory to pass in a properly configured HTTP client with
// timeout, TLS settings, user-agent, and debug logging already applied.
func NewWithHTTPClient(name string, config *Config, httpClient *http.Client, logger *slog.Logger) (*Provider, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	if logger == nil {
		logger = slog.Default()
	}

	p := &Provider{
		name:   name,
		zone:   config.Zone,
		ttl:    config.TTL,
		logger: logger,
	}

	// Create the API client with the provided HTTP client
	p.client = NewClient(config.URL, config.Token, WithHTTPClient(httpClient), WithLogger(logger))

	return p, nil
}
