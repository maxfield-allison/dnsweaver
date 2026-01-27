// dnsweaver provides automatic DNS record management for Docker containers.
// It watches Docker/Swarm for container events, extracts hostnames from reverse
// proxy labels (Traefik, etc.), and syncs DNS records to one or more providers.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"gitlab.bluewillows.net/root/dnsweaver/internal/config"
	"gitlab.bluewillows.net/root/dnsweaver/internal/docker"
	"gitlab.bluewillows.net/root/dnsweaver/internal/health"
	"gitlab.bluewillows.net/root/dnsweaver/internal/metrics"
	"gitlab.bluewillows.net/root/dnsweaver/internal/reconciler"
	"gitlab.bluewillows.net/root/dnsweaver/internal/watcher"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/source"
	"gitlab.bluewillows.net/root/dnsweaver/providers/cloudflare"
	"gitlab.bluewillows.net/root/dnsweaver/providers/dnsmasq"
	"gitlab.bluewillows.net/root/dnsweaver/providers/pihole"
	"gitlab.bluewillows.net/root/dnsweaver/providers/rfc2136"
	"gitlab.bluewillows.net/root/dnsweaver/providers/technitium"
	"gitlab.bluewillows.net/root/dnsweaver/providers/webhook"
	dnsweaversource "gitlab.bluewillows.net/root/dnsweaver/sources/dnsweaver"
	"gitlab.bluewillows.net/root/dnsweaver/sources/traefik"
)

// Version and BuildDate are set via ldflags during build.
// Example: -ldflags="-X main.Version=v1.0.0 -X main.BuildDate=2026-01-03"
var (
	Version   = "dev"
	BuildDate = "unknown"
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", "", "Path to YAML configuration file")
	showVersion := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("dnsweaver %s (built %s)\n", Version, BuildDate)
		os.Exit(0)
	}

	// If --config flag is set, set it as env var so config.Load() picks it up
	// This maintains the priority: env var (DNSWEAVER_CONFIG) > --config flag
	if *configPath != "" && os.Getenv("DNSWEAVER_CONFIG") == "" {
		if err := os.Setenv("DNSWEAVER_CONFIG", *configPath); err != nil {
			slog.Error("failed to set DNSWEAVER_CONFIG", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}

	if err := run(); err != nil {
		slog.Error("fatal error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	// Load configuration first (fail fast per DECISIONS.md)
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}

	// Set up structured logging
	logger := setupLogger(cfg.LogLevel(), cfg.LogFormat())
	slog.SetDefault(logger)

	// Set build info metrics
	metrics.SetBuildInfo(Version, runtime.Version())

	logger.Info("dnsweaver starting",
		slog.String("version", Version),
		slog.String("build_date", BuildDate),
		slog.String("go_version", runtime.Version()),
		slog.Bool("dry_run", cfg.DryRun()),
		slog.Bool("adopt_existing", cfg.AdoptExisting()),
	)

	// Create context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize Docker client
	dockerClient, err := docker.NewClient(ctx,
		docker.WithHost(cfg.DockerHost()),
		docker.WithMode(parseDockerMode(cfg.DockerMode())),
		docker.WithLogger(logger),
		docker.WithCleanupOnStop(cfg.CleanupOnStop()),
	)
	if err != nil {
		return fmt.Errorf("creating docker client: %w", err)
	}
	defer func() { _ = dockerClient.Close() }()

	logger.Info("docker client connected",
		slog.String("mode", dockerClient.Mode().String()),
	)

	// Initialize source registry
	sourceRegistry := source.NewRegistry(logger)
	if err := registerSources(sourceRegistry, cfg, logger); err != nil {
		return fmt.Errorf("registering sources: %w", err)
	}

	// Initialize provider registry and manager (#125)
	// The manager handles graceful initialization - providers that fail to connect
	// are retried in the background instead of causing a fatal error.
	providerRegistry := provider.NewRegistry(logger)
	registerProviderFactories(providerRegistry)

	providerManager := provider.NewManager(providerRegistry,
		provider.WithManagerLogger(logger),
	)
	if err := initializeProviders(providerManager, cfg); err != nil {
		return fmt.Errorf("initializing providers: %w", err)
	}

	// Start provider manager background retry loop
	if err := providerManager.Start(ctx); err != nil {
		return fmt.Errorf("starting provider manager: %w", err)
	}
	defer providerManager.Stop()

	// Log provider status summary
	if providerManager.PendingCount() > 0 {
		logger.Warn("some providers failed to initialize and will be retried",
			slog.Int("ready", providerManager.ReadyCount()),
			slog.Int("pending", providerManager.PendingCount()),
		)
		for _, status := range providerManager.PendingProviders() {
			logger.Warn("pending provider",
				slog.String("provider", status.Name),
				slog.String("type", status.Type),
				slog.String("error", status.LastError),
			)
		}
	}

	// Initialize reconciler
	reconcilerCfg := reconciler.Config{
		DryRun:            cfg.DryRun(),
		CleanupOrphans:    cfg.CleanupOrphans(),
		OwnershipTracking: cfg.OwnershipTracking(),
		AdoptExisting:     cfg.AdoptExisting(),
		ReconcileInterval: cfg.ReconcileInterval(),
		Enabled:           true,
	}
	rec := reconciler.New(dockerClient, sourceRegistry, providerRegistry,
		reconciler.WithConfig(reconcilerCfg),
		reconciler.WithLogger(logger),
	)

	// Recover ownership state from DNS providers on startup (#40)
	// This enables orphan cleanup to work for records created before a restart
	if err := rec.RecoverOwnership(ctx); err != nil {
		logger.Warn("failed to recover ownership state", slog.String("error", err.Error()))
		// Continue anyway - this is not fatal, just means orphan cleanup may miss some records
	}

	// Create reconciliation trigger function
	triggerReconcile := func() {
		result, err := rec.Reconcile(ctx)
		if err != nil {
			logger.Error("reconciliation failed", slog.String("error", err.Error()))
			return
		}
		logger.Info("reconciliation complete",
			slog.Int("created", result.CreatedCount()),
			slog.Int("deleted", result.DeletedCount()),
			slog.Int("skipped", len(result.Skipped())),
			slog.Int("errors", result.FailedCount()),
			slog.Duration("duration", result.Duration()),
		)
	}

	// Initialize Docker event watcher (#5)
	dockerWatcher := watcher.New(dockerClient, triggerReconcile,
		watcher.WithLogger(logger),
		watcher.WithConfig(watcher.Config{
			DebounceInterval:  2 * time.Second,
			ReconnectInterval: 5 * time.Second,
		}),
	)

	// Initialize file watcher for sources with file discovery (#22)
	var fileWatcher *source.FileWatcher
	if cfg.HasFileDiscovery() {
		logger.Info("file discovery enabled, starting file watcher")
		fileWatcher = source.NewFileWatcher(sourceRegistry,
			func(sourceName string, hostnames []source.Hostname) {
				logger.Info("file watcher detected changes",
					slog.String("source", sourceName),
					slog.Int("hostnames", len(hostnames)),
				)
				triggerReconcile()
			},
			source.WithWatcherLogger(logger),
		)
	}

	// Start health server with provider manager status (#10, #125)
	healthServer := health.New(cfg.HealthPort(),
		health.WithLogger(logger),
	)

	// Register provider health checkers for /ready endpoint
	// Ready providers get connectivity checks
	for _, inst := range providerRegistry.All() {
		inst := inst // capture for closure
		healthServer.RegisterChecker("provider:"+inst.Name(), func(ctx context.Context) error {
			return inst.Ping(ctx)
		})
	}

	// Register a degraded checker for pending providers (#125)
	// This reports degraded status (not unhealthy) when providers are pending
	healthServer.RegisterDegradedChecker("provider-manager", func(ctx context.Context) (bool, string) {
		if providerManager.PendingCount() > 0 {
			pending := providerManager.PendingProviders()
			names := make([]string, len(pending))
			for i, p := range pending {
				names[i] = p.Name
			}
			return true, fmt.Sprintf("%d providers pending: %v", len(pending), names)
		}
		return false, ""
	})

	if err := healthServer.Start(); err != nil {
		return fmt.Errorf("starting health server: %w", err)
	}

	// Start watchers
	if err := dockerWatcher.Start(ctx); err != nil {
		return fmt.Errorf("starting docker watcher: %w", err)
	}

	if fileWatcher != nil {
		if err := fileWatcher.Start(ctx); err != nil {
			return fmt.Errorf("starting file watcher: %w", err)
		}
	}

	// Run initial reconciliation
	logger.Info("running initial reconciliation")
	triggerReconcile()

	// Start periodic reconciliation timer as a safety net
	// This catches any missed Docker events and ensures eventual consistency
	if cfg.ReconcileInterval() > 0 {
		go func() {
			ticker := time.NewTicker(cfg.ReconcileInterval())
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					logger.Debug("periodic reconciliation triggered",
						slog.Duration("interval", cfg.ReconcileInterval()),
					)
					triggerReconcile()
				}
			}
		}()
		logger.Info("periodic reconciliation enabled",
			slog.Duration("interval", cfg.ReconcileInterval()),
		)
	}

	logger.Info("dnsweaver initialized, watching for changes",
		slog.Int("sources", sourceRegistry.Count()),
		slog.Int("providers", providerRegistry.Count()),
		slog.Int("health_port", cfg.HealthPort()),
	)

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	sig := <-sigChan
	logger.Info("received shutdown signal", slog.String("signal", sig.String()))

	// Graceful shutdown
	logger.Info("shutting down...")
	cancel()

	dockerWatcher.Stop()
	if fileWatcher != nil {
		fileWatcher.Stop()
	}

	// Shutdown health server with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := healthServer.Shutdown(shutdownCtx); err != nil {
		logger.Warn("health server shutdown error", slog.String("error", err.Error()))
	}

	logger.Info("dnsweaver shutdown complete")
	return nil
}

func setupLogger(level, format string) *slog.Logger {
	logLevel := parseLogLevel(level)

	var handler slog.Handler
	if format == "text" {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})
	} else {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})
	}

	return slog.New(handler)
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

func parseDockerMode(mode string) docker.Mode {
	switch mode {
	case "swarm":
		return docker.ModeSwarm
	case "standalone":
		return docker.ModeStandalone
	default:
		return docker.ModeAuto
	}
}

func registerSources(registry *source.Registry, cfg *config.Config, logger *slog.Logger) error {
	for _, name := range cfg.SourceNames() {
		switch name {
		case "traefik":
			src := createTraefikSource(cfg, logger)
			if err := registry.Register(src); err != nil {
				return fmt.Errorf("registering traefik source: %w", err)
			}
			logger.Info("registered source",
				slog.String("name", name),
				slog.Bool("file_discovery", src.SupportsDiscovery()),
			)
		case "dnsweaver":
			src := dnsweaversource.New(dnsweaversource.WithLogger(logger))
			if err := registry.Register(src); err != nil {
				return fmt.Errorf("registering dnsweaver source: %w", err)
			}
			logger.Info("registered source",
				slog.String("name", name),
				slog.Bool("file_discovery", src.SupportsDiscovery()),
			)
		default:
			logger.Warn("unknown source, skipping", slog.String("source", name))
		}
	}
	return nil
}

func createTraefikSource(cfg *config.Config, logger *slog.Logger) *traefik.Traefik {
	opts := []traefik.Option{
		traefik.WithLogger(logger),
	}

	// Configure file discovery if paths are set
	srcCfg := cfg.GetSourceInstance("traefik")
	if srcCfg != nil && srcCfg.FileDiscovery.IsEnabled() {
		opts = append(opts, traefik.WithFileDiscovery(srcCfg.FileDiscovery))
		logger.Debug("traefik file discovery configured",
			slog.Any("paths", srcCfg.FileDiscovery.FilePaths),
			slog.String("pattern", srcCfg.FileDiscovery.FilePattern),
		)
	}

	return traefik.New(opts...)
}

func registerProviderFactories(registry *provider.Registry) {
	// Register Technitium provider factory (private DNS)
	registry.RegisterFactory("technitium", technitium.Factory())

	// Register Cloudflare provider factory (public DNS)
	registry.RegisterFactory("cloudflare", cloudflare.Factory())

	// Register Webhook provider factory (custom integrations)
	registry.RegisterFactory("webhook", webhook.Factory())

	// Register dnsmasq provider factory (local DNS, Pi-hole backend)
	registry.RegisterFactory("dnsmasq", dnsmasq.Factory())

	// Register Pi-hole provider factory (local DNS via Pi-hole API or file mode)
	registry.RegisterFactory("pihole", pihole.Factory())

	// Register RFC 2136 provider factory (BIND, Windows DNS, PowerDNS, etc.)
	registry.RegisterFactory("rfc2136", rfc2136.Factory())
}

// initializeProviders initializes all configured providers using the manager.
// Unlike createProviderInstances, this method does not fail fatally if a provider
// is temporarily unavailable - it queues it for retry instead.
func initializeProviders(manager *provider.Manager, cfg *config.Config) error {
	for _, inst := range cfg.ProviderInstances {
		providerCfg := inst.ToProviderConfig()
		if err := manager.InitializeProvider(providerCfg); err != nil {
			// Only returns error for invalid configuration (not connection failures)
			return fmt.Errorf("invalid provider config %s: %w", inst.Name, err)
		}
	}
	return nil
}
