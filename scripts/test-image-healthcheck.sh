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

# Copy the fixture through the Docker API so this works when the Docker daemon
# runs outside the CI job container (for example, through a mounted host socket).

# YAML selected through the CLI must drive both the server and image healthcheck.
docker create \
    --name "$container_yaml" \
    "$image" --config /tmp/dnsweaver-config.yml >/dev/null
docker cp "$config_path" "$container_yaml:/tmp/dnsweaver-config.yml"
docker start "$container_yaml" >/dev/null
wait_for_healthy "$container_yaml"
docker exec \
    --env DNSWEAVER_HEALTH_PORT=18080 \
    "$container_yaml" /usr/local/bin/dnsweaver --healthcheck

# An environment override must take precedence over the YAML port for both.
docker create \
    --name "$container_env" \
    --env DNSWEAVER_CONFIG=/tmp/dnsweaver-config.yml \
    --env DNSWEAVER_HEALTH_PORT=18081 \
    "$image" >/dev/null
docker cp "$config_path" "$container_env:/tmp/dnsweaver-config.yml"
docker start "$container_env" >/dev/null
wait_for_healthy "$container_env"
docker exec \
    --env DNSWEAVER_HEALTH_PORT=18081 \
    "$container_env" /usr/local/bin/dnsweaver --healthcheck
