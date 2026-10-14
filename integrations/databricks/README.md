# Perch Pulse ↔ Databricks + MLflow (Milestone F3b)

Optional Python integration that loads **F3a** versioned JSONL exports into
Databricks Delta tables and logs investigation-evaluation runs to MLflow.

**Core Perch CLI/UI do not require this package, Python, Databricks, or MLflow.**

## Status honesty

| Path | Default | Cloud calls |
|------|---------|-------------|
| `validate` / `dry-run` | yes | **never** |
| unit tests | CI | **never** |
| `live-load` / `live-smoke` | opt-in | only with explicit authorize flags |
| `mlflow-log --live` | opt-in | only with `--i-authorize-mlflow-calls` |

Do not claim cloud execution unless `live_executed=true` / `rows_merged=true` was observed for that run.

**Dated live demo validation (2026-10-13):** authorized load of the 37-row sample export into `workspace.analytics_*` on warehouse `e988309748ef6ac6`, identical-rerun counts unchanged, and MLflow run `193935ec502f4496a5a7a1fc44118f6f` under `/Users/sharathjb23@gmail.com/perch-pulse-investigation-eval`. Details: [`docs/verification-matrix.md`](../../docs/verification-matrix.md#2026-10-13--live-databricks-delta--mlflow-demo-sample). Demo/synthetic data only — not production telemetry or live-model quality.

## Install

```bash
cd integrations/databricks
python3 -m venv .venv
source .venv/bin/activate
pip install -e ".[test]"                 # offline tests only
# Optional cloud extras (not needed for CI):
pip install -e ".[databricks,mlflow]"
```

Pinned optional deps: `databricks-sdk==0.67.0`, `mlflow==2.22.1`.

## End-to-end walkthrough (implemented commands)

### 1) Produce a local F3a export (Go)

```bash
# From repo root — local JSONL only
go run ./cmd/perch pulse investigation-eval --dataset f2c --out-dir /tmp/inv-eval --json
go run ./cmd/perch pulse export-analytics \
  --out-dir /tmp/pulse-export \
  --eval-report /tmp/inv-eval/report.json \
  --incident-provenance observed_local \
  --json
```

Sample fixture (no Pulse dir required):

`examples/pulse-export/sample/export/`

### 2) Validate + dry-run (Python, offline)

```bash
cd integrations/databricks
python -m perch_pulse_dbx validate --export-dir ../../examples/pulse-export/sample/export --json
python -m perch_pulse_dbx dry-run --export-dir ../../examples/pulse-export/sample/export --json
python -m perch_pulse_dbx dry-run --export-dir ../../examples/pulse-export/sample/export --out /tmp/merge-plan.json
# Offline staging + MERGE simulation (FakeSQLExecutor; no cloud):
python -m perch_pulse_dbx dry-run --export-dir ../../examples/pulse-export/sample/export --simulate-merge --json
python -m perch_pulse_dbx verify-lineage --export-dir ../../examples/pulse-export/sample/export --json
```

Dry-run emits planned tables, validated counts, merge keys, and SQL previews.
`--simulate-merge` / `verify-lineage` exercise the same staging→MERGE→readback path
in memory (idempotent rerun). They do **not** invent cloud success or cost.

### 3) Offline MLflow logging (fake client)

```bash
python -m perch_pulse_dbx mlflow-log \
  --report ../../examples/pulse-export/sample/eval-report.json \
  --json
```

Missing token/cost metrics are labeled unavailable — never written as `0`.

### 4) Optional live Databricks smoke (authorization required)

Prerequisites:

- Existing SQL warehouse ID (do **not** auto-create paid clusters)
- Approved catalog/schema namespace
- `DATABRICKS_HOST`, `DATABRICKS_TOKEN`, `DATABRICKS_WAREHOUSE_ID`
- Explicit user authorization + bounded cost budget

```bash
export DATABRICKS_HOST=https://…cloud.databricks.com
export DATABRICKS_TOKEN=…          # never commit
export DATABRICKS_WAREHOUSE_ID=…

# Connection-only (default): SELECT 1 — does NOT claim rows_merged
python -m perch_pulse_dbx live-load \
  --export-dir ../../examples/pulse-export/sample/export \
  --warehouse-id "$DATABRICKS_WAREHOUSE_ID" \
  --i-authorize-databricks-calls \
  --connection-only \
  --json

# Full staging + append-only MERGE + lineage readback (bounded batch):
python -m perch_pulse_dbx live-load \
  --export-dir ../../examples/pulse-export/sample/export \
  --warehouse-id "$DATABRICKS_WAREHOUSE_ID" \
  --i-authorize-databricks-calls \
  --execute-merge \
  --max-batch 50 \
  --json

python -m perch_pulse_dbx live-smoke \
  --export-dir ../../examples/pulse-export/sample/export \
  --report ../../examples/pulse-export/sample/eval-report.json \
  --catalog perch_pulse \
  --schema analytics \
  --warehouse-id "$DATABRICKS_WAREHOUSE_ID" \
  --i-authorize-databricks-calls \
  --i-authorize-mlflow-calls \
  --execute-merge \
  --max-batch 50 \
  --json
```

Without authorize flags, live steps are reported **NOT RUN**.  
Without `--execute-merge`, report **LIVE DELTA INGESTION = NOT RUN** even if `SELECT 1` succeeded.

## Architecture

```
F3a JSONL export
  → validate schemas / provenance / checkpoint
  → dry-run MERGE plan (domain-isolated schemas)
  → [opt-in] warehouse connection check (SELECT 1)
  → [opt-in --execute-merge] CREATE SCHEMA/TABLE → stage stg_* → MERGE → COUNT/key readback
  → [opt-in] MLflow eval metrics + redacted artifact
```

### Datasets → Delta tables

| Dataset | Table |
|---------|-------|
| `incidents` | `pulse_incidents` |
| `incident_signals` | `pulse_incident_signals` |
| `investigations` | `pulse_investigations` |
| `hypotheses` | `pulse_hypotheses` |
| `citations` | `pulse_citations` |
| `investigation_feedback` | `pulse_investigation_feedback` |
| `proposals` | `pulse_proposals` |
| `eval_runs` | `pulse_eval_runs` |
| `eval_case_results` | `pulse_eval_case_results` |

### Provenance isolation

Schemas: `{base}_observed`, `{base}_synthetic`, `{base}_model`, `{base}_human`,
`{base}_evaluation`, `{base}_unknown`.

Synthetic ground truth must never be fed back into agent generation inputs.

### MERGE semantics

- Keys: `(dedupe_key, content_fingerprint)` — append-only fingerprint history
- Same key + same fingerprint → skip (idempotent)
- Same key + new fingerprint → INSERT a new history row (matches F3a JSONL lines)
- New key → INSERT
- **Missing key in new export → NOT DELETE**
- `live-load --connection-only` / default live-smoke without `--execute-merge`:
  connectivity only (`rows_merged=false`)
- `live-load --execute-merge`: lands `stg_*` tables from JSONL batches, runs
  append-only MERGE, records count/key lineage (`rows_merged=true` only on success)
- Offline `verify-lineage` simulates that path with FakeSQLExecutor (not cloud)

### MLflow

Logs F2b/F2c aggregates with explicit denominators, method tags, evidence
fingerprints (bounded), and a compact redacted artifact. Repeated uploads with
the same eval_run_id/dataset are updated in the fake client; live MLflow is
versioned by run name without claiming exactly-once.

## Tests

```bash
cd integrations/databricks
pip install -e ".[test]"
pytest
# or from repo root:
make databricks-integration-test
```

## Security / cost limits

- No credentials in logs or plan JSON
- SQL identifiers allowlisted (`^[A-Za-z_][A-Za-z0-9_]*$`)
- No string interpolation of row payloads into SQL in the planner
- No automatic cluster/warehouse creation
- No automatic cleanup of shared Unity Catalog objects
- Live calls require authorization flags; connection checks ≠ data load
- No invented cloud cost figures in dry-run output

See ADR-029 and `docs/pulse-export.md`.
