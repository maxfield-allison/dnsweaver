---
title: dnsmasq DNS Provider
description: Automatically manage dnsmasq A and CNAME records from Docker and Kubernetes with dnsweaver through file-based configuration.
---

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
  - DNSWEAVER_DNSMASQ_TARGET=192.0.2.100
  - DNSWEAVER_DNSMASQ_DOMAINS=*.home.example.com
volumes:
  - /path/to/dnsmasq.d:/etc/dnsmasq.d
```

## Configuration Reference

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TYPE` | Yes | - | Must be `dnsmasq` |
| `CONFIG_DIR` | No | `/etc/dnsmasq.d` | Path to dnsmasq config directory |
| `CONFIG_FILE` | No | `dnsweaver.conf` | Filename for managed records |
| `RELOAD_COMMAND` | No | `systemctl reload dnsmasq` | Command to reload dnsmasq |
| `ZONE` | No | - | DNS zone for record filtering |
| `TTL` | No | `300` | Record TTL in seconds |
| `RECORD_TYPE` | Yes | - | `A` or `CNAME` |
| `TARGET` | Yes | - | Record value |
| `DOMAINS` | Yes | - | Glob patterns to match |
| `EXCLUDE_DOMAINS` | No | - | Patterns to exclude |

### SSH Configuration

When managing a remote dnsmasq instance, add these variables to enable SSH mode:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `SSH_HOST` | Yes* | - | SSH server hostname or IP |
| `SSH_PORT` | No | `22` | SSH server port |
| `SSH_USER` | Yes* | - | SSH username |
| `SSH_KEY_FILE` | No† | - | Path to SSH private key file |
| `SSH_PASSWORD` | No† | - | SSH password |
| `SSH_KNOWN_HOSTS_FILE` | Yes‡ | - | Path to an OpenSSH `known_hosts` file used to verify the server's host key |
| `SSH_STRICT_HOST_KEY_CHECKING` | No | `true` | Verify the server host key against `SSH_KNOWN_HOSTS_FILE`. Set to `false` to disable verification (**insecure**) |

\* Required when SSH mode is enabled (any SSH variable is set).

† At least one authentication method is required: `SSH_KEY_FILE` or `SSH_PASSWORD`. Key-based authentication is strongly recommended.

‡ Required when `SSH_STRICT_HOST_KEY_CHECKING` is `true` (the default). Either provide a `known_hosts` file, or explicitly set `SSH_STRICT_HOST_KEY_CHECKING=false` to opt out of verification.

`SSH_KEY_FILE`, `SSH_PASSWORD`, and `SSH_KNOWN_HOSTS_FILE` support the `_FILE` suffix for [Docker secrets](../configuration/secrets.md). For example, use `SSH_KEY_FILE_FILE` to read the private key path from a secret, or `SSH_PASSWORD_FILE` to read the password from a secret.

## How It Works

dnsweaver creates a configuration file in the dnsmasq directory:

```
# /etc/dnsmasq.d/dnsweaver.conf (managed by dnsweaver)
address=/app.home.example.com/192.0.2.100
address=/web.home.example.com/192.0.2.100
cname=alias.home.example.com,target.home.example.com
```

## Ownership and Managed Mode

!!! info "Target-Based Ownership Inference"
    dnsmasq is file-based and does not support TXT records, so dnsweaver cannot create
    ownership TXT markers (`_dnsweaver.*`) for this provider. In **managed mode**, dnsweaver
    uses **target-based ownership inference** instead: if a record's type and target match the
    provider instance's configured `RECORD_TYPE` and `TARGET`, it is inferred as owned and
    cleaned up when the source workload disappears. Records with different targets are preserved.

    See [Operational Modes](../configuration/modes.md) for details.

## Record Types

### A Records

```yaml
- DNSWEAVER_DNSMASQ_RECORD_TYPE=A
- DNSWEAVER_DNSMASQ_TARGET=192.0.2.100
```

Produces:
```
address=/hostname.example.com/192.0.2.100
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

## SSH Remote Management

dnsweaver can manage dnsmasq instances running on remote hosts via SSH. When SSH mode is enabled, dnsweaver uses SFTP to write configuration files and SSH exec to run reload commands on the remote system — no shared volumes or local mounts required.

### When to Use SSH Mode

| Scenario | SSH Mode? | Notes |
|----------|-----------|-------|
| dnsmasq in a sidecar/shared Docker volume | No | Use volume mounts |
| dnsmasq on a remote server or VM | **Yes** | SSH manages files remotely |
| dnsmasq on a router (OpenWrt, DD-WRT) | **Yes** | SSH is the standard access method |
| dnsmasq in a different Docker host | **Yes** | No shared filesystem |

### Basic SSH Configuration

```yaml
environment:
  - DNSWEAVER_INSTANCES=router

  - DNSWEAVER_ROUTER_TYPE=dnsmasq
  - DNSWEAVER_ROUTER_CONFIG_DIR=/tmp/dnsmasq.d
  - DNSWEAVER_ROUTER_RECORD_TYPE=A
  - DNSWEAVER_ROUTER_TARGET=192.0.2.100
  - DNSWEAVER_ROUTER_DOMAINS=*.home.example.com
  - DNSWEAVER_ROUTER_RELOAD_COMMAND=killall -HUP dnsmasq

  # SSH connection
  - DNSWEAVER_ROUTER_SSH_HOST=192.168.1.1
  - DNSWEAVER_ROUTER_SSH_PORT=22
  - DNSWEAVER_ROUTER_SSH_USER=root
  - DNSWEAVER_ROUTER_SSH_KEY_FILE=/run/secrets/router_ssh_key
  - DNSWEAVER_ROUTER_SSH_KNOWN_HOSTS_FILE=/ssh/known_hosts
```

!!! note
    When SSH mode is enabled, `CONFIG_DIR` refers to a path **on the remote host**, not inside the dnsweaver container. `RELOAD_COMMAND` also executes on the remote host via SSH exec.

### Authentication Methods

SSH supports three authentication methods, in order of preference:

#### 1. Key File (Recommended)

Mount a private key file into the container:

```yaml
environment:
  - DNSWEAVER_ROUTER_SSH_KEY_FILE=/ssh/id_ed25519
volumes:
  - ./ssh_keys/router_key:/ssh/id_ed25519:ro
```

Or use Docker secrets:

```yaml
environment:
  - DNSWEAVER_ROUTER_SSH_KEY_FILE_FILE=/run/secrets/router_ssh_key
secrets:
  - router_ssh_key
```

#### 2. Password Authentication

Use password auth as a fallback (not recommended for production):

```yaml
environment:
  - DNSWEAVER_ROUTER_SSH_PASSWORD_FILE=/run/secrets/router_password
secrets:
  - router_password
```

### Host Key Verification

dnsweaver verifies the remote host key against a `known_hosts` file by default, protecting SSH sessions against man-in-the-middle attacks. `SSH_STRICT_HOST_KEY_CHECKING` defaults to `true`, so a `known_hosts` file is required whenever SSH mode is enabled.

Generate a `known_hosts` entry from a host you trust and mount it into the container:

```yaml
environment:
  - DNSWEAVER_ROUTER_SSH_KNOWN_HOSTS_FILE=/ssh/known_hosts
volumes:
  - ./ssh_keys/known_hosts:/ssh/known_hosts:ro
```

Populate the file with `ssh-keyscan` (verify the fingerprint out-of-band before trusting it):

```bash
ssh-keyscan -t ed25519 192.168.1.1 >> ./ssh_keys/known_hosts
```

The path may also be supplied through a Docker secret with the `_FILE` suffix:

```yaml
environment:
  - DNSWEAVER_ROUTER_SSH_KNOWN_HOSTS_FILE_FILE=/run/secrets/router_known_hosts
secrets:
  - router_known_hosts
```

If the remote host key changes (rotation, reinstall, or an actual MITM), the connection fails fast at startup with a clear error rather than silently trusting the new key. Update the `known_hosts` file once you have confirmed the new key is legitimate.

!!! warning "Disabling verification"
    Setting `SSH_STRICT_HOST_KEY_CHECKING=false` skips host key verification entirely. This is **insecure** and should only be used on fully trusted networks where you accept the MITM risk. When disabled, dnsweaver logs a warning on every connection.

### Docker Compose with SSH

Complete example managing a remote router:

```yaml
services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    environment:
      - DNSWEAVER_INSTANCES=router
      - DNSWEAVER_ROUTER_TYPE=dnsmasq
      - DNSWEAVER_ROUTER_CONFIG_DIR=/tmp/dnsmasq.d
      - DNSWEAVER_ROUTER_CONFIG_FILE=dnsweaver.conf
      - DNSWEAVER_ROUTER_RECORD_TYPE=A
      - DNSWEAVER_ROUTER_TARGET=192.0.2.100
      - DNSWEAVER_ROUTER_DOMAINS=*.home.example.com
      - DNSWEAVER_ROUTER_RELOAD_COMMAND=killall -HUP dnsmasq
      - DNSWEAVER_ROUTER_SSH_HOST=192.168.1.1
      - DNSWEAVER_ROUTER_SSH_USER=root
      - DNSWEAVER_ROUTER_SSH_KEY_FILE_FILE=/run/secrets/router_ssh_key
      - DNSWEAVER_ROUTER_SSH_KNOWN_HOSTS_FILE=/ssh/known_hosts
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./ssh_keys/known_hosts:/ssh/known_hosts:ro
    secrets:
      - router_ssh_key

secrets:
  router_ssh_key:
    file: ./ssh_keys/router_id_ed25519
```

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

For routers running dnsmasq (OpenWrt, DD-WRT, etc.), SSH mode is the recommended approach:

### SSH Mode (Recommended)

Use SSH to manage the router's dnsmasq configuration directly:

```yaml
environment:
  - DNSWEAVER_ROUTER_TYPE=dnsmasq
  - DNSWEAVER_ROUTER_CONFIG_DIR=/tmp/dnsmasq.d
  - DNSWEAVER_ROUTER_RELOAD_COMMAND=killall -HUP dnsmasq
  - DNSWEAVER_ROUTER_SSH_HOST=192.168.1.1
  - DNSWEAVER_ROUTER_SSH_USER=root
  - DNSWEAVER_ROUTER_SSH_KEY_FILE=/ssh/router_key
  - DNSWEAVER_ROUTER_SSH_KNOWN_HOSTS_FILE=/ssh/known_hosts
```

See [SSH Remote Management](#ssh-remote-management) for complete configuration details including authentication and secrets.

### Volume Mount (Alternative)

If the router's filesystem is available via NFS or CIFS:

1. Mount the router's dnsmasq config directory
2. Configure dnsweaver to write to that mount
3. Set up a reload mechanism (SSH command, webhook, etc.)

## Troubleshooting

### Records Not Updating

Check the managed config file:

```bash
cat /etc/dnsmasq.d/dnsweaver.conf
```

For SSH mode, check on the **remote host**:

```bash
ssh root@192.168.1.1 "cat /tmp/dnsmasq.d/dnsweaver.conf"
```

### Reload Not Working

Verify the reload command:

```bash
docker exec dnsweaver /bin/sh -c "$RELOAD_COMMAND"
```

For SSH mode, the reload command runs on the remote host. Verify that the command works when run manually via SSH.

### Permission Denied

Ensure dnsweaver can write to the config directory:

```bash
docker exec dnsweaver touch /etc/dnsmasq.d/test.conf
```

For SSH mode, ensure the SSH user has write access to `CONFIG_DIR` on the remote host.

### SSH Connection Refused

Verify SSH connectivity from the dnsweaver container:

- Check that `SSH_HOST` and `SSH_PORT` are correct
- Ensure the SSH service is running on the remote host
- Verify firewall rules allow the connection
- Check logs for authentication errors (wrong key, wrong user)

### SSH Authentication Failed

- **Key file:** Verify the key file is mounted correctly and readable (`chmod 600`)
- **Password:** Confirm the remote host allows password authentication

### Host Key Verification Failed

By default dnsweaver verifies the remote host key against `SSH_KNOWN_HOSTS_FILE`.
A startup error here means one of the following:

- **No `known_hosts` file provided.** Strict checking is on by default. Provide
  `SSH_KNOWN_HOSTS_FILE`, or set `SSH_STRICT_HOST_KEY_CHECKING=false` to opt out
  (insecure).
- **Host key not found in the file.** Add it with
  `ssh-keyscan -t ed25519 <host> >> known_hosts` after confirming the fingerprint
  out-of-band.
- **Host key mismatch.** The remote key changed (rotation, reinstall) or the
  connection is being intercepted. Confirm the new key is legitimate, then update
  the `known_hosts` entry.

### SSH Timeout

If SSH connections are timing out, verify that the remote host is reachable and the SSH service is running. Check firewall rules and network connectivity.
