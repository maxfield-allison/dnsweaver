#!/bin/sh
# CI and local verification invoke this executable fixture matrix directly.
set -eu

root=$(git rev-parse --show-toplevel)
temp=$(mktemp -d)
trap 'rm -rf "$temp"' EXIT HUP INT TERM
go_bin=${GO_BIN:-/usr/local/go/bin/go}

mkdir -p "$temp/bin"
cat >"$temp/bin/govulncheck-fixture" <<'EOF'
#!/bin/sh
config='{"config":{"protocol_version":"v1.0.0","scanner_name":"govulncheck","scanner_version":"v1.8.0","db":"https://vuln.go.dev","db_last_modified":"2026-09-15T18:39:25Z","go_version":"go1.26.8","scan_level":"symbol","scan_mode":"source"}}'
sbom='{"SBOM":{"go_version":"go1.26.8","modules":[{"path":"example.test/app"},{"path":"example.test/dependency","version":"v1.2.3"}],"roots":["example.test/app"]}}'
osv='{"osv":{"id":"GO-2099-0001"}}'
finding='{"finding":{"osv":"GO-2099-0001","trace":[{"module":"example.test/dependency","version":"v1.2.3","package":"example.test/dependency/client","function":"Open"}]}}'
case "${GATE_CASE:?}" in
    clean) printf '%s\n%s\n' "$config" "$sbom"; exit 0 ;;
    proposed) printf '%s\n%s\n%s\n%s\n' "$config" "$sbom" "$osv" "$finding"; exit 0 ;;
    malformed) printf '%s\n%s\n{' "$config" "$sbom"; exit 0 ;;
    failure-with-finding) printf '%s\n%s\n%s\n%s\n' "$config" "$sbom" "$osv" "$finding"; echo 'database unavailable' >&2; exit 1 ;;
esac
EOF
chmod 755 "$temp/bin/govulncheck-fixture"

run_case() {
    case_name=$1
    expected=$2
    result=pass
    (
        cd "$root"
        GATE_CASE="$case_name" \
        GOVULNCHECK_BIN="$temp/bin/govulncheck-fixture" \
        GOVULNCHECK_ARTIFACT_DIR="$temp/$case_name" \
        GO_BIN="$go_bin" \
        sh scripts/govulncheck-gate.sh
    ) >"$temp/$case_name.log" 2>&1 || result=fail
    if [ "$result" != "$expected" ]; then
        echo "$case_name: got $result, want $expected" >&2
        sed -n '1,120p' "$temp/$case_name.log" >&2
        exit 1
    fi
    printf '%-24s %s\n' "$case_name" "$result"
}

run_case clean pass
run_case proposed fail
run_case malformed fail
run_case failure-with-finding fail

missing=pass
(
    cd "$root"
    GOVULNCHECK_BIN="$temp/bin/missing" \
    GOVULNCHECK_ARTIFACT_DIR="$temp/missing" \
    GO_BIN="$go_bin" \
    sh scripts/govulncheck-gate.sh
) >"$temp/missing.log" 2>&1 || missing=fail
[ "$missing" = fail ] || { echo "missing tool passed" >&2; exit 1; }
printf '%-24s %s\n' missing-tool "$missing"
