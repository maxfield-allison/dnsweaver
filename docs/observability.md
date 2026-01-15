# Observability

dnsweaver provides built-in observability features for monitoring, alerting, and debugging.

## Health Endpoints

dnsweaver exposes HTTP endpoints on port 8080 (configurable via `DNSWEAVER_HEALTH_PORT`):

| Endpoint | Description |
|----------|-------------|
| `/health` | Overall health status |
| `/ready` | Readiness probe (for Kubernetes) |
| `/metrics` | Prometheus metrics |

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

### Available Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `dnsweaver_build_info` | Gauge | Build information (version, commit) |
| `dnsweaver_reconciliations_total` | Counter | Reconciliation cycles run |
| `dnsweaver_reconciliation_duration_seconds` | Histogram | Duration of reconciliation cycles |
| `dnsweaver_workloads_scanned` | Gauge | Number of workloads scanned |
| `dnsweaver_hostnames_discovered` | Gauge | Number of hostnames discovered |
| `dnsweaver_records_created_total` | Counter | Records created since startup |
| `dnsweaver_records_deleted_total` | Counter | Records deleted since startup |
| `dnsweaver_records_skipped_total` | Counter | Records skipped (already exist) |
| `dnsweaver_records_failed_total` | Counter | Record operations that failed |
| `dnsweaver_provider_api_requests_total` | Counter | API requests to providers |
| `dnsweaver_provider_api_duration_seconds` | Histogram | Provider API request duration |
| `dnsweaver_provider_healthy` | Gauge | Provider health status (1=healthy) |
| `dnsweaver_hostnames_extracted_total` | Counter | Hostnames extracted from sources |
| `dnsweaver_docker_events_processed_total` | Counter | Docker events processed |
| `dnsweaver_docker_watcher_reconnects_total` | Counter | Docker watcher reconnections |

### Labels

Metrics include labels for filtering:

- `provider` - Provider instance name
- `record_type` - A, AAAA, CNAME, SRV, TXT
- `status` - API response status (success, error)
- `endpoint` - API endpoint called

### Example Queries

```promql
# Provider health
dnsweaver_provider_healthy

# Record creation rate per provider
rate(dnsweaver_records_created_total[5m])

# Records created per minute
rate(dnsweaver_records_created_total[1m])

# API error rate
rate(dnsweaver_provider_api_requests_total{status="error"}[5m])
```

## Grafana Dashboard

Import the community dashboard or create your own with these panels:

### Key Panels

1. **Provider Health** - `dnsweaver_provider_healthy`
2. **Record Changes** - `rate(dnsweaver_records_created_total[5m])` + `rate(dnsweaver_records_deleted_total[5m])`
3. **API Request Rate** - `rate(dnsweaver_provider_api_requests_total[5m])`
4. **Docker Events** - `rate(dnsweaver_docker_events_processed_total[5m])`

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
  "target": "10.0.0.100"
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

Add to your deployment:

```yaml
healthcheck:
  test: ["CMD", "wget", "-q", "--spider", "http://localhost:8080/health"]
  interval: 30s
  timeout: 10s
  retries: 3
  start_period: 10s
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
