# Sample Pulse analytical export (F3a)

Offline fixture notes for the investigation data pipeline.

## Generate a sample

From the repo root (no Docker, no Databricks):

```bash
# Optional: produce an eval report for inclusion
go run ./cmd/perch pulse investigation-eval --out-dir /tmp/pulse-eval --json >/dev/null

# Seed data comes from your local --pulse-dir; for CI, unit tests build a temp store.
go run ./cmd/perch pulse export-analytics \
  --pulse-dir "${PERCH_PULSE_DIR:-.perch/pulse}" \
  --out-dir /tmp/pulse-export-sample \
  --eval-report /tmp/pulse-eval/report.json \
  --json
```

Unit tests under `internal/pulse/export/test` write deterministic fixture exports
and assert quality-report OK, idempotency, batch resume, and path safety.

Regenerate the checked-in sample:

```bash
PULSE_EXPORT_SAMPLE_DIR=$PWD/examples/pulse-export/sample \
  go test ./internal/pulse/export/test/ -run TestWriteSampleFixture -count=1
```

## Expected artifacts

- `sample/export/*.jsonl` — versioned analytical rows
- `sample/export/checkpoint.json` — incremental idempotency state
- `sample/export/export-manifest.json` — dataset row counts
- `sample/export/quality-report.json` — offline data-quality report
- `sample/eval-report.json` — optional F2b report used as export input

Sample row counts (historical_demo + eval): incidents 1, incident_signals 2,
investigations 1, hypotheses 4, citations 10, investigation_feedback 2,
proposals 1, eval_runs 1, eval_case_results 15.

See [`docs/pulse-export.md`](../../docs/pulse-export.md).
