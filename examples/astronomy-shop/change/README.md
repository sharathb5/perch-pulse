# Astronomy Shop — simulated change / deploy markers

This directory records **simulated** Pulse change events for Phase 2F correlation demos.

**Important:** markers are **not** real cloud or CI deployments. They are typed `pulse.change.v1` events written locally so the correlation engine can be exercised against live detector incidents.

## Record a marker

```bash
./examples/astronomy-shop/change/deploy-marker.sh astronomy/local/payment abc123def
```

Or via CLI (requires built `perch` with embedded web assets):

```bash
export PERCH_PULSE_DIR=./examples/astronomy-shop/change/results
perch pulse change-record \
  --type deployment \
  --service astronomy/local/payment \
  --commit abc123def \
  --env local \
  --simulated
```

## Live correlation eval

With Astronomy Shop already running (see `../README.md`):

```bash
./examples/astronomy-shop/eval/run-change-eval.sh
```

Sequence per fault scenario:

1. Baseline telemetry
2. Simulated deploy marker for the target service
3. Activate flagd fault
4. Detector findings
5. Incident before/during/after snapshot
6. Correlate incident ↔ changes (no scenario labels in scoring)
7. Recover and evaluate top-1 / top-3 attribution

Results: `examples/astronomy-shop/change/results/` and `examples/astronomy-shop/eval/results/` (gitignored).
