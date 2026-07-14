package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/maxfield-allison/dnsweaver/internal/config"
	"github.com/maxfield-allison/dnsweaver/internal/docker"
	k8s "github.com/maxfield-allison/dnsweaver/internal/kubernetes"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/providers/adguard"
	"github.com/maxfield-allison/dnsweaver/providers/cloudflare"
	"github.com/maxfield-allison/dnsweaver/providers/dnsmasq"
	"github.com/maxfield-allison/dnsweaver/providers/opnsense"
	"github.com/maxfield-allison/dnsweaver/providers/ovh"
	"github.com/maxfield-allison/dnsweaver/providers/pfsense"
	"github.com/maxfield-allison/dnsweaver/providers/pihole"
	"github.com/maxfield-allison/dnsweaver/providers/powerdns"
	"github.com/maxfield-allison/dnsweaver/providers/rfc2136"
	"github.com/maxfield-allison/dnsweaver/providers/technitium"
	"github.com/maxfield-allison/dnsweaver/providers/webhook"
	"github.com/maxfield-allison/dnsweaver/sources/caddy"
	dnsweaversource "github.com/maxfield-allison/dnsweaver/sources/dnsweaver"
	incussource "github.com/maxfield-allison/dnsweaver/sources/incus"
	k8ssource "github.com/maxfield-allison/dnsweaver/sources/kubernetes"
	"github.com/maxfield-allison/dnsweaver/sources/nginxproxy"
	proxmoxsource "github.com/maxfield-allison/dnsweaver/sources/proxmox"
	"github.com/maxfield-allison/dnsweaver/sources/traefik"
)

// parseBoolEnv reads an environment variable and returns true if it's a truthy value.
func parseBoolEnv(key string) bool {
	v := os.Getenv(key)
	switch strings.ToLower(v) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool {
	return &b
}

// setupLogger configures structured logging based on application config.
func setupLogger(cfg *config.Config) *slog.Logger {
	logLevel := parseLogLevel(cfg.LogLevel())

	var output io.Writer = os.Stdout

	if logFile := cfg.LogFile(); logFile != "" {
		output = &lumberjack.Logger{
			Filename:   logFile,
			MaxSize:    cfg.LogMaxSize(),
			MaxBackups: cfg.LogMaxBackups(),
			MaxAge:     cfg.LogMaxAge(),
			Compress:   cfg.LogCompress(),
		}
	}

	opts := &slog.HandlerOptions{Level: logLevel}

	var handler slog.Handler
	if cfg.LogFormat() == "text" {
		handler = slog.NewTextHandler(output, opts)
	} else {
		handler = slog.NewJSONHandler(output, opts)
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

// parseDockerMode converts a string Docker mode to the typed constant.
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

// registerSources registers hostname extraction sources based on configuration.
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
		case "caddy":
			src := caddy.New(caddy.WithLogger(logger))
			if err := registry.Register(src); err != nil {
				return fmt.Errorf("registering caddy source: %w", err)
			}
			logger.Info("registered source",
				slog.String("name", name),
				slog.Bool("file_discovery", src.SupportsDiscovery()),
			)
		case "nginx-proxy":
			src := nginxproxy.New(nginxproxy.WithLogger(logger))
			if err := registry.Register(src); err != nil {
				return fmt.Errorf("registering nginx-proxy source: %w", err)
			}
			logger.Info("registered source",
				slog.String("name", name),
				slog.Bool("file_discovery", src.SupportsDiscovery()),
			)
		case "kubernetes":
			src := k8ssource.New(k8ssource.WithLogger(logger))
			if err := registry.Register(src); err != nil {
				return fmt.Errorf("registering kubernetes source: %w", err)
			}
			logger.Info("registered source",
				slog.String("name", name),
			)
		case "proxmox":
			src := proxmoxsource.New(
				proxmoxsource.WithDomain(cfg.ProxmoxDomainSuffix()),
				proxmoxsource.WithHostnameTagPrefix(cfg.ProxmoxHostnameTagPrefix()),
				proxmoxsource.WithTargetMode(proxmoxTargetMode(cfg)),
				proxmoxsource.WithLogger(logger),
			)
			if err := registry.Register(src); err != nil {
				return fmt.Errorf("registering proxmox source: %w", err)
			}
			logger.Info("registered source",
				slog.String("name", name),
			)
		case "incus":
			src := incussource.New(
				incussource.WithDomain(cfg.IncusDomainSuffix()),
				incussource.WithTargetMode(incusTargetMode(cfg)),
				incussource.WithLogger(logger),
			)
			if err := registry.Register(src); err != nil {
				return fmt.Errorf("registering incus source: %w", err)
			}
			logger.Info("registered source",
				slog.String("name", name),
			)
		default:
			logger.Warn("unknown source, skipping", slog.String("source", name))
		}
	}

	// Auto-register kubernetes source when K8s platform is enabled.
	// This source is always needed for K8s workloads (reads pre-extracted
	// hostnames from resource converters). It doesn't need to be explicitly
	// listed in DNSWEAVER_SOURCES — it's platform-implied.
	if cfg.UseKubernetes() {
		if registry.Get("kubernetes") == nil {
			src := k8ssource.New(k8ssource.WithLogger(logger))
			if err := registry.Register(src); err != nil {
				return fmt.Errorf("registering kubernetes source: %w", err)
			}
			logger.Info("auto-registered kubernetes source for K8s platform")
		}
	}

	// Auto-register proxmox source when Proxmox platform is enabled.
	// Mirrors the K8s auto-registration pattern.
	if cfg.UseProxmox() {
		if registry.Get("proxmox") == nil {
			src := proxmoxsource.New(
				proxmoxsource.WithDomain(cfg.ProxmoxDomainSuffix()),
				proxmoxsource.WithHostnameTagPrefix(cfg.ProxmoxHostnameTagPrefix()),
				proxmoxsource.WithTargetMode(proxmoxTargetMode(cfg)),
				proxmoxsource.WithLogger(logger),
			)
			if err := registry.Register(src); err != nil {
				return fmt.Errorf("registering proxmox source: %w", err)
			}
			logger.Info("auto-registered proxmox source for Proxmox platform")
		}
	}

	// Auto-register incus source when Incus is enabled.
	// Mirrors the Proxmox auto-registration pattern.
	if cfg.UseIncus() {
		if registry.Get("incus") == nil {
			src := incussource.New(
				incussource.WithDomain(cfg.IncusDomainSuffix()),
				incussource.WithTargetMode(incusTargetMode(cfg)),
				incussource.WithLogger(logger),
			)
			if err := registry.Register(src); err != nil {
				return fmt.Errorf("registering incus source: %w", err)
			}
			logger.Info("auto-registered incus source for Incus platform")
		}
	}

	return nil
}

// proxmoxTargetMode resolves the Proxmox target mode from config. Validation
// happens during config load — this returns the default for any unrecognized
// value to keep the source constructor strict-typed.
func proxmoxTargetMode(cfg *config.Config) proxmoxsource.TargetMode {
	mode, err := proxmoxsource.ParseTargetMode(cfg.ProxmoxTargetMode())
	if err != nil {
		return proxmoxsource.TargetModeGuestIP
	}
	return mode
}

// incusTargetMode resolves the Incus target mode from config. Validation
// happens during config load — this returns the default for any unrecognized
// value to keep the source constructor strict-typed.
func incusTargetMode(cfg *config.Config) incussource.TargetMode {
	mode, err := incussource.ParseTargetMode(cfg.IncusTargetMode())
	if err != nil {
		return incussource.TargetModeGuestIP
	}
	return mode
}

// incusEndpoint returns a human-readable description of the configured Incus
// endpoint (remote URL or local socket path) for logging.
func incusEndpoint(cfg *config.Config) string {
	if url := cfg.IncusURL(); url != "" {
		return url
	}
	return "unix:" + cfg.IncusSocketPath()
}

// createTraefikSource creates a Traefik label parser with optional file discovery.
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

	if srcCfg != nil && len(srcCfg.DefaultEntryPoints) > 0 {
		opts = append(opts, traefik.WithDefaultEntryPoints(srcCfg.DefaultEntryPoints))
		logger.Debug("traefik default entrypoints configured",
			slog.Any("default_entrypoints", srcCfg.DefaultEntryPoints),
		)
	}

	return traefik.New(opts...)
}

// registerProviderFactories registers all available DNS provider factories.
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

	// Register PowerDNS provider factory (native Authoritative HTTP API)
	registry.RegisterFactory("powerdns", powerdns.Factory())

	// Register AdGuard Home provider factory (local DNS via AdGuard Home API)
	registry.RegisterFactory("adguard", adguard.Factory())

	// Register OVHcloud provider factory (public DNS)
	registry.RegisterFactory("ovh", ovh.Factory())

	// Register OPNsense provider factory (Unbound/Dnsmasq host overrides via REST API)
	registry.RegisterFactory("opnsense", opnsense.Factory())

	// Register pfSense provider factory (Unbound/Dnsmasq host overrides via REST API)
	registry.RegisterFactory("pfsense", pfsense.Factory())
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

// buildK8sConfig converts application config into Kubernetes watcher config.
func buildK8sConfig(cfg *config.Config) k8s.Config {
	k8sCfg := k8s.DefaultConfig()
	k8sCfg.Kubeconfig = cfg.K8sKubeconfig()
	k8sCfg.WatchIngress = cfg.K8sWatchIngress()
	k8sCfg.WatchIngressRoute = cfg.K8sWatchIngressRoute()
	k8sCfg.WatchHTTPRoute = cfg.K8sWatchHTTPRoute()
	k8sCfg.WatchServices = cfg.K8sWatchServices()
	k8sCfg.LabelSelector = cfg.K8sLabelSelector()
	k8sCfg.AnnotationFilter = cfg.K8sAnnotationFilter()

	if ns := cfg.K8sNamespaces(); ns != "" {
		k8sCfg.Namespaces = strings.Split(ns, ",")
		for i := range k8sCfg.Namespaces {
			k8sCfg.Namespaces[i] = strings.TrimSpace(k8sCfg.Namespaces[i])
		}
	}

	return k8sCfg
}
