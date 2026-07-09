// Package pfsense implements the DNSWeaver provider interface for pfSense
// firewalls, driven by the pfSense REST API.
//
// # REST API package
//
// pfSense does not ship a REST API in its base system. This provider talks to
// the community pfSense-pkg-RESTAPI package (https://pfrest.org), REST API v2 —
// the de facto standard, which works on both pfSense CE and pfSense Plus.
// Netgate ships no official REST API, so there is no alternative backend to
// abstract over. The operator must install and enable the package on the
// firewall first; the base pfSense install exposes no compatible endpoints.
//
// Like the OPNsense provider, pfSense exposes two DNS resolvers — the DNS
// Resolver (Unbound, default) and the DNS Forwarder (Dnsmasq) — selected with
// the ENGINE setting. Both manage "host override" records.
//
// The provider is strictly REST-based. It never SSH's into pfSense or edits
// config.xml directly — those approaches fight pfSense's own state machine and
// break under HA (CARP/XMLRPC) sync, upgrades, and backup/restore.
//
// # Configuration
//
// Required environment variables (with DNSWEAVER_{INSTANCE}_ prefix):
//
//	URL       pfSense base URL (e.g. https://pfsense.internal)
//	API_KEY   REST API key, sent as the X-API-Key header
//	          (System > REST API > Keys).
//
// Optional:
//
//	ENGINE            "unbound" (DNS Resolver, default) or "dnsmasq" (DNS Forwarder)
//	ZONE              DNS zone for record filtering (e.g. "example.com")
//	TTL               Informational only — host overrides have no per-record TTL
//	RECONFIGURE_MODE  "per_write" (default) or "never"
//	TLS_*             Unified framework TLS knobs (CA file, skip verify, etc.)
//
// The API key supports the _FILE suffix for Docker secrets:
//
//	API_KEY_FILE=/run/secrets/pfsense_key
//
// # Ownership
//
// pfSense host overrides expose no TXT records, so ownership TXT tracking is
// unavailable (Capabilities.SupportsOwnershipTXT is false). The provider tags
// each record it manages with a `dnsweaver:{instance}` marker in the host
// override description field, and List returns only records carrying that
// marker — so operator-managed host overrides are never touched by orphan
// cleanup.
//
// # Resolver differences
//
// The DNS Resolver (Unbound) stores multiple IPs per host override, keyed by a
// unique (host, domain) pair, so a dual-stack name (A + AAAA) is represented as
// one override with two IPs. The DNS Forwarder (Dnsmasq) stores a single IP per
// override, so it cannot hold both an A and an AAAA record for the same name —
// the provider surfaces that as an explicit error and recommends the Unbound
// engine for dual-stack.
//
// # Reconfigure semantics
//
// pfSense requires an "apply" call to reload the resolver after mutations:
//
//   - per_write (default): apply after every Create/Delete
//   - never: skip auto-apply entirely (operator applies out of band)
//
// # Record types (v1)
//
// A and AAAA on both engines. CNAME and other types are deferred.
package pfsense
