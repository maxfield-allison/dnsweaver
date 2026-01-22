#!/usr/bin/env bash
#
# dnsweaver integration test runner
# Validates dnsweaver functionality against a test stack
#
# Usage:
#   source /path/to/integration-test.env   # Load environment config
#   ./scripts/integration-test.sh          # Run smoke tests (default)
#   ./scripts/integration-test.sh --full   # Run all tests including deep/lifecycle
#   ./scripts/integration-test.sh --help   # Show usage
#
# All configuration comes from environment variables - no sensitive defaults.
# Create an env file with your infrastructure details (see usage for required vars).
#

set -euo pipefail

# ═══════════════════════════════════════════════════════════════════════════════
# Configuration
# ═══════════════════════════════════════════════════════════════════════════════

# All configuration comes from environment variables (no defaults for sensitive values)
# Source your environment config before running this script, e.g.:
#   source /path/to/integration-test.env && ./integration-test.sh
#
# Required variables:
#   SWARM_VIP           - Docker Swarm VIP address
#   TECHNITIUM_HOST     - Technitium DNS server address
#   TECHNITIUM_TOKEN    - Technitium API token
#   SSH_HOST            - Swarm manager hostname for SSH (or DEPLOY_HOST as fallback)
#   TEST_ZONE           - DNS zone for testing
#
# Optional variables:
#   DNSWEAVER_PORT      - Health endpoint port (default: 8089)

DNSWEAVER_PORT="${DNSWEAVER_PORT:-8089}"

# SSH_HOST can fall back to DEPLOY_HOST (common CI variable)
SSH_HOST="${SSH_HOST:-${DEPLOY_HOST:-}}"

# Validate required environment variables
validate_config() {
    local missing=()

    [[ -z "${SWARM_VIP:-}" ]] && missing+=("SWARM_VIP")
    [[ -z "${TECHNITIUM_HOST:-}" ]] && missing+=("TECHNITIUM_HOST")
    [[ -z "${TECHNITIUM_TOKEN:-}" ]] && missing+=("TECHNITIUM_TOKEN")
    [[ -z "${SSH_HOST:-}" ]] && missing+=("SSH_HOST (or DEPLOY_HOST)")
    [[ -z "${TEST_ZONE:-}" ]] && missing+=("TEST_ZONE")

    if [[ ${#missing[@]} -gt 0 ]]; then
        echo "ERROR: Missing required environment variables:" >&2
        printf '  - %s\n' "${missing[@]}" >&2
        echo "" >&2
        echo "Create an environment config file and source it before running:" >&2
        echo "  source /path/to/integration-test.env && $0" >&2
        exit 1
    fi
}

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test counters
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
TESTS_SKIPPED=0

# ═══════════════════════════════════════════════════════════════════════════════
# Helper Functions
# ═══════════════════════════════════════════════════════════════════════════════

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_pass() { echo -e "${GREEN}[PASS]${NC} $1"; }
log_fail() { echo -e "${RED}[FAIL]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_skip() { echo -e "${YELLOW}[SKIP]${NC} $1"; }

test_pass() {
    ((TESTS_RUN++))
    ((TESTS_PASSED++))
    log_pass "$1"
}

test_fail() {
    ((TESTS_RUN++))
    ((TESTS_FAILED++))
    log_fail "$1"
}

test_skip() {
    ((TESTS_RUN++))
    ((TESTS_SKIPPED++))
    log_skip "$1"
}

# Check if required environment variable is set (used within tests)
require_env() {
    local var_name="$1"
    if [[ -z "${!var_name:-}" ]]; then
        echo "ERROR: Required environment variable $var_name is not set" >&2
        return 1
    fi
}

# Print test header
print_header() {
    echo ""
    echo "══════════════════════════════════════════════════════════════════════"
    echo " $1"
    echo "══════════════════════════════════════════════════════════════════════"
}

# ═══════════════════════════════════════════════════════════════════════════════
# Smoke Tests (fast, always run)
# ═══════════════════════════════════════════════════════════════════════════════

test_health_endpoint() {
    print_header "Test: Health Endpoint"

    local response
    local status

    # Test /health endpoint
    response=$(curl -sf "http://${SWARM_VIP}:${DNSWEAVER_PORT}/health" 2>/dev/null || echo "CURL_FAILED")
    if [[ "$response" == "CURL_FAILED" ]]; then
        test_fail "Health endpoint not responding at http://${SWARM_VIP}:${DNSWEAVER_PORT}/health"
        return 1
    fi

    status=$(echo "$response" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status','unknown'))" 2>/dev/null || echo "parse_error")
    if [[ "$status" == "healthy" ]]; then
        test_pass "Health endpoint returns healthy"
    else
        test_fail "Health endpoint status: $status (expected: healthy)"
        return 1
    fi
}

test_ready_endpoint() {
    print_header "Test: Ready Endpoint"

    local response
    local status
    local component_count

    response=$(curl -sf "http://${SWARM_VIP}:${DNSWEAVER_PORT}/ready" 2>/dev/null || echo "CURL_FAILED")
    if [[ "$response" == "CURL_FAILED" ]]; then
        test_fail "Ready endpoint not responding"
        return 1
    fi

    status=$(echo "$response" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status','unknown'))" 2>/dev/null || echo "parse_error")
    component_count=$(echo "$response" | python3 -c "import sys,json; print(len(json.load(sys.stdin).get('components',[])))" 2>/dev/null || echo "0")

    if [[ "$status" == "ready" ]]; then
        test_pass "Ready endpoint returns ready with $component_count providers"
    elif [[ "$status" == "degraded" ]]; then
        log_warn "Ready endpoint returns degraded (some providers unavailable)"
        test_pass "Ready endpoint responding (degraded state acceptable for smoke test)"
    else
        test_fail "Ready endpoint status: $status (expected: ready or degraded)"
        return 1
    fi
}

test_technitium_record_count() {
    print_header "Test: Technitium Record Count"

    local expected_min=50
    local response
    local count

    require_env "TECHNITIUM_TOKEN"

    response=$(curl -sf "http://${TECHNITIUM_HOST}:5380/api/zones/records/get?token=${TECHNITIUM_TOKEN}&domain=${TEST_ZONE}&zone=${TEST_ZONE}&listZone=true" 2>/dev/null || echo "CURL_FAILED")
    if [[ "$response" == "CURL_FAILED" ]]; then
        test_fail "Technitium API not responding"
        return 1
    fi

    count=$(echo "$response" | python3 -c "import sys,json; print(len(json.load(sys.stdin).get('response',{}).get('records',[])))" 2>/dev/null || echo "0")

    if [[ "$count" -ge "$expected_min" ]]; then
        test_pass "Technitium has $count records (expected ≥${expected_min})"
    else
        test_fail "Technitium has $count records (expected ≥${expected_min})"
        return 1
    fi
}

test_pihole_record_count() {
    print_header "Test: Pi-hole Record Count"

    local expected_min=3
    local node
    local count

    # Find which node runs Pi-hole
    node=$(ssh "$SSH_HOST" "docker service ps dnsweaver-dev_test-pihole --format '{{.Node}}' -f 'desired-state=running' | head -1" 2>/dev/null || echo "")
    if [[ -z "$node" ]]; then
        test_skip "Pi-hole service not running - cannot determine node"
        return 0
    fi

    # Query Pi-hole FTL for DNS hosts
    count=$(ssh "$node" 'docker exec $(docker ps -qf "name=test-pihole" | head -1) pihole-FTL --config dns.hosts 2>/dev/null' | python3 -c "
import sys
import re
line = sys.stdin.read().strip()
# Format: [ ip hostname, ip hostname, ... ]
if line.startswith('[') and line.endswith(']'):
    # Count comma-separated entries
    entries = line[1:-1].split(',')
    print(len([e for e in entries if e.strip()]))
else:
    print(0)
" 2>/dev/null || echo "0")

    if [[ "$count" -ge "$expected_min" ]]; then
        test_pass "Pi-hole has $count host entries (expected ≥${expected_min})"
    else
        test_fail "Pi-hole has $count host entries (expected ≥${expected_min})"
        return 1
    fi
}

test_dnsmasq_record_count() {
    print_header "Test: dnsmasq Record Count"

    local expected_min=2
    local node
    local count

    # Find which node runs dnsmasq
    node=$(ssh "$SSH_HOST" "docker service ps dnsweaver-dev_test-dnsmasq --format '{{.Node}}' -f 'desired-state=running' | head -1" 2>/dev/null || echo "")
    if [[ -z "$node" ]]; then
        test_skip "dnsmasq service not running - cannot determine node"
        return 0
    fi

    # Count address= entries in config file
    count=$(ssh "$node" 'docker exec $(docker ps -qf "name=test-dnsmasq" | head -1) cat /etc/dnsmasq.d/dnsweaver.conf 2>/dev/null' | grep -c "^address=" 2>/dev/null || echo "0")

    if [[ "$count" -ge "$expected_min" ]]; then
        test_pass "dnsmasq has $count address entries (expected ≥${expected_min})"
    else
        test_fail "dnsmasq has $count address entries (expected ≥${expected_min})"
        return 1
    fi
}

test_record_types_present() {
    print_header "Test: Record Types Present"

    require_env "TECHNITIUM_TOKEN"

    local response
    local types

    response=$(curl -sf "http://${TECHNITIUM_HOST}:5380/api/zones/records/get?token=${TECHNITIUM_TOKEN}&domain=${TEST_ZONE}&zone=${TEST_ZONE}&listZone=true" 2>/dev/null || echo "CURL_FAILED")
    if [[ "$response" == "CURL_FAILED" ]]; then
        test_fail "Cannot query Technitium for record types"
        return 1
    fi

    types=$(echo "$response" | python3 -c "
import sys,json
data = json.load(sys.stdin)
records = data.get('response',{}).get('records',[])
types = set(r.get('type','') for r in records)
print(' '.join(sorted(types)))
" 2>/dev/null || echo "")

    local all_found=true

    if echo "$types" | grep -qw "A"; then
        test_pass "A records present"
    else
        test_fail "A records missing"
        all_found=false
    fi

    if echo "$types" | grep -qw "AAAA"; then
        test_pass "AAAA records present"
    else
        test_fail "AAAA records missing"
        all_found=false
    fi

    if echo "$types" | grep -qw "CNAME"; then
        test_pass "CNAME records present"
    else
        test_fail "CNAME records missing"
        all_found=false
    fi

    if echo "$types" | grep -qw "SRV"; then
        test_pass "SRV records present"
    else
        test_fail "SRV records missing"
        all_found=false
    fi

    if echo "$types" | grep -qw "TXT"; then
        test_pass "TXT (ownership) records present"
    else
        test_fail "TXT records missing"
        all_found=false
    fi

    $all_found || return 1
}

test_disabled_service() {
    print_header "Test: Disabled Service (dnsweaver.enabled=false)"

    require_env "TECHNITIUM_TOKEN"

    local response
    local count

    # Query for disabled.<TEST_ZONE> - should NOT exist
    response=$(curl -sf "http://${TECHNITIUM_HOST}:5380/api/zones/records/get?token=${TECHNITIUM_TOKEN}&domain=disabled.${TEST_ZONE}&zone=${TEST_ZONE}" 2>/dev/null || echo "CURL_FAILED")
    if [[ "$response" == "CURL_FAILED" ]]; then
        test_fail "Cannot query Technitium for disabled record"
        return 1
    fi

    count=$(echo "$response" | python3 -c "import sys,json; print(len(json.load(sys.stdin).get('response',{}).get('records',[])))" 2>/dev/null || echo "0")

    if [[ "$count" -eq 0 ]]; then
        test_pass "Disabled service has no DNS record (expected)"
    else
        test_fail "Disabled service has $count records (expected 0)"
        return 1
    fi
}

# ═══════════════════════════════════════════════════════════════════════════════
# Deep Tests (slower, run on tag/nightly)
# ═══════════════════════════════════════════════════════════════════════════════

test_orphan_cleanup() {
    print_header "Test: Orphan Cleanup"

    require_env "TECHNITIUM_TOKEN"

    local service_name="dnsweaver-dev_test-orphan-integration"
    local hostname="orphan-integration-test.${TEST_ZONE}"
    local response
    local count

    log_info "Creating temporary test service..."
    ssh "$SSH_HOST" "docker service create --name $service_name \
        --label 'dnsweaver.hostname=$hostname' \
        --network dnsweaver-dev_default \
        --replicas 1 \
        traefik/whoami:latest" >/dev/null 2>&1 || true

    log_info "Waiting 60s for record creation..."
    sleep 60

    # Verify record was created
    response=$(curl -sf "http://${TECHNITIUM_HOST}:5380/api/zones/records/get?token=${TECHNITIUM_TOKEN}&domain=${hostname}&zone=${TEST_ZONE}" 2>/dev/null || echo "CURL_FAILED")
    count=$(echo "$response" | python3 -c "import sys,json; print(len(json.load(sys.stdin).get('response',{}).get('records',[])))" 2>/dev/null || echo "0")

    if [[ "$count" -eq 0 ]]; then
        test_fail "Orphan test: Record was not created"
        ssh "$SSH_HOST" "docker service rm $service_name" >/dev/null 2>&1 || true
        return 1
    fi

    log_info "Record created. Removing service..."
    ssh "$SSH_HOST" "docker service rm $service_name" >/dev/null 2>&1

    log_info "Waiting 45s for orphan cleanup (30s delay + buffer)..."
    sleep 45

    # Verify record was deleted
    response=$(curl -sf "http://${TECHNITIUM_HOST}:5380/api/zones/records/get?token=${TECHNITIUM_TOKEN}&domain=${hostname}&zone=${TEST_ZONE}" 2>/dev/null || echo "CURL_FAILED")
    count=$(echo "$response" | python3 -c "import sys,json; print(len(json.load(sys.stdin).get('response',{}).get('records',[])))" 2>/dev/null || echo "0")

    if [[ "$count" -eq 0 ]]; then
        test_pass "Orphan cleanup: Record deleted after service removal"
    else
        test_fail "Orphan cleanup: Record still exists after service removal"
        # Clean up manually
        curl -sf "http://${TECHNITIUM_HOST}:5380/api/zones/records/delete?token=${TECHNITIUM_TOKEN}&domain=${hostname}&zone=${TEST_ZONE}&type=A" >/dev/null 2>&1 || true
        return 1
    fi
}

test_graceful_degradation() {
    print_header "Test: Graceful Provider Recovery"

    local initial_status
    local degraded_status
    local recovered_status

    log_info "Checking initial health status..."
    initial_status=$(curl -sf "http://${SWARM_VIP}:${DNSWEAVER_PORT}/ready" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status','unknown'))" 2>/dev/null || echo "error")

    if [[ "$initial_status" != "ready" ]]; then
        test_skip "Initial status is not ready ($initial_status), skipping degradation test"
        return 0
    fi

    log_info "Scaling down Pi-hole to simulate provider failure..."
    ssh "$SSH_HOST" "docker service scale dnsweaver-dev_test-pihole=0" >/dev/null 2>&1

    log_info "Forcing dnsweaver restart..."
    ssh "$SSH_HOST" "docker service update --force dnsweaver-dev_dnsweaver" >/dev/null 2>&1

    log_info "Waiting 30s for restart and degraded state..."
    sleep 30

    degraded_status=$(curl -sf "http://${SWARM_VIP}:${DNSWEAVER_PORT}/ready" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status','unknown'))" 2>/dev/null || echo "error")

    if [[ "$degraded_status" == "degraded" || "$degraded_status" == "ready" ]]; then
        test_pass "dnsweaver started in degraded/ready state while Pi-hole down"
    else
        test_fail "dnsweaver status was '$degraded_status' (expected degraded or ready)"
        # Restore Pi-hole
        ssh "$SSH_HOST" "docker service scale dnsweaver-dev_test-pihole=1" >/dev/null 2>&1
        return 1
    fi

    log_info "Scaling Pi-hole back up..."
    ssh "$SSH_HOST" "docker service scale dnsweaver-dev_test-pihole=1" >/dev/null 2>&1

    log_info "Waiting 90s for provider recovery..."
    sleep 90

    recovered_status=$(curl -sf "http://${SWARM_VIP}:${DNSWEAVER_PORT}/ready" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status','unknown'))" 2>/dev/null || echo "error")

    if [[ "$recovered_status" == "ready" ]]; then
        test_pass "dnsweaver recovered to ready state after Pi-hole restored"
    else
        test_fail "dnsweaver status is '$recovered_status' after Pi-hole restored (expected ready)"
        return 1
    fi
}

# ═══════════════════════════════════════════════════════════════════════════════
# Main
# ═══════════════════════════════════════════════════════════════════════════════

run_smoke_tests() {
    print_header "SMOKE TESTS"

    test_health_endpoint || true
    test_ready_endpoint || true
    test_technitium_record_count || true
    test_pihole_record_count || true
    test_dnsmasq_record_count || true
    test_record_types_present || true
    test_disabled_service || true
}

run_deep_tests() {
    print_header "DEEP TESTS"

    test_orphan_cleanup || true
    test_graceful_degradation || true
}

print_summary() {
    print_header "TEST SUMMARY"

    echo ""
    echo "  Total:   $TESTS_RUN"
    echo -e "  ${GREEN}Passed:  $TESTS_PASSED${NC}"
    echo -e "  ${RED}Failed:  $TESTS_FAILED${NC}"
    echo -e "  ${YELLOW}Skipped: $TESTS_SKIPPED${NC}"
    echo ""

    if [[ "$TESTS_FAILED" -gt 0 ]]; then
        echo -e "${RED}RESULT: FAILED${NC}"
        return 1
    else
        echo -e "${GREEN}RESULT: PASSED${NC}"
        return 0
    fi
}

usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --smoke       Run smoke tests only (default)"
    echo "  --full        Run all tests including deep/lifecycle tests"
    echo "  --deep        Run deep tests only"
    echo "  --help        Show this help"
    echo ""
    echo "Required environment variables:"
    echo "  SWARM_VIP         - Docker Swarm VIP address"
    echo "  TECHNITIUM_HOST   - Technitium DNS server address"
    echo "  TECHNITIUM_TOKEN  - Technitium API token"
    echo "  SSH_HOST          - Swarm manager hostname for SSH"
    echo "  TEST_ZONE         - DNS zone for testing"
    echo ""
    echo "Optional:"
    echo "  DNSWEAVER_PORT    - Health endpoint port (default: 8089)"
    echo ""
    echo "Example:"
    echo "  source /path/to/integration-test.env && $0 --smoke"
}

main() {
    local mode="smoke"

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --smoke) mode="smoke"; shift ;;
            --full) mode="full"; shift ;;
            --deep) mode="deep"; shift ;;
            --help|-h) usage; exit 0 ;;
            *) echo "Unknown option: $1"; usage; exit 1 ;;
        esac
    done

    # Validate all required config is present
    validate_config

    echo "═══════════════════════════════════════════════════════════════════════"
    echo " dnsweaver Integration Test Suite"
    echo " Mode: $mode"
    echo " Target: http://${SWARM_VIP}:${DNSWEAVER_PORT}"
    echo "═══════════════════════════════════════════════════════════════════════"

    case "$mode" in
        smoke)
            run_smoke_tests
            ;;
        full)
            run_smoke_tests
            run_deep_tests
            ;;
        deep)
            run_deep_tests
            ;;
    esac

    print_summary
}

main "$@"
