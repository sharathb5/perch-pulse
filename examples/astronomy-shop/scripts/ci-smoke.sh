#!/usr/bin/env bash
# GitHub Actions–oriented Astronomy Shop smoke (LIVE SMOKE).
# Not part of make verify. Prefer workflow_dispatch on a GitHub-hosted runner.
#
# Proves (when resources allow):
#   stack readiness → perch.yaml graph → /api/graph+/api/status → bounded
#   synthetic GET → optional Jaeger/Prometheus visibility → clean stop
#
# Honesty: HTTP 200 alone is not success; we report exactly what was observed.
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

cleanup() {
  local code=$?
  set +e
  echo "---- smoke cleanup (exit=$code) ----"
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

# Start viz briefly and exercise HTTP handlers (not only CLI).
VIZ_PORT="${ASTRONOMY_SMOKE_VIZ_PORT:-18765}"
VIZ_LOG="$ARTIFACT_DIR/viz.log"
"$PERCH_BIN" viz --env local --port "$VIZ_PORT" >"$VIZ_LOG" 2>&1 &
VIZ_PID=$!
viz_down() { kill "$VIZ_PID" 2>/dev/null || true; wait "$VIZ_PID" 2>/dev/null || true; }
viz_ok=0
for _ in $(seq 1 40); do
  if curl -sf --max-time 2 "http://127.0.0.1:${VIZ_PORT}/api/graph?env=local" >"$ARTIFACT_DIR/api-graph.json"; then
    viz_ok=1
    break
  fi
  sleep 0.25
done
if [[ "$viz_ok" -ne 1 ]]; then
  viz_down
  fail "viz /api/graph did not become ready (see $VIZ_LOG)"
fi
grep -q '"shipping"' "$ARTIFACT_DIR/api-graph.json" || { viz_down; fail "/api/graph missing shipping"; }
curl -sf --max-time 30 "http://127.0.0.1:${VIZ_PORT}/api/status?env=local" >"$ARTIFACT_DIR/api-status.json" \
  || { viz_down; fail "viz /api/status failed"; }
grep -q '"nodes"' "$ARTIFACT_DIR/api-status.json" || { viz_down; fail "/api/status missing nodes"; }
ok "viz /api/graph and /api/status responded with nodes"
viz_down

echo "==== bounded safe synthetic traffic (GET storefront only) ===="
# One safe GET to the storefront — no checkout/order/payment/email side effects.
TRAFFIC_CODE="$(curl -sS -o "$ARTIFACT_DIR/frontend-get.body" -w '%{http_code}' --max-time 15 "$BASE_URL/" || echo 000)"
echo "frontend_get_http=$TRAFFIC_CODE" | tee "$ARTIFACT_DIR/traffic.txt"
[[ "$TRAFFIC_CODE" == "200" ]] || fail "storefront GET did not return 200 (got $TRAFFIC_CODE)"
ok "storefront GET observed HTTP $TRAFFIC_CODE (not proof of full checkout path)"

echo "==== telemetry surfaces (best-effort, reported honestly) ===="
PROM_URL="${ASTRONOMY_PROM_URL:-$BASE_URL/}"
# Prometheus is often behind Grafana path; try common demo endpoints.
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

JAEGER_URL="${ASTRONOMY_JAEGER_SERVICES_URL:-$BASE_URL/jaeger/ui/api/services}"
if curl -sf --max-time 10 "$JAEGER_URL" >"$ARTIFACT_DIR/jaeger-services.json" 2>/dev/null; then
  if grep -Eq '"frontend"|"checkout"|"frontend-proxy"' "$ARTIFACT_DIR/jaeger-services.json"; then
    ok "Jaeger lists expected shop service name(s) after traffic"
  else
    warn "Jaeger reachable but expected service names not yet present (load may be cold)"
  fi
else
  warn "Jaeger services API not reachable via $JAEGER_URL"
fi

# Service mapping honesty: identity map is compose-name based (no fuzzy display names).
MAP="$ROOT/internal/pulse/astronomy/service-mapping.yaml"
grep -q 'shipping' "$MAP" || fail "service-mapping.yaml missing shipping"
ok "Pulse astronomy mapping file present (compose names, not display-name fuzzy match)"

note "Observed: frontend HTTP $TRAFFIC_CODE; graph nodes present; verify-live passed."
note "Not claimed: full multi-hop checkout span, payment charge, or email delivery."
ok "Astronomy Shop CI smoke completed within runner budgets"
