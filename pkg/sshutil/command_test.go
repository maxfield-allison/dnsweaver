package sshutil

import (
	"errors"
	"log/slog"
	"os"
	"testing"
)

func TestNewSSHCommandRunner(t *testing.T) {
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
		runner := NewSSHCommandRunner(client)
		if runner == nil {
			t.Fatal("NewSSHCommandRunner() returned nil")
		}
		if runner.client != client {
			t.Error("NewSSHCommandRunner() client not set correctly")
		}
	})

	t.Run("with logger option", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
		runner := NewSSHCommandRunner(client, WithCommandLogger(logger))
		if runner.logger != logger {
			t.Error("WithCommandLogger() option not applied")
		}
	})

	t.Run("with nil logger option", func(t *testing.T) {
		runner := NewSSHCommandRunner(client, WithCommandLogger(nil))
		if runner.logger == nil {
			t.Error("WithCommandLogger(nil) removed default logger")
		}
	})
}

func TestSSHCommandRunner_NotConnected(t *testing.T) {
	config := &Config{
		Host:     "example.com",
		User:     "admin",
		Password: "secret",
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	runner := NewSSHCommandRunner(client)

	t.Run("Run not connected", func(t *testing.T) {
		err := runner.Run(t.Context(), "echo hello")
		if err == nil {
			t.Error("Run() expected error when not connected")
		}
	})

	t.Run("RunWithOutput not connected", func(t *testing.T) {
		_, err := runner.RunWithOutput(t.Context(), "echo hello")
		if err == nil {
			t.Error("RunWithOutput() expected error when not connected")
		}
	})

	t.Run("RunScript not connected", func(t *testing.T) {
		_, err := runner.RunScript(t.Context(), "echo hello\necho world")
		if err == nil {
			t.Error("RunScript() expected error when not connected")
		}
	})

	t.Run("RunWithSudo not connected", func(t *testing.T) {
		_, err := runner.RunWithSudo(t.Context(), "systemctl restart nginx", "")
		if err == nil {
			t.Error("RunWithSudo() expected error when not connected")
		}
	})
}

func TestCommandResult(t *testing.T) {
	result := &CommandResult{
		ExitCode: 0,
		Stdout:   "hello world",
		Stderr:   "",
	}

	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want %v", result.ExitCode, 0)
	}
	if result.Stdout != "hello world" {
		t.Errorf("Stdout = %v, want %v", result.Stdout, "hello world")
	}
	if result.Stderr != "" {
		t.Errorf("Stderr = %v, want empty", result.Stderr)
	}
}

func TestExtractExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "nil error",
			err:  nil,
			want: 0,
		},
		{
			name: "exit status format",
			err:  errors.New("exit status 127"),
			want: 127,
		},
		{
			name: "process exited format",
			err:  errors.New("Process exited with status 1"),
			want: 1,
		},
		{
			name: "other error",
			err:  errors.New("some other error"),
			want: 1, // Default to 1 for any error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractExitCode(tt.err); got != tt.want {
				t.Errorf("extractExitCode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEscapeShellArg(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want string
	}{
		{
			name: "no special characters",
			arg:  "hello",
			want: "hello",
		},
		{
			name: "with single quote",
			arg:  "it's a test",
			want: "it'\"'\"'s a test",
		},
		{
			name: "multiple single quotes",
			arg:  "it's Tom's",
			want: "it'\"'\"'s Tom'\"'\"'s",
		},
		{
			name: "empty string",
			arg:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeShellArg(tt.arg); got != tt.want {
				t.Errorf("escapeShellArg(%q) = %q, want %q", tt.arg, got, tt.want)
			}
		})
	}
}
