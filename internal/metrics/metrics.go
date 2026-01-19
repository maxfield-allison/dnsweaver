// Package metrics provides Prometheus metrics for DNSWeaver.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metric names use the dnsweaver_ prefix as per DECISIONS.md.
const (
	Namespace = "dnsweaver"
)

// Build info metric - set via SetBuildInfo on startup.
var BuildInfo = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "build_info",
		Help:      "Build information for DNSWeaver.",
	},
	[]string{"version", "go_version"},
)

// Reconciliation metrics.
var (
	// ReconciliationsTotal counts total reconciliation runs by status.
	ReconciliationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "reconciliations_total",
			Help:      "Total number of reconciliation runs.",
		},
		[]string{"status"}, // "success", "error"
	)

	// ReconciliationDuration tracks reconciliation duration in seconds.
	ReconciliationDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: Namespace,
			Name:      "reconciliation_duration_seconds",
			Help:      "Duration of reconciliation runs in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
	)

	// WorkloadsScanned counts workloads scanned per reconciliation.
	WorkloadsScanned = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: Namespace,
			Name:      "workloads_scanned",
			Help:      "Number of workloads scanned in the last reconciliation.",
		},
	)

	// HostnamesDiscovered counts hostnames discovered per reconciliation.
	HostnamesDiscovered = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: Namespace,
			Name:      "hostnames_discovered",
			Help:      "Number of hostnames discovered in the last reconciliation.",
		},
	)
)

// Record operation metrics.
var (
	// RecordsCreatedTotal counts DNS records created.
	RecordsCreatedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "records_created_total",
			Help:      "Total number of DNS records created.",
		},
		[]string{"provider"},
	)

	// RecordsDeletedTotal counts DNS records deleted.
	RecordsDeletedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "records_deleted_total",
			Help:      "Total number of DNS records deleted.",
		},
		[]string{"provider"},
	)

	// RecordsSkippedTotal counts skipped record operations.
	RecordsSkippedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "records_skipped_total",
			Help:      "Total number of record operations skipped.",
		},
		[]string{"reason"}, // "no_provider", "dry_run", "already_exists"
	)

	// RecordsFailedTotal counts failed record operations.
	RecordsFailedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "records_failed_total",
			Help:      "Total number of failed record operations.",
		},
		[]string{"provider", "operation"}, // operation: "create", "delete"
	)
)

// Provider API metrics.
var (
	// ProviderAPIRequestsTotal counts API requests to providers.
	ProviderAPIRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "provider_api_requests_total",
			Help:      "Total number of API requests to DNS providers.",
		},
		[]string{"provider", "operation", "status"}, // operation: "ping", "list", "create", "delete"; status: "success", "error"
	)

	// ProviderAPIDuration tracks provider API request duration.
	ProviderAPIDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: Namespace,
			Name:      "provider_api_duration_seconds",
			Help:      "Duration of provider API requests in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"provider", "operation"},
	)

	// ProviderHealthy tracks provider health status (1=healthy, 0=unhealthy).
	ProviderHealthy = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: Namespace,
			Name:      "provider_healthy",
			Help:      "Provider health status (1=healthy, 0=unhealthy).",
		},
		[]string{"provider"},
	)

	// ProviderAvailable tracks provider availability status (1=ready, 0=pending initialization).
	// This is different from ProviderHealthy which tracks connectivity to ready providers.
	ProviderAvailable = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: Namespace,
			Name:      "provider_available",
			Help:      "Provider availability status (1=ready, 0=pending initialization).",
		},
		[]string{"provider", "type"},
	)

	// ProviderInitRetries tracks the number of initialization retry attempts per provider.
	ProviderInitRetries = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "provider_init_retries_total",
			Help:      "Total number of provider initialization retry attempts.",
		},
		[]string{"provider", "status"}, // status: "success", "failed"
	)

	// ProvidersReady tracks the number of ready providers.
	ProvidersReady = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: Namespace,
			Name:      "providers_ready",
			Help:      "Number of providers that are ready (initialized successfully).",
		},
	)

	// ProvidersPending tracks the number of providers pending initialization.
	ProvidersPending = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: Namespace,
			Name:      "providers_pending",
			Help:      "Number of providers pending initialization (failed to connect).",
		},
	)
)

// Source metrics.
var (
	// HostnamesExtractedTotal counts hostnames extracted from sources.
	HostnamesExtractedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "hostnames_extracted_total",
			Help:      "Total number of hostnames extracted from sources.",
		},
		[]string{"source", "method"}, // method: "labels", "files"
	)

	// FileWatcherPolls counts file watcher poll cycles.
	FileWatcherPolls = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "file_watcher_polls_total",
			Help:      "Total number of file watcher poll cycles.",
		},
	)

	// FileWatcherChangesDetected counts file changes detected.
	FileWatcherChangesDetected = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "file_watcher_changes_detected_total",
			Help:      "Total number of file changes detected.",
		},
	)
)

// Docker watcher metrics.
var (
	// DockerEventsProcessed counts Docker events processed.
	DockerEventsProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "docker_events_processed_total",
			Help:      "Total number of Docker events processed.",
		},
		[]string{"event_type"}, // "container_start", "container_stop", "service_create", etc.
	)

	// DockerWatcherReconnects counts Docker watcher reconnections.
	DockerWatcherReconnects = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      "docker_watcher_reconnects_total",
			Help:      "Total number of Docker watcher reconnections.",
		},
	)
)

// SetBuildInfo sets the build info metric with version and go version.
func SetBuildInfo(version, goVersion string) {
	BuildInfo.WithLabelValues(version, goVersion).Set(1)
}
