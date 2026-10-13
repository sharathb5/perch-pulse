#!/usr/bin/env bash
# Regression checks for demo export provenance isolation (no cloud / no Docker).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRIPT="$ROOT/scripts/demo/pulse-demo.sh"
TMP="$(mktemp -d "${TMPDIR:-/tmp}/pulse-demo-prov.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; }

# --- resolve_export_source via dry-run logs ---
out_mock="$TMP/mock-out"
mkdir -p "$out_mock"
log_mock="$TMP/mock.log"
"$SCRIPT" --mode mock --phase preflight --out-dir "$out_mock" --dry-run >"$log_mock" 2>&1
grep -q 'isolated under' "$log_mock" || fail "mock preflight should isolate pulse-dir"
grep -q 'real pulse-dir explicitly selected' "$log_mock" && fail "mock must not select real pulse-dir"
pass "mock isolates pulse-dir"

# mock refuses --use-real-pulse-dir
if "$SCRIPT" --mode mock --phase preflight --out-dir "$out_mock" \
  --use-real-pulse-dir "$TMP/real" --dry-run >"$TMP/mock-refuse.log" 2>&1; then
  fail "mock should refuse --use-real-pulse-dir"
fi
grep -q 'refuses --use-real-pulse-dir' "$TMP/mock-refuse.log" || fail "expected refuse message"
pass "mock refuses real pulse-dir"

# replay also refuses real pulse-dir
if "$SCRIPT" --mode replay --phase preflight --out-dir "$out_mock" \
  --use-real-pulse-dir "$TMP/real" --dry-run >"$TMP/replay-refuse.log" 2>&1; then
  fail "replay should refuse --use-real-pulse-dir"
fi
pass "replay refuses real pulse-dir"

# live + explicit real dir is accepted in dry-run (no docker required)
mkdir -p "$TMP/real-pulse"
log_live="$TMP/live.log"
"$SCRIPT" --mode live --phase offline --out-dir "$TMP/live-out" \
  --use-real-pulse-dir "$TMP/real-pulse" --dry-run >"$log_live" 2>&1
grep -q "real pulse-dir explicitly selected: $TMP/real-pulse" "$log_live" \
  || fail "live should log explicit real pulse-dir selection"
grep -q "provenance=observed_local" "$log_live" || fail "live+real pulse-dir should plan observed_local"
grep -q "pulse-dir=$TMP/real-pulse" "$log_live" || fail "live dry-run should use explicit real pulse-dir"
pass "live accepts explicit real pulse-dir"

# Modes stay distinguishable in logs
"$SCRIPT" --mode mock --phase offline --out-dir "$TMP/offline-dry" --dry-run >"$TMP/offline.log" 2>&1
grep -q '\[pulse-demo\]\[mock\]' "$TMP/offline.log" || fail "mock mode tag missing"
grep -q 'provenance=synthetic_fixture' "$TMP/offline.log" || fail "mock dry-run should plan synthetic_fixture"
grep -q "pulse-dir=$TMP/offline-dry/demo-pulse" "$TMP/offline.log" || fail "mock dry-run should use isolated demo-pulse"
pass "mock offline dry-run plans isolated synthetic export"

"$SCRIPT" --mode replay --phase offline --out-dir "$TMP/replay-dry" --dry-run >"$TMP/replay.log" 2>&1
grep -q '\[pulse-demo\]\[replay\]' "$TMP/replay.log" || fail "replay mode tag missing"
pass "replay mode distinguishable"

echo "All demo provenance checks passed."
