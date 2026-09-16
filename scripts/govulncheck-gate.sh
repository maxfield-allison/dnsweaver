#!/bin/sh
# CI invokes this executable wrapper directly. Run the pinned govulncheck in
# structured mode, retain its evidence, and apply
# only current exceptions that have an explicit maintainer decision.
set -eu

scanner=${GOVULNCHECK_BIN:-govulncheck}
go_bin=${GO_BIN:-go}
timeout_duration=${GOVULNCHECK_TIMEOUT:-5m}
artifact_dir=${GOVULNCHECK_ARTIFACT_DIR:-.artifacts/govulncheck}
exceptions=${GOVULNCHECK_EXCEPTIONS:-security/govulncheck-exceptions.json}
expected_version=${GOVULNCHECK_VERSION:-v1.8.0}
raw_report="$artifact_dir/raw.json"
normalized_report="$artifact_dir/decision.json"
stderr_report="$artifact_dir/stderr.txt"

mkdir -p "$artifact_dir"
if ! command -v "$scanner" >/dev/null 2>&1; then
    echo "govulncheck gate: scanner not found: $scanner" | tee "$stderr_report" >&2
    exit 1
fi
if ! command -v "$go_bin" >/dev/null 2>&1; then
    echo "govulncheck gate: Go tool not found: $go_bin" | tee "$stderr_report" >&2
    exit 1
fi

raw_temp=$(mktemp "$artifact_dir/.raw.XXXXXX")
stderr_temp=$(mktemp "$artifact_dir/.stderr.XXXXXX")
trap 'rm -f "$raw_temp" "$stderr_temp"' EXIT HUP INT TERM

scan_rc=0
timeout -s TERM "$timeout_duration" "$scanner" -json ./... >"$raw_temp" 2>"$stderr_temp" || scan_rc=$?
mv "$raw_temp" "$raw_report"
mv "$stderr_temp" "$stderr_report"
if [ "$scan_rc" -ne 0 ]; then
    cat "$stderr_report" >&2
    echo "govulncheck gate: scanner failed or timed out (exit $scan_rc)" >&2
    exit 1
fi

"$go_bin" run ./cmd/govulncheck-gate \
    --input "$raw_report" \
    --exceptions "$exceptions" \
    --output "$normalized_report" \
    --expected-scanner-version "$expected_version"
