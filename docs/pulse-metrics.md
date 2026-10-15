# Perch Pulse — verified engineering metrics

Defensible metrics from **existing** harnesses only.  
**Offline fixture success ≠ production model quality. Citation resolution ≠ semantic entailment.**

Date of this measurement session: **2026-10-15** (local macOS, repo `sharathb5/perch-pulse` @ branch `fix/final-demo-stabilization`).  
Disk at session start: ~24–25 GiB free on `/`. Astronomy Shop containers were **already running**; no detector live-fault smoke was executed in this session (requires human approval).

## Compact verified table

| Metric | Value | Numerator / denominator | Source | Sample | Command | Env | LIVE/OFFLINE | Limitations |
|--------|-------|-------------------------|--------|--------|---------|-----|--------------|-------------|
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
| Classical detector **precision / recall** | **Not published.** Closest offline signals are fault-detection and control-FP counters in `internal/pulse/evaluate` (`pulse.eval.v1`), exercised by unit tests — no dated live scorecard. |
| Live **detection latency** / **recovery latency** | **Not measured this session.** Requires approved `ci-live-fault.sh` / `run-detector-eval.sh` against running shop + Prometheus. Prior live-fault PASS is **undocumented** in the verification matrix. |
| Live **correct service attribution** rate | **OFFLINE unit coverage only** (evaluate + detect attribution tests). Live rate **IMPLEMENTED BUT UNVERIFIED**. |
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

## Live detector eval (requires approval + disk ≥ 15 GiB)

```bash
# Prefer reusing a healthy existing Astronomy Shop stack.
./examples/astronomy-shop/eval/run-detector-eval.sh
# Optional lifecycle smoke (error-payment):
./examples/astronomy-shop/scripts/ci-live-fault.sh
```

Do **not** cite results until a dated PASS with artifacts is recorded.

## Suggested resume-ready lines (substantiated only)

1. **Offline F1 investigation harness:** deterministic mechanical pass **13/15** (F2b) and **7/9** (F2c) with **0** unresolved F1 citations on both corpora (2026-10-15 offline).
2. **Evidence discipline:** synthetic LLM proposals on F2c achieve **6/6** mechanical pass among available proposals with **0** false-causal totals; mechanical pass ≠ semantic correctness.
3. **Optional warehouse path:** authorized 2026-10-13 demo load of **37** JSONL rows into **10** Delta tables with idempotent rerun **37→37**, plus live MLflow persistence of an offline eval report.

If a stronger “detector precision” headline is required, run the approved live Astronomy Shop detector eval and publish denominators — do not invent them.
