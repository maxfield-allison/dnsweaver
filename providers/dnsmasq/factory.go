package dnsmasq

import (
	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

// Factory returns a provider.Factory for creating dnsmasq provider instances.
// This is the recommended way to register the dnsmasq provider with the registry.
//
// Note: dnsmasq is a file-based provider and does not use HTTP clients,
// so the HTTP configuration from FactoryConfig is not used.
func Factory() provider.Factory {
	return func(cfg provider.FactoryConfig) (provider.Provider, error) {
		// dnsmasq is file-based, so we just pass through to NewFromMap
		// The HTTP config is not used for this provider
		return NewFromMap(cfg.Name, cfg.ProviderConfig)
	}
}
