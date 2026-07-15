// dnsweaver provides automatic DNS record management for Docker containers
// and Kubernetes workloads. It watches platform events, extracts hostnames from
// reverse proxy labels/resources (Traefik, Ingress, etc.), and syncs DNS records
// to one or more providers.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/maxfield-allison/dnsweaver/internal/config"
	"github.com/maxfield-allison/dnsweaver/internal/docker"
	"github.com/maxfield-allison/dnsweaver/internal/health"
	incusclient "github.com/maxfield-allison/dnsweaver/internal/incus"
	k8s "github.com/maxfield-allison/dnsweaver/internal/kubernetes"
	"github.com/maxfield-allison/dnsweaver/internal/metrics"
	proxmoxclient "github.com/maxfield-allison/dnsweaver/internal/proxmox"
	"github.com/maxfield-allison/dnsweaver/internal/reconciler"
	"github.com/maxfield-allison/dnsweaver/internal/watcher"
	"github.com/maxfield-allison/dnsweaver/pkg/httputil"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
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
	validateOnly := flag.Bool("validate", false, "Validate configuration and exit")
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

	// Also check DNSWEAVER_VALIDATE_ONLY env var for container-based validation
	if parseBoolEnv("DNSWEAVER_VALIDATE_ONLY") {
		validateOnly = boolPtr(true)
	}

	if *validateOnly {
		if err := runValidate(); err != nil {
			fmt.Fprintf(os.Stderr, "Configuration validation failed:\n%s\n", err)
			os.Exit(1)
		}
		fmt.Println("Configuration is valid.")
		os.Exit(0)
	}

	if err := run(); err != nil {
		slog.Error("fatal error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// runValidate loads and validates the configuration, printing a summary.
// Returns nil if configuration is valid, or an error with details.
func runValidate() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Print configuration summary
	fmt.Println("Configuration Summary:")
	fmt.Printf("  Platform:           %s\n", cfg.Platform())
	fmt.Printf("  Log Level:          %s\n", cfg.LogLevel())
	fmt.Printf("  Log Format:         %s\n", cfg.LogFormat())
	if cfg.LogFile() != "" {
		fmt.Printf("  Log File:           %s\n", cfg.LogFile())
	}
	fmt.Printf("  Dry Run:            %v\n", cfg.DryRun())
	fmt.Printf("  Default TTL:        %d\n", cfg.Global.DefaultTTL)
	fmt.Printf("  Reconcile Interval: %s\n", cfg.ReconcileInterval())
	fmt.Printf("  Shutdown Timeout:   %s\n", cfg.ShutdownTimeout())
	fmt.Printf("  Health Port:        %d\n", cfg.HealthPort())
	if cfg.InstanceID() != "" {
		fmt.Printf("  Instance ID:        %s\n", cfg.InstanceID())
	}
	fmt.Printf("  Providers:          %d configured\n", len(cfg.ProviderNames))
	for _, name := range cfg.ProviderNames {
		inst, ok := cfg.GetProviderInstance(name)
		if ok {
			fmt.Printf("    - %s (type: %s, record: %s, domains: %d)\n",
				name, inst.TypeName, inst.RecordType, len(inst.Domains)+len(inst.DomainsRegex))
		}
	}

	return nil
}

func run() error {
	// Load configuration first (fail fast per DECISIONS.md)
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}

	// Set up structured logging
	logger := setupLogger(cfg)
	slog.SetDefault(logger)

	// Set build info metrics
	metrics.SetBuildInfo(Version, runtime.Version())

	logger.Info("dnsweaver starting",
		slog.String("version", Version),
		slog.String("build_date", BuildDate),
		slog.String("go_version", runtime.Version()),
		slog.String("platform", cfg.Platform()),
		slog.Bool("dry_run", cfg.DryRun()),
		slog.Bool("adopt_existing", cfg.AdoptExisting()),
		slog.String("instance_id", cfg.InstanceID()),
		slog.String("log_file", cfg.LogFile()),
	)

	// Log validated configuration summary
	for _, name := range cfg.ProviderNames {
		inst, ok := cfg.GetProviderInstance(name)
		if ok {
			domainCount := len(inst.Domains) + len(inst.DomainsRegex)
			logger.Info("provider configured",
				slog.String("name", name),
				slog.String("type", inst.TypeName),
				slog.String("record_type", string(inst.RecordType)),
				slog.String("mode", string(inst.Mode)),
				slog.Int("domains", domainCount),
				slog.Int("ttl", inst.TTL),
			)
		}
	}

	// Create context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Register signal handler early so SIGINT/SIGTERM during initialization
	// still triggers graceful shutdown instead of an ungraceful kill.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Initialize Docker client (when platform includes docker)
	var dockerClient *docker.Client
	if cfg.UseDocker() {
		dockerClient, err = docker.NewClient(ctx,
			docker.WithHost(cfg.DockerHost()),
			docker.WithMode(parseDockerMode(cfg.DockerMode())),
			docker.WithLogger(logger),
			docker.WithCleanupOnStop(cfg.CleanupOnStop()),
			docker.WithConnectTimeout(cfg.DockerConnectTimeout()),
		)
		if err != nil {
			return fmt.Errorf("creating docker client: %w", err)
		}
		defer func() { _ = dockerClient.Close() }()

		logger.Info("docker client connected",
			slog.String("mode", dockerClient.Mode().String()),
		)
	}

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

	// Set instance ID for multi-instance coordination (#84)
	if cfg.InstanceID() != "" {
		providerRegistry.SetInstanceID(cfg.InstanceID())
		logger.Info("multi-instance mode enabled",
			slog.String("instance_id", cfg.InstanceID()),
		)
	}

	providerManager := provider.NewManager(providerRegistry,
		provider.WithManagerLogger(logger),
	)
	if err := initializeProviders(providerManager, cfg); err != nil {
		return fmt.Errorf("initializing providers: %w", err)
	}

	// Surface backend identity collisions once at startup. When two instances
	// share the same provider identity + record type they will race over the
	// same physical records; the reconciler resolves this via first-match-wins
	// (issue #86) but the user should be told their config is ambiguous.
	providerRegistry.WarnDuplicateIdentities()

	// Log provider status summary (manager background retry starts later, after health server)
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
		InstanceID:        cfg.InstanceID(),
	}

	// Build workload listers for each enabled platform
	var listers []workload.Lister
	if dockerClient != nil {
		dockerLister := docker.NewWorkloadListerAdapter(dockerClient)
		listers = append(listers, dockerLister)
	}

	// Initialize Kubernetes watcher (when platform includes kubernetes).
	// Created before reconciler so it can be included as a lister.
	// The reconcile callback is set after reconciler creation via SetReconcileFunc.
	var k8sWatcher *k8s.Watcher
	if cfg.UseKubernetes() {
		k8sCfg := buildK8sConfig(cfg)
		clients, err := k8s.NewClients(k8sCfg.Kubeconfig)
		if err != nil {
			return fmt.Errorf("creating kubernetes clients: %w", err)
		}

		k8sWatcher = k8s.New(clients,
			k8s.WithConfig(k8sCfg),
			k8s.WithLogger(logger),
		)
		listers = append(listers, k8sWatcher)

		logger.Info("kubernetes watcher configured",
			slog.Bool("ingress", k8sCfg.WatchIngress),
			slog.Bool("ingressroute", k8sCfg.WatchIngressRoute),
			slog.Bool("httproute", k8sCfg.WatchHTTPRoute),
			slog.Bool("services", k8sCfg.WatchServices),
			slog.String("namespaces", cfg.K8sNamespaces()),
		)
	}

	// Initialize Proxmox VE lister when DNSWEAVER_PROXMOX_URL is set.
	if cfg.UseProxmox() {
		pveClient, err := proxmoxclient.NewClient(proxmoxclient.ClientConfig{
			BaseURL:     cfg.ProxmoxURL(),
			TokenID:     cfg.ProxmoxTokenID(),
			TokenSecret: cfg.ProxmoxTokenSecret(),
			// Intentional use of deprecated accessor: we pass both the legacy
			// bool and the unified TLS config so operators on the old env var
			// still work. The proxmox client prefers TLS when set.
			VerifyTLS: cfg.ProxmoxVerifyTLS(), //nolint:staticcheck // SA1019: see comment above
			TLS:       cfg.ProxmoxTLS(),
			Logger:    logger,
		})
		if err != nil {
			return fmt.Errorf("creating proxmox client: %w", err)
		}
		proxmoxLister := proxmoxclient.NewWorkloadListerAdapter(pveClient, proxmoxclient.AdapterConfig{
			NodeFilter:  cfg.ProxmoxNodeFilter(),
			TagFilter:   cfg.ProxmoxTagFilter(),
			StateFilter: cfg.ProxmoxStateFilter(),
		}, logger)
		listers = append(listers, proxmoxLister)
		logger.Info("proxmox lister configured",
			slog.String("url", cfg.ProxmoxURL()),
			slog.String("node_filter", cfg.ProxmoxNodeFilter()),
			slog.String("tag_filter", cfg.ProxmoxTagFilter()),
		)
	}

	// Initialize Incus lister(s) when DNSWEAVER_INCUS_URL or _SOCKET_PATH is set.
	// Project scope is controlled by DNSWEAVER_INCUS_ALL_PROJECTS (watch every
	// project via all-projects mode), DNSWEAVER_INCUS_PROJECTS (an explicit list,
	// one client per project), or DNSWEAVER_INCUS_PROJECT (a single project).
	var incusClients []*incusclient.Client
	if cfg.UseIncus() {
		incusTLS := cfg.IncusTLS()

		// Resolve the client certificate: reuse a persisted keypair, enroll a
		// new one via a trust token, or fall back to configured cert/key files
		// (#134). Only applies to remote HTTPS endpoints; socket mode needs no
		// client certificate.
		if cfg.IncusURL() != "" {
			var fallbackCert, fallbackKey string
			if incusTLS != nil {
				fallbackCert, fallbackKey = incusTLS.CertFile, incusTLS.KeyFile
			}
			cc, err := incusclient.EnsureClientCert(ctx, incusclient.EnrollConfig{
				BaseURL:   cfg.IncusURL(),
				Token:     cfg.IncusTrustToken(),
				CertStore: cfg.IncusCertStore(),
				Projects:  cfg.IncusProjects(),
				TLS:       incusTLS,
				Logger:    logger,
			}, fallbackCert, fallbackKey)
			if err != nil {
				return fmt.Errorf("resolving incus client certificate: %w", err)
			}
			if cc.CertFile != "" || cc.KeyFile != "" {
				if incusTLS == nil {
					incusTLS = &httputil.TLSConfig{}
				}
				incusTLS.CertFile = cc.CertFile
				incusTLS.KeyFile = cc.KeyFile
			}
			// Pin the server's leaf certificate (from the trust token or the
			// persisted store) so ongoing lister/watcher connections verify
			// without a CA file, matching the enrollment handshake (#146).
			if cc.PinnedSHA256 != "" {
				if incusTLS == nil {
					incusTLS = &httputil.TLSConfig{}
				}
				incusTLS.PinnedSHA256 = cc.PinnedSHA256
			}
		}

		baseCfg := incusclient.ClientConfig{
			BaseURL:    cfg.IncusURL(),
			SocketPath: cfg.IncusSocketPath(),
			TLS:        incusTLS,
			Logger:     logger,
		}

		var clientCfgs []incusclient.ClientConfig
		var scope string
		switch {
		case cfg.IncusAllProjects():
			c := baseCfg
			c.AllProjects = true
			clientCfgs = append(clientCfgs, c)
			scope = "all-projects"
		case len(cfg.IncusProjects()) > 0:
			for _, p := range cfg.IncusProjects() {
				c := baseCfg
				c.Project = p
				clientCfgs = append(clientCfgs, c)
			}
			scope = strings.Join(cfg.IncusProjects(), ",")
		default:
			c := baseCfg
			c.Project = cfg.IncusProject()
			clientCfgs = append(clientCfgs, c)
			if scope = cfg.IncusProject(); scope == "" {
				scope = "default"
			}
		}

		for _, cc := range clientCfgs {
			incusClient, err := incusclient.NewClient(cc)
			if err != nil {
				return fmt.Errorf("creating incus client: %w", err)
			}
			incusClients = append(incusClients, incusClient)
			incusLister := incusclient.NewWorkloadListerAdapter(incusClient, incusclient.AdapterConfig{
				StateFilter: cfg.IncusStateFilter(),
			}, logger)
			listers = append(listers, incusLister)
		}
		logger.Info("incus lister configured",
			slog.String("endpoint", incusEndpoint(cfg)),
			slog.String("projects", scope),
		)
	}

	if len(listers) == 0 && len(sourceRegistry.DiscoverableSources()) == 0 {
		return fmt.Errorf("no workload listers or discovery sources configured: set DNSWEAVER_PLATFORM to docker, kubernetes, or both; set DNSWEAVER_PROXMOX_URL for Proxmox VE; set DNSWEAVER_INCUS_URL/DNSWEAVER_INCUS_SOCKET_PATH for Incus; or, when running standalone (DNSWEAVER_PLATFORM=none), configure a file-discovery source such as DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS")
	}

	if len(listers) == 0 {
		logger.Info("running in standalone mode with file discovery only (no workload listers)",
			slog.String("platform", cfg.Platform()),
		)
	}

	rec := reconciler.New(listers, sourceRegistry, providerRegistry,
		reconciler.WithConfig(reconcilerCfg),
		reconciler.WithLogger(logger),
	)

	// Recover ownership state from DNS providers on startup (#40)
	// This enables orphan cleanup to work for records created before a restart
	if err := rec.RecoverOwnership(ctx); err != nil {
		logger.Warn("failed to recover ownership state", slog.String("error", err.Error()))
		// Continue anyway - this is not fatal, just means orphan cleanup may miss some records
	}

	// Create reconciliation trigger function with concurrency guard.
	// TryLock ensures only one reconciliation runs at a time. When a trigger
	// is skipped (lock held), reconcilePending is set so the running
	// reconciliation performs a follow-up pass to catch changes that arrived
	// mid-cycle — including Docker/K8s events during the initial startup scan (#55).
	var (
		reconcileMu      sync.Mutex
		reconcilePending atomic.Bool
		reconcileWg      sync.WaitGroup // tracks in-flight reconciliations for graceful shutdown
	)

	doReconcile := func(reason string) {
		result, err := rec.Reconcile(ctx)
		if err != nil {
			logger.Error("reconciliation failed",
				slog.String("reason", reason),
				slog.String("error", err.Error()),
			)
			return
		}
		logger.Info("reconciliation complete",
			slog.String("reason", reason),
			slog.Int("created", result.CreatedCount()),
			slog.Int("deleted", result.DeletedCount()),
			slog.Int("skipped", len(result.Skipped())),
			slog.Int("errors", result.FailedCount()),
			slog.Duration("duration", result.Duration()),
		)
	}

	triggerReconcile := func() {
		if !reconcileMu.TryLock() {
			reconcilePending.Store(true)
			logger.Debug("reconciliation already in progress, marking pending")
			return
		}

		reconcileWg.Add(1)
		defer func() {
			reconcileWg.Done()
			reconcileMu.Unlock()
		}()

		doReconcile("triggered")

		// If a trigger was skipped while we were reconciling, run once more
		// to pick up changes that arrived mid-cycle (e.g., events during startup).
		if reconcilePending.CompareAndSwap(true, false) {
			doReconcile("pending catch-up")
		}
	}

	// Set reconcile callback on K8s watcher (now that triggerReconcile exists)
	if k8sWatcher != nil {
		k8sWatcher.SetReconcileFunc(triggerReconcile)
	}

	// Initialize Docker event watcher (#5)
	var dockerWatcher *watcher.Watcher
	if dockerClient != nil {
		dockerWatcher = watcher.New(dockerClient, triggerReconcile,
			watcher.WithLogger(logger),
			watcher.WithConfig(watcher.Config{
				DebounceInterval:  2 * time.Second,
				ReconnectInterval: 5 * time.Second,
			}),
		)
	}

	// Initialize Incus event watcher(s) for near-instant DNS updates (#132).
	// One watcher per Incus client (one per project, or a single all-projects
	// watcher).
	var incusWatchers []*incusclient.WorkloadWatcher
	for _, incusClient := range incusClients {
		incusWatchers = append(incusWatchers,
			incusclient.NewWatcher(incusClient, triggerReconcile,
				incusclient.WithWatcherLogger(logger),
			),
		)
	}

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

	// Build dynamic target refreshers for provider instances configured with
	// DNSWEAVER_{NAME}_TARGET_MODE (#130). Not started until the reconcile
	// trigger is wired and providers are ready (below).
	targetRefreshers := buildTargetRefreshers(cfg, providerRegistry, triggerReconcile, logger)

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

	// Register recovered-provider callback so health checkers are added
	// when pending providers come online (#127). Must be set before Start().
	providerManager.SetOnProviderReady(func(name string, inst *provider.ProviderInstance) {
		healthServer.RegisterChecker("provider:"+name, func(ctx context.Context) error {
			return inst.Ping(ctx)
		})
		logger.Info("registered health checker for recovered provider",
			slog.String("provider", name),
		)
	})

	// Start provider manager background retry loop (after health callback is wired)
	if err := providerManager.Start(ctx); err != nil {
		return fmt.Errorf("starting provider manager: %w", err)
	}
	defer providerManager.Stop()

	if err := healthServer.Start(); err != nil {
		return fmt.Errorf("starting health server: %w", err)
	}

	// Start watchers
	if dockerWatcher != nil {
		if err := dockerWatcher.Start(ctx); err != nil {
			return fmt.Errorf("starting docker watcher: %w", err)
		}
	}

	if k8sWatcher != nil {
		if err := k8sWatcher.Start(ctx); err != nil {
			return fmt.Errorf("starting kubernetes watcher: %w", err)
		}
	}

	for _, incusWatcher := range incusWatchers {
		if err := incusWatcher.Start(ctx); err != nil {
			return fmt.Errorf("starting incus watcher: %w", err)
		}
	}

	if fileWatcher != nil {
		if err := fileWatcher.Start(ctx); err != nil {
			return fmt.Errorf("starting file watcher: %w", err)
		}
	}

	// Start dynamic target refreshers. Their initial resolution is synchronous,
	// so dynamic targets are set before the first reconcile runs.
	startTargetRefreshers(ctx, targetRefreshers)

	// Run initial reconciliation
	logger.Info("running initial reconciliation")
	triggerReconcile()

	// Start periodic reconciliation timer as a safety net
	// This catches any missed events and ensures eventual consistency
	var wg sync.WaitGroup
	if cfg.ReconcileInterval() > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
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
		slog.String("platform", cfg.Platform()),
		slog.Int("sources", sourceRegistry.Count()),
		slog.Int("providers", providerRegistry.Count()),
		slog.Int("health_port", cfg.HealthPort()),
	)

	// Handle shutdown signals
	// (sigChan registered early — see top of run())

	// Wait for shutdown signal
	sig := <-sigChan
	logger.Info("received shutdown signal", slog.String("signal", sig.String()))

	// Graceful shutdown sequence:
	// 1. Mark health as shutting down (returns 503 to load balancers)
	// 2. Stop accepting new events (watchers)
	// 3. Cancel periodic reconciliation
	// 4. Wait for in-flight reconciliation to complete (with timeout)
	// 5. Cancel context and clean up

	logger.Info("shutting down gracefully",
		slog.Duration("timeout", cfg.ShutdownTimeout()),
	)

	// Step 1: Health endpoint returns 503, signaling orchestrators to drain
	healthServer.SetShuttingDown()

	// Step 2: Stop watchers — no new events will trigger reconciliation
	if dockerWatcher != nil {
		dockerWatcher.Stop()
		logger.Debug("docker watcher stopped")
	}
	if k8sWatcher != nil {
		k8sWatcher.Stop()
		logger.Debug("kubernetes watcher stopped")
	}
	for _, incusWatcher := range incusWatchers {
		incusWatcher.Stop()
	}
	if len(incusWatchers) > 0 {
		logger.Debug("incus watchers stopped")
	}
	if fileWatcher != nil {
		fileWatcher.Stop()
		logger.Debug("file watcher stopped")
	}
	stopTargetRefreshers(targetRefreshers)
	if len(targetRefreshers) > 0 {
		logger.Debug("target refreshers stopped")
	}

	// Step 3: Cancel context to stop periodic reconciliation goroutine
	cancel()
	wg.Wait()
	logger.Debug("periodic reconciliation stopped")

	// Step 4: Wait for in-flight reconciliation to complete (with timeout)
	reconcileDone := make(chan struct{})
	go func() {
		reconcileWg.Wait()
		close(reconcileDone)
	}()

	select {
	case <-reconcileDone:
		logger.Info("in-flight operations completed")
	case <-time.After(cfg.ShutdownTimeout()):
		logger.Warn("shutdown timeout exceeded, in-flight operations may be incomplete",
			slog.Duration("timeout", cfg.ShutdownTimeout()),
		)
	}

	// Step 5: Shutdown health server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := healthServer.Shutdown(shutdownCtx); err != nil {
		logger.Warn("health server shutdown error", slog.String("error", err.Error()))
	}

	logger.Info("dnsweaver shutdown complete")
	return nil
}
