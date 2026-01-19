// Package sshutil provides shared SSH/SFTP client utilities for DNSWeaver providers.
package sshutil

import (
	"context"
	"fmt"
	"io"
	iofs "io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pkg/sftp"
)

// FileSystem defines the interface for file operations.
// This interface matches the one defined in providers/dnsmasq/client.go
// for compatibility and easy migration.
type FileSystem interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	Stat(path string) (os.FileInfo, error)
	MkdirAll(path string, perm os.FileMode) error
}

// SFTPFileSystem implements FileSystem over SFTP.
type SFTPFileSystem struct {
	client *Client
	logger *slog.Logger

	mu         sync.RWMutex
	sftpClient *sftp.Client
}

// SFTPOption is a functional option for configuring the SFTPFileSystem.
type SFTPOption func(*SFTPFileSystem)

// WithSFTPLogger sets a custom logger for SFTP operations.
func WithSFTPLogger(logger *slog.Logger) SFTPOption {
	return func(fs *SFTPFileSystem) {
		if logger != nil {
			fs.logger = logger
		}
	}
}

// NewSFTPFileSystem creates a new SFTP-based FileSystem.
// The underlying SSH client must be connected before use.
func NewSFTPFileSystem(client *Client, opts ...SFTPOption) *SFTPFileSystem {
	fs := &SFTPFileSystem{
		client: client,
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(fs)
	}

	return fs
}

// Connect establishes the SFTP session over the SSH connection.
// The SSH client must be connected before calling this method.
func (fs *SFTPFileSystem) Connect(ctx context.Context) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if fs.sftpClient != nil {
		return nil // Already connected
	}

	sshConn, err := fs.client.GetConnection()
	if err != nil {
		return fmt.Errorf("getting SSH connection: %w", err)
	}

	fs.logger.Debug("establishing SFTP session")

	sftpClient, err := sftp.NewClient(sshConn)
	if err != nil {
		return fmt.Errorf("creating SFTP client: %w", err)
	}

	fs.sftpClient = sftpClient
	fs.logger.Debug("SFTP session established")

	return nil
}

// Close closes the SFTP session.
// Safe to call multiple times. Does not close the underlying SSH connection.
func (fs *SFTPFileSystem) Close() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if fs.sftpClient == nil {
		return nil
	}

	err := fs.sftpClient.Close()
	fs.sftpClient = nil

	fs.logger.Debug("SFTP session closed")

	return err
}

// getSFTP returns the SFTP client, ensuring it's connected.
func (fs *SFTPFileSystem) getSFTP() (*sftp.Client, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if fs.sftpClient == nil {
		return nil, ErrNotConnected
	}

	return fs.sftpClient, nil
}

// ReadFile reads the contents of a file from the remote system.
func (fs *SFTPFileSystem) ReadFile(path string) ([]byte, error) {
	sftpClient, err := fs.getSFTP()
	if err != nil {
		return nil, err
	}

	fs.logger.Debug("reading file", slog.String("path", path))

	file, err := sftpClient.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening file %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", path, err)
	}

	fs.logger.Debug("file read successfully",
		slog.String("path", path),
		slog.Int("bytes", len(data)),
	)

	return data, nil
}

// WriteFile writes data to a file on the remote system.
func (fs *SFTPFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	sftpClient, err := fs.getSFTP()
	if err != nil {
		return err
	}

	fs.logger.Debug("writing file",
		slog.String("path", path),
		slog.Int("bytes", len(data)),
		slog.String("perm", perm.String()),
	)

	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if dir != "." && dir != "/" {
		if mkdirErr := fs.mkdirAllInternal(sftpClient, dir, 0o755); mkdirErr != nil {
			return fmt.Errorf("creating parent directory %s: %w", dir, mkdirErr)
		}
	}

	// Open file for writing (create or truncate)
	file, err := sftpClient.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return fmt.Errorf("opening file %s for write: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	// Write data
	n, err := file.Write(data)
	if err != nil {
		return fmt.Errorf("writing to file %s: %w", path, err)
	}

	if n != len(data) {
		return fmt.Errorf("short write to file %s: wrote %d of %d bytes", path, n, len(data))
	}

	// Set permissions
	if err := sftpClient.Chmod(path, perm); err != nil {
		fs.logger.Warn("failed to set file permissions",
			slog.String("path", path),
			slog.String("error", err.Error()),
		)
		// Don't fail on permission error - file was written successfully
	}

	fs.logger.Debug("file written successfully",
		slog.String("path", path),
		slog.Int("bytes", n),
	)

	return nil
}

// Stat returns file info for a path on the remote system.
func (fs *SFTPFileSystem) Stat(path string) (os.FileInfo, error) {
	sftpClient, err := fs.getSFTP()
	if err != nil {
		return nil, err
	}

	fs.logger.Debug("stat file", slog.String("path", path))

	info, err := sftpClient.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}

	return info, nil
}

// MkdirAll creates a directory and all parent directories on the remote system.
func (fs *SFTPFileSystem) MkdirAll(path string, perm os.FileMode) error {
	sftpClient, err := fs.getSFTP()
	if err != nil {
		return err
	}

	return fs.mkdirAllInternal(sftpClient, path, perm)
}

// mkdirAllInternal creates directories recursively (internal version that takes sftpClient).
func (fs *SFTPFileSystem) mkdirAllInternal(sftpClient *sftp.Client, path string, perm os.FileMode) error {
	fs.logger.Debug("creating directory",
		slog.String("path", path),
		slog.String("perm", perm.String()),
	)

	// Try to create the directory directly first
	err := sftpClient.Mkdir(path)
	if err == nil {
		// Successfully created, set permissions
		if chmodErr := sftpClient.Chmod(path, perm); chmodErr != nil {
			fs.logger.Warn("failed to set directory permissions",
				slog.String("path", path),
				slog.String("error", chmodErr.Error()),
			)
		}
		return nil
	}

	// Check if it already exists as a directory
	info, statErr := sftpClient.Stat(path)
	if statErr == nil {
		if info.IsDir() {
			return nil // Directory already exists
		}
		return fmt.Errorf("path exists but is not a directory: %s", path)
	}

	// Directory doesn't exist, create parent directories recursively
	parent := filepath.Dir(path)
	if parent != path && parent != "/" && parent != "." {
		if err := fs.mkdirAllInternal(sftpClient, parent, perm); err != nil {
			return err
		}
	}

	// Try creating the directory again after parents are created
	if err := sftpClient.Mkdir(path); err != nil {
		// One more check - maybe it was created by another process
		if info, statErr := sftpClient.Stat(path); statErr == nil && info.IsDir() {
			return nil
		}
		return fmt.Errorf("creating directory %s: %w", path, err)
	}

	// Set permissions
	if chmodErr := sftpClient.Chmod(path, perm); chmodErr != nil {
		fs.logger.Warn("failed to set directory permissions",
			slog.String("path", path),
			slog.String("error", chmodErr.Error()),
		)
	}

	return nil
}

// Exists checks if a path exists on the remote system.
func (fs *SFTPFileSystem) Exists(path string) (bool, error) {
	_, err := fs.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Remove removes a file or empty directory from the remote system.
func (fs *SFTPFileSystem) Remove(path string) error {
	sftpClient, err := fs.getSFTP()
	if err != nil {
		return err
	}

	fs.logger.Debug("removing path", slog.String("path", path))

	if err := sftpClient.Remove(path); err != nil {
		return fmt.Errorf("removing %s: %w", path, err)
	}

	return nil
}

// ReadDir reads the contents of a directory on the remote system.
func (fs *SFTPFileSystem) ReadDir(path string) ([]iofs.DirEntry, error) {
	sftpClient, err := fs.getSFTP()
	if err != nil {
		return nil, err
	}

	fs.logger.Debug("reading directory", slog.String("path", path))

	infos, err := sftpClient.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("reading directory %s: %w", path, err)
	}

	entries := make([]iofs.DirEntry, len(infos))
	for i, info := range infos {
		entries[i] = &dirEntry{info: info}
	}

	return entries, nil
}

// Rename renames/moves a file on the remote system.
func (fs *SFTPFileSystem) Rename(oldPath, newPath string) error {
	sftpClient, err := fs.getSFTP()
	if err != nil {
		return err
	}

	fs.logger.Debug("renaming file",
		slog.String("from", oldPath),
		slog.String("to", newPath),
	)

	if err := sftpClient.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("renaming %s to %s: %w", oldPath, newPath, err)
	}

	return nil
}

// dirEntry implements iofs.DirEntry for SFTP directory listings.
type dirEntry struct {
	info os.FileInfo
}

func (d *dirEntry) Name() string                 { return d.info.Name() }
func (d *dirEntry) IsDir() bool                  { return d.info.IsDir() }
func (d *dirEntry) Type() iofs.FileMode          { return d.info.Mode().Type() }
func (d *dirEntry) Info() (iofs.FileInfo, error) { return d.info, nil }

// fileInfo wraps sftp.FileInfo to implement os.FileInfo properly.
// This ensures compatibility with standard library expectations.
type fileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	isDir   bool
}

func (fi *fileInfo) Name() string       { return fi.name }
func (fi *fileInfo) Size() int64        { return fi.size }
func (fi *fileInfo) Mode() os.FileMode  { return fi.mode }
func (fi *fileInfo) ModTime() time.Time { return fi.modTime }
func (fi *fileInfo) IsDir() bool        { return fi.isDir }
func (fi *fileInfo) Sys() interface{}   { return nil }
