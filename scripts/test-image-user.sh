#!/bin/sh

set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
image=${1:?usage: test-image-user.sh IMAGE}
prefix="dnsweaver-user-${DNSWEAVER_TEST_ID:-${CI_JOB_ID:-local}}-$$"
created=""

cleanup() {
    for container in $created; do
        docker rm -f "$container" >/dev/null 2>&1 || true
    done
}
trap cleanup EXIT INT TERM

check_identity() {
    suffix=$1
    uid=$2
    gid=$3
    supplementary=$4
    shift 4
    container="$prefix-$suffix"
    docker create --name "$container" --network none "$@" \
        "$image" --config /tmp/probe.yml >/dev/null
    created="$created $container"
    docker cp "$repo_root/testdata/image-healthcheck-config.yml" "$container:/tmp/probe.yml"
    docker start "$container" >/dev/null

    attempts=0
    until docker exec "$container" dnsweaver --config /tmp/probe.yml --healthcheck >/dev/null 2>&1; do
        attempts=$((attempts + 1))
        if [ "$attempts" -ge 30 ]; then
            docker logs "$container"
            return 1
        fi
        sleep 1
    done

    status=$(docker exec "$container" cat /proc/1/status)
    actual_uid=$(printf '%s\n' "$status" | awk '/^Uid:/ {print $2, $3, $4, $5}')
    actual_gid=$(printf '%s\n' "$status" | awk '/^Gid:/ {print $2, $3, $4, $5}')
    [ "$actual_uid" = "$uid $uid $uid $uid" ]
    [ "$actual_gid" = "$gid $gid $gid $gid" ]
    if [ -n "$supplementary" ]; then
        printf '%s\n' "$status" | awk -v group="$supplementary" \
            '/^Groups:/ {for (i = 2; i <= NF; i++) if ($i == group) found = 1} END {exit !found}'
        # DNSWEAVER_DOCKER_GID must not grant a root group to an explicit user.
        printf '%s\n' "$status" | awk \
            '/^Groups:/ {for (i = 2; i <= NF; i++) if ($i == 0) exit 1}'
    fi
    [ "$(docker exec "$container" cat /proc/1/comm)" = "dnsweaver" ]
    printf '%s: uid=%s gid=%s supplementary=%s\n' "$suffix" "$uid" "$gid" "$supplementary"
    docker stop --time 5 "$container" >/dev/null
    [ "$(docker inspect --format '{{.State.ExitCode}}' "$container")" = "0" ]
}

check_identity default 1000 1000 ""
check_identity custom 12345 23456 34567 \
    --user 12345:23456 --group-add 34567 --env DNSWEAVER_DOCKER_GID=0

# An explicit user must also work without a writable /etc/passwd or /etc/group.
docker run --rm --network none --read-only --user 12345:23456 --group-add 34567 \
    "$image" --version | grep '^dnsweaver '
