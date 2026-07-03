// Package opnsense implements the DNSWeaver provider interface for OPNsense
// firewalls, driven by the OPNsense REST API.
//
// OPNsense ships with two DNS resolvers: Unbound (default) and Dnsmasq (24.7+).
// Both expose "host override" records through mirror-shaped REST endpoints, so
// a single provider covers both via an ENGINE setting.
//
// The provider is strictly REST-based. It never SSH's into OPNsense or edits
// config.xml directly — those approaches fight OPNsense's own state machine
// and break under HA sync, firmware upgrades, and backup/restore. Operators
// who want file-level access to a standalone Linux dnsmasq should use the
// dedicated dnsmasq provider instead.
//
// # Configuration
//
// Required environment variables (with DNSWEAVER_{INSTANCE}_ prefix):
//
//	URL         OPNsense base URL (e.g. https://opnsense.internal)
//	API_KEY     OPNsense API key (per-user, generated in System > Access > Users)
//	API_SECRET  OPNsense API secret (paired with API_KEY)
//	ENGINE      "unbound" (default) or "dnsmasq"
//
// Optional:
//
//	ZONE              DNS zone for record filtering (e.g. "example.com")
//	TTL               Informational only — host overrides have no per-record TTL
//	RECONFIGURE_MODE  "per_write" (default) or "never"
//	TLS_*             Unified framework TLS knobs (CA file, skip verify, etc.)
//
// Credentials support the _FILE suffix (Docker secrets):
//
//	API_KEY_FILE=/run/secrets/opnsense_key
//	API_SECRET_FILE=/run/secrets/opnsense_secret
//
// # Ownership
//
// Neither Unbound nor Dnsmasq host overrides expose TXT records, so ownership
// TXT records are unavailable (Capabilities.SupportsOwnershipTXT is false).
// The provider tags each record it manages with a `dnsweaver:{instance}` marker
// in the OPNsense description field, and List returns only records carrying
// that marker — so operator-managed host overrides are never touched.
//
// # Reconfigure semantics
//
// OPNsense requires a "reconfigure" call to actually reload the resolver
// after mutations. The reconfigure endpoint fully reloads the daemon, so:
//
//   - per_write (default): reconfigure after every Create/Delete
//   - never: skip auto-reconfigure entirely (operator triggers it out of band)
//
// v1 does not batch/debounce reconfigure calls; that is a planned follow-up.
//
// # Record types (v1)
//
// A and AAAA on both engines. CNAME and other types are deferred.
package opnsense
