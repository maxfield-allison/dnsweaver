// Package unifi implements the DNSWeaver provider interface for UniFi Network
// consoles, driven by the official UniFi Network Integration API.
//
// # Integration API
//
// UniFi Network exposes DNS records as "DNS policies" through its Integration
// API (Settings > Control Plane > Integrations), a versioned REST API that is
// authenticated with a single API key sent in the X-API-KEY header. DNS
// policies first appeared in UniFi Network 10.1, so the console must run that
// version or later. The provider deliberately avoids the undocumented legacy
// static-dns endpoints, which need cookie login and CSRF handling and have no
// native update.
//
// The Integration API is served under /proxy/network/integration/v1 on UniFi
// OS consoles (Dream Machine, Cloud Gateway, Cloud Key) and on UniFi OS
// Server, Ubiquiti's self-hosted UniFi OS for Linux. The legacy standalone
// Network Application (the Linux, Windows, macOS and Docker builds) does not
// expose the Integration API or API keys at all and is not supported.
//
// # Configuration
//
// Required environment variables (with DNSWEAVER_{INSTANCE}_ prefix):
//
//	URL       Console base URL (e.g. https://192.168.1.1)
//	API_KEY   Integration API key, sent as the X-API-KEY header
//
// Optional:
//
//	SITE      Site internalReference or UUID (default "default")
//	ZONE      DNS zone for record filtering (e.g. "home.example.com")
//	TTL       TTL for A, AAAA and CNAME records (default 300)
//	TLS_*     Unified framework TLS knobs (CA file, skip verify, etc.)
//
// The API key supports the _FILE suffix for Docker secrets:
//
//	API_KEY_FILE=/run/secrets/unifi_api_key
//
// # Ownership
//
// The Integration API supports TXT records, so this provider uses dnsweaver's
// standard TXT ownership tracking (Capabilities.SupportsOwnershipTXT is true).
// The API requires TXT text containing commas to be wrapped in double quotes;
// every ownership value contains commas, so the provider quotes on write and
// unquotes on read.
//
// # Record types
//
// A, AAAA, CNAME, TXT and SRV. TXT and SRV policies carry no TTL on the
// console, so List reports the instance TTL for them. MX and FORWARD_DOMAIN
// policies are ignored. Only policies the console reports as USER_DEFINED are
// managed; system-derived entries (device names, etc.) are never listed or
// deleted. Disabled policies are treated as absent.
package unifi
