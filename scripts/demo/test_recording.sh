#!/usr/bin/env bash
# Offline tests for scripts/demo/recording.sh (mocked HTTP/Docker; no fault injection
# against a real Astronomy Shop; no paid cloud calls).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRIPT="$ROOT/scripts/demo/recording.sh"
TMP="$(mktemp -d "${TMPDIR:-/tmp}/demo-recording-test.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; }

setup_fixture() {
  local name="$1"
  local dir="$TMP/$name"
  mkdir -p "$dir/http" "$dir/prom_queries" "$dir/state"
  echo "30" >"$dir/disk_free_gib"
  echo "1" >"$dir/docker_ok"
  cat >"$dir/containers_running" <<'EOF'
frontend-proxy
frontend
payment
checkout
otel-collector
load-generator
prometheus
EOF
  cat >"$dir/http_ok_map" <<EOF
http://127.0.0.1:8080/
http://127.0.0.1:8080/loadgen/
http://127.0.0.1:8080/grafana/
http://127.0.0.1:8080/grafana/api/health
http://127.0.0.1:8080/jaeger/ui/
http://127.0.0.1:8080/jaeger/ui/api/services
http://127.0.0.1:9090/-/ready
http://127.0.0.1:3131/
http://127.0.0.1:3131/api/pulse/live/status
http://127.0.0.1:3132/healthz
EOF
  cat >"$dir/prom_query.json" <<'EOF'
{"status":"success","data":{"resultType":"vector","result":[{"metric":{"service_name":"payment"},"value":[1.0,"0.12"]}]}}
EOF
  echo '{"active":false}' >"$dir/scenario_status.json"
  cat >"$dir/live_status.json" <<'EOF'
{
  "schema_version": "pulse.api.live_status.v1",
  "status": {
    "enabled": true,
    "configured": true,
    "available": true,
    "state": "collecting",
    "services_with_baseline": 1
  }
}
EOF
  cat >"$dir/live_services.json" <<'EOF'
{
  "schema_version": "pulse.api.live_services.v1",
  "services": [
    {
      "service_id": "astronomy/local/payment",
      "baseline_ready": true,
      "signals": [
        {"signal": "error_rate", "value": 0.0, "quality": "fresh"}
      ]
    }
  ]
}
EOF
  # Default astronomy graph — required by verify_astronomy_graph on demo-up.
  cat >"$dir/graph_local.json" <<'EOF'
{"appName":"perch-astronomy-shop","environment":"local","nodes":[{"id":"payment"},{"id":"checkout"},{"id":"frontend"},{"id":"cart"}]}
EOF
  echo "$dir"
}

run_cmd() {
  local fixture="$1"
  shift
  DEMO_RECORDING_MOCK=1 \
    DEMO_RECORDING_FIXTURE_DIR="$fixture" \
    DEMO_STATE_DIR="$fixture/state" \
    DEMO_VIZ_PORT=3131 \
    DEMO_METRICS_PORT=3132 \
    "$SCRIPT" "$@"
}

# --- disk hard stop ---
f="$(setup_fixture disk_low)"
echo "10" >"$f/disk_free_gib"
if run_cmd "$f" preflight >"$TMP/disk.log" 2>&1; then
  fail "preflight should hard-stop below 15 GiB"
fi
grep -q 'hard stop' "$TMP/disk.log" || fail "expected hard stop message"
pass "disk hard stop"

# --- healthy status ---
f="$(setup_fixture healthy)"
out="$(run_cmd "$f" status 2>/dev/null)"
echo "$out" | grep -q 'Astronomy Shop: READY' || fail "expected shop READY: $out"
echo "$out" | grep -q 'Prometheus: READY' || fail "expected prom READY"
echo "$out" | grep -q 'Payment telemetry: FRESH' || fail "expected payment FRESH"
echo "$out" | grep -q 'Fault injection: OFF' || fail "expected fault OFF"
echo "$out" | grep -q 'Synthetic traffic: ACTIVE' || fail "expected traffic ACTIVE"
pass "status healthy"

# --- missing payment telemetry ---
f="$(setup_fixture missing_pay)"
cat >"$f/prom_query.json" <<'EOF'
{"status":"success","data":{"resultType":"vector","result":[]}}
EOF
out="$(run_cmd "$f" status 2>/dev/null || true)"
echo "$out" | grep -q 'Payment telemetry: MISSING' || fail "expected MISSING telemetry: $out"
pass "status missing payment telemetry"

# --- up refuses without shop + without approval ---
f="$(setup_fixture no_shop)"
: >"$f/containers_running"
grep -v '8080/$' "$f/http_ok_map" >"$f/http_ok_map.tmp" || true
mv "$f/http_ok_map.tmp" "$f/http_ok_map"
if DEMO_APPROVE_SHOP_START=0 run_cmd "$f" up >"$TMP/up_refuse.log" 2>&1; then
  fail "up should refuse when shop not ready without approval"
fi
grep -q 'DEMO_APPROVE_SHOP_START' "$TMP/up_refuse.log" || fail "expected approval hint"
pass "up refuses without shop approval"

# --- up reuses healthy shop and starts mock viz (no fault) ---
f="$(setup_fixture up_ok)"
run_cmd "$f" up >"$TMP/up_ok.log" 2>&1
grep -q 'reusing containers\|already healthy\|LIVE mode' "$TMP/up_ok.log" || fail "expected reuse/live log: $(cat "$TMP/up_ok.log")"
grep -q 'does NOT inject a fault' "$TMP/up_ok.log" || fail "expected no-fault note"
grep -q 'owner=demo-recording' "$f/state/viz.meta" || fail "expected ownership meta"
grep -q 'env=local' "$TMP/up_ok.log" || fail "expected ?env=local URL guidance"
grep -q 'payment' "$TMP/up_ok.log" || grep -q 'Payment metrics' "$TMP/up_ok.log" || fail "expected payment metrics URL"
st="$(run_cmd "$f" status 2>/dev/null)"
echo "$st" | grep -q 'Fault injection: OFF' || fail "up must not inject fault"
pass "up reuses shop and starts owned viz without fault"

# --- up fails when graph is UI mock seed (github/vercel) ---
f="$(setup_fixture graph_mock)"
cat >"$f/graph_local.json" <<'EOF'
{"appName":"demo","environment":"production","nodes":[{"id":"github"},{"id":"vercel"},{"id":"render"},{"id":"supabase"}]}
EOF
if run_cmd "$f" up >"$TMP/up_mock.log" 2>&1; then
  fail "up should fail when graph is mock seed"
fi
grep -qi 'mock\|Astronomy Shop graph\|github/vercel\|FAIL' "$TMP/up_mock.log" \
  || fail "expected mock-graph failure message: $(cat "$TMP/up_mock.log")"
pass "up refuses GitHub/Vercel mock graph seed"

# --- fault refuses when baseline warming ---
f="$(setup_fixture fault_warm)"
cat >"$f/live_status.json" <<'EOF'
{"status":{"enabled":true,"available":true,"state":"warming_up"}}
EOF
run_cmd "$f" up >/dev/null 2>&1 || true
if run_cmd "$f" fault >"$TMP/fault_warm.log" 2>&1; then
  fail "fault should refuse while warming"
fi
grep -qi 'WARMING\|warming' "$TMP/fault_warm.log" || fail "expected warming refusal"
pass "fault refuses while baseline warming"

# --- fault OFF -> ON ---
f="$(setup_fixture fault_on)"
run_cmd "$f" up >/dev/null 2>&1
run_cmd "$f" fault >"$TMP/fault_on.log" 2>&1
grep -q 'Fault injection: ON' "$TMP/fault_on.log" || fail "expected ON"
grep -q 'Activation timestamp' "$TMP/fault_on.log" || fail "expected activation timestamp"
grep -q 'NOT a fixed latency' "$TMP/fault_on.log" || fail "expected detection warning"
st="$(run_cmd "$f" status 2>/dev/null)"
echo "$st" | grep -q 'Fault injection: ON' || fail "status should show ON"
pass "fault OFF -> ON"

# --- recover ON -> OFF (and already OFF) ---
run_cmd "$f" recover >"$TMP/recover.log" 2>&1
grep -q 'Fault injection: OFF' "$TMP/recover.log" || fail "expected OFF after recover"
run_cmd "$f" recover >"$TMP/recover2.log" 2>&1
grep -q 'Fault injection: OFF' "$TMP/recover2.log" || fail "recover should be idempotent"
pass "recover ON -> OFF and idempotent"

# --- down stops owned viz only; leaves shop ---
f="$(setup_fixture down_ok)"
run_cmd "$f" up >/dev/null 2>&1
run_cmd "$f" fault >/dev/null 2>&1
run_cmd "$f" down >"$TMP/down.log" 2>&1
grep -q 'still RUNNING\|containers preserved' "$TMP/down.log" || fail "expected shop preserved"
grep -q 'Fault injection: OFF' "$TMP/down.log" || fail "expected fault off on down"
[[ ! -f "$f/state/mock_viz_alive" ]] || fail "mock viz should be stopped"
pass "down disables fault and stops owned viz"

# --- unrelated / external viz safety ---
f="$(setup_fixture external_viz)"
mkdir -p "$f/state"
echo "external" >"$f/state/viz_ownership"
run_cmd "$f" down >"$TMP/down_ext.log" 2>&1
grep -q 'external — not stopping\|external' "$TMP/down_ext.log" || fail "should not stop external viz"
pass "down refuses to stop external viz"

# --- status NOT READY when shop down ---
f="$(setup_fixture docker_bad)"
echo "0" >"$f/docker_ok"
: >"$f/containers_running"
out="$(run_cmd "$f" status 2>/dev/null || true)"
echo "$out" | grep -q 'Astronomy Shop: NOT READY' || fail "expected NOT READY without containers"
pass "status NOT READY when shop down"

echo "All demo recording orchestration checks passed."
