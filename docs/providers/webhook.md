# Webhook Provider

The webhook provider sends HTTP requests to custom endpoints, enabling integration with any DNS system.

## Use Cases

- DNS providers without native dnsweaver support
- Custom internal DNS systems
- Integration with automation platforms
- Audit logging to external systems

## Basic Configuration

```yaml
environment:
  - DNSWEAVER_INSTANCES=custom

  - DNSWEAVER_CUSTOM_TYPE=webhook
  - DNSWEAVER_CUSTOM_URL=http://dns-api.internal/records
  - DNSWEAVER_CUSTOM_AUTH_TOKEN_FILE=/run/secrets/dns_api_token
  - DNSWEAVER_CUSTOM_RECORD_TYPE=A
  - DNSWEAVER_CUSTOM_TARGET=10.0.0.100
  - DNSWEAVER_CUSTOM_DOMAINS=*.example.com
```

## Configuration Reference

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TYPE` | Yes | - | Must be `webhook` |
| `URL` | Yes | - | Base URL for webhook endpoint |
| `AUTH_TOKEN` | No | - | Bearer token for authentication |
| `AUTH_TOKEN_FILE` | Alt | - | Path to token file |
| `AUTH_HEADER` | No | `Authorization` | Header name for auth token |
| `RECORD_TYPE` | Yes | - | Record type for the provider |
| `TARGET` | Yes | - | Record value |
| `DOMAINS` | Yes | - | Glob patterns to match |
| `EXCLUDE_DOMAINS` | No | - | Patterns to exclude |
| `CREATE_METHOD` | No | `POST` | HTTP method for create |
| `DELETE_METHOD` | No | `DELETE` | HTTP method for delete |
| `TIMEOUT` | No | `30s` | Request timeout |

## Webhook Payloads

### Create Record

When a record should be created, dnsweaver sends:

```http
POST /records HTTP/1.1
Host: dns-api.internal
Authorization: Bearer <token>
Content-Type: application/json

{
  "action": "create",
  "hostname": "app.example.com",
  "record_type": "A",
  "value": "10.0.0.100",
  "ttl": 300,
  "source": "docker",
  "container_id": "abc123...",
  "labels": {
    "traefik.http.routers.app.rule": "Host(`app.example.com`)"
  }
}
```

### Delete Record

When a record should be deleted:

```http
DELETE /records HTTP/1.1
Host: dns-api.internal
Authorization: Bearer <token>
Content-Type: application/json

{
  "action": "delete",
  "hostname": "app.example.com",
  "record_type": "A",
  "value": "10.0.0.100"
}
```

## Expected Responses

dnsweaver expects:

- **2xx**: Success
- **404**: Record not found (for delete, treated as success)
- **4xx/5xx**: Error (logged, may retry)

Response body is logged but not parsed.

## Authentication Options

### Bearer Token

```yaml
- DNSWEAVER_CUSTOM_AUTH_TOKEN_FILE=/run/secrets/api_token
```

Sends: `Authorization: Bearer <token>`

### Custom Header

```yaml
- DNSWEAVER_CUSTOM_AUTH_HEADER=X-API-Key
- DNSWEAVER_CUSTOM_AUTH_TOKEN=your-api-key
```

Sends: `X-API-Key: your-api-key`

### No Authentication

Omit `AUTH_TOKEN` and `AUTH_TOKEN_FILE`.

## Example: Home Assistant Integration

Use webhooks to trigger Home Assistant automations:

```yaml
environment:
  - DNSWEAVER_HASS_TYPE=webhook
  - DNSWEAVER_HASS_URL=http://homeassistant:8123/api/webhook/dns_update
  - DNSWEAVER_HASS_AUTH_TOKEN_FILE=/run/secrets/hass_token
  - DNSWEAVER_HASS_RECORD_TYPE=A
  - DNSWEAVER_HASS_TARGET=10.0.0.100
  - DNSWEAVER_HASS_DOMAINS=*.home.example.com
```

## Example: Custom DNS API

Building a receiver for the webhook:

```python
from flask import Flask, request, jsonify

app = Flask(__name__)

@app.route('/records', methods=['POST', 'DELETE'])
def handle_record():
    data = request.json

    if request.method == 'POST':
        # Create record in your DNS system
        create_dns_record(
            hostname=data['hostname'],
            record_type=data['record_type'],
            value=data['value'],
            ttl=data.get('ttl', 300)
        )
        return jsonify({'status': 'created'}), 201

    elif request.method == 'DELETE':
        # Delete record from your DNS system
        delete_dns_record(
            hostname=data['hostname'],
            record_type=data['record_type']
        )
        return jsonify({'status': 'deleted'}), 200
```

## Troubleshooting

### Connection Refused

Verify the webhook endpoint is accessible:

```bash
docker exec dnsweaver curl -v http://dns-api.internal/records
```

### Authentication Errors

Check the token and header format:

```bash
curl -H "Authorization: Bearer $(cat /run/secrets/api_token)" \
  http://dns-api.internal/records
```

### Timeout

Increase the timeout for slow endpoints:

```yaml
- DNSWEAVER_CUSTOM_TIMEOUT=60s
```
