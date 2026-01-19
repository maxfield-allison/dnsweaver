// Package sshutil provides shared SSH/SFTP client utilities for DNSWeaver providers.
package sshutil

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// CommandRunner defines the interface for executing commands.
// This interface matches the one defined in providers/dnsmasq/client.go
// for compatibility and easy migration.
type CommandRunner interface {
	Run(ctx context.Context, command string) error
}

// CommandResult holds the result of a command execution.
type CommandResult struct {
	// ExitCode is the exit status of the command.
	ExitCode int

	// Stdout is the standard output of the command.
	Stdout string

	// Stderr is the standard error of the command.
	Stderr string
}

// SSHCommandRunner implements CommandRunner over SSH.
type SSHCommandRunner struct {
	client *Client
	logger *slog.Logger

	mu sync.RWMutex
}

// CommandRunnerOption is a functional option for configuring the SSHCommandRunner.
type CommandRunnerOption func(*SSHCommandRunner)

// WithCommandLogger sets a custom logger for command execution.
func WithCommandLogger(logger *slog.Logger) CommandRunnerOption {
	return func(cr *SSHCommandRunner) {
		if logger != nil {
			cr.logger = logger
		}
	}
}

// NewSSHCommandRunner creates a new SSH-based CommandRunner.
// The underlying SSH client must be connected before use.
func NewSSHCommandRunner(client *Client, opts ...CommandRunnerOption) *SSHCommandRunner {
	cr := &SSHCommandRunner{
		client: client,
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(cr)
	}

	return cr
}

// Run executes a command on the remote system.
// It returns an error if the command fails (non-zero exit code) or if there's a communication error.
func (cr *SSHCommandRunner) Run(ctx context.Context, command string) error {
	result, err := cr.RunWithOutput(ctx, command)
	if err != nil {
		return err
	}

	if result.ExitCode != 0 {
		errMsg := strings.TrimSpace(result.Stderr)
		if errMsg == "" {
			errMsg = strings.TrimSpace(result.Stdout)
		}
		return fmt.Errorf("command failed with exit code %d: %s", result.ExitCode, errMsg)
	}

	return nil
}

// RunWithOutput executes a command and returns the full result including stdout/stderr.
// This is useful when you need to capture command output.
func (cr *SSHCommandRunner) RunWithOutput(ctx context.Context, command string) (*CommandResult, error) {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	sshConn, err := cr.client.GetConnection()
	if err != nil {
		return nil, fmt.Errorf("getting SSH connection: %w", err)
	}

	cr.logger.Debug("executing command",
		slog.String("command", command),
	)

	// Create a new session for this command
	session, err := sshConn.NewSession()
	if err != nil {
		return nil, fmt.Errorf("creating SSH session: %w", err)
	}
	defer func() { _ = session.Close() }()

	// Capture stdout and stderr
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	// Run the command with context cancellation support
	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()

	select {
	case <-ctx.Done():
		// Context canceled - try to close the session
		_ = session.Close()
		return nil, ctx.Err()
	case err := <-done:
		result := &CommandResult{
			ExitCode: 0,
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
		}

		// Extract exit code from error
		if err != nil {
			result.ExitCode = extractExitCode(err)
		}

		cr.logger.Debug("command completed",
			slog.String("command", command),
			slog.Int("exit_code", result.ExitCode),
			slog.Int("stdout_len", len(result.Stdout)),
			slog.Int("stderr_len", len(result.Stderr)),
		)

		// Return nil error - the exit code is in the result
		// Callers should check result.ExitCode
		return result, nil
	}
}

// RunScript executes a multi-line script on the remote system.
// The script is executed using "sh -c" to handle multi-line content.
func (cr *SSHCommandRunner) RunScript(ctx context.Context, script string) (*CommandResult, error) {
	// Escape single quotes in the script for shell execution
	escapedScript := strings.ReplaceAll(script, "'", "'\"'\"'")
	command := fmt.Sprintf("sh -c '%s'", escapedScript)

	return cr.RunWithOutput(ctx, command)
}

// RunWithSudo executes a command with sudo on the remote system.
// If password is non-empty, it will be provided to sudo via stdin.
func (cr *SSHCommandRunner) RunWithSudo(ctx context.Context, command, password string) (*CommandResult, error) {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	sshConn, err := cr.client.GetConnection()
	if err != nil {
		return nil, fmt.Errorf("getting SSH connection: %w", err)
	}

	cr.logger.Debug("executing command with sudo",
		slog.String("command", command),
	)

	session, err := sshConn.NewSession()
	if err != nil {
		return nil, fmt.Errorf("creating SSH session: %w", err)
	}
	defer func() { _ = session.Close() }()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	// Build sudo command
	sudoCmd := command
	if password != "" {
		// Use sudo -S to read password from stdin
		sudoCmd = fmt.Sprintf("echo '%s' | sudo -S %s", escapeShellArg(password), command)
	} else {
		// Assume passwordless sudo
		sudoCmd = "sudo " + command
	}

	done := make(chan error, 1)
	go func() {
		done <- session.Run(sudoCmd)
	}()

	select {
	case <-ctx.Done():
		_ = session.Close()
		return nil, ctx.Err()
	case err := <-done:
		result := &CommandResult{
			ExitCode: 0,
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
		}

		if err != nil {
			result.ExitCode = extractExitCode(err)
		}

		cr.logger.Debug("sudo command completed",
			slog.Int("exit_code", result.ExitCode),
		)

		return result, nil
	}
}

// extractExitCode attempts to extract the exit code from an SSH error.
func extractExitCode(err error) int {
	if err == nil {
		return 0
	}

	// Try to extract exit code from the error message
	// SSH errors typically include the exit status
	errStr := err.Error()
	if strings.Contains(errStr, "exit status") {
		var code int
		if _, scanErr := fmt.Sscanf(errStr, "Process exited with status %d", &code); scanErr == nil {
			return code
		}
		// Try alternative format
		if _, scanErr := fmt.Sscanf(errStr, "exit status %d", &code); scanErr == nil {
			return code
		}
	}

	// Default to 1 for any error
	return 1
}

// escapeShellArg escapes a string for safe use in shell commands.
func escapeShellArg(arg string) string {
	// Replace single quotes with escaped version
	return strings.ReplaceAll(arg, "'", "'\"'\"'")
}
