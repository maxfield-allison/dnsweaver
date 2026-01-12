package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestSetBuildInfo(t *testing.T) {
	// Reset metrics for testing
	BuildInfo.Reset()

	SetBuildInfo("v1.0.0", "go1.24")

	// Check that metric was set
	count := testutil.CollectAndCount(BuildInfo)
	if count != 1 {
		t.Errorf("expected 1 metric, got %d", count)
	}

	// Verify the value is 1
	value := testutil.ToFloat64(BuildInfo.WithLabelValues("v1.0.0", "go1.24"))
	if value != 1 {
		t.Errorf("expected value 1, got %f", value)
	}
}

func TestReconciliationMetrics(t *testing.T) {
	// Reset metrics for testing
	ReconciliationsTotal.Reset()
	// Histograms don't have Reset, but we can still test by observing values

	// Simulate recording reconciliation metrics
	ReconciliationsTotal.WithLabelValues("success").Inc()
	ReconciliationsTotal.WithLabelValues("success").Inc()
	ReconciliationsTotal.WithLabelValues("error").Inc()
	ReconciliationDuration.Observe(0.5)
	ReconciliationDuration.Observe(1.2)

	// Check counts
	successCount := testutil.ToFloat64(ReconciliationsTotal.WithLabelValues("success"))
	if successCount < 2 {
		t.Errorf("expected at least 2 success reconciliations, got %f", successCount)
	}

	errorCount := testutil.ToFloat64(ReconciliationsTotal.WithLabelValues("error"))
	if errorCount < 1 {
		t.Errorf("expected at least 1 error reconciliation, got %f", errorCount)
	}
}

func TestRecordMetrics(t *testing.T) {
	// Reset metrics for testing
	RecordsCreatedTotal.Reset()
	RecordsDeletedTotal.Reset()
	RecordsSkippedTotal.Reset()
	RecordsFailedTotal.Reset()

	// Simulate recording record operations
	RecordsCreatedTotal.WithLabelValues("internal-dns").Add(5)
	RecordsDeletedTotal.WithLabelValues("internal-dns").Add(2)
	RecordsSkippedTotal.WithLabelValues("no_provider").Add(3)
	RecordsFailedTotal.WithLabelValues("internal-dns", "create").Inc()

	// Verify counts
	created := testutil.ToFloat64(RecordsCreatedTotal.WithLabelValues("internal-dns"))
	if created != 5 {
		t.Errorf("expected 5 created, got %f", created)
	}

	deleted := testutil.ToFloat64(RecordsDeletedTotal.WithLabelValues("internal-dns"))
	if deleted != 2 {
		t.Errorf("expected 2 deleted, got %f", deleted)
	}

	skipped := testutil.ToFloat64(RecordsSkippedTotal.WithLabelValues("no_provider"))
	if skipped != 3 {
		t.Errorf("expected 3 skipped, got %f", skipped)
	}

	failed := testutil.ToFloat64(RecordsFailedTotal.WithLabelValues("internal-dns", "create"))
	if failed != 1 {
		t.Errorf("expected 1 failed, got %f", failed)
	}
}

func TestProviderAPIMetrics(t *testing.T) {
	// Reset metrics for testing
	ProviderAPIRequestsTotal.Reset()
	ProviderAPIDuration.Reset()
	ProviderHealthy.Reset()

	// Simulate API requests
	ProviderAPIRequestsTotal.WithLabelValues("internal-dns", "ping", "success").Inc()
	ProviderAPIRequestsTotal.WithLabelValues("internal-dns", "create", "success").Add(5)
	ProviderAPIRequestsTotal.WithLabelValues("internal-dns", "create", "error").Inc()
	ProviderAPIDuration.WithLabelValues("internal-dns", "create").Observe(0.1)
	ProviderHealthy.WithLabelValues("internal-dns").Set(1)

	// Verify
	pingCount := testutil.ToFloat64(ProviderAPIRequestsTotal.WithLabelValues("internal-dns", "ping", "success"))
	if pingCount != 1 {
		t.Errorf("expected 1 ping success, got %f", pingCount)
	}

	createSuccess := testutil.ToFloat64(ProviderAPIRequestsTotal.WithLabelValues("internal-dns", "create", "success"))
	if createSuccess != 5 {
		t.Errorf("expected 5 create success, got %f", createSuccess)
	}

	createError := testutil.ToFloat64(ProviderAPIRequestsTotal.WithLabelValues("internal-dns", "create", "error"))
	if createError != 1 {
		t.Errorf("expected 1 create error, got %f", createError)
	}

	healthy := testutil.ToFloat64(ProviderHealthy.WithLabelValues("internal-dns"))
	if healthy != 1 {
		t.Errorf("expected healthy=1, got %f", healthy)
	}
}

func TestMetricNames(t *testing.T) {
	// Verify all metrics use the correct namespace prefix
	expectedPrefix := "dnsweaver_"

	metrics := []prometheus.Collector{
		BuildInfo,
		ReconciliationsTotal,
		ReconciliationDuration,
		WorkloadsScanned,
		HostnamesDiscovered,
		RecordsCreatedTotal,
		RecordsDeletedTotal,
		RecordsSkippedTotal,
		RecordsFailedTotal,
		ProviderAPIRequestsTotal,
		ProviderAPIDuration,
		ProviderHealthy,
		HostnamesExtractedTotal,
		FileWatcherPolls,
		FileWatcherChangesDetected,
		DockerEventsProcessed,
		DockerWatcherReconnects,
	}

	for _, m := range metrics {
		// Get metric descriptions
		ch := make(chan *prometheus.Desc, 10)
		m.Describe(ch)
		close(ch)

		for desc := range ch {
			name := desc.String()
			if !strings.Contains(name, expectedPrefix) {
				t.Errorf("metric %s does not have expected prefix %s", name, expectedPrefix)
			}
		}
	}
}
