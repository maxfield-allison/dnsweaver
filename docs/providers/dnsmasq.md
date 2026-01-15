# dnsmasq

dnsmasq is a lightweight DNS/DHCP server commonly used in routers and containers. dnsweaver manages dnsmasq through configuration files.

## Requirements

- Write access to dnsmasq's configuration directory
- Ability to signal dnsmasq to reload (or dnsmasq configured to watch files)

## Basic Configuration

```yaml
environment:
  - DNSWEAVER_INSTANCES=dnsmasq

  - DNSWEAVER_DNSMASQ_TYPE=dnsmasq
  - DNSWEAVER_DNSMASQ_CONFIG_DIR=/etc/dnsmasq.d
  - DNSWEAVER_DNSMASQ_RECORD_TYPE=A
  - DNSWEAVER_DNSMASQ_TARGET=10.0.0.100
  - DNSWEAVER_DNSMASQ_DOMAINS=*.home.example.com
volumes:
  - /path/to/dnsmasq.d:/etc/dnsmasq.d
```

## Configuration Reference

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TYPE` | Yes | - | Must be `dnsmasq` |
| `CONFIG_DIR` | Yes | - | Path to dnsmasq config directory |
| `CONFIG_FILE` | No | `dnsweaver.conf` | Filename for managed records |
| `RECORD_TYPE` | Yes | - | `A`, `AAAA`, or `CNAME` |
| `TARGET` | Yes | - | Record value |
| `DOMAINS` | Yes | - | Glob patterns to match |
| `EXCLUDE_DOMAINS` | No | - | Patterns to exclude |
| `RELOAD_COMMAND` | No | - | Command to reload dnsmasq |

## How It Works

dnsweaver creates a configuration file in the dnsmasq directory:

```
# /etc/dnsmasq.d/dnsweaver.conf (managed by dnsweaver)
address=/app.home.example.com/10.0.0.100
address=/web.home.example.com/10.0.0.100
cname=alias.home.example.com,target.home.example.com
```

## Record Types

### A Records

```yaml
- DNSWEAVER_DNSMASQ_RECORD_TYPE=A
- DNSWEAVER_DNSMASQ_TARGET=10.0.0.100
```

Produces:
```
address=/hostname.example.com/10.0.0.100
```

### AAAA Records

```yaml
- DNSWEAVER_DNSMASQ_RECORD_TYPE=AAAA
- DNSWEAVER_DNSMASQ_TARGET=2001:db8::1
```

Produces:
```
address=/hostname.example.com/2001:db8::1
```

### CNAME Records

```yaml
- DNSWEAVER_DNSMASQ_RECORD_TYPE=CNAME
- DNSWEAVER_DNSMASQ_TARGET=proxy.example.com
```

Produces:
```
cname=hostname.example.com,proxy.example.com
```

## Reloading dnsmasq

After file changes, dnsmasq needs to reload its configuration. Options:

### 1. Automatic (with inotify)

Some dnsmasq versions support watching for file changes. No reload command needed.

### 2. SIGHUP

Send HUP signal to reload:

```yaml
- DNSWEAVER_DNSMASQ_RELOAD_COMMAND=pkill -HUP dnsmasq
```

### 3. Restart Service

For systemd-managed dnsmasq:

```yaml
- DNSWEAVER_DNSMASQ_RELOAD_COMMAND=systemctl reload dnsmasq
```

!!! note
    The reload command runs inside the dnsweaver container. For remote dnsmasq, you may need a different approach (SSH, HTTP trigger, etc.).

## Docker Deployment

When running dnsweaver with dnsmasq in Docker:

```yaml
services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    environment:
      - DNSWEAVER_DNSMASQ_CONFIG_DIR=/etc/dnsmasq.d
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - dnsmasq_config:/etc/dnsmasq.d

  dnsmasq:
    image: jpillora/dnsmasq:latest
    volumes:
      - dnsmasq_config:/etc/dnsmasq.d
    ports:
      - "53:53/udp"

volumes:
  dnsmasq_config:
```

## Router Integration

For routers running dnsmasq (OpenWrt, DD-WRT, etc.):

1. Mount the router's dnsmasq config directory via NFS/CIFS
2. Configure dnsweaver to write to that mount
3. Set up a reload mechanism (SSH command, webhook, etc.)

## Troubleshooting

### Records Not Updating

Check the managed config file:

```bash
cat /etc/dnsmasq.d/dnsweaver.conf
```

### Reload Not Working

Verify the reload command:

```bash
docker exec dnsweaver /bin/sh -c "$RELOAD_COMMAND"
```

### Permission Denied

Ensure dnsweaver can write to the config directory:

```bash
docker exec dnsweaver touch /etc/dnsmasq.d/test.conf
```
