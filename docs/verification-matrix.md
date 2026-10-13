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
| 14 | Evaluation compares methods | **OFFLINE VERIFIED** | F2b/F2c corpora; explicit denominators |
| 15 | JSONL analytical export | **OFFLINE VERIFIED** | F3a `export-analytics` + sample fixture |
| 16 | Databricks Delta ingest | **OFFLINE VERIFIED** (validate/stage-sim/MERGE plan); live **BLOCKED BY AUTHORIZATION** | Staging+MERGE code path exists; `LIVE DELTA INGESTION = NOT RUN` without authorize |
| 17 | MLflow tracks evaluations | **OFFLINE VERIFIED** (fake client); live **BLOCKED BY AUTHORIZATION** | `LIVE MLFLOW TRACKING = NOT RUN` |
| 18 | Engineer verifies lineage | **OFFLINE VERIFIED** (simulate); live **BLOCKED BY AUTHORIZATION** | `perch_pulse_dbx verify-lineage` (fake); live lineage via `--execute-merge` readback |

## External validation flags (this release candidate)

| Check | Status |
|-------|--------|
| REAL LLM VALIDATION | **NOT RUN** |
| LIVE DELTA INGESTION | **NOT RUN** |
| LIVE MLFLOW TRACKING | **NOT RUN** |
| Full Astronomy Shop live-fault → UI | Prefer prior 2026-10-09 shop evidence + offline offline UI; do not re-run heavy Docker when disk &lt; 15 GiB |

## Automated gates (no paid services)

| Gate | Command |
|------|---------|
| Full local verify | `make verify` |
| Databricks offline | `make databricks-integration-test` |
| Web unit | `make web-test` |
| Playwright | `make web-e2e` |
| Demo dry-run | `./scripts/demo/pulse-demo.sh --mode mock --phase preflight --dry-run` |
| Demo offline | `./scripts/demo/pulse-demo.sh --mode mock --phase offline` |

## Previously verified live workflow evidence

- **2026-10-09** — Astronomy Shop minimal stack + scenario harness including `error-payment` activation/recovery ([`astronomy-shop.md`](astronomy-shop.md)).
- Live-fault CI shipping-latency attempt failed historically; CI scenario selected is `error-payment` ([`pulse-live.md`](pulse-live.md)).

Do not cite fixture eval scores as production model quality.
