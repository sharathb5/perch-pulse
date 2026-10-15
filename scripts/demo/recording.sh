#!/usr/bin/env bash
# Presenter-facing LIVE recording orchestration for Perch Pulse.
# Reuses Astronomy Shop + scenario harness + perch viz Live Pulse.
# Does NOT inject a fault on "up". Does NOT stop shop containers on "down".
# Does NOT call paid LLM / Databricks. Does NOT prune Docker or rebuild images.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

SHOP="$ROOT/examples/astronomy-shop"
SCENARIO_SH="$SHOP/scenarios/scenario.sh"
VERIFY_LIVE="$SHOP/scripts/verify-live.sh"
START_SHOP="$SHOP/scripts/start.sh"
CLONE_SHOP="$SHOP/scripts/clone.sh"

STATE_DIR="${DEMO_STATE_DIR:-$ROOT/.perch/demo-recording}"
VIZ_PORT="${DEMO_VIZ_PORT:-3131}"
VIZ_PID_FILE="$STATE_DIR/viz.pid"
VIZ_META_FILE="$STATE_DIR/viz.meta"
VIZ_LOG_FILE="$STATE_DIR/viz.log"
PERCH_BIN_FILE="$STATE_DIR/perch"
PULSE_DIR="${PERCH_PULSE_DIR:-$STATE_DIR/pulse-dir}"

BASE_URL="${ASTRONOMY_BASE_URL:-http://127.0.0.1:8080}"
PROM_URL="${PERCH_LIVE_PULSE_PROM_URL:-http://127.0.0.1:9090}"
JAEGER_UI="${ASTRONOMY_JAEGER_UI_BASE:-$BASE_URL/jaeger/ui}"
FLAGD_API="${ASTRONOMY_FLAGD_API_BASE:-$BASE_URL/feature/api}"
SCENARIO_ID="${DEMO_SCENARIO_ID:-error-payment}"
MIN_DISK_GIB="${DEMO_MIN_DISK_GIB:-15}"
DOCKER_TIMEOUT_SECS="${ASTRONOMY_DOCKER_TIMEOUT_SECS:-15}"

# Test/mock hooks (unit tests only — never enable for real demos).
MOCK_MODE="${DEMO_RECORDING_MOCK:-0}"
MOCK_FIXTURE_DIR="${DEMO_RECORDING_FIXTURE_DIR:-}"

usage() {
  cat <<'EOF'
Usage: scripts/demo/recording.sh <preflight|up|status|fault|recover|down>

  preflight  Disk + Docker + shop/telemetry readiness (read-only checks)
  up         Reuse healthy shop; start Live Pulse viz; print URLs (no fault)
  status     Read-only readiness summary
  fault      Require healthy baseline; enable error-payment scenario
  recover    Disable error-payment (idempotent); preserve incidents
  down       Disable fault; stop only demo-owned perch viz; leave shop up

Env (optional):
  DEMO_VIZ_PORT              default 3131
  DEMO_STATE_DIR             default .perch/demo-recording
  DEMO_APPROVE_SHOP_START=1  allow starting Astronomy Shop when not running
  PERCH_LIVE_PULSE_PROM_URL  default http://127.0.0.1:9090
  ASTRONOMY_BASE_URL         default http://127.0.0.1:8080
EOF
}

# Logs go to stderr so command substitutions (e.g. ensure_perch_bin) stay clean.
log() { printf '[demo-recording] %s\n' "$*" >&2; }
warn() { printf '[demo-recording] WARN: %s\n' "$*" >&2; }
fail() { printf '[demo-recording] FAIL: %s\n' "$*" >&2; exit 1; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing dependency: $1"
}

mkdir_state() {
  mkdir -p "$STATE_DIR" "$PULSE_DIR"
}

# --- disk / docker ------------------------------------------------------------

disk_free_gib() {
  if [[ "$MOCK_MODE" == "1" && -n "$MOCK_FIXTURE_DIR" && -f "$MOCK_FIXTURE_DIR/disk_free_gib" ]]; then
    cat "$MOCK_FIXTURE_DIR/disk_free_gib"
    return 0
  fi
  local avail_k
  avail_k="$(df -k / 2>/dev/null | awk 'NR==2 {print $4}' || echo 0)"
  echo $((avail_k / 1024 / 1024))
}

check_disk_hard() {
  local gib
  gib="$(disk_free_gib)"
  log "disk free ≈${gib} GiB on /"
  if [[ "$gib" =~ ^[0-9]+$ ]] && (( gib < MIN_DISK_GIB )); then
    fail "disk free ${gib} GiB < ${MIN_DISK_GIB} GiB hard stop"
  fi
}

docker_ok() {
  if [[ "$MOCK_MODE" == "1" && -n "$MOCK_FIXTURE_DIR" && -f "$MOCK_FIXTURE_DIR/docker_ok" ]]; then
    [[ "$(cat "$MOCK_FIXTURE_DIR/docker_ok")" == "1" ]]
    return
  fi
  command -v docker >/dev/null 2>&1 || return 1
  timeout "$DOCKER_TIMEOUT_SECS" docker info >/dev/null 2>&1
}

# --- HTTP helpers -------------------------------------------------------------

http_ok() {
  local url="$1"
  local timeout="${2:-5}"
  local code
  if [[ "$MOCK_MODE" == "1" && -n "$MOCK_FIXTURE_DIR" ]]; then
    local key
    key="$(echo "$url" | tr '/:?' '___')"
    if [[ -f "$MOCK_FIXTURE_DIR/http/${key}.ok" ]]; then
      return 0
    fi
    if [[ -f "$MOCK_FIXTURE_DIR/http_ok_map" ]] && grep -Fxq "$url" "$MOCK_FIXTURE_DIR/http_ok_map"; then
      return 0
    fi
    return 1
  fi
  # Prefer HEAD so large UIs (loadgen ~20MiB HTML) do not blow --max-time.
  # Do not append fallback text inside $(curl || echo) — curl -w already prints 000 on failure.
  code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time "$timeout" -I "$url" 2>/dev/null)" || code="000"
  if [[ "$code" =~ ^[23][0-9][0-9]$ ]]; then
    return 0
  fi
  # Some routes reject HEAD — fall back to a tiny ranged GET.
  code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time "$timeout" -r 0-2047 "$url" 2>/dev/null)" || code="000"
  [[ "$code" =~ ^[23][0-9][0-9]$ ]]
}

http_body() {
  local url="$1"
  local timeout="${2:-5}"
  if [[ "$MOCK_MODE" == "1" && -n "$MOCK_FIXTURE_DIR" ]]; then
    local key
    key="$(echo "$url" | tr '/:?' '___')"
    if [[ -f "$MOCK_FIXTURE_DIR/http/${key}.body" ]]; then
      cat "$MOCK_FIXTURE_DIR/http/${key}.body"
      return 0
    fi
    # Fallback short names for common endpoints in fixtures.
    case "$url" in
      */api/v1/query*) [[ -f "$MOCK_FIXTURE_DIR/prom_query.json" ]] && cat "$MOCK_FIXTURE_DIR/prom_query.json" && return 0 ;;
      */live/status*) [[ -f "$MOCK_FIXTURE_DIR/live_status.json" ]] && cat "$MOCK_FIXTURE_DIR/live_status.json" && return 0 ;;
      */live/services*) [[ -f "$MOCK_FIXTURE_DIR/live_services.json" ]] && cat "$MOCK_FIXTURE_DIR/live_services.json" && return 0 ;;
    esac
    return 1
  fi
  curl -sf --max-time "$timeout" "$url" 2>/dev/null || true
}

prom_query() {
  local q="$1"
  if [[ "$MOCK_MODE" == "1" && -n "$MOCK_FIXTURE_DIR" && -f "$MOCK_FIXTURE_DIR/prom_queries/${q}.json" ]]; then
    cat "$MOCK_FIXTURE_DIR/prom_queries/${q}.json"
    return 0
  fi
  if [[ "$MOCK_MODE" == "1" && -n "$MOCK_FIXTURE_DIR" && -f "$MOCK_FIXTURE_DIR/prom_query.json" ]]; then
    cat "$MOCK_FIXTURE_DIR/prom_query.json"
    return 0
  fi
  curl -sgG --max-time 8 "${PROM_URL}/api/v1/query" --data-urlencode "query=${q}" 2>/dev/null || true
}

prom_scalar() {
  # Extract first vector sample value from Prometheus instant-query JSON.
  local json="$1"
  python3 -c '
import json,sys
try:
  d=json.load(sys.stdin)
  r=d.get("data",{}).get("result") or []
  if not r:
    print("")
  else:
    v=r[0].get("value")
    print(v[1] if isinstance(v,(list,tuple)) and len(v)>1 else "")
except Exception:
  print("")
' <<<"$json"
}

# --- shop / telemetry probes --------------------------------------------------

container_running() {
  local name="$1"
  if [[ "$MOCK_MODE" == "1" && -n "$MOCK_FIXTURE_DIR" ]]; then
    if [[ -f "$MOCK_FIXTURE_DIR/containers_running" ]] && grep -Fxq "$name" "$MOCK_FIXTURE_DIR/containers_running"; then
      return 0
    fi
    return 1
  fi
  local status
  status="$(docker inspect -f '{{.State.Status}}' "$name" 2>/dev/null || echo missing)"
  [[ "$status" == "running" ]]
}

shop_core_up() {
  local required=(frontend-proxy frontend payment checkout otel-collector load-generator prometheus)
  local c
  for c in "${required[@]}"; do
    container_running "$c" || return 1
  done
  http_ok "${BASE_URL}/" 3 || return 1
  return 0
}

payment_call_rate_json() {
  prom_query 'sum by (service_name) (rate(traces_span_metrics_calls_total{service_name="payment"}[2m]))'
}

payment_error_rate_json() {
  prom_query 'sum by (service_name) (increase(traces_span_metrics_calls_total{service_name="payment",status_code="STATUS_CODE_ERROR"}[2m])) / clamp_min(sum by (service_name) (increase(traces_span_metrics_calls_total{service_name="payment"}[2m])), 1)'
}

payment_telemetry_quality() {
  # FRESH if nonempty call_rate sample; MISSING if empty; STALE not directly
  # observable from Prom alone without series timestamps — report MISSING/FRESH.
  local body rate
  body="$(payment_call_rate_json)"
  rate="$(prom_scalar "$body")"
  if [[ -z "$rate" ]]; then
    echo "MISSING"
  else
    echo "FRESH"
  fi
}

scenario_status_json() {
  if [[ "$MOCK_MODE" == "1" && -n "$MOCK_FIXTURE_DIR" && -f "$MOCK_FIXTURE_DIR/scenario_status.json" ]]; then
    cat "$MOCK_FIXTURE_DIR/scenario_status.json"
    return 0
  fi
  "$SCENARIO_SH" status 2>/dev/null || echo '{"active":false}'
}

scenario_active_id() {
  local json
  json="$(scenario_status_json)"
  python3 -c '
import json,sys
try:
  d=json.load(sys.stdin)
  if not d.get("active"):
    print("")
  else:
    run=d.get("run") or {}
    print(run.get("scenario_id") or run.get("id") or "unknown")
except Exception:
  print("")
' <<<"$json"
}

fault_injection_state() {
  local id
  id="$(scenario_active_id)"
  if [[ -z "$id" ]]; then
    echo "OFF"
  elif [[ "$id" == "$SCENARIO_ID" ]]; then
    echo "ON"
  else
    echo "UNKNOWN"
  fi
}

# --- viz process ownership ----------------------------------------------------

viz_base() {
  echo "http://127.0.0.1:${VIZ_PORT}"
}

viz_pid_alive() {
  local pid="${1:-}"
  [[ -n "$pid" ]] || return 1
  if [[ "$MOCK_MODE" == "1" ]]; then
    [[ -f "$STATE_DIR/mock_viz_alive" ]] && return 0
    return 1
  fi
  kill -0 "$pid" 2>/dev/null
}

read_owned_viz_pid() {
  [[ -f "$VIZ_PID_FILE" ]] || return 1
  local pid
  pid="$(tr -d '[:space:]' <"$VIZ_PID_FILE")"
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  echo "$pid"
}

is_demo_owned_viz() {
  local pid="$1"
  viz_pid_alive "$pid" || return 1
  if [[ "$MOCK_MODE" == "1" ]]; then
    [[ -f "$VIZ_META_FILE" ]] || return 1
    grep -q "owner=demo-recording" "$VIZ_META_FILE" 2>/dev/null
    return
  fi
  # Confirm process still looks like our perch viz on the expected port.
  local args
  args="$(ps -p "$pid" -o args= 2>/dev/null || true)"
  [[ "$args" == *"viz"* ]] || return 1
  [[ "$args" == *"--port ${VIZ_PORT}"* || "$args" == *"--port=${VIZ_PORT}"* ]] || return 1
  [[ -f "$VIZ_META_FILE" ]] || return 1
  grep -q "owner=demo-recording" "$VIZ_META_FILE" 2>/dev/null
}

viz_http_ready() {
  http_ok "$(viz_base)/" 3 || return 1
  http_ok "$(viz_base)/api/pulse/live/status" 3
}

live_status_json() {
  http_body "$(viz_base)/api/pulse/live/status" 5
}

live_services_json() {
  http_body "$(viz_base)/api/pulse/live/services" 8
}

live_status_inner() {
  # API wraps StatusView under {"status": {...}}.
  python3 -c '
import json,sys
try:
  d=json.load(sys.stdin)
  st=d.get("status") if isinstance(d.get("status"), dict) else d
  if not isinstance(st, dict):
    st={}
  print(json.dumps(st))
except Exception:
  print("{}")
'
}

live_enabled() {
  local json inner
  json="$(live_status_json)"
  inner="$(live_status_inner <<<"$json")"
  python3 -c '
import json,sys
try:
  d=json.load(sys.stdin)
  print("1" if d.get("enabled") and d.get("available") else "0")
except Exception:
  print("0")
' <<<"$inner"
}

live_state() {
  local json inner
  json="$(live_status_json)"
  inner="$(live_status_inner <<<"$json")"
  python3 -c '
import json,sys
try:
  d=json.load(sys.stdin)
  print(d.get("state") or "UNKNOWN")
except Exception:
  print("UNKNOWN")
' <<<"$inner"
}

payment_error_rate_observed() {
  # Prefer Live Pulse service snapshot when viz is up; else Prometheus.
  local services rate body
  if viz_http_ready 2>/dev/null; then
    services="$(live_services_json)"
    rate="$(python3 -c '
import json,sys
try:
  d=json.load(sys.stdin)
  svcs=d.get("services") or d.get("Services") or []
  if isinstance(d, list):
    svcs=d
  for s in svcs:
    sid=(s.get("service_id") or "")
    if sid.endswith("/payment") or sid=="payment":
      for sig in s.get("signals") or []:
        if sig.get("signal")=="error_rate" and sig.get("value") is not None:
          print(sig["value"]); raise SystemExit
  print("")
except Exception:
  print("")
' <<<"$services")"
    if [[ -n "$rate" ]]; then
      echo "$rate"
      return 0
    fi
  fi
  body="$(payment_error_rate_json)"
  prom_scalar "$body"
}

baseline_readiness() {
  # Observable from Live Pulse status/services; else UNKNOWN.
  if ! viz_http_ready 2>/dev/null; then
    echo "UNKNOWN"
    return 0
  fi
  local st services
  st="$(live_state)"
  case "$st" in
    warming_up) echo "WARMING"; return 0 ;;
    collecting)
      services="$(live_services_json)"
      python3 -c '
import json,sys
try:
  d=json.load(sys.stdin)
  svcs=d.get("services") or []
  if isinstance(d, list):
    svcs=d
  for s in svcs:
    sid=(s.get("service_id") or "")
    if sid.endswith("/payment"):
      print("READY" if s.get("baseline_ready") else "WARMING")
      raise SystemExit
  # collecting but payment not listed yet
  print("WARMING")
except Exception:
  print("UNKNOWN")
' <<<"$services"
      return 0
      ;;
    disabled|unavailable|error) echo "UNKNOWN"; return 0 ;;
    *) echo "UNKNOWN"; return 0 ;;
  esac
}

ensure_perch_bin() {
  if [[ "$MOCK_MODE" == "1" ]]; then
    echo "$PERCH_BIN_FILE"
    return 0
  fi
  if [[ -n "${PERCH_BIN:-}" && -x "${PERCH_BIN}" ]]; then
    echo "$PERCH_BIN"
    return 0
  fi
  if [[ -x "$PERCH_BIN_FILE" ]]; then
    echo "$PERCH_BIN_FILE"
    return 0
  fi
  mkdir_state
  if [[ ! -f "$ROOT/web/dist/index.html" ]]; then
    log "building embedded web assets (web/dist missing)"
    make web-build-embed
  fi
  log "building perch binary → $PERCH_BIN_FILE"
  (cd "$ROOT" && go build -o "$PERCH_BIN_FILE" ./cmd/perch)
  echo "$PERCH_BIN_FILE"
}

start_demo_viz() {
  local bin pid
  mkdir_state
  if pid="$(read_owned_viz_pid 2>/dev/null || true)" && is_demo_owned_viz "$pid" && viz_http_ready; then
    log "reusing demo-owned perch viz pid=$pid port=$VIZ_PORT"
    return 0
  fi
  # Do not steal an unrelated listener on the port (real mode only).
  # Mock fixtures may list viz URLs as reachable before ownership is established.
  if [[ "$MOCK_MODE" != "1" ]] && http_ok "$(viz_base)/" 2; then
    if [[ "$(live_enabled)" == "1" ]]; then
      warn "port ${VIZ_PORT} already serves Live Pulse, but is not demo-owned — leaving it alone"
      echo "external" >"$STATE_DIR/viz_ownership"
      return 0
    fi
    fail "port ${VIZ_PORT} is occupied by a non-Live-Pulse server; set DEMO_VIZ_PORT or stop that process"
  fi

  bin="$(ensure_perch_bin)"
  export PERCH_LIVE_PULSE_ENABLED=1
  export PERCH_LIVE_PULSE_PROM_URL="$PROM_URL"
  export PERCH_LIVE_PULSE_ENVIRONMENT=local
  export PERCH_LIVE_PULSE_POLL_INTERVAL="${PERCH_LIVE_PULSE_POLL_INTERVAL:-15s}"
  export PERCH_PULSE_DIR="$PULSE_DIR"
  export PERCH_RUN_PULSE_DEMO_BASE_URL="${PERCH_RUN_PULSE_DEMO_BASE_URL:-$BASE_URL}"
  export PERCH_RUN_PULSE_JAEGER_BASE="${PERCH_RUN_PULSE_JAEGER_BASE:-$JAEGER_UI}"

  if [[ "$MOCK_MODE" == "1" ]]; then
    echo "99999" >"$VIZ_PID_FILE"
    cat >"$VIZ_META_FILE" <<EOF
owner=demo-recording
port=${VIZ_PORT}
started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
pulse_dir=${PULSE_DIR}
EOF
    touch "$STATE_DIR/mock_viz_alive"
    echo "demo" >"$STATE_DIR/viz_ownership"
    return 0
  fi

  log "starting perch viz Live Pulse on 127.0.0.1:${VIZ_PORT}"
  # nohup + exec: survive make/shell exit (SIGHUP) and keep PID = perch binary
  # so ownership checks can match `viz --port`.
  (
    cd "$SHOP"
    # shellcheck disable=SC2086
    exec nohup "$bin" viz --env local --port "$VIZ_PORT"
  ) >"$VIZ_LOG_FILE" 2>&1 &
  pid=$!
  echo "$pid" >"$VIZ_PID_FILE"
  cat >"$VIZ_META_FILE" <<EOF
owner=demo-recording
port=${VIZ_PORT}
pid=${pid}
started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
pulse_dir=${PULSE_DIR}
bin=${bin}
EOF
  echo "demo" >"$STATE_DIR/viz_ownership"

  local i
  for i in $(seq 1 60); do
    if viz_http_ready && [[ "$(live_enabled)" == "1" ]]; then
      log "Live Pulse enabled (pid=$pid)"
      return 0
    fi
    if ! viz_pid_alive "$pid"; then
      fail "perch viz exited early — see $VIZ_LOG_FILE"
    fi
    sleep 0.5
  done
  fail "perch viz did not become ready with Live Pulse — see $VIZ_LOG_FILE"
}

stop_demo_viz() {
  local pid ownership
  ownership="$(cat "$STATE_DIR/viz_ownership" 2>/dev/null || echo "")"
  if [[ "$ownership" == "external" ]]; then
    log "viz on port ${VIZ_PORT} was external — not stopping"
    return 0
  fi
  pid="$(read_owned_viz_pid 2>/dev/null || true)"
  if [[ -z "$pid" ]]; then
    log "no demo-owned viz pid file — nothing to stop"
    return 0
  fi
  if ! is_demo_owned_viz "$pid"; then
    warn "pid $pid is not a live demo-owned viz — refusing to kill"
    rm -f "$VIZ_PID_FILE"
    return 0
  fi
  if [[ "$MOCK_MODE" == "1" ]]; then
    rm -f "$STATE_DIR/mock_viz_alive" "$VIZ_PID_FILE"
    log "stopped mock demo viz"
    return 0
  fi
  log "stopping demo-owned perch viz pid=$pid"
  kill "$pid" 2>/dev/null || true
  local i
  for i in $(seq 1 20); do
    viz_pid_alive "$pid" || break
    sleep 0.25
  done
  if viz_pid_alive "$pid"; then
    warn "viz pid $pid still alive after SIGTERM — leaving process (no SIGKILL)"
  fi
  rm -f "$VIZ_PID_FILE"
}

print_urls() {
  cat <<EOF

=== Demo URLs ===
Astronomy Shop:  ${BASE_URL}/
Load generator:  ${BASE_URL}/loadgen/
Feature flags:   ${BASE_URL}/feature/
Grafana:         ${BASE_URL}/grafana/
  Spanmetrics:   ${BASE_URL}/grafana/d/W2gX2zHVk48/spanmetrics-demo-dashboard?var-service=payment
Prometheus:      ${PROM_URL}/
  Payment rate:  ${PROM_URL}/graph?g0.expr=sum(rate(traces_span_metrics_calls_total%7Bservice_name%3D%22payment%22%7D%5B2m%5D))&g0.tab=0
  Payment errs:  ${PROM_URL}/graph?g0.expr=sum(rate(traces_span_metrics_calls_total%7Bservice_name%3D%22payment%22%2Cstatus_code%3D%22STATUS_CODE_ERROR%22%7D%5B2m%5D))&g0.tab=0
Jaeger:          ${JAEGER_UI}/
Perch viz:       $(viz_base)/
Pulse live API:  $(viz_base)/api/pulse/live/status
EOF
}

remediate() {
  local item="$1"
  case "$item" in
    shop)
      echo "  Remediation: ./examples/astronomy-shop/scripts/clone.sh && DEMO_APPROVE_SHOP_START=1 make demo-up"
      echo "               or: ./examples/astronomy-shop/scripts/start.sh && ./examples/astronomy-shop/scripts/verify-live.sh"
      ;;
    prom)
      echo "  Remediation: ensure prometheus container is up; check ${PROM_URL}/-/ready"
      ;;
    payment)
      echo "  Remediation: wait for load-generator traffic; confirm payment in Prom label values"
      ;;
    viz)
      echo "  Remediation: make demo-up  (starts demo-owned Live Pulse viz)"
      ;;
    baseline)
      echo "  Remediation: wait ~2–3 minutes after Live Pulse starts (warming_up → collecting)"
      ;;
    fault)
      echo "  Remediation: make demo-recover   then make demo-fault"
      ;;
    *) ;;
  esac
}

# --- commands -----------------------------------------------------------------

cmd_preflight() {
  require_cmd curl
  require_cmd python3
  check_disk_hard
  if docker_ok; then
    log "Docker: OK"
  else
    fail "Docker unavailable — start Docker Desktop and retry"
  fi
  if shop_core_up; then
    log "Astronomy Shop: READY"
  else
    log "Astronomy Shop: NOT READY"
    remediate shop
    return 1
  fi
  if http_ok "${PROM_URL}/-/ready" 5; then
    log "Prometheus: READY"
  else
    log "Prometheus: NOT READY"
    remediate prom
    return 1
  fi
  local pay_q
  pay_q="$(payment_telemetry_quality)"
  log "Payment telemetry: ${pay_q}"
  [[ "$pay_q" == "FRESH" ]] || { remediate payment; return 1; }
  if http_ok "${BASE_URL}/grafana/api/health" 8 || http_ok "${BASE_URL}/grafana/" 8 || \
     { [[ "$MOCK_MODE" != "1" ]] && curl -sf --max-time 8 "${BASE_URL}/grafana/api/health" >/dev/null 2>&1; }; then
    log "Grafana: READY"
  else
    warn "Grafana: NOT READY (non-blocking for Live Pulse)"
  fi
  if http_ok "${JAEGER_UI}/api/services" 8 || http_ok "${JAEGER_UI}/" 8; then
    log "Jaeger: READY"
  else
    warn "Jaeger: NOT READY (needed for Run Pulse; Live Pulse uses Prometheus)"
  fi
  # Prefer container + payment series over Locust HTML (large; HEAD often times out).
  if container_running load-generator && [[ "$(payment_telemetry_quality)" == "FRESH" ]]; then
    log "Synthetic traffic: ACTIVE"
  else
    warn "Synthetic traffic: INACTIVE"
  fi
  log "Fault injection: $(fault_injection_state)"
  log "preflight OK"
}

cmd_up() {
  require_cmd curl
  require_cmd python3
  require_cmd go
  check_disk_hard
  docker_ok || fail "Docker unavailable"

  if ! shop_core_up; then
    cat <<EOF
Astronomy Shop is not READY.

Documented start (no image rebuild/prune by these scripts):
  ./examples/astronomy-shop/scripts/clone.sh   # once
  ./examples/astronomy-shop/scripts/start.sh   # make start-minimal in .demo
  ./examples/astronomy-shop/scripts/verify-live.sh

To authorize this command to start the shop once it is cloned:
  DEMO_APPROVE_SHOP_START=1 make demo-up

EOF
    if [[ "${DEMO_APPROVE_SHOP_START:-0}" != "1" ]]; then
      fail "shop not ready — explicit DEMO_APPROVE_SHOP_START=1 required before starting containers"
    fi
    [[ -x "$CLONE_SHOP" ]] || fail "missing $CLONE_SHOP"
    [[ -x "$START_SHOP" ]] || fail "missing $START_SHOP"
    if [[ ! -d "${PERCH_ASTRONOMY_DEMO_DIR:-$SHOP/.demo}" ]]; then
      log "cloning Astronomy Shop pin (approved)"
      "$CLONE_SHOP"
    fi
    log "starting Astronomy Shop (approved via DEMO_APPROVE_SHOP_START=1)"
    export ASTRONOMY_MODE="${ASTRONOMY_MODE:-minimal}"
    "$START_SHOP"
    "$VERIFY_LIVE" || fail "verify-live failed after start"
  else
    log "Astronomy Shop already healthy — reusing containers (no rebuild)"
  fi

  http_ok "${BASE_URL}/" 5 || fail "storefront not reachable at ${BASE_URL}/"
  http_ok "${PROM_URL}/-/ready" 5 || fail "Prometheus not ready at ${PROM_URL}"
  if ! http_ok "${JAEGER_UI}/api/services" 5 && ! http_ok "${JAEGER_UI}/" 5; then
    warn "Jaeger not reachable — Run Pulse may be unavailable; Live Pulse can still run"
  fi
  [[ "$(payment_telemetry_quality)" == "FRESH" ]] || fail "payment telemetry missing — wait for load-generator"
  if container_running load-generator; then
    log "Synthetic traffic: ACTIVE"
  else
    warn "load-generator container not running"
  fi

  # Never inject a fault during up.
  if [[ "$(fault_injection_state)" == "ON" ]]; then
    warn "fault ${SCENARIO_ID} is currently ON — demo-up does not change it; run make demo-recover if undesired"
  fi

  start_demo_viz
  [[ "$(live_enabled)" == "1" ]] || fail "LIVE mode not active"
  log "LIVE mode: active (state=$(live_state))"
  print_urls
  log "Next: make demo-status   then (when ready) make demo-fault"
  log "NOTE: demo-up does NOT inject a fault."
}

cmd_status() {
  require_cmd curl
  require_cmd python3

  local shop_st traffic_st prom_st pay_q base_st err_rate viz_st pulse_st graf_st jaeger_st fault_st

  if shop_core_up; then shop_st="READY"; else shop_st="NOT READY"; fi
  if http_ok "${PROM_URL}/-/ready" 5; then prom_st="READY"; else prom_st="NOT READY"; fi
  pay_q="$(payment_telemetry_quality)"
  if container_running load-generator && [[ "$pay_q" == "FRESH" ]]; then
    traffic_st="ACTIVE"
  else
    traffic_st="INACTIVE"
  fi
  base_st="$(baseline_readiness)"
  err_rate="$(payment_error_rate_observed)"
  [[ -n "$err_rate" ]] || err_rate="n/a"

  if viz_http_ready; then viz_st="READY"; else viz_st="NOT READY"; fi
  if [[ "$viz_st" == "READY" ]] && [[ "$(live_enabled)" == "1" ]]; then
    pulse_st="READY ($(live_state))"
  elif [[ "$viz_st" == "READY" ]]; then
    pulse_st="NOT READY"
  else
    pulse_st="NOT READY"
  fi
  # Grafana health JSON is small — prefer GET body over flaky HEAD through Envoy.
  if http_ok "${BASE_URL}/grafana/api/health" 8 || http_ok "${BASE_URL}/grafana/" 8; then
    graf_st="READY"
  elif [[ "$MOCK_MODE" != "1" ]] && curl -sf --max-time 8 "${BASE_URL}/grafana/api/health" >/dev/null 2>&1; then
    graf_st="READY"
  else
    graf_st="NOT READY"
  fi
  if http_ok "${JAEGER_UI}/api/services" 8 || http_ok "${JAEGER_UI}/" 8; then
    jaeger_st="READY"
  else
    jaeger_st="NOT READY"
  fi
  fault_st="$(fault_injection_state)"

  printf 'Astronomy Shop: %s\n' "$shop_st"
  printf 'Synthetic traffic: %s\n' "$traffic_st"
  printf 'Prometheus: %s\n' "$prom_st"
  printf 'Payment telemetry: %s\n' "$pay_q"
  printf 'Baseline: %s\n' "$base_st"
  printf 'Payment error rate: %s\n' "$err_rate"
  printf 'Perch viz: %s\n' "$viz_st"
  printf 'Pulse API: %s\n' "$pulse_st"
  printf 'Grafana: %s\n' "$graf_st"
  printf 'Jaeger: %s\n' "$jaeger_st"
  printf 'Fault injection: %s\n' "$fault_st"

  if [[ "$shop_st" != "READY" ]]; then remediate shop; fi
  if [[ "$prom_st" != "READY" ]]; then remediate prom; fi
  if [[ "$pay_q" == "MISSING" ]]; then remediate payment; fi
  if [[ "$viz_st" != "READY" ]]; then remediate viz; fi
  if [[ "$base_st" == "WARMING" ]]; then remediate baseline; fi
}

cmd_fault() {
  require_cmd curl
  require_cmd python3
  require_cmd go

  # Healthy preflight required.
  if ! shop_core_up; then
    remediate shop
    fail "refusing fault injection — Astronomy Shop not READY"
  fi
  http_ok "${PROM_URL}/-/ready" 5 || fail "refusing fault injection — Prometheus not READY"
  [[ "$(payment_telemetry_quality)" == "FRESH" ]] || fail "refusing fault injection — payment telemetry MISSING"
  if ! viz_http_ready || [[ "$(live_enabled)" != "1" ]]; then
    remediate viz
    fail "refusing fault injection — Live Pulse viz not READY (run make demo-up)"
  fi
  local base
  base="$(baseline_readiness)"
  if [[ "$base" == "WARMING" ]]; then
    remediate baseline
    fail "refusing fault injection — baseline still WARMING (wait for READY)"
  fi
  if [[ "$base" == "UNKNOWN" ]]; then
    warn "baseline readiness UNKNOWN — proceeding only if payment error rate looks healthy"
  fi

  local fault_st
  fault_st="$(fault_injection_state)"
  if [[ "$fault_st" == "ON" ]]; then
    fail "scenario ${SCENARIO_ID} already ON — run make demo-recover first, or continue the demo"
  fi
  if [[ "$fault_st" == "UNKNOWN" ]]; then
    fail "another scenario appears active — stop it before injecting ${SCENARIO_ID}"
  fi

  local ts
  ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  log "enabling scenario ${SCENARIO_ID} at ${ts}"
  if [[ "$MOCK_MODE" == "1" ]]; then
    echo '{"active":true,"run":{"scenario_id":"error-payment"}}' >"${MOCK_FIXTURE_DIR}/scenario_status.json"
  else
    "$SCENARIO_SH" start "$SCENARIO_ID"
  fi
  ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  fault_st="$(fault_injection_state)"
  [[ "$fault_st" == "ON" ]] || fail "scenario did not activate (state=${fault_st})"
  echo "$ts" >"$STATE_DIR/fault_on_at"
  log "Fault injection: ON"
  log "Activation timestamp (UTC): ${ts}"
  cat <<EOF

Watch:
  - Prometheus payment errors: ${PROM_URL}/graph?g0.expr=sum(rate(traces_span_metrics_calls_total%7Bservice_name%3D%22payment%22%2Cstatus_code%3D%22STATUS_CODE_ERROR%22%7D%5B2m%5D))&g0.tab=0
  - Grafana Spanmetrics (service=payment): ${BASE_URL}/grafana/d/W2gX2zHVk48/spanmetrics-demo-dashboard?var-service=payment
  - Perch Pulse sidebar → Incidents (ACTIVE)

WARNING: Detection is NOT a fixed latency. One measured run saw ~70s; your run
may need multiple Live Pulse collection windows (poll ≈15s) before ACTIVE.

This command exits after activation — it does NOT auto-disable the fault.
When finished demonstrating: make demo-recover
EOF
}

cmd_recover() {
  require_cmd python3
  local ts fault_st
  ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  log "disabling scenario ${SCENARIO_ID} at ${ts}"
  if [[ "$MOCK_MODE" == "1" ]]; then
    echo '{"active":false}' >"${MOCK_FIXTURE_DIR}/scenario_status.json"
  else
    # Idempotent: stop even if already off.
    "$SCENARIO_SH" stop "$SCENARIO_ID" 2>/dev/null || "$SCENARIO_SH" stop 2>/dev/null || true
  fi
  ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  fault_st="$(fault_injection_state)"
  if [[ "$fault_st" != "OFF" ]]; then
    warn "scenario state after stop: ${fault_st} (expected OFF)"
  fi
  echo "$ts" >"$STATE_DIR/fault_off_at"
  log "Fault injection: ${fault_st}"
  log "Disable timestamp (UTC): ${ts}"
  cat <<EOF

Watch Perch Pulse sidebar for lifecycle → RECOVERED (same incident_id).
One measured recovery took ~130s after fault off — not a guarantee.

Shop is left running. Incident history is preserved (not deleted).
EOF
}

cmd_down() {
  require_cmd python3
  log "safe cleanup — disable fault, stop demo-owned viz only"
  if [[ "$MOCK_MODE" == "1" ]]; then
    echo '{"active":false}' >"${MOCK_FIXTURE_DIR}/scenario_status.json"
  else
    "$SCENARIO_SH" stop "$SCENARIO_ID" 2>/dev/null || "$SCENARIO_SH" stop 2>/dev/null || true
  fi
  local fault_st
  fault_st="$(fault_injection_state)"
  log "Fault injection: ${fault_st}"
  stop_demo_viz
  if shop_core_up; then
    log "Astronomy Shop: still RUNNING (containers preserved; volumes untouched)"
  else
    log "Astronomy Shop: not fully ready (this command did not stop it intentionally)"
  fi
  log "Databricks resources: untouched"
  log "Pulse dir preserved at: $PULSE_DIR"
}

main() {
  local cmd="${1:-}"
  case "$cmd" in
    preflight) cmd_preflight ;;
    up) cmd_up ;;
    status) cmd_status ;;
    fault) cmd_fault ;;
    recover) cmd_recover ;;
    down) cmd_down ;;
    -h|--help|help|"") usage; [[ -n "$cmd" ]] || exit 2 ;;
    *) echo "unknown command: $cmd" >&2; usage; exit 2 ;;
  esac
}

main "$@"
