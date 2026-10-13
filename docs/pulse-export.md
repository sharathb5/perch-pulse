# Pulse investigation analytical export (Milestone F3a)

Local, Databricks-ready export of Pulse incident / investigation / proposal /
feedback / evaluation records. **No Databricks workspace, cluster, or cloud
credentials are used.**

## Command

```bash
perch pulse export-analytics --out-dir ./tmp/pulse-export --json
perch pulse export-analytics --inventory
perch pulse export-analytics --out-dir ./tmp/pulse-export \
  --eval-report ./tmp/eval/report.json \
  --incident-provenance observed_local
```

Flags:

| Flag | Meaning |
|------|---------|
| `--out-dir` | Required export directory (JSONL + checkpoint) |
| `--allowed-root` | Reject path traversal / symlink escape outside root (default: cwd) |
| `--max-batch` | Bound new rows per run (default 500) |
| `--eval-report` | Optional `pulse.investigation.eval_report.v1` JSON |
| `--incident-provenance` | Label for pulse-dir incidents/investigations |
| `--inventory` | Print data inventory and exit |

Pulse sources are read from `--pulse-dir` / `PERCH_PULSE_DIR` / `.perch/pulse`.
**Source files are never modified.**

## Export format

- Version: `pulse.export.analytics.v1`
- Files: `<dataset>.jsonl` (UTF-8, one JSON object per line, `\n` terminated)
- Sidecars: `checkpoint.json`, `export-manifest.json`, `quality-report.json`
- Timestamps: UTC RFC3339 via Go `time.Time` JSON encoding
- Ordering: deterministic by `dedupe_key` ascending within each transform

### Datasets

| Dataset | Primary key (`dedupe_key`) | Source |
|---------|----------------------------|--------|
| `incidents` | `incident_id` | `pulse.incident.v1` |
| `incident_signals` | `incident_id\|phase\|signal` | incident digests |
| `investigations` | `investigation_id` | `pulse.investigation.v1` |
| `hypotheses` | `parent_kind\|parent_id\|hypothesis_id\|rejected` | F1 + proposals |
| `citations` | `investigation_id\|ref` | investigation `source_references` |
| `investigation_feedback` | `target_kind\|feedback_id` | F1 + proposal feedback |
| `proposals` | `proposal_id` | `pulse.investigation.proposal.v1` |
| `eval_runs` | `eval_run_id` | optional eval report |
| `eval_case_results` | `eval_run_id\|case_id` | optional eval report |

Observed evidence (`incidents`, `incident_signals`, `citations`) is separate from
inferred `hypotheses`, subjective `investigation_feedback`, and evaluation labels.

Hypothesis **statement text is not exported** (sha256 hash + rune count only).
Provider prompts/responses, auth headers, raw logs, and unrestricted traces are
never exported.

## Checkpoint model

`checkpoint.json` (`pulse.export.checkpoint.v1`):

```json
{
  "schema_version": "pulse.export.checkpoint.v1",
  "export_schema_version": "pulse.export.analytics.v1",
  "updated_at": "...",
  "datasets": {
    "incidents": {
      "keys": { "<dedupe_key>": "<content_fingerprint>" },
      "last_key": "...",
      "row_count": 1
    }
  }
}
```

- Idempotent: same key + fingerprint → skip
- Content change: new fingerprint → emit another JSONL line (warehouse MERGE later; quality report allows fingerprint history)
- Checkpoint advances after each successful dataset JSONL rename
- On startup, checkpoint keys are reconciled from existing JSONL so a crash between rename and checkpoint save does not duplicate rows
- Failed writes leave prior checkpoint and prior JSONL intact
- Stale `*.tmp` files are removed at run start
- Export returns an error if the post-commit quality report is not OK

## Provenance

| Class | Use |
|-------|-----|
| `observed_local` | Default for pulse-dir FileStore reads |
| `historical_demo` | Explicit operator label for demo corpora |
| `synthetic_fixture` | Builtin eval fixtures (when labeled) |
| `model_generated` | LLM proposals / their hypotheses |
| `human_assessment` | Feedback rows |
| `evaluation_annotation` | Eval **run** aggregate rows |
| `synthetic_fixture` | Builtin eval **case** result rows (`BuiltinDatasetID`) |
| `unknown` | Eval case rows from unrecognized datasets; or when classification cannot be established |

Do not silently join synthetic eval rows with live incidents without filtering
`provenance_class`.

## Validation

Each successful export writes `quality-report.json` covering schema compliance,
stable IDs, duplicates, joins, timestamps, redaction suspects, fingerprints,
checkpoint consistency, and orphaned feedback.

## F3b implementation (optional Python package)

Executable offline validation, dry-run MERGE planning, and opt-in live load /
MLflow logging live under [`integrations/databricks/`](../integrations/databricks/).
See [ADR-029](DECISIONS.md). **Core Perch does not import this package.**

```bash
cd integrations/databricks && pip install -e ".[test]"
python -m perch_pulse_dbx validate --export-dir ../../examples/pulse-export/sample/export
python -m perch_pulse_dbx dry-run --export-dir ../../examples/pulse-export/sample/export --json
python -m perch_pulse_dbx dry-run --export-dir ../../examples/pulse-export/sample/export --simulate-merge --json
python -m perch_pulse_dbx verify-lineage --export-dir ../../examples/pulse-export/sample/export --json
python -m perch_pulse_dbx mlflow-log --report ../../examples/pulse-export/sample/eval-report.json --json
make -C ../.. databricks-integration-test
```

Live Databricks/MLflow requires explicit authorize flags and an existing warehouse.
Use `live-load --execute-merge` for staging+MERGE+lineage; `--connection-only`
is SELECT 1 only (`rows_merged=false`). F3a still does **not** perform
Delta/MLflow execution by itself.

---

## Data inventory

See also `perch pulse export-analytics --inventory` (generated from the same
source list as `export.Inventory()`).

Supported FileStores: `incidents/`, `investigations/` (+ feedback),
`investigation-proposals/` (+ feedback), optional eval `report.json`.

Not exported as datasets: raw observation blobs, RunPulse full payloads (only
`linked_run_pulse_count` on investigations), provider transcripts.

---

## F3b: Databricks + MLflow design notes

Executable tooling is in `integrations/databricks/` (ADR-029). The notes below
remain the warehouse design constraints. **Do not provision clusters or store
cloud credentials in this repository.**

### Loading JSONL into Delta

**Implemented path** (`perch_pulse_dbx` with `--execute-merge`): validate export →
CREATE SCHEMA/TABLE → `stg_*` land from JSONL batches → append-only MERGE on
`(dedupe_key, content_fingerprint)` → COUNT/sample-key lineage.

**Alternate operator path:** land versioned export dirs as cloud objects, then
Autoloader / `COPY INTO` into staging before the same MERGE keys.

### Suggested keys

| Delta table | Merge keys |
|-------------|-----------------|
| all `pulse_*` datasets | `(dedupe_key, content_fingerprint)` append-only |

### Incremental MERGE

```sql
MERGE INTO pulse_investigations t
USING stg_pulse_investigations s
ON t.dedupe_key = s.dedupe_key
AND t.content_fingerprint = s.content_fingerprint
WHEN NOT MATCHED THEN INSERT *
-- absence never DELETE; fingerprint history retained
```

### Provenance / lineage

- Retain `provenance_class`, `source_schema_version`, `exported_at`, `observation_at`.
- Tag Unity Catalog tables with export manifest hash.
- Separate schemas/catalogs for `synthetic` vs `observed_local`.

### Evaluation + MLflow

- Register eval datasets from `eval_case_results` + frozen evidence fingerprints.
- Log MLflow metrics from `eval_runs` aggregates with **explicit denominators**
  (never invent LLM scores for missing proposals).
- Compare agents by `generation_method` joins to hypotheses/proposals.
- Human feedback metrics stay in a separate experiment tag (`subjective_only`).

### Historical retrieval

- Point investigations at `content_fingerprint` + `evidence_collected_at` for
  reproducible replay; do not treat warehouse joins as causation.

### Retention and cost

- Hot Delta: 90 days; cold clone / archive JSONL exports longer.
- Partition by `exported_at` date and `provenance_class`.
- Cap Autoloader concurrency; prefer serverless SQL warehouses over always-on clusters.
- Redaction job rejects rows failing privacy allowlist before MERGE.

---

## Limitations (F3a)

- No actual Databricks or MLflow execution.
- Checkpoint key maps are suitable for local retention-bounded stores, not
  multi-million-row warehouses (F3b should externalize watermarks).
- Incident upserts with changed content emit additional JSONL lines; consumers
  must MERGE by fingerprint.
- Eval cases themselves are not exported as a warehouse dataset—only scored
  reports via `--eval-report`.
