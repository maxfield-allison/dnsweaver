package sshutil

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
)

func TestNewSFTPFileSystem(t *testing.T) {
	config := &Config{
		Host:     "example.com",
		User:     "admin",
		Password: "secret",
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	t.Run("basic creation", func(t *testing.T) {
		fs := NewSFTPFileSystem(client)
		if fs == nil {
			t.Fatal("NewSFTPFileSystem() returned nil")
		}
		if fs.client != client {
			t.Error("NewSFTPFileSystem() client not set correctly")
		}
	})

	t.Run("with logger option", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
		fs := NewSFTPFileSystem(client, WithSFTPLogger(logger))
		if fs.logger != logger {
			t.Error("WithSFTPLogger() option not applied")
		}
	})

	t.Run("with nil logger option", func(t *testing.T) {
		fs := NewSFTPFileSystem(client, WithSFTPLogger(nil))
		if fs.logger == nil {
			t.Error("WithSFTPLogger(nil) removed default logger")
		}
	})
}

func TestSFTPFileSystem_NotConnected(t *testing.T) {
	config := &Config{
		Host:     "example.com",
		User:     "admin",
		Password: "secret",
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	fs := NewSFTPFileSystem(client)

	t.Run("ReadFile not connected", func(t *testing.T) {
		_, err := fs.ReadFile("/path/to/file")
		if !errors.Is(err, ErrNotConnected) {
			t.Errorf("ReadFile() error = %v, want %v", err, ErrNotConnected)
		}
	})

	t.Run("WriteFile not connected", func(t *testing.T) {
		err := fs.WriteFile("/path/to/file", []byte("data"), 0o644)
		if !errors.Is(err, ErrNotConnected) {
			t.Errorf("WriteFile() error = %v, want %v", err, ErrNotConnected)
		}
	})

	t.Run("Stat not connected", func(t *testing.T) {
		_, err := fs.Stat("/path/to/file")
		if !errors.Is(err, ErrNotConnected) {
			t.Errorf("Stat() error = %v, want %v", err, ErrNotConnected)
		}
	})

	t.Run("MkdirAll not connected", func(t *testing.T) {
		err := fs.MkdirAll("/path/to/dir", 0o755)
		if !errors.Is(err, ErrNotConnected) {
			t.Errorf("MkdirAll() error = %v, want %v", err, ErrNotConnected)
		}
	})

	t.Run("Exists not connected", func(t *testing.T) {
		_, err := fs.Exists("/path/to/file")
		if !errors.Is(err, ErrNotConnected) {
			t.Errorf("Exists() error = %v, want %v", err, ErrNotConnected)
		}
	})

	t.Run("Remove not connected", func(t *testing.T) {
		err := fs.Remove("/path/to/file")
		if !errors.Is(err, ErrNotConnected) {
			t.Errorf("Remove() error = %v, want %v", err, ErrNotConnected)
		}
	})

	t.Run("ReadDir not connected", func(t *testing.T) {
		_, err := fs.ReadDir("/path/to/dir")
		if !errors.Is(err, ErrNotConnected) {
			t.Errorf("ReadDir() error = %v, want %v", err, ErrNotConnected)
		}
	})

	t.Run("Rename not connected", func(t *testing.T) {
		err := fs.Rename("/old/path", "/new/path")
		if !errors.Is(err, ErrNotConnected) {
			t.Errorf("Rename() error = %v, want %v", err, ErrNotConnected)
		}
	})
}

func TestSFTPFileSystem_Close(t *testing.T) {
	config := &Config{
		Host:     "example.com",
		User:     "admin",
		Password: "secret",
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	fs := NewSFTPFileSystem(client)

	// Close should be safe to call even when not connected
	t.Run("close when not connected", func(t *testing.T) {
		err := fs.Close()
		if err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	})

	// Close should be safe to call multiple times
	t.Run("close multiple times", func(t *testing.T) {
		if err := fs.Close(); err != nil {
			t.Errorf("First Close() error = %v", err)
		}
		if err := fs.Close(); err != nil {
			t.Errorf("Second Close() error = %v", err)
		}
	})
}

func TestSFTPFileSystem_Connect_NoSSHConnection(t *testing.T) {
	config := &Config{
		Host:     "example.com",
		User:     "admin",
		Password: "secret",
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	fs := NewSFTPFileSystem(client)

	// Try to connect SFTP without SSH connection
	err = fs.Connect(context.Background())
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("Connect() error = %v, want error about SSH not connected", err)
	}
}

func TestDirEntry(t *testing.T) {
	// Create a mock FileInfo
	mockInfo := &fileInfo{
		name:  "test.txt",
		size:  100,
		mode:  0o644,
		isDir: false,
	}

	entry := &dirEntry{info: mockInfo}

	t.Run("Name", func(t *testing.T) {
		if got := entry.Name(); got != "test.txt" {
			t.Errorf("Name() = %v, want %v", got, "test.txt")
		}
	})

	t.Run("IsDir", func(t *testing.T) {
		if got := entry.IsDir(); got != false {
			t.Errorf("IsDir() = %v, want %v", got, false)
		}
	})

	t.Run("Type", func(t *testing.T) {
		if got := entry.Type(); got != 0 {
			t.Errorf("Type() = %v, want %v", got, 0)
		}
	})

	t.Run("Info", func(t *testing.T) {
		info, err := entry.Info()
		if err != nil {
			t.Fatalf("Info() error = %v", err)
		}
		if info != mockInfo {
			t.Error("Info() returned different info")
		}
	})
}

func TestFileInfo(t *testing.T) {
	fi := &fileInfo{
		name:  "test.txt",
		size:  12345,
		mode:  0o755,
		isDir: true,
	}

	if fi.Name() != "test.txt" {
		t.Errorf("Name() = %v, want %v", fi.Name(), "test.txt")
	}
	if fi.Size() != 12345 {
		t.Errorf("Size() = %v, want %v", fi.Size(), 12345)
	}
	if fi.Mode() != 0o755 {
		t.Errorf("Mode() = %v, want %v", fi.Mode(), 0o755)
	}
	if !fi.IsDir() {
		t.Error("IsDir() = false, want true")
	}
	if fi.Sys() != nil {
		t.Errorf("Sys() = %v, want nil", fi.Sys())
	}
}
