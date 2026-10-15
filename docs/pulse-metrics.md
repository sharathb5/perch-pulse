# Perch Pulse — verified engineering metrics

Defensible metrics from **existing** harnesses only.  
**Offline fixture success ≠ production model quality. Citation resolution ≠ semantic entailment.**

Date of this measurement session: **2026-10-15** (local macOS, repo `sharathb5/perch-pulse` @ branch `fix/final-demo-stabilization`).  
Disk: ~24 GiB free at start; ~22 GiB after live smoke. Astronomy Shop containers were **already running** (reused; no rebuild).

## Compact verified table

| Metric | Value | Numerator / denominator | Source | Sample | Command | Env | LIVE/OFFLINE | Limitations |
|--------|-------|-------------------------|--------|--------|---------|-----|--------------|-------------|
| Live detection latency (`error-payment`) | **≈70.3 s** | fault_on→first_detected wall clock | `live-fault-smoke` summary | n=1 run | See [procedure](#live-error-payment-2026-10-15) | local shop + Prom | **LIVE** | Single controlled run; not a distribution |
| Live recovery latency (`error-payment`) | **≈130.0 s** | fault_off→recovered_at wall clock | same | n=1 | same | same | **LIVE** | Measured recovery (not flag-inferred) |
| Live payment attribution | **PASS** | required target `astronomy/local/payment` + `error_rate` | same | n=1 | same | same | **LIVE** | Collateral latency incidents on other services also observed |
| Payment error_rate during fault | **0.5** | Prom/Live Pulse sample | `live-services-during-fault.json` | 1 snapshot | same | same | **LIVE** | Baseline was 0; ambient Locust traffic |
| F1 mechanical pass (F2c corpus) | **7/9** | 7 pass / 9 cases | `investigation-eval` dataset `f2c` | n=9 synthetic cases | `go run ./cmd/perch pulse investigation-eval --dataset f2c --out-dir /tmp/pulse-metrics-f2c --json` | local Go | **OFFLINE** | Harness/validator behavior; not live telemetry |
| F1 mechanical pass (F2b astronomy fixtures) | **13/15** | 13 / 15 | `investigation-eval` dataset `f2b` | n=15 | `go run ./cmd/perch pulse investigation-eval --dataset f2b --out-dir /tmp/pulse-metrics-f2b --json` | local Go | **OFFLINE** | Same |
| LLM mechanical pass among available (F2c) | **6/6** | 6 / 6 available (6/9 have proposals) | F2c report aggregate | n=6 synthetic proposals | same F2c command | local Go | **OFFLINE** | Synthetic fixtures ≠ live model quality; 3 missing proposals excluded from pass denom |
| LLM mechanical pass among available (F2b) | **7/8** | 7 / 8 available (8/15 have proposals) | F2b report aggregate | n=8 synthetic | same F2b command | local Go | **OFFLINE** | 1 false-causal + 1 unresolved citation in LLM totals |
| Unresolved citations (F1, F2c) | **0** | 0 unresolved across 9 F1 cases | F2c `unresolved_citations_deterministic_total` | n=9 | F2c command | local Go | **OFFLINE** | Mechanical ID resolve only |
| Unresolved citations (F1, F2b) | **0** | 0 / F1 cases | F2b aggregate | n=15 | F2b command | local Go | **OFFLINE** | Mechanical ID resolve only |
| False causal claims (F1 F2c/F2b) | **0** | phrase-heuristic totals | F2c/F2b aggregates | n=9 / n=15 | above | local Go | **OFFLINE** | Heuristic, not NLP entailment |
| Detector unit + evaluate package | **all PASS** | test suite | `internal/pulse/detect`, `evaluate`, `live/test` | suite size as of HEAD | `go test ./internal/pulse/detect/... ./internal/pulse/evaluate/... ./internal/pulse/live/test/... -count=1` | local Go | **OFFLINE** | Synthetic Prom fixtures; **not** classical precision/recall |
| Change ranking unit eval | **PASS** (`TestChangeEvalTop1Top3`) | fixture cases in package | `internal/pulse/changeeval` via correlate tests | small fixtures | `go test ./internal/pulse/correlate/... ./internal/pulse/changeeval/... -count=1` | local Go | **OFFLINE** | Correlation ≠ causation |
| Live fault lifecycle (httptest Prom) | **PASS** | binary | `TestLive_FaultLifecycle_ShippingLatency` | 1 scenario fixture | `go test ./internal/pulse/live/test/... -count=1 -run FaultLifecycle` | local Go | **OFFLINE** | Not Astronomy Shop live |
| Databricks Delta demo ingest | **37 → 37** idempotent | 37 rows / 10 tables | dated evidence 2026-10-13 | 37 demo rows | see [`verification-matrix.md`](verification-matrix.md#2026-10-13--live-databricks-delta--mlflow-demo-sample) | Databricks warehouse `e988309748ef6ac6` | **LIVE** (prior session) | Demo/synthetic export only |
| MLflow eval log | run `193935ec…` FINISHED | 1 authorized run | same | 1 offline eval report | same | Databricks MLflow | **LIVE** (prior session) | Not real-model SLA |

## Metrics that are **not** claimed here

| Requested metric | Status |
|------------------|--------|
| Classical detector **precision / recall** | **Not published** as aggregates. Offline unit counters exist; live evidence is a single `error-payment` lifecycle PASS, not a multi-scenario scorecard. |
| Live **correct service attribution** rate | **1/1** on the required payment target for the 2026-10-15 smoke; not a multi-scenario rate. Offline unit coverage remains. |
| Browser UI ACTIVE→Investigate during live fault | **Not measured** (no second injection; no Playwright against live viz in ACTIVE window). |
| Semantic correctness of LLM proposals | **NOT EVALUATED** (human rubric empty; real provider **NOT RUN** this RC). |
| End-to-end smoke wall-clock | Must not be cited as algorithm latency. |

## Reproduce investigation metrics

```bash
go run ./cmd/perch pulse investigation-eval --dataset f2c --out-dir /tmp/pulse-metrics-f2c --json
go run ./cmd/perch pulse investigation-eval --dataset f2b --out-dir /tmp/pulse-metrics-f2b --json
# Inspect aggregate blocks in report.json / report.md
```

## Reproduce offline detector / live-runtime checks

```bash
go test ./internal/pulse/detect/... ./internal/pulse/evaluate/... ./internal/pulse/telem/... -count=1
go test ./internal/pulse/live/test/... -count=1
go test ./internal/pulse/correlate/... ./internal/pulse/changeeval/... -count=1
```

## Live `error-payment` (2026-10-15)

Reuse a healthy existing Astronomy Shop (do **not** use full `ci-live-fault.sh` if you must avoid `stop.sh` tearing down containers). Equivalent procedure used this session:

```bash
# From examples/astronomy-shop with Live Pulse env set:
# PERCH_LIVE_PULSE_ENABLED=1 PERCH_LIVE_PULSE_PROM_URL=http://127.0.0.1:9090 …
perch viz --env local --port 18765
go build -o /tmp/live-fault-smoke ./examples/astronomy-shop/cmd/live-fault-smoke
/tmp/live-fault-smoke \
  --viz-base-url http://127.0.0.1:18765 \
  --flagd-api-base http://127.0.0.1:8080/feature/api \
  --artifact-dir examples/astronomy-shop/.smoke-artifacts/live-fault-YYYYMMDD \
  --scenario-id error-payment \
  --target-service astronomy/local/payment \
  --expect-signal error_rate
# Always: go run ./examples/astronomy-shop/scenarios/cmd/scenario stop
```

Timestamps (UTC) from `live-fault-summary.txt`: baseline ready `05:25:30.205269Z`; fault on `05:25:30.207514Z`; active `05:26:40.520133Z`; fault off `05:27:00.491031Z`; recovered `05:29:10.52155Z`. Details: [`verification-matrix.md`](verification-matrix.md#2026-10-15--live-error-payment-fault-lifecycle).

Multi-scenario `run-detector-eval.sh` was **not** run.

## Suggested resume-ready lines (substantiated only)

1. **Live payment fault lifecycle (2026-10-15):** detected `error_rate` incident on `astronomy/local/payment` in **≈70 s** after flagd `paymentFailure=50%`; measured recovery in **≈130 s** after fault clear (single controlled run).
2. **Offline F1 investigation harness:** deterministic mechanical pass **13/15** (F2b) and **7/9** (F2c) with **0** unresolved F1 citations (2026-10-15 offline).
3. **Optional warehouse path:** authorized 2026-10-13 demo load of **37** JSONL rows into **10** Delta tables with idempotent rerun **37→37**, plus live MLflow persistence of an offline eval report.

Do not cite the single live smoke as classical detector precision/recall.
