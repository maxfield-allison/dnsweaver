package opnsense

// hostRecord is the engine-agnostic representation of a single OPNsense
// host override. Both the Unbound and Dnsmasq engines map their native
// payload shapes to and from this struct.
type hostRecord struct {
	// UUID is OPNsense's internal identifier for the row. Empty on records
	// we are about to create; populated on records returned by search.
	UUID string
	// Hostname is the short hostname (no domain). "web" in "web.example.com".
	Hostname string
	// Domain is the parent domain. "example.com" in "web.example.com".
	Domain string
	// Type is the DNS record type: "A" or "AAAA". CNAME and other types are
	// deferred to a future release.
	Type string
	// Target is the IP address for A/AAAA records.
	Target string
	// Description is a free-form field. dnsweaver stores its ownership marker here.
	Description string
	// Enabled reflects the OPNsense "enabled" toggle on the row.
	Enabled bool
}

// engine adapts the shared OPNsense REST surface (search / add / delete /
// reconfigure) to the per-resolver endpoint paths and JSON payload shapes.
//
// Each concrete engine (unbound, dnsmasq) is a stateless value: the client
// holds all mutable state (HTTP transport, credentials, logger).
type engine interface {
	// Name returns the engine identifier ("unbound" or "dnsmasq"). Used in
	// logs and errors; does not affect wire behavior.
	Name() Engine

	// SearchPath is the API path for listing host overrides.
	// e.g. "/api/unbound/settings/searchHostOverride".
	SearchPath() string

	// AddPath is the API path for creating a new host override.
	AddPath() string

	// DelPath returns the API path for deleting a host override by UUID.
	DelPath(uuid string) string

	// ReconfigurePath is the API path that reloads the resolver so newly
	// written records take effect.
	ReconfigurePath() string

	// EncodeAddPayload marshals a hostRecord into the engine's add-request
	// JSON body.
	EncodeAddPayload(rec hostRecord) ([]byte, error)

	// DecodeSearchResponse parses the engine's search response into
	// hostRecords. Errors from malformed responses surface as-is so the
	// caller can attach transport context.
	DecodeSearchResponse(body []byte) ([]hostRecord, error)
}

// newEngine returns the engine implementation for the given identifier.
// The caller must have already validated the Engine value via Config.Validate.
func newEngine(e Engine) engine {
	switch e {
	case EngineDnsmasq:
		return dnsmasqEngine{}
	default:
		return unboundEngine{}
	}
}
