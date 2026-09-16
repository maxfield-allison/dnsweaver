#!/bin/sh
set -eu

# Bound package-loader workers on shared runners. Keep the same five-minute
# limit; verbose timings distinguish loading stalls from actual lint findings.
export GOMAXPROCS=${GOMAXPROCS:-2}
mkdir -p .artifacts/lint
go version > .artifacts/lint/environment.txt
go env GOOS GOARCH GOFLAGS GOCACHE GOPATH >> .artifacts/lint/environment.txt
cat /proc/loadavg >> .artifacts/lint/environment.txt
golangci-lint run --config .golangci.yml --timeout 5m --concurrency "$GOMAXPROCS" --verbose > .artifacts/lint/output.txt 2>&1 || {
    status=$?
    cat .artifacts/lint/output.txt
    exit "$status"
}
cat .artifacts/lint/output.txt
