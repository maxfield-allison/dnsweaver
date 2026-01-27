package dnsupdate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Sentinel errors for RFC 2136 operations.
var (
	// ErrNotConfigured is returned when the client is not properly configured.
	ErrNotConfigured = errors.New("dnsupdate client is not configured")

	// ErrUpdateFailed is returned when the DNS UPDATE operation fails.
	ErrUpdateFailed = errors.New("dns update failed")

	// ErrRecordNotFound is returned when a record cannot be found for deletion/update.
	ErrRecordNotFound = errors.New("record not found")

	// ErrRecordExists is returned when trying to create a record that already exists.
	ErrRecordExists = errors.New("record already exists")

	// ErrAuthenticationFailed is returned when TSIG authentication fails.
	ErrAuthenticationFailed = errors.New("tsig authentication failed")

	// ErrConnectionFailed is returned when the connection to the DNS server fails.
	ErrConnectionFailed = errors.New("connection to dns server failed")

	// ErrZoneMismatch is returned when a record name doesn't match the configured zone.
	ErrZoneMismatch = errors.New("record name does not match configured zone")

	// ErrAXFRFailed is returned when a zone transfer (AXFR) fails.
	// This typically happens when the server blocks zone transfers.
	ErrAXFRFailed = errors.New("zone transfer (AXFR) failed")
)

// Client handles RFC 2136 Dynamic DNS updates.
type Client struct {
	config *Config
	tsig   *TSIG
	logger *slog.Logger

	mu         sync.RWMutex
	dnsClient  *dns.Client
	lastUpdate time.Time
}

// ClientOption is a functional option for configuring the Client.
type ClientOption func(*Client)

// WithLogger sets a custom logger for the DNS update client.
func WithLogger(logger *slog.Logger) ClientOption {
	return func(c *Client) {
		if logger != nil {
			c.logger = logger
		}
	}
}

// NewClient creates a new RFC 2136 Dynamic DNS client with the given configuration.
func NewClient(config *Config, opts ...ClientOption) (*Client, error) {
	if config == nil {
		return nil, errors.New("config is required")
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Create TSIG if configured
	tsig, err := TSIGFromConfig(config)
	if err != nil {
		return nil, fmt.Errorf("invalid TSIG configuration: %w", err)
	}

	c := &Client{
		config: config,
		tsig:   tsig,
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(c)
	}

	// Initialize DNS client
	c.dnsClient = &dns.Client{
		Timeout: config.GetTimeout(),
	}

	if config.UseTCP {
		c.dnsClient.Net = "tcp"
	} else {
		c.dnsClient.Net = "udp"
	}

	// Apply TSIG to client if configured
	if tsig != nil {
		tsig.ApplyToClient(c.dnsClient)
	}

	c.logger.Debug("RFC 2136 client initialized",
		slog.String("server", config.GetServer()),
		slog.String("zone", config.Zone),
		slog.Bool("tsig", tsig != nil),
		slog.Bool("tcp", config.UseTCP),
	)

	return c, nil
}

// Ping verifies connectivity to the DNS server by querying the SOA record.
func (c *Client) Ping(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	msg := new(dns.Msg)
	msg.SetQuestion(c.config.Zone, dns.TypeSOA)
	msg.RecursionDesired = false

	c.logger.Debug("pinging DNS server",
		slog.String("server", c.config.GetServer()),
		slog.String("zone", c.config.Zone),
	)

	resp, rtt, err := c.exchangeWithContext(ctx, msg)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrConnectionFailed, err)
	}

	if resp.Rcode != dns.RcodeSuccess {
		return fmt.Errorf("%w: server returned %s", ErrConnectionFailed, dns.RcodeToString[resp.Rcode])
	}

	c.logger.Debug("DNS server ping successful",
		slog.Duration("rtt", rtt),
		slog.Int("answers", len(resp.Answer)),
	)

	return nil
}

// Create adds a new DNS record.
// Returns ErrRecordExists if a record with the same name and type already exists.
func (c *Client) Create(ctx context.Context, record Record) error {
	if err := c.validateRecord(record); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	rr, err := record.ToRR()
	if err != nil {
		return fmt.Errorf("invalid record: %w", err)
	}

	// Build UPDATE message
	msg := new(dns.Msg)
	msg.SetUpdate(c.config.Zone)

	// Add prerequisite: RRset does not exist (for exact create semantics)
	// Comment this out if you want "create or update" semantics instead
	// msg.Ns = append(msg.Ns, dns.TypeNone)

	// Add the record
	msg.Insert([]dns.RR{rr})

	// Apply TSIG if configured
	if c.tsig != nil {
		c.tsig.ApplyToMessage(msg)
	}

	c.logger.Debug("creating DNS record",
		slog.String("name", record.Name),
		slog.String("type", record.TypeString()),
		slog.String("rdata", record.RData),
		slog.Uint64("ttl", uint64(record.TTL)),
	)

	resp, _, err := c.exchangeWithContext(ctx, msg)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUpdateFailed, err)
	}

	if err := c.checkResponse(resp); err != nil {
		return err
	}

	c.lastUpdate = time.Now()
	c.logger.Info("DNS record created",
		slog.String("name", record.Name),
		slog.String("type", record.TypeString()),
	)

	return nil
}

// Update modifies an existing DNS record.
// This deletes the old record and creates the new one atomically.
func (c *Client) Update(ctx context.Context, oldRecord, newRecord Record) error {
	if err := c.validateRecord(oldRecord); err != nil {
		return fmt.Errorf("invalid old record: %w", err)
	}
	if err := c.validateRecord(newRecord); err != nil {
		return fmt.Errorf("invalid new record: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	oldRR, err := oldRecord.ToRR()
	if err != nil {
		return fmt.Errorf("invalid old record: %w", err)
	}

	newRR, err := newRecord.ToRR()
	if err != nil {
		return fmt.Errorf("invalid new record: %w", err)
	}

	// Build UPDATE message
	msg := new(dns.Msg)
	msg.SetUpdate(c.config.Zone)

	// Remove old, insert new
	msg.Remove([]dns.RR{oldRR})
	msg.Insert([]dns.RR{newRR})

	// Apply TSIG if configured
	if c.tsig != nil {
		c.tsig.ApplyToMessage(msg)
	}

	c.logger.Debug("updating DNS record",
		slog.String("name", oldRecord.Name),
		slog.String("type", oldRecord.TypeString()),
		slog.String("old_rdata", oldRecord.RData),
		slog.String("new_rdata", newRecord.RData),
	)

	resp, _, err := c.exchangeWithContext(ctx, msg)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUpdateFailed, err)
	}

	if err := c.checkResponse(resp); err != nil {
		return err
	}

	c.lastUpdate = time.Now()
	c.logger.Info("DNS record updated",
		slog.String("name", newRecord.Name),
		slog.String("type", newRecord.TypeString()),
	)

	return nil
}

// Delete removes a specific DNS record.
func (c *Client) Delete(ctx context.Context, record Record) error {
	if err := c.validateRecord(record); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	rr, err := record.ToRR()
	if err != nil {
		return fmt.Errorf("invalid record: %w", err)
	}

	// Build UPDATE message
	msg := new(dns.Msg)
	msg.SetUpdate(c.config.Zone)

	// Remove the specific record
	msg.Remove([]dns.RR{rr})

	// Apply TSIG if configured
	if c.tsig != nil {
		c.tsig.ApplyToMessage(msg)
	}

	c.logger.Debug("deleting DNS record",
		slog.String("name", record.Name),
		slog.String("type", record.TypeString()),
		slog.String("rdata", record.RData),
	)

	resp, _, err := c.exchangeWithContext(ctx, msg)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUpdateFailed, err)
	}

	if err := c.checkResponse(resp); err != nil {
		return err
	}

	c.lastUpdate = time.Now()
	c.logger.Info("DNS record deleted",
		slog.String("name", record.Name),
		slog.String("type", record.TypeString()),
	)

	return nil
}

// DeleteAll removes all records of a given type for a name.
func (c *Client) DeleteAll(ctx context.Context, name string, recordType uint16) error {
	fqdn := c.ensureFQDN(name)
	if !c.isInZone(fqdn) {
		return fmt.Errorf("%w: %s not in zone %s", ErrZoneMismatch, fqdn, c.config.Zone)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Build UPDATE message
	msg := new(dns.Msg)
	msg.SetUpdate(c.config.Zone)

	// Remove all RRs of the type for the name
	// Use dns.TypeANY with class NONE to delete all records of a type
	rr := &dns.ANY{
		Hdr: dns.RR_Header{
			Name:   fqdn,
			Rrtype: recordType,
			Class:  dns.ClassANY,
			Ttl:    0,
		},
	}
	msg.Ns = append(msg.Ns, rr)

	// Apply TSIG if configured
	if c.tsig != nil {
		c.tsig.ApplyToMessage(msg)
	}

	c.logger.Debug("deleting all DNS records of type",
		slog.String("name", fqdn),
		slog.String("type", dns.TypeToString[recordType]),
	)

	resp, _, err := c.exchangeWithContext(ctx, msg)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUpdateFailed, err)
	}

	if err := c.checkResponse(resp); err != nil {
		return err
	}

	c.lastUpdate = time.Now()
	c.logger.Info("DNS records deleted",
		slog.String("name", fqdn),
		slog.String("type", dns.TypeToString[recordType]),
	)

	return nil
}

// Query retrieves existing records of a given type for a name.
// This uses standard DNS queries (not UPDATE).
func (c *Client) Query(ctx context.Context, name string, recordType uint16) ([]Record, error) {
	fqdn := c.ensureFQDN(name)

	c.mu.RLock()
	defer c.mu.RUnlock()

	msg := new(dns.Msg)
	msg.SetQuestion(fqdn, recordType)
	msg.RecursionDesired = false

	c.logger.Debug("querying DNS records",
		slog.String("name", fqdn),
		slog.String("type", dns.TypeToString[recordType]),
	)

	resp, _, err := c.exchangeWithContext(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("dns query failed: %w", err)
	}

	// NXDOMAIN means no records exist
	if resp.Rcode == dns.RcodeNameError {
		return []Record{}, nil
	}

	if resp.Rcode != dns.RcodeSuccess {
		return nil, fmt.Errorf("dns query returned %s", dns.RcodeToString[resp.Rcode])
	}

	records := make([]Record, 0, len(resp.Answer))
	for _, rr := range resp.Answer {
		record, err := RecordFromRR(rr)
		if err != nil {
			c.logger.Warn("failed to parse DNS record",
				slog.String("error", err.Error()),
				slog.String("rr", rr.String()),
			)
			continue
		}
		records = append(records, record)
	}

	c.logger.Debug("DNS query complete",
		slog.String("name", fqdn),
		slog.Int("count", len(records)),
	)

	return records, nil
}

// Zone returns the configured zone name.
func (c *Client) Zone() string {
	return c.config.Zone
}

// Server returns the configured server address.
func (c *Client) Server() string {
	return c.config.GetServer()
}

// LastUpdate returns the time of the last successful update operation.
func (c *Client) LastUpdate() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastUpdate
}

// exchangeWithContext performs DNS exchange with context support.
func (c *Client) exchangeWithContext(ctx context.Context, msg *dns.Msg) (*dns.Msg, time.Duration, error) {
	// Create a channel for the result
	type result struct {
		resp *dns.Msg
		rtt  time.Duration
		err  error
	}
	ch := make(chan result, 1)

	go func() {
		resp, rtt, err := c.dnsClient.Exchange(msg, c.config.GetServer())
		ch <- result{resp, rtt, err}
	}()

	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	case r := <-ch:
		return r.resp, r.rtt, r.err
	}
}

// checkResponse checks the DNS response for errors.
func (c *Client) checkResponse(resp *dns.Msg) error {
	if resp == nil {
		return fmt.Errorf("%w: no response from server", ErrUpdateFailed)
	}

	switch resp.Rcode {
	case dns.RcodeSuccess:
		return nil

	case dns.RcodeYXRrset:
		// RRset exists when it should not (for prerequisites)
		return ErrRecordExists

	case dns.RcodeNXRrset:
		// RRset does not exist when it should (for prerequisites)
		return ErrRecordNotFound

	case dns.RcodeNotAuth:
		// Server is not authoritative or TSIG failed
		if resp.IsTsig() != nil {
			return fmt.Errorf("%w: %s", ErrAuthenticationFailed, dns.RcodeToString[resp.Rcode])
		}
		return fmt.Errorf("%w: server not authoritative for zone", ErrUpdateFailed)

	case dns.RcodeRefused:
		// Server refused the update (policy or TSIG)
		return fmt.Errorf("%w: update refused (check server policy or TSIG configuration)", ErrUpdateFailed)

	case dns.RcodeNotZone:
		// Name not in zone
		return ErrZoneMismatch

	default:
		return fmt.Errorf("%w: %s", ErrUpdateFailed, dns.RcodeToString[resp.Rcode])
	}
}

// validateRecord validates a record before operations.
func (c *Client) validateRecord(record Record) error {
	if record.Name == "" {
		return errors.New("record name is required")
	}

	fqdn := c.ensureFQDN(record.Name)
	if !c.isInZone(fqdn) {
		return fmt.Errorf("%w: %s not in zone %s", ErrZoneMismatch, fqdn, c.config.Zone)
	}

	return nil
}

// ensureFQDN ensures the name ends with a dot.
func (c *Client) ensureFQDN(name string) string {
	if strings.HasSuffix(name, ".") {
		return name
	}
	return name + "."
}

// isInZone checks if a FQDN is within the configured zone.
func (c *Client) isInZone(fqdn string) bool {
	zone := c.config.Zone
	if !strings.HasSuffix(zone, ".") {
		zone += "."
	}
	return strings.HasSuffix(strings.ToLower(fqdn), strings.ToLower(zone))
}

// Close releases any resources held by the client.
// For RFC 2136, this is a no-op as connections are not persistent.
func (c *Client) Close() error {
	return nil
}

// ListByAXFR performs a zone transfer (AXFR) to retrieve all records in the zone.
// This requires the server to allow zone transfers from this client.
// Many DNS servers restrict AXFR to specific IPs for security reasons.
// Returns ErrAXFRFailed if the zone transfer is refused or fails.
func (c *Client) ListByAXFR(ctx context.Context) ([]Record, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// AXFR always uses TCP
	transfer := &dns.Transfer{
		TsigSecret: nil,
	}

	// Apply TSIG if configured
	if c.tsig != nil {
		transfer.TsigSecret = map[string]string{
			c.tsig.Name: c.tsig.Secret,
		}
	}

	msg := new(dns.Msg)
	msg.SetAxfr(c.config.Zone)

	// Apply TSIG to the message if configured
	if c.tsig != nil {
		c.tsig.ApplyToMessage(msg)
	}

	c.logger.Debug("initiating AXFR zone transfer",
		slog.String("server", c.config.GetServer()),
		slog.String("zone", c.config.Zone),
	)

	// Perform the zone transfer
	env, err := transfer.In(msg, c.config.GetServer())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAXFRFailed, err)
	}

	var records []Record
	for e := range env {
		if e.Error != nil {
			// Log the error but continue processing
			c.logger.Warn("AXFR envelope error",
				slog.String("error", e.Error.Error()),
			)
			continue
		}

		for _, rr := range e.RR {
			// Skip SOA and NS records (zone infrastructure)
			header := rr.Header()
			if header.Rrtype == dns.TypeSOA || header.Rrtype == dns.TypeNS {
				continue
			}

			record, err := RecordFromRR(rr)
			if err != nil {
				c.logger.Debug("skipping unsupported record type",
					slog.String("type", dns.TypeToString[header.Rrtype]),
					slog.String("name", header.Name),
				)
				continue
			}
			records = append(records, record)
		}
	}

	c.logger.Debug("AXFR zone transfer complete",
		slog.String("zone", c.config.Zone),
		slog.Int("records", len(records)),
	)

	return records, nil
}

// RcodeToError converts a DNS rcode to an appropriate error.
func RcodeToError(rcode int) error {
	switch rcode {
	case dns.RcodeSuccess:
		return nil
	case dns.RcodeYXRrset:
		return ErrRecordExists
	case dns.RcodeNXRrset:
		return ErrRecordNotFound
	case dns.RcodeNotAuth:
		return ErrAuthenticationFailed
	default:
		return fmt.Errorf("%w: %s", ErrUpdateFailed, dns.RcodeToString[rcode])
	}
}

// IsNetworkError checks if an error is a network-related error.
func IsNetworkError(err error) bool {
	if err == nil {
		return false
	}

	var netErr net.Error
	return errors.As(err, &netErr)
}

// IsAuthError checks if an error is an authentication error.
func IsAuthError(err error) bool {
	return errors.Is(err, ErrAuthenticationFailed)
}
