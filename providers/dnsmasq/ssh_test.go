package dnsmasq

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/sshutil"
)

// recordingRunner is a CommandRunner that records the commands it was asked to run.
type recordingRunner struct {
	commands []string
	err      error
}

func (r *recordingRunner) Run(_ context.Context, command string) error {
	r.commands = append(r.commands, command)
	return r.err
}

func TestClient_Reload_UsesInjectedRunner(t *testing.T) {
	runner := &recordingRunner{}
	client := NewClient("/etc/dnsmasq.d", "test.conf", "supervisorctl restart dnsmasq", "",
		WithCommandRunner(runner))

	if err := client.Reload(context.Background()); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	if len(runner.commands) != 1 || runner.commands[0] != "supervisorctl restart dnsmasq" {
		t.Fatalf("injected runner received %v, want [supervisorctl restart dnsmasq]", runner.commands)
	}
}

func TestClient_Reload_DefaultRunnerIsLocal(t *testing.T) {
	// A harmless command that exists on the test host. This proves the default
	// runner executes locally when no runner is injected.
	client := NewClient("/etc/dnsmasq.d", "test.conf", "true", "")
	if err := client.Reload(context.Background()); err != nil {
		t.Fatalf("Reload() with default runner error = %v", err)
	}
}

func TestWithCommandRunner_NilIgnored(t *testing.T) {
	// Passing a nil runner must not clobber the default local runner.
	client := NewClient("/etc/dnsmasq.d", "test.conf", "true", "", WithCommandRunner(nil))
	if client.runner == nil {
		t.Fatal("nil WithCommandRunner left runner unset; expected default osCommandRunner")
	}
}

func TestIsConnectionError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"not connected", sshutil.ErrNotConnected, true},
		{"connection timeout", sshutil.ErrConnectionTimeout, true},
		{"eof", io.EOF, true},
		{"unexpected eof", io.ErrUnexpectedEOF, true},
		{"wrapped eof", fmt.Errorf("reading file: %w", io.EOF), true},
		{"broken pipe", errors.New("write: broken pipe"), true},
		{"connection reset", errors.New("read: connection reset by peer"), true},
		{"closed network", errors.New("use of closed network connection"), true},
		{"session closed", errors.New("session is closed"), true},
		{"file not found", errors.New("opening file /etc/dnsmasq.d/x.conf: file does not exist"), false},
		{"permission denied", errors.New("permission denied"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isConnectionError(tt.err); got != tt.want {
				t.Errorf("isConnectionError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestNew_SSHEnabled_FailFastOnUnreachable(t *testing.T) {
	// Bind a port, then close it so connections are actively refused. This
	// guarantees a fast, deterministic failure instead of a 30s dial timeout.
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving port: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	port := addr.Port
	_ = ln.Close()

	cfg := &Config{
		ConfigDir:     "/etc/dnsmasq.d",
		ConfigFile:    "dnsweaver.conf",
		ReloadCommand: "supervisorctl restart dnsmasq",
		TTL:           DefaultTTL,
		SSHHost:       "127.0.0.1",
		SSHPort:       port,
		SSHUser:       "test",
		SSHPassword:   "test",
	}

	p, err := New("router", cfg)
	if err == nil {
		if p != nil {
			_ = p.Close()
		}
		t.Fatal("New() with unreachable SSH host returned nil error; expected fail-fast")
	}
	if p != nil {
		t.Fatal("New() returned a non-nil provider alongside an error")
	}
}

func TestProvider_Close_NoTransport(t *testing.T) {
	cfg := &Config{
		ConfigDir:     "/etc/dnsmasq.d",
		ConfigFile:    "dnsweaver.conf",
		ReloadCommand: "true",
		TTL:           DefaultTTL,
	}

	p, err := New("local", cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Local provider holds no transport; Close must be a safe no-op, repeatedly.
	for i := 0; i < 3; i++ {
		if err := p.Close(); err != nil {
			t.Fatalf("Close() call %d error = %v", i, err)
		}
	}
}
