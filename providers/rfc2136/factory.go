package rfc2136

import (
	"log/slog"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/dnsupdate"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

// Factory returns a provider.Factory for creating RFC 2136 provider instances.
// This is the recommended way to register the RFC 2136 provider with the registry.
func Factory() provider.Factory {
	return func(cfg provider.FactoryConfig) (provider.Provider, error) {
		// Parse provider-specific configuration from the map
		providerCfg, err := LoadConfigFromMap(cfg.Name, cfg.ProviderConfig)
		if err != nil {
			return nil, err
		}

		// Use logger from HTTP config (even though we don't use HTTP, it's our config source)
		logger := cfg.HTTP.Logger
		if logger == nil {
			logger = slog.Default()
		}

		// Create the dnsupdate client
		client, err := dnsupdate.NewClient(providerCfg.ToDNSUpdateConfig(), dnsupdate.WithLogger(logger))
		if err != nil {
			return nil, err
		}

		// Create the catalog for hostname enumeration
		catalog := dnsupdate.NewCatalog(client, providerCfg.Zone, logger)

		// Create the provider
		p := &Provider{
			name:    cfg.Name,
			zone:    providerCfg.Zone,
			ttl:     providerCfg.TTL,
			client:  client,
			catalog: catalog,
			logger:  logger,
		}

		logger.Info("RFC 2136 provider created",
			slog.String("name", cfg.Name),
			slog.String("server", providerCfg.Server),
			slog.String("zone", providerCfg.Zone),
			slog.Bool("tsig", providerCfg.TSIGKeyName != ""),
			slog.Bool("tcp", providerCfg.UseTCP),
			slog.Bool("catalog_enabled", true),
		)

		return p, nil
	}
}
