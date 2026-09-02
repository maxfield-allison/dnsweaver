# Observability

dnsweaver provides built-in observability features for monitoring, alerting, and debugging.

## Health Endpoints

dnsweaver exposes HTTP endpoints on port 8080 (configurable via `DNSWEAVER_HEALTH_PORT`):

| Endpoint | Description |
|----------|-------------|
| `/health` | Overall health status |
| `/ready` | Readiness probe (for load balancers and Kubernetes) |
| `/metrics` | Prometheus metrics |

!!! tip "Kubernetes probes"
    The `/health` and `/ready` endpoints map directly to Kubernetes `livenessProbe` and `readinessProbe`. The Helm chart configures these automatically.

### Health Check

```bash
curl http://localhost:8080/health
```

Response:
```json
{
  "status": "healthy",
  "providers": {
    "internal": "ok",
    "external": "ok"
  },
  "docker": "connected"
}
```

### Readiness Check

```bash
curl http://localhost:8080/ready
```

Returns `200 OK` when ready to process events, `503` otherwise.

## Prometheus Metrics

dnsweaver exposes Prometheus-compatible metrics at `/metrics`:

```bash
curl http://localhost:8080/metrics
```

### Build Info

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `dnsweaver_build_info` | Gauge | `version`, `go_version` | Build information |

### Reconciliation

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `dnsweaver_reconciliations_total` | Counter | `status` | Reconciliation cycles (success/error) |
| `dnsweaver_reconciliation_duration_seconds` | Histogram | — | Duration of reconciliation cycles |
| `dnsweaver_workloads_scanned` | Gauge | — | Workloads scanned in last reconciliation |
| `dnsweaver_hostnames_discovered` | Gauge | — | Hostnames discovered in last reconciliation |

### Record Operations

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `dnsweaver_records_created_total` | Counter | `provider` | Records created since startup |
| `dnsweaver_records_deleted_total` | Counter | `provider` | Records deleted since startup |
| `dnsweaver_records_skipped_total` | Counter | `reason` | Records skipped (already exist, filtered, etc.) |
| `dnsweaver_records_failed_total` | Counter | `provider`, `operation` | Failed record operations (create/delete/update) |

### Provider

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `dnsweaver_provider_api_requests_total` | Counter | `provider`, `operation`, `status` | API requests to providers |
| `dnsweaver_provider_api_duration_seconds` | Histogram | `provider`, `operation` | Provider API request duration |
| `dnsweaver_provider_healthy` | Gauge | `provider` | Provider health status (1=healthy, 0=unhealthy) |
| `dnsweaver_provider_available` | Gauge | `provider`, `type` | Provider availability (1=available, 0=unavailable) |
| `dnsweaver_provider_init_retries_total` | Counter | `provider`, `status` | Provider initialization retry attempts |
| `dnsweaver_providers_ready` | Gauge | — | Number of providers ready |
| `dnsweaver_providers_pending` | Gauge | — | Number of providers pending initialization |

### Source Discovery

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `dnsweaver_hostnames_extracted_total` | Counter | `source`, `method` | Hostnames extracted (source: traefik/dnsweaver/kubernetes, method: labels/files) |
| `dnsweaver_file_watcher_polls_total` | Counter | — | File discovery poll cycles |
| `dnsweaver_file_watcher_changes_detected_total` | Counter | — | File discovery changes detected |

### Docker

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `dnsweaver_docker_events_processed_total` | Counter | `event_type` | Docker events processed (e.g., container_start, service_create) |
| `dnsweaver_docker_watcher_reconnects_total` | Counter | — | Docker event stream reconnections |

### Example Queries

```promql
# Provider health
dnsweaver_provider_healthy

# Providers still initializing
dnsweaver_providers_pending > 0

# Record creation rate per provider
rate(dnsweaver_records_created_total[5m])

# Failed record operations
rate(dnsweaver_records_failed_total[5m])

# Provider API error rate
rate(dnsweaver_provider_api_requests_total{status="error"}[5m])

# Provider API latency (p95)
histogram_quantile(0.95, rate(dnsweaver_provider_api_duration_seconds_bucket[5m]))

# Reconciliation success rate
rate(dnsweaver_reconciliations_total{status="success"}[5m])
  / rate(dnsweaver_reconciliations_total[5m])

# Hostname extraction rate by source
rate(dnsweaver_hostnames_extracted_total[5m])

# Docker event rate by type
rate(dnsweaver_docker_events_processed_total[5m])
```

## Grafana Dashboard

Import the community dashboard or create your own with these panels:

### Key Panels

1. **Provider Health** - `dnsweaver_provider_healthy`
2. **Providers Ready / Pending** - `dnsweaver_providers_ready` / `dnsweaver_providers_pending`
3. **Record Changes** - `rate(dnsweaver_records_created_total[5m])` + `rate(dnsweaver_records_deleted_total[5m])`
4. **Record Failures** - `rate(dnsweaver_records_failed_total[5m])`
5. **API Request Rate** - `rate(dnsweaver_provider_api_requests_total[5m])`
6. **API Latency** - `histogram_quantile(0.95, rate(dnsweaver_provider_api_duration_seconds_bucket[5m]))`
7. **Docker Events** - `rate(dnsweaver_docker_events_processed_total[5m])`
8. **Workloads & Hostnames** - `dnsweaver_workloads_scanned` + `dnsweaver_hostnames_discovered`

### Example Dashboard JSON

```json
{
  "panels": [
    {
      "title": "Provider Health",
      "type": "stat",
      "targets": [
        {
          "expr": "dnsweaver_provider_healthy"
        }
      ]
    }
  ]
}
```

## Logging

dnsweaver outputs structured logs to stdout.

### Log Levels

Configure via `DNSWEAVER_LOG_LEVEL`:

| Level | Description |
|-------|-------------|
| `debug` | Detailed information for debugging |
| `info` | Normal operational messages (default) |
| `warn` | Warning conditions |
| `error` | Error conditions |

### Log Format

Configure via `DNSWEAVER_LOG_FORMAT`:

| Format | Description |
|--------|-------------|
| `json` | JSON-structured logs (default) |
| `text` | Human-readable text format |

### JSON Log Example

```json
{
  "time": "2024-01-15T10:30:00Z",
  "level": "info",
  "msg": "record created",
  "provider": "internal",
  "hostname": "app.example.com",
  "record_type": "A",
  "target": "192.0.2.100"
}
```

### Filtering Logs

```bash
# View only errors
docker logs dnsweaver 2>&1 | jq 'select(.level == "error")'

# View record changes
docker logs dnsweaver 2>&1 | jq 'select(.msg | contains("record"))'

# View specific provider
docker logs dnsweaver 2>&1 | jq 'select(.provider == "internal")'
```

## Alerting

### Prometheus Alerting Rules

```yaml
groups:
  - name: dnsweaver
    rules:
      - alert: DNSWeaverDown
        expr: up{job="dnsweaver"} == 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "dnsweaver is down"

      - alert: DNSWeaverProviderUnhealthy
        expr: dnsweaver_provider_healthy == 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "dnsweaver provider unhealthy"

      - alert: DNSWeaverAPIErrors
        expr: rate(dnsweaver_provider_api_requests_total{status="error"}[5m]) > 0.1
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "dnsweaver provider API errors detected"

      - alert: DNSWeaverNoReconciliation
        expr: increase(dnsweaver_reconciliations_total[10m]) == 0
        for: 15m
        labels:
          severity: warning
        annotations:
          summary: "dnsweaver reconciliation not running"
```

## Docker Health Check

Add to your Docker Compose or Swarm deployment:

```yaml
healthcheck:
  test: ["CMD", "/usr/local/bin/dnsweaver", "--healthcheck"]
  interval: 30s
  timeout: 10s
  retries: 3
  start_period: 10s
```

## Kubernetes Monitoring

### ServiceMonitor (Prometheus Operator)

If you use the Prometheus Operator, create a ServiceMonitor to scrape dnsweaver metrics:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: dnsweaver
  namespace: dnsweaver
  labels:
    release: prometheus  # Match your Prometheus Operator selector
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: dnsweaver
  endpoints:
    - port: http
      path: /metrics
      interval: 30s
```

The Helm chart can create this automatically with `serviceMonitor.enabled=true`.

### Pod Probes

The Helm chart configures these by default:

```yaml
livenessProbe:
  httpGet:
    path: /health
    port: http
  initialDelaySeconds: 10
  periodSeconds: 30
readinessProbe:
  httpGet:
    path: /ready
    port: http
  initialDelaySeconds: 5
  periodSeconds: 10
```

## Debug Mode

For troubleshooting, enable debug logging:

```yaml
environment:
  - DNSWEAVER_LOG_LEVEL=debug
  - DNSWEAVER_LOG_FORMAT=text  # Easier to read
```

Debug mode logs:
- Every Docker event received
- Hostname extraction from labels
- Provider matching decisions
- API requests/responses
- Reconciliation details
