# Pi-hole

Pi-hole is a network-wide ad blocker that also serves as a local DNS server. dnsweaver supports Pi-hole through its API or by direct file manipulation.

## Requirements

- Pi-hole v5.0+ for API mode
- Pi-hole with accessible `/etc/pihole/` for file mode

## API Mode (Recommended)

For Pi-hole with API access:

```yaml
environment:
  - DNSWEAVER_INSTANCES=pihole

  - DNSWEAVER_PIHOLE_TYPE=pihole
  - DNSWEAVER_PIHOLE_MODE=api
  - DNSWEAVER_PIHOLE_URL=http://pihole:80
  - DNSWEAVER_PIHOLE_PASSWORD_FILE=/run/secrets/pihole_password
  - DNSWEAVER_PIHOLE_RECORD_TYPE=A
  - DNSWEAVER_PIHOLE_TARGET=10.0.0.100
  - DNSWEAVER_PIHOLE_DOMAINS=*.home.example.com
```

## File Mode

For direct file access (when dnsweaver can mount Pi-hole's config directory):

```yaml
environment:
  - DNSWEAVER_INSTANCES=pihole

  - DNSWEAVER_PIHOLE_TYPE=pihole
  - DNSWEAVER_PIHOLE_MODE=file
  - DNSWEAVER_PIHOLE_CONFIG_DIR=/etc/pihole
  - DNSWEAVER_PIHOLE_RECORD_TYPE=A
  - DNSWEAVER_PIHOLE_TARGET=10.0.0.100
  - DNSWEAVER_PIHOLE_DOMAINS=*.home.example.com
volumes:
  - /path/to/pihole/etc:/etc/pihole
```

## Configuration Reference

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TYPE` | Yes | - | Must be `pihole` |
| `MODE` | No | `api` | `api` or `file` |
| `URL` | API mode | - | Pi-hole web interface URL |
| `PASSWORD` | API mode | - | Web interface password |
| `PASSWORD_FILE` | API alt | - | Path to password file |
| `CONFIG_DIR` | File mode | - | Path to Pi-hole config directory |
| `RECORD_TYPE` | Yes | - | `A`, `AAAA`, or `CNAME` |
| `TARGET` | Yes | - | Record value |
| `DOMAINS` | Yes | - | Glob patterns to match |
| `EXCLUDE_DOMAINS` | No | - | Patterns to exclude |

## Record Types

### A Records (Local DNS)

Pi-hole stores local DNS entries in `/etc/pihole/custom.list`:

```yaml
- DNSWEAVER_PIHOLE_RECORD_TYPE=A
- DNSWEAVER_PIHOLE_TARGET=10.0.0.100
```

### CNAME Records

CNAME records require Pi-hole's FTL CNAME feature:

```yaml
- DNSWEAVER_PIHOLE_RECORD_TYPE=CNAME
- DNSWEAVER_PIHOLE_TARGET=proxy.example.com
```

## Getting the API Password

The Pi-hole API uses your web interface password. To set or retrieve it:

```bash
# Set a new password
pihole -a -p newpassword

# Or use the existing password from setup
```

## Docker Deployment Considerations

When running dnsweaver alongside Pi-hole in Docker:

### Same Docker Host

```yaml
services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    environment:
      - DNSWEAVER_PIHOLE_URL=http://pihole:80
    networks:
      - pihole_network

  pihole:
    image: pihole/pihole:latest
    networks:
      - pihole_network

networks:
  pihole_network:
```

### Remote Pi-hole

```yaml
services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    environment:
      - DNSWEAVER_PIHOLE_URL=http://192.168.1.100:80
```

## File Mode Details

In file mode, dnsweaver manages these files:

- `custom.list` - A/AAAA records
- `05-pihole-custom-cname.conf` - CNAME records

!!! warning
    File mode requires dnsweaver to have write access to Pi-hole's config directory. Changes may require restarting Pi-hole's FTL service.

## Troubleshooting

### API Authentication Failed

Check your password:

```bash
curl -X POST "http://pihole/admin/api.php?auth=$(echo -n 'password' | sha256sum | cut -d' ' -f1)"
```

### Records Not Resolving

After file mode changes, restart FTL:

```bash
pihole restartdns
```

### Permission Denied (File Mode)

Ensure the dnsweaver container runs as a user that can write to Pi-hole's config directory.
