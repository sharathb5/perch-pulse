# Perch Pulse — verification matrix

Honest status of the end-to-end investigation journey.  
**Offline test success ≠ live integration validation.**

Legend:

| Label | Meaning |
|-------|---------|
| LIVE VERIFIED | Ran against real telemetry/cloud with dated evidence |
| OFFLINE VERIFIED | Automated fixtures / unit / Playwright / dry-run |
| IMPLEMENTED BUT UNVERIFIED | Code path exists; no successful dated live run recorded here |
| MISSING | Not implemented as a product surface |
| BLOCKED BY AUTHORIZATION | Requires explicit human approve + budget; not run in this RC |

## Journey (18 steps)

| # | Step | Status | Evidence / notes |
|---|------|--------|------------------|
| 1 | Astronomy Shop emits telemetry | **LIVE VERIFIED** | [`astronomy-shop.md`](astronomy-shop.md) 2026-10-09 `verify-live.sh` |
| 2 | Pulse observes a baseline | **OFFLINE VERIFIED**; live **IMPLEMENTED BUT UNVERIFIED** | `internal/pulse/live` fixture tests; live wait in `ci-live-fault.sh` |
| 3 | Real fault increases errors/latency | **LIVE VERIFIED** (injection) | 2026-10-09 `error-payment` flagd `paymentFailure=50%` |
| 4 | Pulse detects an incident | **OFFLINE VERIFIED**; live detector **IMPLEMENTED BUT UNVERIFIED** | Pipeline + `live-fault-smoke`; no dated live-fault PASS in docs |
| 5 | Incident in graph/sidebar | **OFFLINE VERIFIED** (Playwright fixtures) | `web/e2e/pulse-sidebar.spec.js` |
| 6 | Engineer opens investigation | **OFFLINE VERIFIED** | Playwright Investigate flow |
| 7 | F1 immutable evidence | **OFFLINE VERIFIED** | `internal/pulse/investigation`, fingerprint immutability tests |
| 8 | Deterministic facts / hypotheses / unknowns | **OFFLINE VERIFIED** | UI badges + Go rules `deterministic_rules_v1` |
| 9 | Optional LLM proposal | **OFFLINE VERIFIED**; real calls **BLOCKED BY AUTHORIZATION** | `llm-provider-smoke` offline default |
| 10 | Citations validated | **OFFLINE VERIFIED** | Mechanical resolve; ≠ semantic entailment |
| 11 | Inspect F1 vs LLM | **OFFLINE VERIFIED** (UI + eval); live model compare **BLOCKED BY AUTHORIZATION** | Sidebar + `investigation-eval` |
| 12 | Explicit Run Pulse | **OFFLINE VERIFIED** + smoke tooling | [`pulse-run.md`](pulse-run.md); GHA dispatch-only for full shop |
| 13 | Human feedback | **OFFLINE VERIFIED** | Playwright + FileStore API |
| 14 | Evaluation compares methods | **OFFLINE VERIFIED** | F2b/F2c corpora; explicit denominators — see [`pulse-metrics.md`](pulse-metrics.md) (2026-10-15) |
| 15 | JSONL analytical export | **OFFLINE VERIFIED** | F3a `export-analytics` + sample fixture |
| 16 | Databricks Delta ingest | **LIVE VERIFIED** (2026-10-13 demo sample); also **OFFLINE VERIFIED** | See [dated evidence](#2026-10-13--live-databricks-delta--mlflow-demo-sample). **Not re-executed** in 2026-10-15 stabilization session. |
| 17 | MLflow tracks evaluations | **LIVE VERIFIED** (2026-10-13 demo eval report); also **OFFLINE VERIFIED** (fake client) | Experiment `/Users/sharathjb23@gmail.com/perch-pulse-investigation-eval`; synthetic fixtures ≠ live model quality. **Not re-executed** 2026-10-15. |
| 18 | Engineer verifies lineage | **LIVE VERIFIED** (COUNT/key readback + idempotent rerun); also **OFFLINE VERIFIED** (fake) | Identical 37-row rerun left all table counts unchanged |

## UI refresh / demo reliability (2026-10-15 audit)

| Channel | Interval | Notes |
|---------|----------|-------|
| Pulse historical + live APIs | **8s** client poll | `web/src/hooks/usePulseData.js` — no WebSocket; failed refresh marks snapshot stale (stale ≠ healthy) |
| Graph + status | **10s** | `usePerchData.js` — active probes independent of Pulse |
| Run Pulse run status | **1.5s** while non-terminal | `usePulseRun.js` |
| Live collector (server) | **15s** default | `PERCH_LIVE_PULSE_POLL_INTERVAL`; freshness window default **2m** |
| Investigation / LLM | on demand | No background poll |

Investigation detail overlays polled list `lifecycle`/`liveSource` so an ACTIVE live incident does not flip to HISTORICAL when opening Investigate (`overlayIncidentLifecycle`).

## External validation flags (this release candidate)

| Check | Status |
|-------|--------|
| REAL LLM VALIDATION | **NOT RUN** |
| LIVE DELTA INGESTION | **EXECUTED** (2026-10-13) — demo/synthetic sample only |
| LIVE MLFLOW TRACKING | **EXECUTED** (2026-10-13) — offline eval report logged live; not real-model performance |
| Full Astronomy Shop live-fault → UI | Prefer prior 2026-10-09 shop evidence + lighter offline UI; do not re-run heavy Docker when disk &lt; 15 GiB |

## Automated gates (no paid services)

| Gate | Command |
|------|---------|
| Full local verify | `make verify` |
| Databricks offline | `make databricks-integration-test` |
| Web unit | `make web-test` |
| Playwright | `make web-e2e` |
| Demo dry-run | `./scripts/demo/pulse-demo.sh --mode mock --phase preflight --dry-run` |
| Demo offline | `./scripts/demo/pulse-demo.sh --mode mock --phase offline` |
| Metrics reproduce | See [`pulse-metrics.md`](pulse-metrics.md) |

## Previously verified live workflow evidence

- **2026-10-09** — Astronomy Shop minimal stack + scenario harness including `error-payment` activation/recovery ([`astronomy-shop.md`](astronomy-shop.md)).
- Live-fault CI shipping-latency attempt failed historically; CI scenario selected is `error-payment` ([`pulse-live.md`](pulse-live.md)).

### 2026-10-13 — Live Databricks Delta + MLflow (demo sample)

One-time authorized validation against workspace `dbc-1f1b342b-da90.cloud.databricks.com` using existing warehouse `e988309748ef6ac6` (Serverless Starter, 2X-Small, auto-stop 10 min). **No new compute created. No DROP/DELETE. No paid LLM calls.**

| Item | Value |
|------|--------|
| Input | `examples/pulse-export/sample/export/` — **37 demo rows** (`historical_demo` / `synthetic_fixture` / `model_generated` / `human_assessment` / `evaluation_annotation`) |
| Catalog / base schema | `workspace` / `analytics` (domain schemas `analytics_observed`, `analytics_synthetic`, `analytics_model`, `analytics_human`, `analytics_evaluation`) |
| First load | `rows_merged=true`; ~161 s; 10 target tables; counts 0→expected |
| Idempotent rerun | Identical load; all `count_before == count_after`; totals **37 → 37** (no duplicate key+fingerprint rows). Does **not** claim exactly-once delivery. |
| Merge keys | `(dedupe_key, content_fingerprint)` append-only |
| MLflow experiment | `/Users/sharathjb23@gmail.com/perch-pulse-investigation-eval` (id `605098882307471`) |
| MLflow run | `193935ec502f4496a5a7a1fc44118f6f` (`FINISHED`); artifact `eval/eval_summary.json`; token/cost metrics **unavailable** (not zero) |
| Warehouse after session | Left **RUNNING** (existing auto-stop unchanged) |
| Disk after session | ~24 GiB free on `/` |

**Honesty:** This proves F3a JSONL → staging → Delta MERGE → readback → idempotent skip and live MLflow persistence for the **demo fixture**. It does not prove production telemetry quality, statistical model performance, or exactly-once warehouse semantics.

Do not cite fixture eval scores as production model quality.
