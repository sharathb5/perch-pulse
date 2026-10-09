#!/usr/bin/env bash
# Live verification of catalog scenarios against a running Astronomy Shop.
# Not part of make verify / CI. Deterministic checks only (no LLM).
#
# Preconditions:
#   - df -h / shows >= 15GiB free (hard stop below that)
#   - Docker responds
#   - Astronomy Shop minimal mode already up (prefer reusing containers)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
SCENARIO="$ROOT/examples/astronomy-shop/scenarios/scenario.sh"
BASE_URL="${ASTRONOMY_BASE_URL:-http://127.0.0.1:8080}"
FLAGD_READ="${PERCH_FLAGD_API_BASE:-$BASE_URL/feature/api}/read"
DOCKER_TIMEOUT_SECS="${ASTRONOMY_DOCKER_TIMEOUT_SECS:-15}"
DWELL_SECS="${ASTRONOMY_SCENARIO_DWELL_SECS:-20}"
RESULTS_DIR="${PERCH_SCENARIO_RESULTS_DIR:-$ROOT/examples/astronomy-shop/scenarios/results}"
export PERCH_SCENARIO_RESULTS_DIR="$RESULTS_DIR"

fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "OK: $*"; }
info() { echo "INFO: $*"; }

# --- Resource safety ----------------------------------------------------------
avail_kb="$(df -k / | awk 'NR==2{print $4}')"
# 15 GiB = 15728640 KiB
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

if ! curl -sf --max-time 5 "$BASE_URL/" >/dev/null; then
  fail "Astronomy Shop frontend not reachable at $BASE_URL/ (start via examples/astronomy-shop/scripts/start.sh)"
fi
ok "frontend reachable"

if ! curl -sf --max-time 5 "$FLAGD_READ" >/dev/null; then
  fail "flagd-ui API not reachable at $FLAGD_READ"
fi
ok "flagd-ui API reachable"

mkdir -p "$RESULTS_DIR"
SUMMARY="$RESULTS_DIR/live-verify-summary.json"
partial="$RESULTS_DIR/live-verify-partial.jsonl"
: >"$RESULTS_DIR/live-verify.log"
: >"$partial"

# Ensure no leftover active run
if "$SCENARIO" status | grep -q '"active": true'; then
  info "stopping leftover active scenario"
  "$SCENARIO" stop >/dev/null || true
fi

SCENARIOS=(
  latency-shipping-intl
  error-payment
  outage-payment
  control-emit-raw-pii
)

flag_variant() {
  local flag="$1"
  curl -sf --max-time 5 "$FLAGD_READ" | python3 -c "
import json,sys
flag=sys.argv[1]
doc=json.load(sys.stdin)
print(doc.get('flags',{}).get(flag,{}).get('defaultVariant',''))
" "$flag"
}

run_one() {
  local id="$1"
  info "===== scenario $id ====="
  local start_json stop_json
  start_json="$("$SCENARIO" start "$id")"
  echo "$start_json" >>"$RESULTS_DIR/live-verify.log"
  local flag active idle classification
  flag="$(echo "$start_json" | python3 -c 'import json,sys; print(json.load(sys.stdin)["mechanism"]["flag"])')"
  active="$(echo "$start_json" | python3 -c 'import json,sys; print(json.load(sys.stdin)["mechanism"]["active_variant"])')"
  idle="$(echo "$start_json" | python3 -c 'import json,sys; print(json.load(sys.stdin)["mechanism"]["idle_variant"])')"
  classification="$(echo "$start_json" | python3 -c 'import json,sys; print(json.load(sys.stdin)["classification"])')"

  local got
  got="$(flag_variant "$flag")"
  [[ "$got" == "$active" ]] || fail "$id: flag $flag want active=$active got=$got"
  ok "$id: fault/control active (flag=$flag variant=$got)"

  curl -sf --max-time 5 "$BASE_URL/" >/dev/null || fail "$id: frontend died during scenario"
  docker inspect -f '{{.State.Status}}' load-generator 2>/dev/null | grep -q running \
    || fail "$id: load-generator not running"
  ok "$id: workload still active (frontend + load-generator)"

  info "$id: dwelling ${DWELL_SECS}s for telemetry to flow"
  sleep "$DWELL_SECS"

  if curl -sf --max-time 5 "$BASE_URL/jaeger/ui/api/services" >/dev/null 2>&1; then
    ok "$id: Jaeger services API still reachable"
  else
    echo "WARN: $id: Jaeger services API not reachable during dwell"
  fi

  stop_json="$("$SCENARIO" stop "$id")"
  echo "$stop_json" >>"$RESULTS_DIR/live-verify.log"
  got="$(flag_variant "$flag")"
  [[ "$got" == "$idle" ]] || fail "$id: after stop flag $flag want idle=$idle got=$got"
  recovered="$(echo "$stop_json" | python3 -c 'import json,sys; d=json.load(sys.stdin); print("true" if d.get("recovered") else "false")')"
  [[ "$recovered" == "true" ]] || fail "$id: recovery_verified false"
  ok "$id: recovered to idle variant=$got"

  python3 -c "
import json
print(json.dumps({
  'id': '$id',
  'classification': '$classification',
  'activation': True,
  'recovery': True,
  'flag': '$flag',
}))
" >>"$partial"
}

for s in "${SCENARIOS[@]}"; do
  run_one "$s"
done

python3 - "$SUMMARY" "$partial" "$DWELL_SECS" "$RESULTS_DIR" <<'PY'
import json, sys
summary_path, partial_path, dwell, results_dir = sys.argv[1:5]
scenarios = []
with open(partial_path, encoding="utf-8") as f:
    for line in f:
        line = line.strip()
        if line:
            scenarios.append(json.loads(line))
out = {
    "ok": True,
    "dwell_secs": int(dwell),
    "results_dir": results_dir,
    "scenarios": scenarios,
    "negative_control": next((s for s in scenarios if s["classification"] == "control"), None),
}
with open(summary_path, "w", encoding="utf-8") as f:
    json.dump(out, f, indent=2)
    f.write("\n")
print(json.dumps(out, indent=2))
PY

ok "all scenarios verified; summary at $SUMMARY"
