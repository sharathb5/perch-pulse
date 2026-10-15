# Perch Pulse — demo runbook

Developer-facing orchestration of **existing** components for a recruiter-ready demo.  
Primary scenario: **`error-payment`** (payment `error_rate` / flagd `paymentFailure`).

Modes (never silently mixed):

| Mode | What it uses | Label in artifacts |
|------|----------------|--------------------|
| **MOCK** (default) | Synthetic fixtures, offline eval/export from an **isolated empty** `--pulse-dir` under `--out-dir` | `synthetic_fixture` |
| **REPLAY** | Labeled historical sample export + UI fixtures (not ambient `~/.perch`) | sample / `historical_demo` |
| **LIVE** | Real Astronomy Shop telemetry + real fault | observed local / live collector |

Orchestrator: [`scripts/demo/pulse-demo.sh`](../scripts/demo/pulse-demo.sh)

**LIVE recording (presenter commands):** Prefer [`docs/demo-recording.md`](demo-recording.md) and `make demo-up|status|fault|recover|down` ([`scripts/demo/recording.sh`](../scripts/demo/recording.sh)). Those targets reuse a healthy Astronomy Shop, never inject a fault on `demo-up`, and do not stop shop containers on `demo-down`. Do not use `pulse-demo.sh --mode live --phase all` for recording — it delegates to `ci-live-fault.sh`, which stops Docker on cleanup.

**Provenance rule:** mock/replay never export ambient `PERCH_PULSE_DIR` / `~/.perch` records as synthetic. To export real persisted Pulse data, pass `--use-real-pulse-dir DIR` with `--mode live` (labels `observed_local`). The export CLI also refuses `synthetic_fixture` when the pulse-dir is non-empty.

```bash
./scripts/demo/pulse-demo.sh --mode mock --phase all
./scripts/demo/pulse-demo.sh --mode mock --phase offline
./scripts/demo/pulse-demo.sh --mode replay --phase offline
./scripts/demo/pulse-demo.sh --mode live --phase all   # heavy; disk hard-stop <15 GiB
# Explicit real-data export (after live detection wrote incidents):
./scripts/demo/pulse-demo.sh --mode live --phase offline --use-real-pulse-dir "$HOME/.perch/pulse"
```

---

## Preflight

- **Dependencies:** Go (see `go.mod`), Node for UI (`web/`), Python 3 for optional Databricks package, Docker only for LIVE.
- **Disk:** `df -h /` — hard stop under **15 GiB** for LIVE/Docker. Offline mock may warn and continue.
- **Ports (LIVE):** frontend `8080`, Prometheus `9090`, Jaeger `16686`, flagd `8081` (typical Astronomy Shop minimal).
- **Telemetry endpoints (LIVE):** Prometheus metrics for payment error rate; Jaeger for traces (Run Pulse).
- **Incident storage:** local Pulse dir under `~/.perch` (never commit; demo script does not wipe it).
- **Privacy / credentials:** no API keys in repo; LLM/Databricks env vars only on operator machine; viz is localhost-oriented.

```bash
./scripts/demo/pulse-demo.sh --mode mock --phase preflight --dry-run
```

---

## Live demonstration (`error-payment`)

Prefer GitHub `astronomy-smoke` / `ci-live-fault` when local disk is tight. Do not invent detector success from flagd-only activation.

**Env names only (LIVE viz):** `PERCH_LIVE_PULSE_ENABLED`, `PERCH_LIVE_PULSE_PROM_URL` (e.g. `http://127.0.0.1:9090`), optional `PERCH_LIVE_PULSE_POLL_INTERVAL` / `PERCH_LIVE_PULSE_FRESHNESS`, `PERCH_PULSE_DIR`, `PERCH_RUN_PULSE_DEMO_BASE_URL`. Never commit values.

**UI refresh:** Pulse + live incidents poll every **8s**; graph/status every **10s**; live collector default **15s**. No manual Perch restart required for incoming telemetry once Live Pulse is enabled.

1. **Healthy baseline** — shop up; payment errors near zero; enable Live Pulse; wait until collector leaves `warming_up` (baseline windows).
2. **Fault enablement** — `./examples/astronomy-shop/scenarios/scenario.sh start error-payment`
3. **Incident detection** — wait for detector / `live-fault-smoke` expect `error_rate` on payment (`astronomy/local/payment`).
4. **Inspection** — `perch viz` → Pulse sidebar → incident (ACTIVE) → Investigate (F1). Optional Run Pulse / LLM only if configured.
5. **Fault disablement** — `scenario.sh stop error-payment` (mandatory even on failure).
6. **Observed recovery** — incident lifecycle → RECOVERED; record remains historical (not a current outage).

Scripted path:

```bash
ASTRONOMY_LIVE_FAULT_SCENARIO=error-payment ./examples/astronomy-shop/scripts/ci-live-fault.sh
```

**Honesty:** 2026-10-09 verified shop telemetry + flagd scenario activation/recovery. **2026-10-15** verified live-fault→Live Pulse API ACTIVE→RECOVERED for `error-payment` (detection ≈70 s, recovery ≈130 s) on existing containers — see [`verification-matrix.md`](verification-matrix.md#2026-10-15--live-error-payment-fault-lifecycle). Browser sidebar during the ACTIVE window was **not** re-verified that session.

---

## Investigation

1. Open incident → **Investigate** → F1 immutable evidence snapshot (OBSERVED / HYPOTHESIS / UNKNOWN).
2. Optional LLM (server opt-in + credentials): **Generate AI Investigation** — separate proposal; citations must resolve.
3. CLI offline smoke (default; no network):

```bash
go run ./cmd/perch pulse llm-provider-smoke --case-id case_payment_error --json
```

4. Real model (billable; both flags required — **do not run without budget approval**):

```bash
go run ./cmd/perch pulse llm-provider-smoke --case-id case_payment_error \
  --real-provider --i-authorize-provider-calls --json
```

5. Submit human feedback in the sidebar (subjective usefulness, not ground truth).
6. **Run Pulse** tab for explicit cross-service diagnostics (not automatic root-cause proof).

Setup checklist (F2c real smoke):

| Variable / flag | Required |
|-----------------|----------|
| `PERCH_PULSE_LLM_ENABLED=1` | for viz propose |
| `PERCH_PULSE_LLM_API_KEY` | never commit |
| `PERCH_PULSE_LLM_BASE_URL` | OpenAI-compatible |
| `PERCH_PULSE_LLM_MODEL` | documented target `gpt-4o-mini` |
| `--real-provider` **and** `--i-authorize-provider-calls` | CLI smoke |

Without authorization: **REAL LLM VALIDATION = NOT RUN**.

---

## Analysis

```bash
./scripts/demo/pulse-demo.sh --mode mock --phase offline
```

This runs:

1. `investigation-eval --dataset f2c`
2. Offline `llm-provider-smoke`
3. `export-analytics` JSONL
4. `perch_pulse_dbx validate` / `dry-run --simulate-merge` / `verify-lineage`
5. Offline `mlflow-log` (fake client)

Optional authorized cloud:

```bash
export DATABRICKS_HOST=…
export DATABRICKS_TOKEN=…          # never commit
export DATABRICKS_WAREHOUSE_ID=…   # existing warehouse only

./scripts/demo/pulse-demo.sh --mode mock --phase offline \
  --i-authorize-databricks-calls --i-authorize-mlflow-calls
# Staging+MERGE (not connection-only):
PULSE_DEMO_EXECUTE_MERGE=1 ./scripts/demo/pulse-demo.sh …
```

Without flags: **LIVE DELTA INGESTION = NOT RUN**, **LIVE MLFLOW TRACKING = NOT RUN**.

Dated one-time live PASS (demo sample only): **2026-10-13** — see [`verification-matrix.md`](verification-matrix.md#2026-10-13--live-databricks-delta--mlflow-demo-sample). Do not re-run paid cloud paths without a fresh authorization budget.

---

## Aftercare

- Ensure `error-payment` (and any other scenarios) are **stopped**.
- Summarize artifacts under `.perch/demo-out/` (or `--out-dir` under the repo).
- Preserve evidence; **do not overwrite** real investigation records to “reset” a demo.
- Leave Docker/shop in a safe state; no unpaid clusters created by this tooling.

---

## Failure safety

- Scripts use `set -euo pipefail` and fail closed on missing deps.
- Offline paths never claim `real_provider_executed` / `rows_merged` from fixtures alone.
- Connection-only Databricks ≠ completed ingestion.
- Historical replay artifacts must keep `historical_demo` / UI “recovered historical” labels.
