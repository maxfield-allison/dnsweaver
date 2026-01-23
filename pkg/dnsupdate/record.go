package dnsupdate

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/miekg/dns"
)

// Record represents a DNS record for RFC 2136 operations.
type Record struct {
	// Name is the DNS name (e.g., "myhost.example.com." or "myhost").
	// Will be normalized to FQDN format with trailing dot.
	Name string

	// Type is the DNS record type (e.g., dns.TypeA, dns.TypeCNAME).
	Type uint16

	// TTL is the time-to-live in seconds.
	TTL uint32

	// RData is the record data (e.g., IP address, target hostname).
	// Format depends on record type.
	RData string

	// Priority is used for MX and SRV records.
	Priority uint16

	// Weight is used for SRV records.
	Weight uint16

	// Port is used for SRV records.
	Port uint16
}

// TypeString returns the string representation of the record type.
func (r Record) TypeString() string {
	if name, ok := dns.TypeToString[r.Type]; ok {
		return name
	}
	return fmt.Sprintf("TYPE%d", r.Type)
}

// ToRR converts the Record to a dns.RR (Resource Record).
func (r Record) ToRR() (dns.RR, error) {
	name := r.Name
	if !strings.HasSuffix(name, ".") {
		name += "."
	}

	header := dns.RR_Header{
		Name:   name,
		Rrtype: r.Type,
		Class:  dns.ClassINET,
		Ttl:    r.TTL,
	}

	switch r.Type {
	case dns.TypeA:
		ip := net.ParseIP(r.RData)
		if ip == nil || ip.To4() == nil {
			return nil, fmt.Errorf("invalid IPv4 address: %s", r.RData)
		}
		return &dns.A{Hdr: header, A: ip.To4()}, nil

	case dns.TypeAAAA:
		ip := net.ParseIP(r.RData)
		if ip == nil || ip.To16() == nil || ip.To4() != nil {
			return nil, fmt.Errorf("invalid IPv6 address: %s", r.RData)
		}
		return &dns.AAAA{Hdr: header, AAAA: ip.To16()}, nil

	case dns.TypeCNAME:
		target := r.RData
		if !strings.HasSuffix(target, ".") {
			target += "."
		}
		return &dns.CNAME{Hdr: header, Target: target}, nil

	case dns.TypeTXT:
		// TXT records can contain multiple strings
		return &dns.TXT{Hdr: header, Txt: []string{r.RData}}, nil

	case dns.TypeMX:
		target := r.RData
		if !strings.HasSuffix(target, ".") {
			target += "."
		}
		return &dns.MX{Hdr: header, Preference: r.Priority, Mx: target}, nil

	case dns.TypeSRV:
		target := r.RData
		if !strings.HasSuffix(target, ".") {
			target += "."
		}
		return &dns.SRV{
			Hdr:      header,
			Priority: r.Priority,
			Weight:   r.Weight,
			Port:     r.Port,
			Target:   target,
		}, nil

	case dns.TypePTR:
		target := r.RData
		if !strings.HasSuffix(target, ".") {
			target += "."
		}
		return &dns.PTR{Hdr: header, Ptr: target}, nil

	case dns.TypeNS:
		ns := r.RData
		if !strings.HasSuffix(ns, ".") {
			ns += "."
		}
		return &dns.NS{Hdr: header, Ns: ns}, nil

	case dns.TypeCAA:
		// CAA format: flag tag value (e.g., "0 issue letsencrypt.org")
		parts := strings.SplitN(r.RData, " ", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid CAA format: expected 'flag tag value', got: %s", r.RData)
		}
		flag, err := strconv.ParseUint(parts[0], 10, 8)
		if err != nil {
			return nil, fmt.Errorf("invalid CAA flag: %w", err)
		}
		return &dns.CAA{
			Hdr:   header,
			Flag:  uint8(flag),
			Tag:   parts[1],
			Value: parts[2],
		}, nil

	default:
		return nil, fmt.Errorf("unsupported record type: %s", r.TypeString())
	}
}

// RecordFromRR creates a Record from a dns.RR.
func RecordFromRR(rr dns.RR) (Record, error) {
	header := rr.Header()
	record := Record{
		Name: header.Name,
		Type: header.Rrtype,
		TTL:  header.Ttl,
	}

	switch v := rr.(type) {
	case *dns.A:
		record.RData = v.A.String()

	case *dns.AAAA:
		record.RData = v.AAAA.String()

	case *dns.CNAME:
		record.RData = v.Target

	case *dns.TXT:
		record.RData = strings.Join(v.Txt, " ")

	case *dns.MX:
		record.RData = v.Mx
		record.Priority = v.Preference

	case *dns.SRV:
		record.RData = v.Target
		record.Priority = v.Priority
		record.Weight = v.Weight
		record.Port = v.Port

	case *dns.PTR:
		record.RData = v.Ptr

	case *dns.NS:
		record.RData = v.Ns

	case *dns.CAA:
		record.RData = fmt.Sprintf("%d %s %s", v.Flag, v.Tag, v.Value)

	default:
		return record, fmt.Errorf("unsupported record type: %s", dns.TypeToString[header.Rrtype])
	}

	return record, nil
}

// NewARecord creates a new A record.
func NewARecord(name, ip string, ttl uint32) Record {
	return Record{
		Name:  name,
		Type:  dns.TypeA,
		TTL:   ttl,
		RData: ip,
	}
}

// NewAAAARecord creates a new AAAA record.
func NewAAAARecord(name, ip string, ttl uint32) Record {
	return Record{
		Name:  name,
		Type:  dns.TypeAAAA,
		TTL:   ttl,
		RData: ip,
	}
}

// NewCNAMERecord creates a new CNAME record.
func NewCNAMERecord(name, target string, ttl uint32) Record {
	return Record{
		Name:  name,
		Type:  dns.TypeCNAME,
		TTL:   ttl,
		RData: target,
	}
}

// NewTXTRecord creates a new TXT record.
func NewTXTRecord(name, text string, ttl uint32) Record {
	return Record{
		Name:  name,
		Type:  dns.TypeTXT,
		TTL:   ttl,
		RData: text,
	}
}

// NewMXRecord creates a new MX record.
func NewMXRecord(name, mailserver string, priority uint16, ttl uint32) Record {
	return Record{
		Name:     name,
		Type:     dns.TypeMX,
		TTL:      ttl,
		RData:    mailserver,
		Priority: priority,
	}
}

// NewSRVRecord creates a new SRV record.
func NewSRVRecord(name, target string, priority, weight, port uint16, ttl uint32) Record {
	return Record{
		Name:     name,
		Type:     dns.TypeSRV,
		TTL:      ttl,
		RData:    target,
		Priority: priority,
		Weight:   weight,
		Port:     port,
	}
}

// NewPTRRecord creates a new PTR record.
func NewPTRRecord(name, target string, ttl uint32) Record {
	return Record{
		Name:  name,
		Type:  dns.TypePTR,
		TTL:   ttl,
		RData: target,
	}
}

// NewNSRecord creates a new NS record.
func NewNSRecord(name, nameserver string, ttl uint32) Record {
	return Record{
		Name:  name,
		Type:  dns.TypeNS,
		TTL:   ttl,
		RData: nameserver,
	}
}

// StringToType converts a record type string to its uint16 value.
func StringToType(s string) (uint16, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if t, ok := dns.StringToType[s]; ok {
		return t, nil
	}
	return 0, fmt.Errorf("unknown record type: %s", s)
}

// SupportedTypes returns a list of record types supported by this package.
func SupportedTypes() []uint16 {
	return []uint16{
		dns.TypeA,
		dns.TypeAAAA,
		dns.TypeCNAME,
		dns.TypeTXT,
		dns.TypeMX,
		dns.TypeSRV,
		dns.TypePTR,
		dns.TypeNS,
		dns.TypeCAA,
	}
}

// SupportedTypeStrings returns a list of supported record type names.
func SupportedTypeStrings() []string {
	types := SupportedTypes()
	names := make([]string, len(types))
	for i, t := range types {
		names[i] = dns.TypeToString[t]
	}
	return names
}

// IsTypeSupported checks if a record type is supported.
func IsTypeSupported(recordType uint16) bool {
	for _, t := range SupportedTypes() {
		if t == recordType {
			return true
		}
	}
	return false
}
