package source

// Hostname represents a hostname extracted from container labels.
//
// Each hostname carries context about where it was discovered, which is
// useful for logging, debugging, and potentially for determining which
// DNS provider should handle it.
type Hostname struct {
	// Name is the fully qualified hostname (e.g., "app.example.com").
	Name string

	// Source identifies which source extracted this hostname (e.g., "traefik").
	// This matches the value returned by Source.Name().
	Source string

	// Router is an optional identifier for the router/upstream that defined this hostname.
	// For Traefik, this would be the router name (e.g., "myapp@docker").
	// May be empty if the source doesn't support this concept.
	Router string
}

// String returns a human-readable representation of the hostname.
func (h Hostname) String() string {
	if h.Router != "" {
		return h.Name + " (from " + h.Source + ":" + h.Router + ")"
	}
	return h.Name + " (from " + h.Source + ")"
}

// Hostnames is a slice of Hostname with helper methods.
type Hostnames []Hostname

// Names returns just the hostname strings from the slice.
func (hs Hostnames) Names() []string {
	names := make([]string, len(hs))
	for i, h := range hs {
		names[i] = h.Name
	}
	return names
}

// Deduplicate returns a new slice with duplicate hostnames removed.
// The first occurrence of each hostname is kept.
func (hs Hostnames) Deduplicate() Hostnames {
	seen := make(map[string]struct{}, len(hs))
	result := make(Hostnames, 0, len(hs))

	for _, h := range hs {
		if _, exists := seen[h.Name]; !exists {
			seen[h.Name] = struct{}{}
			result = append(result, h)
		}
	}

	return result
}

// Filter returns a new slice containing only hostnames where the predicate returns true.
func (hs Hostnames) Filter(predicate func(Hostname) bool) Hostnames {
	result := make(Hostnames, 0)
	for _, h := range hs {
		if predicate(h) {
			result = append(result, h)
		}
	}
	return result
}

// FromSource returns a new slice containing only hostnames from the specified source.
func (hs Hostnames) FromSource(sourceName string) Hostnames {
	return hs.Filter(func(h Hostname) bool {
		return h.Source == sourceName
	})
}
