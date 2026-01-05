// dnsweaver provides automatic DNS record management for Docker containers.
// It watches Docker/Swarm for container events, extracts hostnames from reverse
// proxy labels (Traefik, etc.), and syncs DNS records to one or more providers.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
)

// Version and BuildDate are set via ldflags during build.
// Example: -ldflags="-X main.Version=v1.0.0 -X main.BuildDate=2026-01-03"
var (
	Version   = "dev"
	BuildDate = "unknown"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	// Set up structured logging (JSON by default per DECISIONS.md)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	logger.Info("dnsweaver starting",
		slog.String("version", Version),
		slog.String("build_date", BuildDate),
		slog.String("go_version", runtime.Version()),
	)

	// Create context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// TODO: Initialize components (Issue #2+)
	// - Config loader
	// - Docker client
	// - Source (Traefik parser)
	// - Provider registry
	// - Reconciler
	// - Health server

	logger.Info("dnsweaver initialized, waiting for shutdown signal")

	// Wait for shutdown signal
	select {
	case sig := <-sigChan:
		logger.Info("received shutdown signal", slog.String("signal", sig.String()))
	case <-ctx.Done():
		logger.Info("context cancelled")
	}

	logger.Info("dnsweaver shutdown complete")
	return nil
}

// parseLogLevel converts a string log level to slog.Level.
func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// printUsage outputs help information.
func printUsage() {
	fmt.Fprintf(os.Stderr, `dnsweaver - Automatic DNS record management for Docker containers

Usage: dnsweaver [options]

Environment Variables:
  DNSWEAVER_LOG_LEVEL        Log level: debug, info, warn, error (default: info)
  DNSWEAVER_LOG_FORMAT       Log format: json, text (default: json)
  DNSWEAVER_DRY_RUN          Log changes without applying (default: false)
  DNSWEAVER_PROVIDERS        Comma-separated list of provider instance names
  DNSWEAVER_HEALTH_PORT      Port for health/metrics endpoints (default: 8080)

For more information, see: https://github.com/maxfield-allison/dnsweaver
`)
}
