package config

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Global configuration defaults.
const (
	DefaultLogLevel          = "info"
	DefaultLogFormat         = "json"
	DefaultLogMaxSize        = 100  // megabytes
	DefaultLogMaxBackups     = 5    // number of old log files to keep
	DefaultLogMaxAge         = 30   // days to retain old logs
	DefaultLogCompress       = true // compress rotated log files
	DefaultDryRun            = false
	DefaultCleanupOrphans    = true
	DefaultCleanupOnStop     = true
	DefaultOwnershipTracking = true
	DefaultAdoptExisting     = false
	DefaultTTL               = 300
	DefaultReconcileInterval = 60 * time.Second
	DefaultShutdownTimeout   = 30 * time.Second
	DefaultHealthPort        = 8080
	DefaultDockerHost        = "unix:///var/run/docker.sock"
	DefaultDockerMode        = "auto"
	DefaultSource            = "traefik"
	DefaultInstanceID        = ""
	DefaultPlatform          = "docker"
)

// InstanceID validation constraints.
const (
	MaxInstanceIDLength = 63 // DNS label safe
)

// GlobalConfig holds application-wide settings.
// These are parsed from DNSWEAVER_* environment variables.
type GlobalConfig struct {
	// Logging configuration
	LogLevel      string // debug, info, warn, error
	LogFormat     string // json, text
	LogFile       string // Path to log file; empty means stdout only
	LogMaxSize    int    // Max size in MB before rotation
	LogMaxBackups int    // Number of old log files to keep
	LogMaxAge     int    // Days to retain old log files
	LogCompress   bool   // Compress rotated log files with gzip

	// Behavior
	DryRun            bool          // If true, don't make actual DNS changes
	CleanupOrphans    bool          // If true, delete DNS records for removed workloads
	CleanupOnStop     bool          // If true, delete DNS records when containers stop; if false, only when removed
	OwnershipTracking bool          // If true, use TXT records to track record ownership
	AdoptExisting     bool          // If true, adopt existing DNS records by creating ownership TXT records
	DefaultTTL        int           // Default TTL for records if not specified per-provider
	ReconcileInterval time.Duration // How often to reconcile DNS records
	ShutdownTimeout   time.Duration // Max time to wait for in-flight operations during shutdown
	HealthPort        int           // Port for health/metrics endpoints

	// Platform selection
	Platform string // docker, kubernetes, both

	// Docker connection
	DockerHost string // Docker socket path or TCP URL
	DockerMode string // auto, swarm, standalone

	// Kubernetes settings
	K8sKubeconfig        string // Path to kubeconfig file; empty = in-cluster
	K8sNamespaces        string // Comma-separated namespace list; empty = all
	K8sWatchIngress      bool   // Watch networking.k8s.io/v1 Ingress resources
	K8sWatchIngressRoute bool   // Watch traefik.io/v1alpha1 IngressRoute CRDs
	K8sWatchHTTPRoute    bool   // Watch gateway.networking.k8s.io/v1 HTTPRoute CRDs
	K8sWatchServices     bool   // Watch v1 Service resources (opt-in, can be noisy)
	K8sLabelSelector     string // Label selector for filtering resources
	K8sAnnotationFilter  string // Annotation key=value filter

	// Source
	Source string // traefik, labels, or custom source name

	// Proxmox VE settings
	ProxmoxURL          string // PVE API base URL (e.g., https://pve-00:8006)
	ProxmoxTokenID      string // PVE API token ID (e.g., user@pam!dnsweaver)
	ProxmoxTokenSecret  string // PVE API token secret (UUID); supports _FILE suffix
	ProxmoxNodeFilter   string // Optional: limit to a specific node (empty = all nodes)
	ProxmoxTagFilter    string // Optional: prefix match on PVE tags to filter workloads
	ProxmoxStateFilter  string // Filter by PVE resource status (default: "running")
	ProxmoxDomainSuffix string // Domain suffix to append to VM names (e.g., "home.example.com")
	ProxmoxVerifyTLS    bool   // Verify TLS certificate on PVE API endpoint (DEPRECATED in v1.5: prefer ProxmoxTLSSkipVerify)
	ProxmoxTargetMode   string // Target resolution mode: "guest-ip" (default) or "instance"

	// Unified PVE TLS configuration (v1.5+). Populated from DNSWEAVER_PROXMOX_TLS_*
	// env vars and consumed by internal/proxmox via httputil.TLSConfig.
	ProxmoxTLSCAFile     string
	ProxmoxTLSCertFile   string
	ProxmoxTLSKeyFile    string
	ProxmoxTLSServerName string
	ProxmoxTLSSkipVerify bool
	ProxmoxTLSMinVersion string

	// Incus settings
	IncusURL          string // Remote Incus API base URL (e.g., https://incus.example.com:8443)
	IncusSocketPath   string // Local Incus Unix socket path (e.g., /var/lib/incus/unix.socket)
	IncusProject      string // Incus project to query (default: "default")
	IncusStateFilter  string // Filter by instance status (default: "running")
	IncusDomainSuffix string // Domain suffix to append to instance names (e.g., "home.example.com")
	IncusTargetMode   string // Target resolution mode: "guest-ip" (default) or "instance"

	// Unified Incus TLS configuration for remote HTTPS endpoints. Populated from
	// DNSWEAVER_INCUS_TLS_* env vars and consumed by internal/incus via httputil.TLSConfig.
	// Incus remote authentication uses a TLS client certificate/key pair.
	IncusTLSCAFile     string
	IncusTLSCertFile   string
	IncusTLSKeyFile    string
	IncusTLSServerName string
	IncusTLSSkipVerify bool
	IncusTLSMinVersion string

	// Multi-instance coordination
	InstanceID string // Unique identifier for this dnsweaver instance (for shared zone management)
}

// normalizePlatform lowercases the platform value and maps recognized aliases
// to their canonical form. "standalone" is an alias for "none" — both mean
// "create no container-runtime client" so dnsweaver can run as a bare binary
// on a host, VM, or LXC using only non-container sources (Proxmox, file
// discovery). Unknown values are returned lowercased for the caller to reject.
func normalizePlatform(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "standalone":
		return "none"
	default:
		return strings.ToLower(strings.TrimSpace(v))
	}
}

// loadGlobalConfig loads global configuration from environment variables.
// Returns a list of validation errors (may be empty).
func loadGlobalConfig() (*GlobalConfig, []*ConfigError) {
	var errs []*ConfigError

	cfg := &GlobalConfig{
		LogLevel:   getEnv("DNSWEAVER_LOG_LEVEL"),
		LogFormat:  getEnv("DNSWEAVER_LOG_FORMAT"),
		LogFile:    getEnv("DNSWEAVER_LOG_FILE"),
		DockerHost: getEnv("DNSWEAVER_DOCKER_HOST"),
		DockerMode: getEnv("DNSWEAVER_DOCKER_MODE"),
		Source:     DefaultSource, // Deprecated: derived from DNSWEAVER_SOURCES via parseSources()
		Platform:   getEnv("DNSWEAVER_PLATFORM"),
	}

	// Apply defaults for empty values
	if cfg.LogLevel == "" {
		cfg.LogLevel = DefaultLogLevel
	}
	if cfg.LogFormat == "" {
		cfg.LogFormat = DefaultLogFormat
	}
	if cfg.DockerHost == "" {
		cfg.DockerHost = DefaultDockerHost
	}
	if cfg.DockerMode == "" {
		cfg.DockerMode = DefaultDockerMode
	}
	if cfg.Source == "" {
		cfg.Source = DefaultSource // Deprecated field; prefer DNSWEAVER_SOURCES
	}

	// Platform defaults and validation
	if cfg.Platform == "" {
		cfg.Platform = DefaultPlatform
	}
	cfg.Platform = normalizePlatform(cfg.Platform)
	switch cfg.Platform {
	case "docker", "kubernetes", "both", "none":
		// Valid
	default:
		errs = append(errs, configErrFull("DNSWEAVER_PLATFORM", fmt.Sprintf("invalid value %q", cfg.Platform), "Must be one of: docker, kubernetes, both, none", "DNSWEAVER_PLATFORM=docker"))
	}

	// Validate log level
	cfg.LogLevel = strings.ToLower(cfg.LogLevel)
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
		// Valid
	default:
		errs = append(errs, configErrFull("DNSWEAVER_LOG_LEVEL", fmt.Sprintf("invalid value %q", cfg.LogLevel), "Must be one of: debug, info, warn, error", "DNSWEAVER_LOG_LEVEL=info"))
	}

	// Validate log format
	cfg.LogFormat = strings.ToLower(cfg.LogFormat)
	switch cfg.LogFormat {
	case "json", "text":
		// Valid
	default:
		errs = append(errs, configErrFull("DNSWEAVER_LOG_FORMAT", fmt.Sprintf("invalid value %q", cfg.LogFormat), "Must be one of: json, text", "DNSWEAVER_LOG_FORMAT=json"))
	}

	// Parse log rotation settings (only relevant when LogFile is set)
	if maxSizeStr := getEnv("DNSWEAVER_LOG_MAX_SIZE"); maxSizeStr != "" {
		maxSize, err := strconv.Atoi(maxSizeStr)
		if err != nil {
			errs = append(errs, configErrFull("DNSWEAVER_LOG_MAX_SIZE", fmt.Sprintf("invalid integer %q", maxSizeStr), "Must be a positive integer representing megabytes", "DNSWEAVER_LOG_MAX_SIZE=100"))
		} else if maxSize < 1 {
			errs = append(errs, configErrFull("DNSWEAVER_LOG_MAX_SIZE", "must be at least 1 (MB)", "Minimum log file size is 1 MB", "DNSWEAVER_LOG_MAX_SIZE=100"))
		} else {
			cfg.LogMaxSize = maxSize
		}
	} else {
		cfg.LogMaxSize = DefaultLogMaxSize
	}

	if maxBackupsStr := getEnv("DNSWEAVER_LOG_MAX_BACKUPS"); maxBackupsStr != "" {
		maxBackups, err := strconv.Atoi(maxBackupsStr)
		if err != nil {
			errs = append(errs, configErrFull("DNSWEAVER_LOG_MAX_BACKUPS", fmt.Sprintf("invalid integer %q", maxBackupsStr), "Must be a non-negative integer", "DNSWEAVER_LOG_MAX_BACKUPS=5"))
		} else if maxBackups < 0 {
			errs = append(errs, configErrFull("DNSWEAVER_LOG_MAX_BACKUPS", "must be non-negative", "Use 0 to keep all old log files", "DNSWEAVER_LOG_MAX_BACKUPS=5"))
		} else {
			cfg.LogMaxBackups = maxBackups
		}
	} else {
		cfg.LogMaxBackups = DefaultLogMaxBackups
	}

	if maxAgeStr := getEnv("DNSWEAVER_LOG_MAX_AGE"); maxAgeStr != "" {
		maxAge, err := strconv.Atoi(maxAgeStr)
		if err != nil {
			errs = append(errs, configErrFull("DNSWEAVER_LOG_MAX_AGE", fmt.Sprintf("invalid integer %q", maxAgeStr), "Must be a non-negative integer (days)", "DNSWEAVER_LOG_MAX_AGE=30"))
		} else if maxAge < 0 {
			errs = append(errs, configErrFull("DNSWEAVER_LOG_MAX_AGE", "must be non-negative", "Use 0 to keep old log files indefinitely", "DNSWEAVER_LOG_MAX_AGE=30"))
		} else {
			cfg.LogMaxAge = maxAge
		}
	} else {
		cfg.LogMaxAge = DefaultLogMaxAge
	}

	if compressStr := getEnv("DNSWEAVER_LOG_COMPRESS"); compressStr != "" {
		cfg.LogCompress = parseBool(compressStr, DefaultLogCompress)
	} else {
		cfg.LogCompress = DefaultLogCompress
	}

	// Validate Docker mode
	cfg.DockerMode = strings.ToLower(cfg.DockerMode)
	switch cfg.DockerMode {
	case "auto", "swarm", "standalone":
		// Valid
	default:
		errs = append(errs, configErrFull("DNSWEAVER_DOCKER_MODE", fmt.Sprintf("invalid value %q", cfg.DockerMode), "Must be one of: auto, swarm, standalone", "DNSWEAVER_DOCKER_MODE=auto"))
	}

	// Parse DRY_RUN
	if dryRunStr := getEnv("DNSWEAVER_DRY_RUN"); dryRunStr != "" {
		cfg.DryRun = parseBool(dryRunStr, DefaultDryRun)
	} else {
		cfg.DryRun = DefaultDryRun
	}

	// Parse CLEANUP_ORPHANS
	if cleanupStr := getEnv("DNSWEAVER_CLEANUP_ORPHANS"); cleanupStr != "" {
		cfg.CleanupOrphans = parseBool(cleanupStr, DefaultCleanupOrphans)
	} else {
		cfg.CleanupOrphans = DefaultCleanupOrphans
	}

	// Parse CLEANUP_ON_STOP
	if cleanupOnStopStr := getEnv("DNSWEAVER_CLEANUP_ON_STOP"); cleanupOnStopStr != "" {
		cfg.CleanupOnStop = parseBool(cleanupOnStopStr, DefaultCleanupOnStop)
	} else {
		cfg.CleanupOnStop = DefaultCleanupOnStop
	}

	// Parse OWNERSHIP_TRACKING
	if ownershipStr := getEnv("DNSWEAVER_OWNERSHIP_TRACKING"); ownershipStr != "" {
		cfg.OwnershipTracking = parseBool(ownershipStr, DefaultOwnershipTracking)
	} else {
		cfg.OwnershipTracking = DefaultOwnershipTracking
	}

	// Parse ADOPT_EXISTING
	if adoptStr := getEnv("DNSWEAVER_ADOPT_EXISTING"); adoptStr != "" {
		cfg.AdoptExisting = parseBool(adoptStr, DefaultAdoptExisting)
	} else {
		cfg.AdoptExisting = DefaultAdoptExisting
	}

	// Parse DEFAULT_TTL
	if ttlStr := getEnv("DNSWEAVER_DEFAULT_TTL"); ttlStr != "" {
		ttl, err := strconv.Atoi(ttlStr)
		if err != nil {
			errs = append(errs, configErrFull("DNSWEAVER_DEFAULT_TTL", fmt.Sprintf("invalid integer %q", ttlStr), "Must be a positive integer (seconds)", "DNSWEAVER_DEFAULT_TTL=300"))
		} else if ttl < 1 {
			errs = append(errs, configErrFull("DNSWEAVER_DEFAULT_TTL", "must be at least 1", "TTL is in seconds; typical values are 60-3600", "DNSWEAVER_DEFAULT_TTL=300"))
		} else {
			cfg.DefaultTTL = ttl
		}
	} else {
		cfg.DefaultTTL = DefaultTTL
	}

	// Parse RECONCILE_INTERVAL (supports Go duration format: 60s, 5m, etc.)
	if intervalStr := getEnv("DNSWEAVER_RECONCILE_INTERVAL"); intervalStr != "" {
		interval, err := time.ParseDuration(intervalStr)
		if err != nil {
			errs = append(errs, configErrFull("DNSWEAVER_RECONCILE_INTERVAL", fmt.Sprintf("invalid duration %q", intervalStr), "Use Go duration format: 60s, 5m, 1h", "DNSWEAVER_RECONCILE_INTERVAL=60s"))
		} else if interval < time.Second {
			errs = append(errs, configErrFull("DNSWEAVER_RECONCILE_INTERVAL", "must be at least 1s", "Shorter intervals cause excessive API calls", "DNSWEAVER_RECONCILE_INTERVAL=60s"))
		} else {
			cfg.ReconcileInterval = interval
		}
	} else {
		cfg.ReconcileInterval = DefaultReconcileInterval
	}

	// Parse SHUTDOWN_TIMEOUT (supports Go duration format: 30s, 1m, etc.)
	if timeoutStr := getEnv("DNSWEAVER_SHUTDOWN_TIMEOUT"); timeoutStr != "" {
		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			errs = append(errs, configErrFull("DNSWEAVER_SHUTDOWN_TIMEOUT", fmt.Sprintf("invalid duration %q", timeoutStr), "Use Go duration format: 30s, 1m, 2m", "DNSWEAVER_SHUTDOWN_TIMEOUT=30s"))
		} else if timeout < time.Second {
			errs = append(errs, configErrFull("DNSWEAVER_SHUTDOWN_TIMEOUT", "must be at least 1s", "Allow enough time for in-flight DNS updates to complete", "DNSWEAVER_SHUTDOWN_TIMEOUT=30s"))
		} else {
			cfg.ShutdownTimeout = timeout
		}
	} else {
		cfg.ShutdownTimeout = DefaultShutdownTimeout
	}

	// Parse HEALTH_PORT
	if portStr := getEnv("DNSWEAVER_HEALTH_PORT"); portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err != nil {
			errs = append(errs, configErrFull("DNSWEAVER_HEALTH_PORT", fmt.Sprintf("invalid integer %q", portStr), "Must be a valid TCP port number", "DNSWEAVER_HEALTH_PORT=8080"))
		} else if port < 1 || port > 65535 {
			errs = append(errs, configErrFull("DNSWEAVER_HEALTH_PORT", fmt.Sprintf("must be between 1 and 65535, got %d", port), "Choose an unprivileged port (1024-65535)", "DNSWEAVER_HEALTH_PORT=8080"))
		} else {
			cfg.HealthPort = port
		}
	} else {
		cfg.HealthPort = DefaultHealthPort
	}

	// Parse INSTANCE_ID
	if instanceID := getEnv("DNSWEAVER_INSTANCE_ID"); instanceID != "" {
		if err := validateInstanceID(instanceID); err != nil {
			errs = append(errs, configErrFull("DNSWEAVER_INSTANCE_ID", err.Error(), "Must be 1-63 alphanumeric characters with hyphens, underscores, or dots", "DNSWEAVER_INSTANCE_ID=prod-01"))
		} else {
			cfg.InstanceID = instanceID
		}
	}

	// Parse Kubernetes settings (only relevant when platform includes kubernetes)
	cfg.K8sKubeconfig = getEnv("DNSWEAVER_K8S_KUBECONFIG")
	cfg.K8sNamespaces = getEnv("DNSWEAVER_K8S_NAMESPACES")
	cfg.K8sLabelSelector = getEnv("DNSWEAVER_K8S_LABEL_SELECTOR")
	cfg.K8sAnnotationFilter = getEnv("DNSWEAVER_K8S_ANNOTATION_FILTER")

	// K8s boolean settings with defaults matching kubernetes.DefaultConfig()
	if v := getEnv("DNSWEAVER_K8S_WATCH_INGRESS"); v != "" {
		cfg.K8sWatchIngress = parseBool(v, true)
	} else {
		cfg.K8sWatchIngress = true
	}
	if v := getEnv("DNSWEAVER_K8S_WATCH_INGRESSROUTE"); v != "" {
		cfg.K8sWatchIngressRoute = parseBool(v, true)
	} else {
		cfg.K8sWatchIngressRoute = true
	}
	if v := getEnv("DNSWEAVER_K8S_WATCH_HTTPROUTE"); v != "" {
		cfg.K8sWatchHTTPRoute = parseBool(v, true)
	} else {
		cfg.K8sWatchHTTPRoute = true
	}
	if v := getEnv("DNSWEAVER_K8S_WATCH_SERVICES"); v != "" {
		cfg.K8sWatchServices = parseBool(v, false)
	} else {
		cfg.K8sWatchServices = false
	}

	// Proxmox VE settings
	cfg.ProxmoxURL = getEnv("DNSWEAVER_PROXMOX_URL")
	cfg.ProxmoxTokenID = getEnv("DNSWEAVER_PROXMOX_TOKEN_ID")
	cfg.ProxmoxTokenSecret = getEnvOrFile("DNSWEAVER_PROXMOX_TOKEN_SECRET", "DNSWEAVER_PROXMOX_TOKEN_SECRET_FILE")
	cfg.ProxmoxNodeFilter = getEnv("DNSWEAVER_PROXMOX_NODE_FILTER")
	cfg.ProxmoxTagFilter = getEnv("DNSWEAVER_PROXMOX_TAG_FILTER")
	cfg.ProxmoxStateFilter = getEnv("DNSWEAVER_PROXMOX_STATE_FILTER")
	cfg.ProxmoxDomainSuffix = getEnv("DNSWEAVER_PROXMOX_DOMAIN_SUFFIX")
	cfg.ProxmoxTargetMode = getEnv("DNSWEAVER_PROXMOX_TARGET_MODE")
	if cfg.ProxmoxTargetMode != "" {
		switch strings.ToLower(strings.TrimSpace(cfg.ProxmoxTargetMode)) {
		case "guest-ip", "instance":
			// valid
		default:
			errs = append(errs, configErrFull(
				"DNSWEAVER_PROXMOX_TARGET_MODE",
				fmt.Sprintf("invalid value %q", cfg.ProxmoxTargetMode),
				"Must be one of: guest-ip (default, A record per guest IP), instance (defer to instance TARGET/RECORD_TYPE)",
				"DNSWEAVER_PROXMOX_TARGET_MODE=instance",
			))
		}
	}
	if v := getEnv("DNSWEAVER_PROXMOX_VERIFY_TLS"); v != "" {
		cfg.ProxmoxVerifyTLS = parseBool(v, false)
	}

	// Unified PVE TLS env vars (v1.5+). The legacy DNSWEAVER_PROXMOX_VERIFY_TLS
	// is honored as a back-compat alias (inverted polarity) with a deprecation
	// warning. When both are set, the new TLS_SKIP_VERIFY wins and a conflict
	// warning is emitted. Legacy alias is scheduled for removal in a future
	// major release.
	cfg.ProxmoxTLSCAFile = getEnv("DNSWEAVER_PROXMOX_TLS_CA_FILE")
	cfg.ProxmoxTLSCertFile = getEnv("DNSWEAVER_PROXMOX_TLS_CERT_FILE")
	cfg.ProxmoxTLSKeyFile = getEnv("DNSWEAVER_PROXMOX_TLS_KEY_FILE")
	cfg.ProxmoxTLSServerName = getEnv("DNSWEAVER_PROXMOX_TLS_SERVER_NAME")
	cfg.ProxmoxTLSMinVersion = getEnv("DNSWEAVER_PROXMOX_TLS_MIN_VERSION")

	skipNew, hasNew := os.LookupEnv("DNSWEAVER_PROXMOX_TLS_SKIP_VERIFY")
	_, hasLegacy := os.LookupEnv("DNSWEAVER_PROXMOX_VERIFY_TLS")
	switch {
	case hasNew && hasLegacy:
		slog.Warn("both DNSWEAVER_PROXMOX_VERIFY_TLS (deprecated) and DNSWEAVER_PROXMOX_TLS_SKIP_VERIFY are set; the new TLS_SKIP_VERIFY value wins. Remove VERIFY_TLS — it will be removed in a future major release.")
		cfg.ProxmoxTLSSkipVerify = parseBool(skipNew, false)
	case hasNew:
		cfg.ProxmoxTLSSkipVerify = parseBool(skipNew, false)
	case hasLegacy:
		slog.Warn("DNSWEAVER_PROXMOX_VERIFY_TLS is deprecated; use DNSWEAVER_PROXMOX_TLS_SKIP_VERIFY instead (legacy alias will be removed in a future major release)")
		// Invert polarity: VERIFY_TLS=true → TLS_SKIP_VERIFY=false
		cfg.ProxmoxTLSSkipVerify = !cfg.ProxmoxVerifyTLS
	}

	// Incus settings
	cfg.IncusURL = getEnv("DNSWEAVER_INCUS_URL")
	cfg.IncusSocketPath = getEnv("DNSWEAVER_INCUS_SOCKET_PATH")
	cfg.IncusProject = getEnv("DNSWEAVER_INCUS_PROJECT")
	cfg.IncusStateFilter = getEnv("DNSWEAVER_INCUS_STATE_FILTER")
	cfg.IncusDomainSuffix = getEnv("DNSWEAVER_INCUS_DOMAIN_SUFFIX")
	cfg.IncusTargetMode = getEnv("DNSWEAVER_INCUS_TARGET_MODE")
	if cfg.IncusTargetMode != "" {
		switch strings.ToLower(strings.TrimSpace(cfg.IncusTargetMode)) {
		case "guest-ip", "instance":
			// valid
		default:
			errs = append(errs, configErrFull(
				"DNSWEAVER_INCUS_TARGET_MODE",
				fmt.Sprintf("invalid value %q", cfg.IncusTargetMode),
				"Must be one of: guest-ip (default, A record per guest IP), instance (defer to instance TARGET/RECORD_TYPE)",
				"DNSWEAVER_INCUS_TARGET_MODE=instance",
			))
		}
	}
	if cfg.IncusURL != "" && cfg.IncusSocketPath != "" {
		errs = append(errs, configErrFull(
			"DNSWEAVER_INCUS_URL",
			"both DNSWEAVER_INCUS_URL and DNSWEAVER_INCUS_SOCKET_PATH are set",
			"Set exactly one: URL for a remote HTTPS endpoint, or SOCKET_PATH for the local Unix socket.",
			"DNSWEAVER_INCUS_SOCKET_PATH=/var/lib/incus/unix.socket",
		))
	}

	// Unified Incus TLS env vars for remote HTTPS endpoints. Remote Incus
	// authentication uses a TLS client certificate/key pair (CERT_FILE/KEY_FILE).
	cfg.IncusTLSCAFile = getEnv("DNSWEAVER_INCUS_TLS_CA_FILE")
	cfg.IncusTLSCertFile = getEnv("DNSWEAVER_INCUS_TLS_CERT_FILE")
	cfg.IncusTLSKeyFile = getEnv("DNSWEAVER_INCUS_TLS_KEY_FILE")
	cfg.IncusTLSServerName = getEnv("DNSWEAVER_INCUS_TLS_SERVER_NAME")
	cfg.IncusTLSMinVersion = getEnv("DNSWEAVER_INCUS_TLS_MIN_VERSION")
	if v := getEnv("DNSWEAVER_INCUS_TLS_SKIP_VERIFY"); v != "" {
		cfg.IncusTLSSkipVerify = parseBool(v, false)
	}

	return cfg, errs
}

// instanceIDPattern matches valid instance IDs: alphanumeric, hyphens, underscores, dots.
var instanceIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// validateInstanceID checks that an instance ID is valid.
// Valid IDs are 1-63 characters, alphanumeric with hyphens, underscores, and dots.
// Must start with an alphanumeric character.
func validateInstanceID(id string) error {
	if id == "" {
		return nil // Empty means single-instance mode (legacy)
	}
	if len(id) > MaxInstanceIDLength {
		return fmt.Errorf("must be at most %d characters, got %d", MaxInstanceIDLength, len(id))
	}
	if !instanceIDPattern.MatchString(id) {
		return fmt.Errorf("invalid format %q (must be alphanumeric, hyphens, underscores, dots; must start with alphanumeric)", id)
	}
	return nil
}
