// Package metrics provides Prometheus metrics for DNSWeaver.
package metrics

// Metric names use the dnsweaver_ prefix as per DECISIONS.md.
const (
	Namespace = "dnsweaver"
)

// TODO: Implement in Issue #8 - Health & metrics endpoints
// Metrics to expose:
// - dnsweaver_records_total (gauge, labels: provider, record_type)
// - dnsweaver_reconcile_duration_seconds (histogram)
// - dnsweaver_provider_errors_total (counter, labels: provider)
// - dnsweaver_events_processed_total (counter, labels: event_type)
// - dnsweaver_build_info (gauge with version, go_version labels)
