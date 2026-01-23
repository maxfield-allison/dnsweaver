// Package dnsupdate provides RFC 2136 Dynamic DNS Update client utilities for DNSWeaver providers.
//
// This package enables providers to manage DNS records on any RFC 2136-compliant server,
// including BIND, Windows DNS Server, PowerDNS, Knot DNS, and many others.
//
// Key features:
//   - Full RFC 2136 (Dynamic Updates in DNS) support
//   - TSIG authentication (RFC 2845) with HMAC-MD5, HMAC-SHA256, HMAC-SHA512
//   - Support for all common record types (A, AAAA, CNAME, TXT, MX, SRV, PTR, NS)
//   - Docker secrets support (_FILE suffix pattern)
//   - Connection reuse with configurable timeouts
//   - Both UDP and TCP transport
//
// # Usage
//
// Create a client with configuration from environment variables:
//
//	config, err := dnsupdate.LoadConfig("DNSWEAVER_BIND_DNS_")
//	if err != nil {
//	    return err
//	}
//
//	client, err := dnsupdate.NewClient(config)
//	if err != nil {
//	    return err
//	}
//
//	// Create a record
//	err = client.Create(ctx, dnsupdate.Record{
//	    Name:  "myhost.example.com.",
//	    Type:  dns.TypeA,
//	    TTL:   300,
//	    RData: "192.168.1.100",
//	})
//
// # Environment Variables
//
// The following environment variables are supported (with prefix):
//
//	{PREFIX}SERVER          - DNS server address (e.g., "ns1.example.com:53")
//	{PREFIX}ZONE            - Zone name (e.g., "example.com.")
//	{PREFIX}TSIG_KEY_NAME   - TSIG key name (e.g., "dnsweaver.")
//	{PREFIX}TSIG_SECRET     - TSIG secret (base64-encoded)
//	{PREFIX}TSIG_SECRET_FILE - Path to file containing TSIG secret (Docker secrets)
//	{PREFIX}TSIG_ALGORITHM  - TSIG algorithm (hmac-sha256, hmac-sha512, hmac-md5)
//	{PREFIX}TIMEOUT         - Connection timeout in seconds (default: 10)
//	{PREFIX}USE_TCP         - Force TCP transport (default: false, uses UDP)
//
// # TSIG Authentication
//
// TSIG (Transaction Signature) is the standard authentication method for RFC 2136.
// Generate TSIG keys using BIND's dnssec-keygen or tsig-keygen:
//
//	tsig-keygen -a hmac-sha256 dnsweaver > dnsweaver.key
//
// Configure the key on your DNS server and provide the name and secret to DNSWeaver.
package dnsupdate
