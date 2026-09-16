// Package dnsmasq implements the DNSWeaver provider interface for dnsmasq DNS server.
package dnsmasq

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/maxfield-allison/dnsweaver/pkg/sshutil"
)

// sshTransport manages a remote dnsmasq backend over SSH. It implements both
// the FileSystem and CommandRunner interfaces so the existing Client can write
// config via SFTP and run the reload command over SSH exec without any other
// changes to its logic.
//
// The underlying SSH keepalive does not proactively close dead connections
// (it logs and lets the next operation discover the failure), and IsConnected
// only reports whether a connection object exists, not whether it is healthy.
// To stay robust across idle disconnects and network blips, every operation is
// retried once after a transparent reconnect when it fails with a
// connection-level error.
type sshTransport struct {
	cfg    *sshutil.Config
	logger *slog.Logger

	mu     sync.Mutex
	client *sshutil.Client
	sftp   *sshutil.SFTPFileSystem
	runner *sshutil.SSHCommandRunner
}

// Compile-time checks: sshTransport satisfies the interfaces the Client uses.
var (
	_ FileSystem    = (*sshTransport)(nil)
	_ CommandRunner = (*sshTransport)(nil)
)

// newSSHTransport builds an SSH transport from the dnsmasq SSH configuration and
// establishes the initial connection. A failure here is intentional fail-fast
// behavior: the provider factory returns the error so the provider retry loop
// surfaces it instead of silently falling back to local execution.
func newSSHTransport(ctx context.Context, cfg *Config, logger *slog.Logger) (*sshTransport, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// Host-key verification is opt-in via SSH_KNOWN_HOSTS_FILE. When a
	// known_hosts file is provided, sshutil verifies the remote host key
	// against it; SSH_STRICT_HOST_KEY_CHECKING additionally requires that a
	// file be present (fail closed). Without a known_hosts file, sshutil falls
	// back to insecure-ignore with a loud warning, matching the documented
	// default for trusted internal networks.
	sshCfg := &sshutil.Config{
		Host:                  cfg.SSHHost,
		Port:                  cfg.SSHPort,
		User:                  cfg.SSHUser,
		KeyFile:               cfg.SSHKeyFile,
		Password:              cfg.SSHPassword,
		HostKeyCallback:       cfg.SSHKnownHostsFile,
		StrictHostKeyChecking: cfg.SSHStrictHostKey,
	}

	client, err := sshutil.NewClient(sshCfg, sshutil.WithLogger(logger))
	if err != nil {
		return nil, fmt.Errorf("creating SSH client: %w", err)
	}

	t := &sshTransport{
		cfg:    sshCfg,
		logger: logger,
		client: client,
	}

	if err := t.establish(ctx); err != nil {
		_ = t.Close()
		return nil, fmt.Errorf("connecting to %s: %w", sshCfg.Address(), err)
	}

	logger.Info("dnsmasq SSH transport established",
		slog.String("host", sshCfg.Host),
		slog.Int("port", sshCfg.Port),
		slog.String("user", sshCfg.User),
	)

	return t, nil
}

// establish (re)connects the SSH session and a fresh SFTP session on top of it.
// Reconnect closes any existing connection first, so this is safe to call for
// both the initial connect and subsequent reconnects. Callers must hold t.mu,
// except newSSHTransport which runs before the transport is shared.
func (t *sshTransport) establish(ctx context.Context) error {
	if t.sftp != nil {
		_ = t.sftp.Close()
		t.sftp = nil
	}

	if err := t.client.Reconnect(ctx); err != nil {
		return err
	}

	sftp := sshutil.NewSFTPFileSystem(t.client, sshutil.WithSFTPLogger(t.logger))
	if err := sftp.Connect(ctx); err != nil {
		return fmt.Errorf("establishing SFTP session: %w", err)
	}

	t.sftp = sftp
	t.runner = sshutil.NewSSHCommandRunner(t.client, sshutil.WithCommandLogger(t.logger))
	return nil
}

// do runs op under the transport lock, reconnecting and retrying once if the
// operation fails with a connection-level error.
func (t *sshTransport) do(ctx context.Context, op func() error) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.sftp == nil {
		if err := t.establish(ctx); err != nil {
			return err
		}
	}

	err := op()
	if err == nil || !isConnectionError(err) {
		return err
	}

	t.logger.Warn("dnsmasq SSH operation failed, reconnecting",
		slog.String("host", t.cfg.Host),
		slog.String("error", err.Error()),
	)

	if rerr := t.establish(ctx); rerr != nil {
		return fmt.Errorf("reconnecting after operation error (%s): %w", err.Error(), rerr)
	}

	return op()
}

// ReadFile reads a file from the remote host over SFTP.
func (t *sshTransport) ReadFile(path string) ([]byte, error) {
	var data []byte
	err := t.do(context.Background(), func() error {
		var e error
		data, e = t.sftp.ReadFile(path)
		return e
	})
	return data, err
}

// WriteFileAtomic replaces a managed file on the remote host over SFTP.
func (t *sshTransport) WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	return t.do(context.Background(), func() error {
		return t.sftp.WriteFileAtomic(path, data, perm)
	})
}

// Stat returns file info for a path on the remote host.
func (t *sshTransport) Stat(path string) (os.FileInfo, error) {
	var info os.FileInfo
	err := t.do(context.Background(), func() error {
		var e error
		info, e = t.sftp.Stat(path)
		return e
	})
	return info, err
}

// MkdirAll creates a directory tree on the remote host.
func (t *sshTransport) MkdirAll(path string, perm os.FileMode) error {
	return t.do(context.Background(), func() error {
		return t.sftp.MkdirAll(path, perm)
	})
}

// Run executes the reload command on the remote host via SSH exec.
func (t *sshTransport) Run(ctx context.Context, command string) error {
	return t.do(ctx, func() error {
		return t.runner.Run(ctx, command)
	})
}

// Close tears down the SFTP and SSH sessions. Safe to call multiple times.
func (t *sshTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	var err error
	if t.sftp != nil {
		err = t.sftp.Close()
		t.sftp = nil
	}
	if t.client != nil {
		if cerr := t.client.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	return err
}

// isConnectionError reports whether err indicates a lost or unusable SSH/SFTP
// connection, as opposed to a logical error (e.g. file not found, permission
// denied) that retrying would not fix.
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sshutil.ErrNotConnected) ||
		errors.Is(err, sshutil.ErrConnectionTimeout) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"connection lost",
		"connection reset",
		"broken pipe",
		"use of closed network connection",
		"connection refused",
		"no route to host",
		"session is closed",
		"failed to send packet",
		"eof",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}
