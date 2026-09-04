package technitium

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/maxfield-allison/dnsweaver/pkg/httputil"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
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

		// Create HTTP client with the framework-supplied HTTP configuration.
		// TLS settings (custom CA, mTLS, SNI, min version, skip verify) are
		// passed through cfg.HTTP.TLS — no per-provider TLS interpretation
		// here. httputil.NewClient logs a WARN itself when skip verify is on.
		httpClient := httputil.NewClient(&httputil.ClientConfig{
			Timeout:   cfg.HTTP.Timeout,
			TLS:       cfg.HTTP.TLS,
			UserAgent: cfg.HTTP.UserAgent,
			Logger:    cfg.HTTP.Logger,
		})

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
		name:             name,
		url:              config.URL,
		zone:             config.Zone,
		ttl:              config.TTL,
		autoHTTPSRecords: config.AutoHTTPSRecords,
		autoHTTPSALPN:    config.AutoHTTPSALPN,
		logger:           logger,
	}

	if p.autoHTTPSRecords {
		p.logger.Info("companion HTTPS records enabled (disable with AUTO_HTTPS_RECORDS=false)",
			slog.String("provider", name),
			slog.String("alpn", p.autoHTTPSALPN),
		)
	}

	// Create the API client with the provided HTTP client
	p.client = NewClient(config.URL, config.Token, WithHTTPClient(httpClient), WithLogger(logger))

	return p, nil
}
