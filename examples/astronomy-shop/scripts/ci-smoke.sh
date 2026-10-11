#!/usr/bin/env bash
# GitHub Actions–oriented Astronomy Shop smoke (LIVE SMOKE).
# Not part of make verify. Prefer workflow_dispatch on a GitHub-hosted runner.
#
# Proves (when resources allow):
#   stack readiness → perch.yaml graph → /api/graph+/api/status →
#   /api/pulse/workflows available → bounded storefront GET →
#   recommendations-browse synthetic traffic (cmd/trace-smoke) → same-trace
#   multi-service Jaeger evidence → REAL Run Pulse HTTP API
#   (POST /api/pulse/runs + GET /api/pulse/runs/{id}) → clean stop
#
# Honesty: HTTP 200 alone is not success; missing multi-service trace evidence FAILs.
# Both trace-smoke and Run Pulse API stages run against the same Astronomy Shop
# stack: trace-smoke proves library-level Jaeger qualification; Run Pulse proves
# the viz HTTP API initiates and returns the same class of diagnostic.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
SHOP="$ROOT/examples/astronomy-shop"
DEST="${PERCH_ASTRONOMY_DEMO_DIR:-$SHOP/.demo}"
BASE_URL="${ASTRONOMY_BASE_URL:-http://127.0.0.1:8080}"
PIN="${ASTRONOMY_DEMO_PIN:-3.1.0}"
# Standard ubuntu-latest is ~14GiB disk / 7–16GiB RAM depending on generation.
MIN_DISK_KB="${ASTRONOMY_MIN_DISK_KB:-8388608}"   # ~8 GiB free
MIN_MEM_KB="${ASTRONOMY_MIN_MEM_KB:-3145728}"     # ~3 GiB MemAvailable
READY_SECS="${ASTRONOMY_VERIFY_WAIT_SECS:-300}"
JOB_DEADLINE_SECS="${ASTRONOMY_SMOKE_DEADLINE_SECS:-2400}"
ARTIFACT_DIR="${ASTRONOMY_SMOKE_ARTIFACT_DIR:-$SHOP/.smoke-artifacts}"
PERCH_BIN="${PERCH_BIN:-}"

fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "OK: $*"; }
warn() { echo "WARN: $*" >&2; }
note() { echo "NOTE: $*"; }

mkdir -p "$ARTIFACT_DIR"
START_TS=$SECONDS

VIZ_PID=""
cleanup() {
  local code=$?
  set +e
  echo "---- smoke cleanup (exit=$code) ----"
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

if (( SECONDS > JOB_DEADLINE_SECS )); then
  fail "job deadline exceeded before start"
fi

echo "==== Astronomy Shop CI smoke preflight ===="
echo "pin=$PIN dest=$DEST base_url=$BASE_URL"

command -v docker >/dev/null || fail "docker not installed"
command -v curl >/dev/null || fail "curl not installed"
command -v go >/dev/null || fail "go not installed (needed to build perch)"
timeout 20 docker info >/dev/null 2>&1 || fail "Docker daemon not responding"

avail_kb="$(df -k / | awk 'NR==2{print $4}')"
mem_kb="$(awk '/MemAvailable:/ {print $2}' /proc/meminfo 2>/dev/null || echo 0)"
echo "disk_avail_kb=$avail_kb mem_available_kb=$mem_kb"
echo "$avail_kb" >"$ARTIFACT_DIR/disk_avail_kb.txt"
echo "$mem_kb" >"$ARTIFACT_DIR/mem_available_kb.txt"

if [[ -n "$avail_kb" && "$avail_kb" -lt "$MIN_DISK_KB" ]]; then
  fail "insufficient disk for Astronomy Shop pull (avail_kb=$avail_kb need>=$MIN_DISK_KB). Workflow unavailable on this runner — do not claim success."
fi
if [[ -n "$mem_kb" && "$mem_kb" -gt 0 && "$mem_kb" -lt "$MIN_MEM_KB" ]]; then
  fail "insufficient MemAvailable for minimal stack (mem_kb=$mem_kb need>=$MIN_MEM_KB). Mark smoke pending larger runner approval."
fi

echo "==== clone pin $PIN ===="
"$SHOP/scripts/clone.sh" | tee "$ARTIFACT_DIR/clone.log"

echo "==== start minimal stack ===="
export ASTRONOMY_MODE=minimal
"$SHOP/scripts/start.sh" | tee "$ARTIFACT_DIR/start.log"

echo "==== verify-live (containers + frontend) ===="
export ASTRONOMY_VERIFY_WAIT_SECS="$READY_SECS"
"$SHOP/scripts/verify-live.sh" | tee "$ARTIFACT_DIR/verify-live.log"

echo "==== Perch graph/status CLI + viz HTTP APIs ===="
cd "$SHOP"
if [[ -z "$PERCH_BIN" || ! -x "$PERCH_BIN" ]]; then
  PERCH_BIN="$ARTIFACT_DIR/perch"
  (cd "$ROOT" && make web-build-embed && go build -o "$PERCH_BIN" ./cmd/perch)
fi
"$PERCH_BIN" graph --json --env local >"$ARTIFACT_DIR/perch-graph.json"
grep -q '"shipping"' "$ARTIFACT_DIR/perch-graph.json" || fail "graph missing shipping node"
grep -q '"checkout"' "$ARTIFACT_DIR/perch-graph.json" || fail "graph missing checkout node"
ok "perch graph contains expected Astronomy Shop nodes"

# Bounded status collect (custom docker inspect probes — no cloud creds).
if ! "$PERCH_BIN" status --json --env local >"$ARTIFACT_DIR/perch-status.json"; then
  fail "perch status --json failed (see $ARTIFACT_DIR/perch-status.json)"
fi
grep -q '"nodes"' "$ARTIFACT_DIR/perch-status.json" || fail "status JSON missing nodes"
ok "perch status JSON collected with nodes"

# Start the real Go viz server with Run Pulse enabled (supported env config).
# Same security model as normal execution: loopback allowlist, no CSRF bypass.
VIZ_PORT="${ASTRONOMY_SMOKE_VIZ_PORT:-18765}"
VIZ_LOG="$ARTIFACT_DIR/viz.log"
export PERCH_RUN_PULSE_DEMO_BASE_URL="${PERCH_RUN_PULSE_DEMO_BASE_URL:-$BASE_URL}"
export PERCH_RUN_PULSE_JAEGER_BASE="${PERCH_RUN_PULSE_JAEGER_BASE:-${ASTRONOMY_JAEGER_UI_BASE:-$BASE_URL/jaeger/ui}}"
echo "PERCH_RUN_PULSE_DEMO_BASE_URL=$PERCH_RUN_PULSE_DEMO_BASE_URL" | tee "$ARTIFACT_DIR/run-pulse-env.txt"
echo "PERCH_RUN_PULSE_JAEGER_BASE=$PERCH_RUN_PULSE_JAEGER_BASE" | tee -a "$ARTIFACT_DIR/run-pulse-env.txt"
"$PERCH_BIN" viz --env local --port "$VIZ_PORT" >"$VIZ_LOG" 2>&1 &
VIZ_PID=$!
viz_ok=0
for _ in $(seq 1 40); do
  if curl -sf --max-time 2 "http://127.0.0.1:${VIZ_PORT}/api/graph?env=local" >"$ARTIFACT_DIR/api-graph.json"; then
    viz_ok=1
    break
  fi
  sleep 0.25
done
if [[ "$viz_ok" -ne 1 ]]; then
  fail "viz /api/graph did not become ready (see $VIZ_LOG)"
fi
grep -q '"shipping"' "$ARTIFACT_DIR/api-graph.json" || fail "/api/graph missing shipping"
curl -sf --max-time 30 "http://127.0.0.1:${VIZ_PORT}/api/status?env=local" >"$ARTIFACT_DIR/api-status.json" \
  || fail "viz /api/status failed"
grep -q '"nodes"' "$ARTIFACT_DIR/api-status.json" || fail "/api/status missing nodes"
ok "viz /api/graph and /api/status responded with nodes"

echo "==== Run Pulse workflows catalog ===="
curl -sf --max-time 15 "http://127.0.0.1:${VIZ_PORT}/api/pulse/workflows" >"$ARTIFACT_DIR/api-pulse-workflows.json" \
  || fail "GET /api/pulse/workflows failed (is Run Pulse mounted? see $VIZ_LOG)"
# Fail if the Astronomy workflow is missing or available=false (HTTP 200 alone is insufficient).
if ! grep -q '"id":"astronomy.recommendations-browse.v1"' "$ARTIFACT_DIR/api-pulse-workflows.json" && \
   ! grep -q '"id": "astronomy.recommendations-browse.v1"' "$ARTIFACT_DIR/api-pulse-workflows.json"; then
  fail "workflow astronomy.recommendations-browse.v1 not listed in /api/pulse/workflows"
fi
if grep -q '"available": false' "$ARTIFACT_DIR/api-pulse-workflows.json" || grep -q '"available":false' "$ARTIFACT_DIR/api-pulse-workflows.json"; then
  # Confirm it is our workflow that is unavailable (catalog has one entry in MVP).
  fail "astronomy.recommendations-browse.v1 listed but unavailable — set PERCH_RUN_PULSE_DEMO_BASE_URL (see $ARTIFACT_DIR/api-pulse-workflows.json and run-pulse-env.txt)"
fi
if ! grep -q '"available": true' "$ARTIFACT_DIR/api-pulse-workflows.json" && ! grep -q '"available":true' "$ARTIFACT_DIR/api-pulse-workflows.json"; then
  fail "workflow available=true not observed in /api/pulse/workflows"
fi
ok "GET /api/pulse/workflows lists astronomy.recommendations-browse.v1 as available"

echo "==== bounded safe synthetic traffic (GET storefront) ===="
# Storefront HTML reachability — client-side JS is NOT executed by curl, so this
# alone cannot prove product-catalog / recommendation spans.
TRAFFIC_CODE="$(curl -sS -o "$ARTIFACT_DIR/frontend-get.body" -w '%{http_code}' --max-time 15 "$BASE_URL/" || echo 000)"
echo "frontend_get_http=$TRAFFIC_CODE" | tee "$ARTIFACT_DIR/traffic.txt"
[[ "$TRAFFIC_CODE" == "200" ]] || fail "storefront GET did not return 200 (got $TRAFFIC_CODE)"
ok "storefront GET observed HTTP $TRAFFIC_CODE (HTML only; not multi-service proof)"

echo "==== telemetry surfaces (Prometheus/Grafana best-effort) ===="
PROM_OK=0
for u in \
  "$BASE_URL/grafana/api/health" \
  "http://127.0.0.1:9090/api/v1/status/runtimeinfo" \
  "http://127.0.0.1:9090/-/ready"
do
  if curl -sf --max-time 5 "$u" >"$ARTIFACT_DIR/prom-probe.json" 2>/dev/null; then
    echo "prometheus_or_grafana_probe=$u" | tee -a "$ARTIFACT_DIR/telemetry.txt"
    PROM_OK=1
    break
  fi
done
[[ "$PROM_OK" -eq 1 ]] && ok "observability HTTP surface reachable" || warn "Prometheus/Grafana probe not confirmed"

echo "==== required multi-service trace smoke (recommendations browse) ===="
# Mirrors Locust WebsiteUser.get_recommendations (read-only). Hard-fail unless
# Jaeger returns one same-trace with >=2 app services and a cross-service edge.
TRACE_DEADLINE_SECS="${ASTRONOMY_TRACE_DEADLINE_SECS:-120}"
TRACE_POLL_SECS="${ASTRONOMY_TRACE_POLL_SECS:-3}"
TRACE_MARKER="${ASTRONOMY_TRACE_MARKER:-perch-astronomy-smoke/${GITHUB_RUN_ID:-local}-$(date +%s)}"
TRACE_BIN="$ARTIFACT_DIR/trace-smoke"
(cd "$ROOT" && go build -o "$TRACE_BIN" ./examples/astronomy-shop/cmd/trace-smoke)
"$TRACE_BIN" \
  --base-url "$BASE_URL" \
  --jaeger-base "${ASTRONOMY_JAEGER_UI_BASE:-$BASE_URL/jaeger/ui}" \
  --artifact-dir "$ARTIFACT_DIR" \
  --marker "$TRACE_MARKER" \
  --product-id "${ASTRONOMY_TRACE_PRODUCT_ID:-OLJCESPC7Z}" \
  --deadline-secs "$TRACE_DEADLINE_SECS" \
  --poll-secs "$TRACE_POLL_SECS" \
  --max-requests "${ASTRONOMY_TRACE_MAX_REQUESTS:-5}" \
  --request-timeout-secs "${ASTRONOMY_TRACE_REQUEST_TIMEOUT_SECS:-15}" \
  | tee "$ARTIFACT_DIR/trace-smoke.log"
ok "multi-service Jaeger trace evidence collected for recommendations-browse"

echo "==== required Run Pulse HTTP API smoke (POST/GET /api/pulse/runs) ===="
# Same Astronomy Shop + viz process. Separate probe from trace-smoke on purpose:
# this stage proves the real viz HTTP API starts the diagnostic and returns
# qualified evidence — not a direct Go package / CLI substitute.
note "Why a second recommendations probe: trace-smoke validates Jaeger qualification outside viz; Run Pulse validates POST/GET /api/pulse/runs through the mounted API (bounded; max 3 probe GETs inside the engine)."
RUN_PULSE_BIN="$ARTIFACT_DIR/run-pulse-api-smoke"
(cd "$ROOT" && go build -o "$RUN_PULSE_BIN" ./examples/astronomy-shop/cmd/run-pulse-api-smoke)
"$RUN_PULSE_BIN" \
  --viz-base-url "http://127.0.0.1:${VIZ_PORT}" \
  --artifact-dir "$ARTIFACT_DIR" \
  --graph-json "$ARTIFACT_DIR/api-graph.json" \
  --workflow-id "astronomy.recommendations-browse.v1" \
  --deadline-secs "${ASTRONOMY_RUN_PULSE_DEADLINE_SECS:-120}" \
  --poll-secs "${ASTRONOMY_RUN_PULSE_POLL_SECS:-2}" \
  --max-attempts "${ASTRONOMY_RUN_PULSE_MAX_ATTEMPTS:-60}" \
  | tee "$ARTIFACT_DIR/run-pulse-api-smoke.log"
ok "Run Pulse HTTP API returned completed diagnostic with qualified multi-service evidence"

# Service mapping honesty: identity map is compose-name based (no fuzzy display names).
MAP="$ROOT/internal/pulse/astronomy/service-mapping.yaml"
grep -q 'shipping' "$MAP" || fail "service-mapping.yaml missing shipping"
ok "Pulse astronomy mapping file present (compose names, not display-name fuzzy match)"

# Stop viz before compose teardown (cleanup also kills VIZ_PID if still set).
if [[ -n "${VIZ_PID}" ]]; then
  kill "$VIZ_PID" 2>/dev/null || true
  wait "$VIZ_PID" 2>/dev/null || true
  VIZ_PID=""
fi

note "Observed: frontend HTTP $TRAFFIC_CODE; graph/status/workflows OK; trace-smoke qualified; Run Pulse API completed with mapped services + cross-service edges."
note "Not claimed: checkout/payment/email path, fault injection, continuous monitoring, or UI screenshot proof in this workflow."
ok "Astronomy Shop CI smoke completed within runner budgets"
