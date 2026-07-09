package pfsense

// resolver describes the REST resource layout for one pfSense DNS resolver.
// It is the small, engine-specific slice of behavior that the otherwise
// resolver-agnostic backend needs: which endpoints to hit and whether the
// resolver stores multiple IPs per host override.
type resolver struct {
	// name is the engine identifier ("unbound" or "dnsmasq").
	name Engine
	// single is the singular host-override endpoint (POST/PATCH/DELETE by id).
	single string
	// plural is the plural host-override endpoint (GET all rows).
	plural string
	// apply is the endpoint that reloads the resolver after mutations.
	apply string
	// multiIP is true when the resolver stores multiple IPs per (host, domain)
	// override (Unbound). Dnsmasq stores exactly one, so multiIP is false.
	multiIP bool
}

// resolverFor returns the resource layout for the given engine.
//
// Endpoints follow the community pfSense-pkg-RESTAPI (pfrest) v2 scheme:
//
//	Unbound  → /api/v2/services/dns_resolver/host_override(s), .../apply
//	Dnsmasq  → /api/v2/services/dns_forwarder/host_override(s), .../apply
func resolverFor(e Engine) resolver {
	switch e {
	case EngineDnsmasq:
		return resolver{
			name:    EngineDnsmasq,
			single:  "/api/v2/services/dns_forwarder/host_override",
			plural:  "/api/v2/services/dns_forwarder/host_overrides",
			apply:   "/api/v2/services/dns_forwarder/apply",
			multiIP: false,
		}
	default:
		return resolver{
			name:    EngineUnbound,
			single:  "/api/v2/services/dns_resolver/host_override",
			plural:  "/api/v2/services/dns_resolver/host_overrides",
			apply:   "/api/v2/services/dns_resolver/apply",
			multiIP: true,
		}
	}
}
