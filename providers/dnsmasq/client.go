// Package dnsmasq implements the DNSWeaver provider interface for dnsmasq DNS server.
package dnsmasq

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

// dnsmasqRecord represents a parsed dnsmasq DNS record.
type dnsmasqRecord struct {
	Hostname string
	Type     provider.RecordType
	Target   string
}

// Client handles file operations and reload commands for dnsmasq.
type Client struct {
	configDir     string
	configFile    string
	reloadCommand string
	zone          string
	logger        *slog.Logger
	mu            sync.RWMutex

	// FileSystem abstraction for testing
	fs FileSystem
}

// FileSystem abstracts file operations for testing.
type FileSystem interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	Stat(path string) (os.FileInfo, error)
	MkdirAll(path string, perm os.FileMode) error
}

// osFileSystem implements FileSystem using the real OS.
type osFileSystem struct{}

func (osFileSystem) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (osFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func (osFileSystem) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

func (osFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// CommandRunner abstracts command execution for testing.
type CommandRunner interface {
	Run(ctx context.Context, command string) error
}

// osCommandRunner implements CommandRunner using the real OS.
type osCommandRunner struct {
	logger *slog.Logger
}

func (r *osCommandRunner) Run(ctx context.Context, command string) error {
	r.logger.Debug("executing command", slog.String("command", command))
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command failed: %w, output: %s", err, string(output))
	}
	return nil
}

// ClientOption is a functional option for configuring the Client.
type ClientOption func(*Client)

// WithLogger sets a custom logger.
func WithLogger(logger *slog.Logger) ClientOption {
	return func(c *Client) {
		if logger != nil {
			c.logger = logger
		}
	}
}

// WithFileSystem sets a custom file system (for testing).
func WithFileSystem(fs FileSystem) ClientOption {
	return func(c *Client) {
		if fs != nil {
			c.fs = fs
		}
	}
}

// NewClient creates a new dnsmasq client.
func NewClient(configDir, configFile, reloadCommand, zone string, opts ...ClientOption) *Client {
	c := &Client{
		configDir:     configDir,
		configFile:    configFile,
		reloadCommand: reloadCommand,
		zone:          zone,
		logger:        slog.Default(),
		fs:            osFileSystem{},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// ConfigFilePath returns the full path to the dnsweaver config file.
func (c *Client) ConfigFilePath() string {
	return filepath.Join(c.configDir, c.configFile)
}

// Ping checks if the config directory exists and is writable.
func (c *Client) Ping(ctx context.Context) error {
	info, err := c.fs.Stat(c.configDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("config directory does not exist: %s", c.configDir)
		}
		return fmt.Errorf("checking config directory: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("config path is not a directory: %s", c.configDir)
	}

	return nil
}

// List reads all DNS records from the dnsweaver config file.
func (c *Client) List(ctx context.Context) ([]dnsmasqRecord, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	configPath := c.ConfigFilePath()
	content, err := c.fs.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No config file yet, return empty list
			c.logger.Debug("config file does not exist, returning empty list",
				slog.String("path", configPath))
			return nil, nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	return c.parseConfigContent(string(content))
}

// parseConfigContent parses dnsmasq config content into records.
func (c *Client) parseConfigContent(content string) ([]dnsmasqRecord, error) {
	var records []dnsmasqRecord

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		record, err := c.parseLine(line)
		if err != nil {
			c.logger.Warn("failed to parse line",
				slog.String("line", line),
				slog.String("error", err.Error()))
			continue
		}

		if record != nil {
			// Filter by zone if configured
			if c.zone != "" && !strings.HasSuffix(record.Hostname, "."+c.zone) && record.Hostname != c.zone {
				continue
			}
			records = append(records, *record)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning config content: %w", err)
	}

	return records, nil
}

// addressPattern matches dnsmasq address= directive.
// Format: address=/hostname/ip
var addressPattern = regexp.MustCompile(`^address=/([^/]+)/(.+)$`)

// cnamePattern matches dnsmasq cname= directive.
// Format: cname=alias,target
var cnamePattern = regexp.MustCompile(`^cname=([^,]+),(.+)$`)

// parseLine parses a single dnsmasq config line into a record.
func (c *Client) parseLine(line string) (*dnsmasqRecord, error) {
	// Try address= directive (A/AAAA records)
	if matches := addressPattern.FindStringSubmatch(line); matches != nil {
		hostname := matches[1]
		target := matches[2]

		// Determine record type from IP address format
		ip := net.ParseIP(target)
		if ip == nil {
			return nil, fmt.Errorf("invalid IP address: %s", target)
		}

		recordType := provider.RecordTypeA
		if ip.To4() == nil && ip.To16() != nil {
			recordType = provider.RecordTypeAAAA
		}

		return &dnsmasqRecord{
			Hostname: hostname,
			Type:     recordType,
			Target:   target,
		}, nil
	}

	// Try cname= directive (CNAME records)
	if matches := cnamePattern.FindStringSubmatch(line); matches != nil {
		return &dnsmasqRecord{
			Hostname: matches[1],
			Type:     provider.RecordTypeCNAME,
			Target:   matches[2],
		}, nil
	}

	// Unknown directive, skip
	return nil, nil
}

// Create adds a DNS record to the config file.
func (c *Client) Create(ctx context.Context, record dnsmasqRecord) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Read existing content
	configPath := c.ConfigFilePath()
	existingContent, err := c.fs.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading config file: %w", err)
	}

	// Generate the new line
	newLine, err := c.formatRecord(record)
	if err != nil {
		return fmt.Errorf("formatting record: %w", err)
	}

	// Check if record already exists
	if strings.Contains(string(existingContent), newLine) {
		c.logger.Debug("record already exists, skipping",
			slog.String("hostname", record.Hostname))
		return nil
	}

	// Append the new record
	var newContent string
	if len(existingContent) > 0 {
		// Ensure existing content ends with newline
		existing := string(existingContent)
		if !strings.HasSuffix(existing, "\n") {
			existing += "\n"
		}
		newContent = existing + newLine + "\n"
	} else {
		// New file, add header comment
		newContent = c.fileHeader() + newLine + "\n"
	}

	// Ensure directory exists
	if err := c.fs.MkdirAll(c.configDir, 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	// Write updated content
	if err := c.fs.WriteFile(configPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	c.logger.Debug("created record",
		slog.String("hostname", record.Hostname),
		slog.String("type", string(record.Type)),
		slog.String("target", record.Target))

	return nil
}

// Delete removes a DNS record from the config file.
func (c *Client) Delete(ctx context.Context, record dnsmasqRecord) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	configPath := c.ConfigFilePath()
	content, err := c.fs.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, nothing to delete
			return nil
		}
		return fmt.Errorf("reading config file: %w", err)
	}

	// Generate the line to remove
	targetLine, err := c.formatRecord(record)
	if err != nil {
		return fmt.Errorf("formatting record: %w", err)
	}

	// Remove matching lines
	var newLines []string
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	removed := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == targetLine {
			removed = true
			continue
		}
		newLines = append(newLines, line)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanning config content: %w", err)
	}

	if !removed {
		c.logger.Debug("record not found, nothing to delete",
			slog.String("hostname", record.Hostname))
		return nil
	}

	// Write updated content
	newContent := strings.Join(newLines, "\n")
	if len(newLines) > 0 && !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}

	if err := c.fs.WriteFile(configPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	c.logger.Debug("deleted record",
		slog.String("hostname", record.Hostname),
		slog.String("type", string(record.Type)))

	return nil
}

// formatRecord formats a record as a dnsmasq config line.
func (c *Client) formatRecord(record dnsmasqRecord) (string, error) {
	switch record.Type {
	case provider.RecordTypeA, provider.RecordTypeAAAA:
		// address=/hostname/ip
		return fmt.Sprintf("address=/%s/%s", record.Hostname, record.Target), nil
	case provider.RecordTypeCNAME:
		// cname=alias,target
		return fmt.Sprintf("cname=%s,%s", record.Hostname, record.Target), nil
	default:
		return "", fmt.Errorf("unsupported record type: %s", record.Type)
	}
}

// fileHeader returns the header comment for the config file.
func (c *Client) fileHeader() string {
	return `# Managed by dnsweaver - DO NOT EDIT MANUALLY
# This file is automatically generated and any manual changes will be overwritten.
# See https://github.com/maxfield-allison/dnsweaver for more information.

`
}

// Reload signals dnsmasq to reload its configuration.
func (c *Client) Reload(ctx context.Context) error {
	runner := &osCommandRunner{logger: c.logger}
	return runner.Run(ctx, c.reloadCommand)
}

// ReloadWithRunner signals dnsmasq to reload using a custom runner.
func (c *Client) ReloadWithRunner(ctx context.Context, runner CommandRunner) error {
	return runner.Run(ctx, c.reloadCommand)
}

// WriteRecords writes multiple records to the config file, replacing existing content.
func (c *Client) WriteRecords(ctx context.Context, records []dnsmasqRecord) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var lines []string
	for _, record := range records {
		line, err := c.formatRecord(record)
		if err != nil {
			return fmt.Errorf("formatting record %s: %w", record.Hostname, err)
		}
		lines = append(lines, line)
	}

	content := c.fileHeader() + strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}

	// Ensure directory exists
	if err := c.fs.MkdirAll(c.configDir, 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	configPath := c.ConfigFilePath()
	if err := c.fs.WriteFile(configPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	c.logger.Debug("wrote records",
		slog.String("path", configPath),
		slog.Int("count", len(records)))

	return nil
}

// ReadAll reads the entire config file content.
func (c *Client) ReadAll(ctx context.Context) (io.Reader, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	content, err := c.fs.ReadFile(c.ConfigFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return strings.NewReader(""), nil
		}
		return nil, err
	}

	return strings.NewReader(string(content)), nil
}
