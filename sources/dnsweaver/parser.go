package dnsweaver

import (
	"log/slog"
	"regexp"
	"strconv"
	"strings"
)

// Label prefixes for dnsweaver labels.
const (
	// SimpleHostnameLabel is the label for simple hostname definition.
	SimpleHostnameLabel = "dnsweaver.hostname"

	// RecordsPrefix is the prefix for named record definitions.
	// Format: dnsweaver.records.<name>.<field>
	RecordsPrefix = "dnsweaver.records."
)

// Record fields for named records.
const (
	FieldHostname = "hostname"
	FieldType     = "type"
	FieldTarget   = "target"
	FieldProvider = "provider"
	FieldTTL      = "ttl"
	FieldPort     = "port"
	FieldPriority = "priority"
	FieldWeight   = "weight"
)

// namedRecordRegex matches dnsweaver.records.<name>.<field> labels.
// Captures: [1]=name, [2]=field
var namedRecordRegex = regexp.MustCompile(`^dnsweaver\.records\.([a-zA-Z0-9_-]+)\.([a-zA-Z0-9_]+)$`)

// SRVData contains SRV record-specific fields.
type SRVData struct {
	Port     uint16
	Priority uint16
	Weight   uint16
}

// Extraction represents a hostname extracted from dnsweaver labels.
type Extraction struct {
	// Hostname is the FQDN extracted from labels.
	Hostname string

	// RecordName is the identifier for named records (empty for simple hostname).
	RecordName string

	// Type is the record type override (A, AAAA, CNAME, SRV, PTR, TXT).
	// Empty means use provider default.
	Type string

	// Target is the record target override.
	// Empty means use provider default.
	Target string

	// Provider is the target provider instance name.
	// Empty means use domain matching.
	Provider string

	// TTL is the record TTL override.
	// Zero means use provider default.
	TTL int

	// SRV contains SRV-specific fields when Type is "SRV".
	SRV *SRVData
}

// HasHints returns true if any hint fields are set.
func (e Extraction) HasHints() bool {
	return e.Type != "" || e.Target != "" || e.Provider != "" || e.TTL > 0 || e.SRV != nil
}

// Parser extracts hostnames from dnsweaver labels.
type Parser struct {
	logger *slog.Logger
}

// ParserOption is a functional option for configuring Parser.
type ParserOption func(*Parser)

// WithParserLogger sets a custom logger for the parser.
func WithParserLogger(logger *slog.Logger) ParserOption {
	return func(p *Parser) {
		p.logger = logger
	}
}

// NewParser creates a new dnsweaver label parser.
func NewParser(opts ...ParserOption) *Parser {
	p := &Parser{
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// ExtractHostnames parses dnsweaver labels and returns all discovered hostnames.
func (p *Parser) ExtractHostnames(labels map[string]string) []Extraction {
	var extractions []Extraction

	// Handle simple hostname label
	if hostname, ok := labels[SimpleHostnameLabel]; ok {
		hostname = strings.TrimSpace(hostname)
		if hostname != "" {
			extractions = append(extractions, Extraction{
				Hostname: hostname,
			})
			p.logger.Debug("found simple dnsweaver hostname",
				slog.String("hostname", hostname),
			)
		}
	}

	// Collect named record fields
	namedRecords := make(map[string]map[string]string)

	for key, value := range labels {
		matches := namedRecordRegex.FindStringSubmatch(key)
		if matches == nil {
			continue
		}

		recordName := matches[1]
		field := strings.ToLower(matches[2])
		value = strings.TrimSpace(value)

		if namedRecords[recordName] == nil {
			namedRecords[recordName] = make(map[string]string)
		}
		namedRecords[recordName][field] = value
	}

	// Process named records
	for name, fields := range namedRecords {
		hostname, ok := fields[FieldHostname]
		if !ok || hostname == "" {
			p.logger.Warn("named record missing hostname",
				slog.String("record", name),
			)
			continue
		}

		extraction := Extraction{
			Hostname:   hostname,
			RecordName: name,
			Type:       strings.ToUpper(fields[FieldType]),
			Target:     fields[FieldTarget],
			Provider:   fields[FieldProvider],
		}

		// Parse TTL
		if ttlStr, ok := fields[FieldTTL]; ok && ttlStr != "" {
			if ttl, err := strconv.Atoi(ttlStr); err == nil && ttl > 0 {
				extraction.TTL = ttl
			} else {
				p.logger.Warn("invalid TTL value",
					slog.String("record", name),
					slog.String("ttl", ttlStr),
				)
			}
		}

		// Parse SRV fields if type is SRV or if port is specified
		if extraction.Type == "SRV" || fields[FieldPort] != "" {
			srv := &SRVData{}
			hasSRVData := false

			if portStr, ok := fields[FieldPort]; ok && portStr != "" {
				if port, err := strconv.ParseUint(portStr, 10, 16); err == nil {
					srv.Port = uint16(port)
					hasSRVData = true
				} else {
					p.logger.Warn("invalid port value",
						slog.String("record", name),
						slog.String("port", portStr),
					)
				}
			}

			if priorityStr, ok := fields[FieldPriority]; ok && priorityStr != "" {
				if priority, err := strconv.ParseUint(priorityStr, 10, 16); err == nil {
					srv.Priority = uint16(priority)
					hasSRVData = true
				} else {
					p.logger.Warn("invalid priority value",
						slog.String("record", name),
						slog.String("priority", priorityStr),
					)
				}
			}

			if weightStr, ok := fields[FieldWeight]; ok && weightStr != "" {
				if weight, err := strconv.ParseUint(weightStr, 10, 16); err == nil {
					srv.Weight = uint16(weight)
					hasSRVData = true
				} else {
					p.logger.Warn("invalid weight value",
						slog.String("record", name),
						slog.String("weight", weightStr),
					)
				}
			}

			if hasSRVData {
				extraction.SRV = srv
			}
		}

		extractions = append(extractions, extraction)
		p.logger.Debug("found named dnsweaver record",
			slog.String("name", name),
			slog.String("hostname", hostname),
			slog.String("type", extraction.Type),
			slog.String("target", extraction.Target),
			slog.String("provider", extraction.Provider),
		)
	}

	return extractions
}
