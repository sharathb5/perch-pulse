#!/usr/bin/env bash
# Live detector evaluation harness (not part of make verify / CI).
# baseline → scenario → detect → recover → evaluate against ground truth.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
BASE_URL="${ASTRONOMY_BASE_URL:-http://127.0.0.1:8080}"
PROM_URL="${PERCH_PROM_URL:-http://127.0.0.1:9090}"
DOCKER_TIMEOUT_SECS="${ASTRONOMY_DOCKER_TIMEOUT_SECS:-15}"

fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "OK: $*"; }

avail_kb="$(df -k / | awk 'NR==2{print $4}')"
if [[ -n "$avail_kb" && "$avail_kb" -lt 15728640 ]]; then
  fail "less than 15 GiB free on / (avail_kb=$avail_kb); hard stop"
fi
ok "disk free ~$((avail_kb / 1048576)) GiB"

if ! command -v docker >/dev/null 2>&1; then
  fail "docker not installed"
fi
if ! timeout "$DOCKER_TIMEOUT_SECS" docker info >/dev/null 2>&1; then
  fail "Docker daemon not responding within ${DOCKER_TIMEOUT_SECS}s"
fi
ok "docker responding"

curl -sf --max-time 5 "$BASE_URL/" >/dev/null || fail "Astronomy Shop frontend not reachable at $BASE_URL/"
ok "frontend reachable"
curl -sf --max-time 5 "$PROM_URL/-/healthy" >/dev/null || fail "Prometheus not healthy at $PROM_URL"
ok "prometheus healthy"
curl -sf --max-time 5 "$BASE_URL/feature/api/read" >/dev/null || fail "flagd-ui API not reachable"
ok "flagd-ui reachable"

export PERCH_PROM_URL="$PROM_URL"
export PERCH_FLAGD_API_BASE="${PERCH_FLAGD_API_BASE:-$BASE_URL/feature/api}"
# Demo-friendly durations (override via env). Total ~4 scenarios * ~4 min.
export PERCH_BASELINE_SECS="${PERCH_BASELINE_SECS:-90}"
# Fault window must cover Prom 2m rate() + sparse intl shipping hits for p99.
export PERCH_FAULT_SECS="${PERCH_FAULT_SECS:-120}"
export PERCH_RECOVER_SECS="${PERCH_RECOVER_SECS:-90}"
export PERCH_POLL_SECS="${PERCH_POLL_SECS:-10}"

cd "$ROOT"
exec go run ./examples/astronomy-shop/eval/cmd/detector-eval
