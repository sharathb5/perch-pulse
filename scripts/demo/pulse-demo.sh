#!/usr/bin/env bash
# Perch Pulse demo orchestrator — coordinates EXISTING components.
# Modes: mock (default, offline fixtures) | replay (labeled historical) | live (real telemetry; heavy)
# Never silently mixes modes. Never overwrites investigation records.
# Does not call paid LLM / Databricks unless explicitly authorized.
# Never exports ambient ~/.perch / PERCH_PULSE_DIR records as synthetic_fixture.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

MODE="mock"
PHASE="all"
AUTHORIZE_LLM=0
AUTHORIZE_DBX=0
AUTHORIZE_MLFLOW=0
# Explicit path required to export real persisted Pulse records (observed_local).
USE_REAL_PULSE_DIR=""
# Export path must stay under repo (export-analytics --allowed-root defaults to cwd).
OUT_DIR="${PULSE_DEMO_OUT:-$ROOT/.perch/demo-out}"
DRY_RUN=0

usage() {
  cat <<'EOF'
Usage: scripts/demo/pulse-demo.sh [--mode mock|replay|live] [--phase preflight|offline|live|analysis|aftercare|all]
                                  [--out-dir DIR] [--dry-run]
                                  [--use-real-pulse-dir DIR]
                                  [--i-authorize-provider-calls] [--i-authorize-databricks-calls]
                                  [--i-authorize-mlflow-calls]

Modes (never mixed silently):
  mock   Offline fixtures only (DEFAULT). Isolated empty --pulse-dir. No Docker. No paid APIs.
  replay Labeled historical sample export + UI fixtures. Not live telemetry.
  live   Real Astronomy Shop + error-payment fault (requires disk/ports; heavy).

Real persisted Pulse data:
  Pass --use-real-pulse-dir DIR explicitly. Provenance becomes observed_local.
  Mock/replay refuse ambient ~/.perch and PERCH_PULSE_DIR for export.

Authorization flags are required for any billable/cloud step. Default is NOT RUN.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode) MODE="$2"; shift 2 ;;
    --phase) PHASE="$2"; shift 2 ;;
    --out-dir) OUT_DIR="$2"; shift 2 ;;
    --use-real-pulse-dir) USE_REAL_PULSE_DIR="$2"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --i-authorize-provider-calls) AUTHORIZE_LLM=1; shift ;;
    --i-authorize-databricks-calls) AUTHORIZE_DBX=1; shift ;;
    --i-authorize-mlflow-calls) AUTHORIZE_MLFLOW=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown arg: $1" >&2; usage; exit 2 ;;
  esac
done

case "$MODE" in
  mock|replay|live) ;;
  *) echo "invalid mode: $MODE" >&2; exit 2 ;;
esac

log() { printf '[pulse-demo][%s] %s\n' "$MODE" "$*"; }
warn() { printf '[pulse-demo][%s] WARN: %s\n' "$MODE" "$*" >&2; }
fail() { printf '[pulse-demo][%s] FAIL: %s\n' "$MODE" "$*" >&2; exit 1; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing dependency: $1"
}

check_disk() {
  # macOS df -g truncates; prefer 1K-blocks → GiB for the hard stop.
  local avail_k avail_gi
  avail_k="$(df -k / 2>/dev/null | awk 'NR==2 {print $4}' || echo 0)"
  avail_gi=$((avail_k / 1024 / 1024))
  log "disk free ≈${avail_gi} GiB on / (from ${avail_k} 1K-blocks)"
  # Soft warn always; hard-stop only when caller passes "hard".
  if [[ "${avail_gi}" =~ ^[0-9]+$ ]] && (( avail_gi < 15 )); then
    if [[ "${1:-}" == "hard" ]]; then
      fail "disk free ${avail_gi} GiB < 15 GiB hard stop (heavy/live ops)"
    fi
    warn "disk free ${avail_gi} GiB < 15 GiB — skip Docker/live; offline mock only"
  fi
}

# Resolve export pulse-dir + provenance from mode and explicit real-data selection.
# Prints: <pulse_dir><TAB><provenance>
resolve_export_source() {
  local isolated="$OUT_DIR/demo-pulse"
  mkdir -p "$isolated"

  if [[ -n "$USE_REAL_PULSE_DIR" ]]; then
    if [[ "$MODE" == "mock" || "$MODE" == "replay" ]]; then
      fail "$MODE mode refuses --use-real-pulse-dir (use --mode live for observed_local exports)"
    fi
    [[ -d "$USE_REAL_PULSE_DIR" ]] || fail "--use-real-pulse-dir is not a directory: $USE_REAL_PULSE_DIR"
    printf '%s\t%s\n' "$USE_REAL_PULSE_DIR" "observed_local"
    return 0
  fi

  # Isolate from ambient real stores. Do not read PERCH_PULSE_DIR / ~/.perch.
  printf '%s\t%s\n' "$isolated" "synthetic_fixture"
}

phase_preflight() {
  log "PREFLIGHT"
  require_cmd go
  require_cmd python3
  require_cmd make
  mkdir -p "$OUT_DIR"
  log "out_dir=$OUT_DIR"
  log "mode=$MODE (explicit; not mixed)"
  if [[ -n "$USE_REAL_PULSE_DIR" ]]; then
    if [[ "$MODE" == "mock" || "$MODE" == "replay" ]]; then
      fail "$MODE mode refuses --use-real-pulse-dir (use --mode live for observed_local exports)"
    fi
    [[ -d "$USE_REAL_PULSE_DIR" ]] || fail "--use-real-pulse-dir is not a directory: $USE_REAL_PULSE_DIR"
    log "real pulse-dir explicitly selected: $USE_REAL_PULSE_DIR"
  else
    log "export pulse-dir: isolated under $OUT_DIR/demo-pulse (ambient PERCH_PULSE_DIR ignored)"
  fi
  if [[ "$MODE" == "live" ]]; then
    if [[ "$DRY_RUN" -eq 1 ]]; then
      log "dry-run: skipping live disk/docker hard requirements"
    else
      check_disk hard
      require_cmd docker
      warn "LIVE mode may pull/start Astronomy Shop images — ensure disk/RAM headroom"
      warn "Ports commonly needed: 8080 (frontend), 9090 (prometheus), 16686 (jaeger), 8081 (flagd)"
    fi
  else
    check_disk
  fi
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "dry-run: preflight ok"
  fi
}

phase_offline() {
  log "OFFLINE JOURNEY (investigation → eval → export → databricks dry-run)"
  local eval_dir="$OUT_DIR/inv-eval"
  local export_dir="$OUT_DIR/export"
  mkdir -p "$eval_dir" "$export_dir"

  local src_line pulse_dir provenance
  src_line="$(resolve_export_source)"
  pulse_dir="${src_line%%$'\t'*}"
  provenance="${src_line#*$'\t'}"

  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "dry-run: would run web-build-embed, investigation-eval, llm-provider-smoke, export-analytics"
    log "dry-run: export --pulse-dir=$pulse_dir --incident-provenance=$provenance"
    return 0
  fi

  # cmd/perch embeds web/dist — required on clean checkouts (AGENTS.md).
  if [[ ! -f "$ROOT/web/dist/index.html" ]]; then
    log "building embedded web assets (web/dist missing)"
    make web-build-embed
  else
    log "web/dist present — skipping rebuild"
  fi

  go run ./cmd/perch pulse investigation-eval --dataset f2c --out-dir "$eval_dir" --json \
    | tee "$OUT_DIR/investigation-eval.json" >/dev/null
  log "investigation-eval → $eval_dir"

  go run ./cmd/perch pulse llm-provider-smoke --case-id case_payment_error --json \
    | tee "$OUT_DIR/llm-provider-smoke.json"
  if grep -q '"real_provider_executed"[[:space:]]*:[[:space:]]*true' "$OUT_DIR/llm-provider-smoke.json" 2>/dev/null; then
    fail "offline smoke unexpectedly set real_provider_executed=true"
  fi
  log "REAL LLM VALIDATION = NOT RUN (offline default)"

  if [[ "$AUTHORIZE_LLM" -eq 1 ]]; then
    warn "authorized real provider smoke requested — billable"
    go run ./cmd/perch pulse llm-provider-smoke --case-id case_payment_error \
      --real-provider --i-authorize-provider-calls --json \
      | tee "$OUT_DIR/llm-provider-smoke-real.json"
  fi

  # Explicit --pulse-dir wins over ambient PERCH_PULSE_DIR (CLI preference).
  # synthetic_fixture is only used with the isolated empty demo-pulse dir.
  log "export source mode=$MODE pulse-dir=$pulse_dir provenance=$provenance"
  go run ./cmd/perch pulse export-analytics \
    --pulse-dir "$pulse_dir" \
    --out-dir "$export_dir" \
    --eval-report "$eval_dir/report.json" \
    --incident-provenance "$provenance" \
    --json | tee "$OUT_DIR/export-analytics.json" >/dev/null
  log "export → $export_dir (provenance=$provenance)"

  if [[ "$MODE" == "replay" ]]; then
    log "REPLAY: also validating labeled historical sample export"
    SAMPLE="$ROOT/examples/pulse-export/sample/export"
    [[ -d "$SAMPLE" ]] || fail "missing sample export $SAMPLE"
  fi

  local dbx_dir="$ROOT/integrations/databricks"
  if [[ ! -d "$dbx_dir/.venv" ]]; then
    warn "databricks venv missing — creating lightweight test venv"
    python3 -m venv "$dbx_dir/.venv"
    # shellcheck disable=SC1091
    source "$dbx_dir/.venv/bin/activate"
    (cd "$dbx_dir" && pip install -e ".[test]" -q)
  else
    # shellcheck disable=SC1091
    source "$dbx_dir/.venv/bin/activate"
  fi

  local use_export="$export_dir"
  if [[ "$MODE" == "replay" ]]; then
    use_export="$ROOT/examples/pulse-export/sample/export"
    log "using HISTORICAL sample export (labeled historical_demo / sample provenance)"
  elif [[ "$MODE" == "mock" ]]; then
    log "using MOCK export from isolated pulse-dir (synthetic_fixture eval rows only)"
  else
    log "using LIVE-mode export (provenance=$provenance)"
  fi

  python -m perch_pulse_dbx validate --export-dir "$use_export" --json \
    | tee "$OUT_DIR/dbx-validate.json"
  python -m perch_pulse_dbx dry-run --export-dir "$use_export" --simulate-merge --json \
    | tee "$OUT_DIR/dbx-dry-run.json"
  python -m perch_pulse_dbx verify-lineage --export-dir "$use_export" --json \
    | tee "$OUT_DIR/dbx-lineage.json"
  python -m perch_pulse_dbx mlflow-log --report "$eval_dir/report.json" --json \
    | tee "$OUT_DIR/mlflow-offline.json"
  log "LIVE DELTA INGESTION = NOT RUN"
  log "LIVE MLFLOW TRACKING = NOT RUN"

  if [[ "$AUTHORIZE_DBX" -eq 1 ]]; then
    warn "authorized Databricks live path — using existing warehouse only"
    local live_args=(live-smoke --export-dir "$use_export" --max-batch 50 --json --i-authorize-databricks-calls)
    if [[ "$AUTHORIZE_MLFLOW" -eq 1 ]]; then
      live_args+=(--report "$eval_dir/report.json" --i-authorize-mlflow-calls)
    fi
    if [[ -n "${DATABRICKS_WAREHOUSE_ID:-}" ]]; then
      live_args+=(--warehouse-id "$DATABRICKS_WAREHOUSE_ID")
    fi
    # Default connection-only unless PULSE_DEMO_EXECUTE_MERGE=1
    if [[ "${PULSE_DEMO_EXECUTE_MERGE:-0}" == "1" ]]; then
      live_args+=(--execute-merge)
      log "PULSE_DEMO_EXECUTE_MERGE=1 — will stage+MERGE"
    else
      log "Databricks live-smoke connection-only (set PULSE_DEMO_EXECUTE_MERGE=1 to MERGE)"
    fi
    python -m perch_pulse_dbx "${live_args[@]}" | tee "$OUT_DIR/dbx-live-smoke.json"
  fi
}

phase_live() {
  if [[ "$MODE" != "live" ]]; then
    log "skip live phase (mode=$MODE)"
    return 0
  fi
  log "LIVE phase — Astronomy Shop error-payment (real telemetry)"
  check_disk
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "dry-run: would start shop, baseline, enable error-payment, detect, disable, recover"
    return 0
  fi
  [[ -x "$ROOT/examples/astronomy-shop/scripts/verify-live.sh" ]] || fail "missing verify-live.sh"
  warn "Prefer GitHub astronomy-smoke / ci-live-fault when local disk is tight"
  warn "Fault disable is mandatory aftercare — see docs/demo-runbook.md"
  log "Delegate to examples/astronomy-shop/scripts/ci-live-fault.sh (error-payment)"
  ASTRONOMY_LIVE_FAULT_SCENARIO=error-payment \
    "$ROOT/examples/astronomy-shop/scripts/ci-live-fault.sh" | tee "$OUT_DIR/live-fault.log"
}

phase_aftercare() {
  log "AFTERCARE"
  if [[ "$MODE" == "live" && "$DRY_RUN" -eq 0 ]]; then
    if [[ -x "$ROOT/examples/astronomy-shop/scenarios/scenario.sh" ]]; then
      "$ROOT/examples/astronomy-shop/scenarios/scenario.sh" stop error-payment 2>/dev/null || true
      log "ensured error-payment scenario stop attempted"
    fi
  fi
  log "artifacts under $OUT_DIR (preserve; do not overwrite real ~/.perch investigations)"
  log "summary: mode=$MODE REAL_LLM=${AUTHORIZE_LLM} DBX=${AUTHORIZE_DBX} MLFLOW=${AUTHORIZE_MLFLOW}"
  if [[ -n "$USE_REAL_PULSE_DIR" ]]; then
    log "real pulse export selected: $USE_REAL_PULSE_DIR (observed_local)"
  else
    log "real pulse export = NOT SELECTED (isolated demo-pulse only)"
  fi
  if [[ "$AUTHORIZE_LLM" -eq 0 ]]; then
    log "REAL LLM VALIDATION = NOT RUN"
  fi
  if [[ "$AUTHORIZE_DBX" -eq 0 ]]; then
    log "LIVE DELTA INGESTION = NOT RUN"
  fi
  if [[ "$AUTHORIZE_MLFLOW" -eq 0 ]]; then
    log "LIVE MLFLOW TRACKING = NOT RUN"
  fi
}

main() {
  case "$PHASE" in
    preflight) phase_preflight ;;
    offline) phase_preflight; phase_offline ;;
    live) phase_preflight; phase_live ;;
    analysis) phase_offline ;;
    aftercare) phase_aftercare ;;
    all)
      phase_preflight
      if [[ "$MODE" == "live" ]]; then
        phase_live
      fi
      phase_offline
      phase_aftercare
      ;;
    *) fail "unknown phase: $PHASE" ;;
  esac
  log "DONE"
}

main
