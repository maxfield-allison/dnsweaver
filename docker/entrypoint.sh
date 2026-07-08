#!/bin/sh
# =============================================================================
# dnsweaver - Container entrypoint
# =============================================================================
#
# Handles Docker socket GID auto-detection so the standard compose example
# works out of the box. Without this, users have to look up their host's
# docker group GID and add it via `group_add` in compose, which is friction
# we don't need.
#
# Behavior:
#   - If /var/run/docker.sock is mounted, read its GID and add the dnsweaver
#     user to a group with that GID (creating the group if needed).
#   - GID 0 (root-owned socket) is NOT granted automatically — that would give
#     the dnsweaver user root-group access without the operator asking for it.
#     Platforms that force a root-owned socket (e.g. Synology Container Manager)
#     should use a socket proxy (recommended) or opt in with DNSWEAVER_DOCKER_GID.
#   - If no socket is mounted (k8s-only mode, socket proxy, etc.), skip the
#     logic entirely and just drop privileges.
#   - Always exec the binary as the unprivileged dnsweaver user via su-exec.
#
# Escape hatch (DNSWEAVER_DOCKER_GID): explicitly grant the dnsweaver user
# membership in a specific GID. Intended for platforms with a root-owned socket
# that can't be chgrp'd (set DNSWEAVER_DOCKER_GID=0). The process still drops to
# the unprivileged dnsweaver user via su-exec — only a supplementary group is
# added, the application never runs as root.
# =============================================================================

set -e

DOCKER_SOCK="${DOCKER_SOCK:-/var/run/docker.sock}"

# add_to_gid GID
# Ensure the dnsweaver user is a member (in /etc/group) of a group with the
# given GID, creating the group if one doesn't already exist. Writing to
# /etc/group is what makes the membership survive the su-exec privilege drop:
# su-exec calls initgroups(), which rebuilds supplementary groups from
# /etc/group rather than inheriting the container's initial (root) groups.
add_to_gid() {
    _gid="$1"
    [ -z "$_gid" ] && return 0

    # Already a member? Nothing to do.
    if id -G dnsweaver | tr ' ' '\n' | grep -qx "$_gid"; then
        return 0
    fi

    _group_name=$(getent group "$_gid" | cut -d: -f1)
    if [ -z "$_group_name" ]; then
        _group_name="docker-host-$_gid"
        addgroup -g "$_gid" "$_group_name" 2>/dev/null || true
    fi
    # Add dnsweaver to the group (idempotent; ignore failure).
    addgroup dnsweaver "$_group_name" 2>/dev/null || true
}

# 1. Auto-detect the mounted socket's GID (standard low-friction path).
if [ -S "$DOCKER_SOCK" ]; then
    SOCK_GID=$(stat -c '%g' "$DOCKER_SOCK")

    if [ "$SOCK_GID" != "0" ]; then
        add_to_gid "$SOCK_GID"
    elif [ -z "$DNSWEAVER_DOCKER_GID" ]; then
        # Root-owned socket and no explicit opt-in: warn instead of silently
        # granting root-group access.
        echo "dnsweaver: docker socket $DOCKER_SOCK is owned by GID 0 (root)." >&2
        echo "dnsweaver: not granting root-group access automatically." >&2
        echo "dnsweaver: on platforms that force a root-owned socket (e.g. Synology)," >&2
        echo "dnsweaver: use a socket proxy (recommended) or set DNSWEAVER_DOCKER_GID=0" >&2
        echo "dnsweaver: to opt in. See docs/sources/docker.md." >&2
    fi
fi

# 2. Explicit GID opt-in (escape hatch for root-owned or otherwise
#    undetectable sockets). The process still drops privileges below.
if [ -n "$DNSWEAVER_DOCKER_GID" ]; then
    if [ "$DNSWEAVER_DOCKER_GID" = "0" ]; then
        echo "dnsweaver: WARNING DNSWEAVER_DOCKER_GID=0 — granting the dnsweaver user" >&2
        echo "dnsweaver: membership in the root group so it can read a root-owned docker" >&2
        echo "dnsweaver: socket. The process still runs unprivileged (uid 1000), but a" >&2
        echo "dnsweaver: socket proxy gives stronger isolation. See docs/sources/docker.md." >&2
    fi
    add_to_gid "$DNSWEAVER_DOCKER_GID"
fi

# Note: use `dnsweaver` (not `dnsweaver:dnsweaver`) so su-exec picks up
# supplementary groups from /etc/group. Specifying an explicit group resets
# supplementary groups to the empty set, which would undo the group
# membership we just added.
exec su-exec dnsweaver /usr/local/bin/dnsweaver "$@"
