package pfsense

import (
	"net"
	"strings"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// hostOverride is the engine-agnostic representation of a single pfSense host
// override. The DNS Resolver (Unbound) stores multiple IPs per override; the
// DNS Forwarder (Dnsmasq) stores exactly one. The backend maps this struct to
// and from the wire shape for the selected resolver.
type hostOverride struct {
	// ID is pfSense's object identifier for the row (an index into the config
	// array for the community pfrest backend). Empty on records we are about
	// to create; populated on records returned by List.
	ID string
	// Host is the short hostname (no domain). "web" in "web.example.com".
	Host string
	// Domain is the parent domain. "example.com" in "web.example.com".
	Domain string
	// IPs are the A/AAAA targets for this override. Unbound may carry several;
	// Dnsmasq carries exactly one.
	IPs []string
	// Descr is the free-form description. dnsweaver stores its ownership marker
	// here.
	Descr string
}

// containsIP reports whether the override already targets the given IP.
func (h hostOverride) containsIP(ip string) bool {
	for _, existing := range h.IPs {
		if strings.EqualFold(strings.TrimSpace(existing), strings.TrimSpace(ip)) {
			return true
		}
	}
	return false
}

// withoutIP returns a copy of the override's IPs with the given IP removed.
func (h hostOverride) withoutIP(ip string) []string {
	out := make([]string, 0, len(h.IPs))
	for _, existing := range h.IPs {
		if strings.EqualFold(strings.TrimSpace(existing), strings.TrimSpace(ip)) {
			continue
		}
		out = append(out, existing)
	}
	return out
}

// recordTypeForIP returns the DNS record type an IP address maps to. Only A and
// AAAA are supported; a non-IP target returns ok=false.
func recordTypeForIP(ip string) (provider.RecordType, bool) {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return "", false
	}
	if parsed.To4() != nil {
		return provider.RecordTypeA, true
	}
	return provider.RecordTypeAAAA, true
}

// joinFQDN glues a hostname and domain into a fully qualified name, trimming
// trailing dots and lowercasing so comparisons are stable.
func joinFQDN(hostname, domain string) string {
	hostname = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(hostname)), ".")
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	switch {
	case hostname == "" && domain == "":
		return ""
	case hostname == "":
		return domain
	case domain == "":
		return hostname
	default:
		return hostname + "." + domain
	}
}

// splitFQDN cleaves a FQDN into (hostname, domain). Returns ok=false if there is
// no dot to split on — pfSense host overrides always require both a hostname
// and a domain.
func splitFQDN(fqdn string) (hostname, domain string, ok bool) {
	fqdn = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(fqdn)), ".")
	idx := strings.Index(fqdn, ".")
	if idx <= 0 || idx == len(fqdn)-1 {
		return "", "", false
	}
	return fqdn[:idx], fqdn[idx+1:], true
}

// inZone returns true if fqdn is equal to or a subdomain of zone.
func inZone(fqdn, zone string) bool {
	fqdn = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(fqdn)), ".")
	zone = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(zone)), ".")
	if zone == "" {
		return true
	}
	return fqdn == zone || strings.HasSuffix(fqdn, "."+zone)
}
