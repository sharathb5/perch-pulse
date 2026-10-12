#!/usr/bin/env bash
# LIVE FAULT LIFECYCLE — workflow_dispatch only. Not a PR required check.
#
# Starts Astronomy Shop (minimal) + perch viz with Live Pulse enabled against
# loopback Prometheus, then runs cmd/live-fault-smoke:
#   baseline → latency-shipping-intl fault → active incident → recover → historical
#
# Does NOT inject synthetic Prometheus fixtures. Does NOT claim success on
# partial evidence. Guarantees flagd fault cleanup on exit.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
SHOP="$ROOT/examples/astronomy-shop"
DEST="${PERCH_ASTRONOMY_DEMO_DIR:-$SHOP/.demo}"
BASE_URL="${ASTRONOMY_BASE_URL:-http://127.0.0.1:8080}"
PIN="${ASTRONOMY_DEMO_PIN:-3.1.0}"
MIN_DISK_KB="${ASTRONOMY_MIN_DISK_KB:-8388608}"
MIN_MEM_KB="${ASTRONOMY_MIN_MEM_KB:-3145728}"
READY_SECS="${ASTRONOMY_VERIFY_WAIT_SECS:-300}"
JOB_DEADLINE_SECS="${ASTRONOMY_LIVE_FAULT_DEADLINE_SECS:-2700}"
ARTIFACT_DIR="${ASTRONOMY_SMOKE_ARTIFACT_DIR:-$SHOP/.smoke-artifacts/live-fault}"
PERCH_BIN="${PERCH_BIN:-}"
PROM_URL="${PERCH_LIVE_PULSE_PROM_URL:-http://127.0.0.1:9090}"
VIZ_PORT="${ASTRONOMY_SMOKE_VIZ_PORT:-18765}"
PULSE_DIR="${PERCH_PULSE_DIR:-$ARTIFACT_DIR/pulse-dir}"

fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "OK: $*"; }
warn() { echo "WARN: $*" >&2; }
note() { echo "NOTE: $*"; }

mkdir -p "$ARTIFACT_DIR" "$PULSE_DIR"
START_TS=$SECONDS
VIZ_PID=""

cleanup() {
  local code=$?
  set +e
  echo "---- live-fault cleanup (exit=$code) ----"
  # Best-effort flagd reset even if smoke binary crashed mid-fault.
  # Match harness dirs/URLs used by live-fault-smoke so stop finds the active record.
  export PERCH_SCENARIO_RESULTS_DIR="${PERCH_SCENARIO_RESULTS_DIR:-$ARTIFACT_DIR/scenario-results}"
  export PERCH_FLAGD_API_BASE="${ASTRONOMY_FLAGD_API_BASE:-$BASE_URL/feature/api}"
  (cd "$ROOT" && go run ./examples/astronomy-shop/scenarios/cmd/scenario stop) \
    >"$ARTIFACT_DIR/flagd-cleanup.txt" 2>&1 || warn "flagd cleanup stop returned nonzero (see flagd-cleanup.txt)"
  if [[ -n "${VIZ_PID}" ]]; then
    kill "$VIZ_PID" 2>/dev/null || true
    wait "$VIZ_PID" 2>/dev/null || true
    VIZ_PID=""
  fi
  if [[ -x "$SHOP/scripts/stop.sh" ]]; then
    timeout 180 "$SHOP/scripts/stop.sh" || warn "stop.sh returned nonzero"
  fi
  docker ps --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}' >"$ARTIFACT_DIR/docker-ps.txt" 2>&1 || true
  df -h / >"$ARTIFACT_DIR/df.txt" 2>&1 || true
  free -m >"$ARTIFACT_DIR/free.txt" 2>&1 || true
  echo "elapsed_secs=$((SECONDS - START_TS))" | tee "$ARTIFACT_DIR/timing.txt"
  exit "$code"
}
trap cleanup EXIT

(( SECONDS <= JOB_DEADLINE_SECS )) || fail "job deadline exceeded before start"

echo "==== Live fault lifecycle preflight ===="
echo "pin=$PIN dest=$DEST prom=$PROM_URL pulse_dir=$PULSE_DIR"
command -v docker >/dev/null || fail "docker not installed"
command -v curl >/dev/null || fail "curl not installed"
command -v go >/dev/null || fail "go not installed"
timeout 20 docker info >/dev/null 2>&1 || fail "Docker daemon not responding"

avail_kb="$(df -k / | awk 'NR==2{print $4}')"
mem_kb="$(awk '/MemAvailable:/ {print $2}' /proc/meminfo 2>/dev/null || echo 0)"
echo "disk_avail_kb=$avail_kb mem_available_kb=$mem_kb" | tee "$ARTIFACT_DIR/resources.txt"
[[ -n "$avail_kb" && "$avail_kb" -ge "$MIN_DISK_KB" ]] || fail "insufficient disk (avail_kb=$avail_kb)"
[[ -z "$mem_kb" || "$mem_kb" -eq 0 || "$mem_kb" -ge "$MIN_MEM_KB" ]] || fail "insufficient MemAvailable (mem_kb=$mem_kb)"

echo "==== clone + start minimal stack ===="
"$SHOP/scripts/clone.sh" | tee "$ARTIFACT_DIR/clone.log"
export ASTRONOMY_MODE=minimal
"$SHOP/scripts/start.sh" | tee "$ARTIFACT_DIR/start.log"
export ASTRONOMY_VERIFY_WAIT_SECS="$READY_SECS"
"$SHOP/scripts/verify-live.sh" | tee "$ARTIFACT_DIR/verify-live.log"

# Confirm Prometheus before enabling Live Pulse.
prom_ok=0
for u in "$PROM_URL/-/ready" "$PROM_URL/api/v1/status/runtimeinfo" "$BASE_URL/grafana/api/health"; do
  if curl -sf --max-time 5 "$u" >"$ARTIFACT_DIR/prom-probe.json" 2>/dev/null; then
    echo "prom_probe=$u" | tee -a "$ARTIFACT_DIR/telemetry.txt"
    prom_ok=1
    break
  fi
done
[[ "$prom_ok" -eq 1 ]] || fail "Prometheus/observability not reachable at $PROM_URL — cannot run live fault"

cd "$SHOP"
if [[ -z "$PERCH_BIN" || ! -x "$PERCH_BIN" ]]; then
  PERCH_BIN="$ARTIFACT_DIR/perch"
  (cd "$ROOT" && make web-build-embed && go build -o "$PERCH_BIN" ./cmd/perch)
fi

echo "==== start viz with Live Pulse ===="
VIZ_LOG="$ARTIFACT_DIR/viz.log"
export PERCH_LIVE_PULSE_ENABLED=1
export PERCH_LIVE_PULSE_PROM_URL="$PROM_URL"
export PERCH_LIVE_PULSE_ENVIRONMENT=local
export PERCH_LIVE_PULSE_POLL_INTERVAL="${PERCH_LIVE_PULSE_POLL_INTERVAL:-15s}"
export PERCH_PULSE_DIR="$PULSE_DIR"
# Keep Run Pulse independent / available for regression checks.
export PERCH_RUN_PULSE_DEMO_BASE_URL="${PERCH_RUN_PULSE_DEMO_BASE_URL:-$BASE_URL}"
export PERCH_RUN_PULSE_JAEGER_BASE="${PERCH_RUN_PULSE_JAEGER_BASE:-${ASTRONOMY_JAEGER_UI_BASE:-$BASE_URL/jaeger/ui}}"

env | grep -E '^(PERCH_LIVE_PULSE_|PERCH_PULSE_DIR|PERCH_RUN_PULSE_)' | grep -viE 'token|secret|password|key' \
  | tee "$ARTIFACT_DIR/live-pulse-env.txt"

"$PERCH_BIN" viz --env local --port "$VIZ_PORT" >"$VIZ_LOG" 2>&1 &
VIZ_PID=$!
viz_ok=0
for _ in $(seq 1 60); do
  if curl -sf --max-time 2 "http://127.0.0.1:${VIZ_PORT}/api/pulse/live/status" >"$ARTIFACT_DIR/api-live-status-boot.json"; then
    viz_ok=1
    break
  fi
  sleep 0.5
done
[[ "$viz_ok" -eq 1 ]] || fail "Live Pulse status endpoint not ready (see $VIZ_LOG)"
grep -q '"enabled": true\|"enabled":true' "$ARTIFACT_DIR/api-live-status-boot.json" \
  || fail "Live Pulse not enabled in status response"
ok "viz Live Pulse endpoints responding"

# Bounded ambient traffic while baseline warms (load-generator already in demo).
note "allowing demo load-generator ambient traffic during baseline; no custom load script"

SMOKE_BIN="$ARTIFACT_DIR/live-fault-smoke"
(cd "$ROOT" && go build -o "$SMOKE_BIN" ./examples/astronomy-shop/cmd/live-fault-smoke)

"$SMOKE_BIN" \
  --viz-base-url "http://127.0.0.1:${VIZ_PORT}" \
  --flagd-api-base "${ASTRONOMY_FLAGD_API_BASE:-$BASE_URL/feature/api}" \
  --artifact-dir "$ARTIFACT_DIR" \
  --scenario-id "${ASTRONOMY_LIVE_FAULT_SCENARIO:-latency-shipping-intl}" \
  --target-service "${ASTRONOMY_LIVE_FAULT_SERVICE:-astronomy/local/shipping}" \
  --target-graph-node "${ASTRONOMY_LIVE_FAULT_GRAPH_NODE:-shipping}" \
  --baseline-secs "${ASTRONOMY_LIVE_FAULT_BASELINE_SECS:-180}" \
  --fault-secs "${ASTRONOMY_LIVE_FAULT_FAULT_SECS:-150}" \
  --recover-secs "${ASTRONOMY_LIVE_FAULT_RECOVER_SECS:-150}" \
  --poll-secs "${ASTRONOMY_LIVE_FAULT_POLL_SECS:-10}" \
  --scenario-results-dir "$ARTIFACT_DIR/scenario-results" \
  | tee "$ARTIFACT_DIR/live-fault-smoke.log"

ok "Live fault lifecycle smoke completed"
note "Claimed: real Prom ingest → baseline → shipping latency fault → live incident → recovery historical."
note "Not claimed: Databricks, agent workflows, or Run Pulse fault detection."
