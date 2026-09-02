#!/bin/sh

set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
image=dnsweaver:healthcheck-test
if [ "$#" -gt 0 ]; then
    image=$1
fi
config_path="$repo_root/testdata/image-healthcheck-config.yml"
container_prefix="dnsweaver-healthcheck-$$"
container_yaml="$container_prefix-yaml"
container_env="$container_prefix-env"

cleanup() {
    docker rm -f "$container_yaml" "$container_env" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

wait_for_healthy() {
    container_name=$1
    attempts=0

    while [ "$attempts" -lt 30 ]; do
        status=$(docker inspect --format '{{.State.Health.Status}}' "$container_name")
        case "$status" in
            healthy)
                return 0
                ;;
            unhealthy)
                docker inspect --format '{{json .State.Health.Log}}' "$container_name"
                docker logs "$container_name"
                return 1
                ;;
        esac

        attempts=$((attempts + 1))
        sleep 2
    done

    docker inspect --format '{{json .State.Health}}' "$container_name"
    docker logs "$container_name"
    return 1
}

# YAML selected through the CLI must drive both the server and image healthcheck.
docker run --detach \
    --name "$container_yaml" \
    --mount "type=bind,source=$config_path,target=/etc/dnsweaver/config.yml,readonly" \
    "$image" --config /etc/dnsweaver/config.yml >/dev/null
wait_for_healthy "$container_yaml"
docker exec \
    --env DNSWEAVER_HEALTH_PORT=18080 \
    "$container_yaml" /usr/local/bin/dnsweaver --healthcheck

# An environment override must take precedence over the YAML port for both.
docker run --detach \
    --name "$container_env" \
    --mount "type=bind,source=$config_path,target=/etc/dnsweaver/config.yml,readonly" \
    --env DNSWEAVER_CONFIG=/etc/dnsweaver/config.yml \
    --env DNSWEAVER_HEALTH_PORT=18081 \
    "$image" >/dev/null
wait_for_healthy "$container_env"
docker exec \
    --env DNSWEAVER_HEALTH_PORT=18081 \
    "$container_env" /usr/local/bin/dnsweaver --healthcheck
