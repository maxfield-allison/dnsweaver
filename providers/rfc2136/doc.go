// Package rfc2136 implements the DNSWeaver provider interface for RFC 2136 Dynamic DNS updates.
//
// RFC 2136 is the industry-standard protocol for programmatic DNS updates, supported by
// virtually all authoritative DNS servers including BIND, Windows DNS, PowerDNS, Knot DNS,
// NSD, and Technitium.
//
// # Features
//
//   - Native RFC 2136 Dynamic DNS support
//   - TSIG authentication (HMAC-SHA256, SHA512, MD5)
//   - Record types: A, AAAA, CNAME, TXT, SRV (MX, PTR via pkg/dnsupdate, pending provider interface #133)
//   - Atomic update operations via native DNS UPDATE
//   - TCP or UDP transport
//   - Ownership tracking via TXT records
//
// # Limitations
//
//   - List() returns empty (AXFR zone transfers not implemented - relies on ownership TXT records)
//
// # Configuration
//
// The provider is configured via environment variables:
//
//	# Required
//	DNSWEAVER_BIND_TYPE=rfc2136
//	DNSWEAVER_BIND_SERVER=ns1.example.com:53
//	DNSWEAVER_BIND_ZONE=example.com.
//	DNSWEAVER_BIND_DOMAINS=*.example.com
//
//	# TSIG Authentication (recommended)
//	DNSWEAVER_BIND_TSIG_KEY_NAME=dnsweaver.
//	DNSWEAVER_BIND_TSIG_SECRET=base64-encoded-secret
//	DNSWEAVER_BIND_TSIG_SECRET_FILE=/run/secrets/tsig-key  # Docker secrets pattern
//	DNSWEAVER_BIND_TSIG_ALGORITHM=hmac-sha256
//
//	# Optional
//	DNSWEAVER_BIND_TTL=300
//	DNSWEAVER_BIND_TIMEOUT=10
//	DNSWEAVER_BIND_USE_TCP=false
//
// # Usage
//
// The provider is registered with the provider registry using the Factory function:
//
//	registry.RegisterFactory("rfc2136", rfc2136.Factory())
//
// # When to Use RFC 2136 vs API Providers
//
// Choose RFC 2136 when:
//   - Your DNS server supports RFC 2136 but has no dedicated DNSWeaver provider
//   - You want a single provider configuration for multiple RFC 2136 servers
//   - You prefer using the industry-standard DNS protocol
//   - You need fine-grained TSIG-based authentication
//
// Choose dedicated API providers when:
//   - The provider offers richer features (Cloudflare proxy mode, etc.)
//   - Your server doesn't support RFC 2136 (cloud DNS services)
//   - You prefer REST/HTTP over the DNS protocol
//   - Zone transfers (AXFR) are disabled for security
package rfc2136
