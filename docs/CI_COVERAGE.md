# CI coverage matrix (Milestone C.5)

Tracks what Perch + Pulse verification actually proves. Related: issue [#26](https://github.com/sharathb5/perch-pulse/issues/26) (parent [#17](https://github.com/sharathb5/perch-pulse/issues/17)), ADR-018.

**Honesty labels**

| Label | Meaning |
|-------|---------|
| UNIT | Function/package behavior |
| INTEGRATION | Real packages exchange data (no Docker) |
| BROWSER | Playwright against mocked or fixture APIs |
| LIVE SMOKE | Real containers/telemetry (manual workflow only) |

## Coverage matrix

| Behavior | Existing test | Missing coverage (pre C.5) | Proposed / added check |
|----------|---------------|----------------------------|------------------------|
| CLI commands register; core flags work | `internal/cli/test/cli_test.go` | Explicit unknown-arg failure; Pulse-off startup | `internal/cli/test/perch_regression_test.go` (UNIT) |
| CLI does not require Pulse storage / Databricks | Implicit via empty env | Documented Pulse-off matrix | Same + `PERCH_PULSE_DIR` empty/missing (UNIT) |
| Config load; invalid edges rejected | `internal/config/test/validate_test.go` | — | Reuse; astronomy YAML already in `astronomy_shop_yaml_test.go` |
| Service graph identities / Astronomy topology | `internal/graph/test`, astronomy YAML test | Stable node IDs without credentials | `internal/graph/test/astronomy_graph_test.go` (UNIT) |
| Healthy custom probe succeeds | `customstatus/test`, `collect_custom_test` | Timeout / connection-fail ≠ healthy in Collect | `internal/stackstatus/test/probe_semantics_test.go` (UNIT) |
| Unchecked / missing creds ≠ healthy | format + adapt tests | Collect path for unchecked providers | Same probe semantics test (UNIT) |
| Probe latency measured | — | **Gap:** `NodeReport` has no latency field | Documented limitation (no fake field) |
| Pulse never overwrites probe health | `pulse/api`, `stackstatusadapt`, frontend | — | Reuse + pipeline INTEGRATION |
| `/api/graph`, `/api/status` | viz mux tests partial | Direct `/api/status` + corrupt Pulse | `internal/cli/viz_regression_test.go` (UNIT/INTEGRATION) |
| Viz starts without Pulse; corrupt artifacts safe | empty Pulse API tests | Corrupt JSON under Pulse dir | Same viz regression (INTEGRATION) |
| Agent context base + additive Pulse; secrets redacted | context + API redaction | Injected secret never in `--for-agent` | `perch_regression_test.go` agent secret case (UNIT) |
| Samples → detect → incident → persist → API | Layered package tests | Single path exercising real packages | `internal/pulse/pipeline` + `pipeline/test` (INTEGRATION) |
| Healthy baseline → no false incident | `detect/test` noisy/control | Through persistence + API | Pipeline scenario (INTEGRATION) |
| Latency / error / call-rate / recovery | `detect/test`, `incident/test` | Full path + API shape | Pipeline scenarios (INTEGRATION) |
| Stale/missing telemetry ≠ healthy | observation/store/API | Through services API after pipeline | Pipeline + observation (INTEGRATION) |
| Duplicate / out-of-order samples | `detect/test` | Through incident persistence | Pipeline scenario (INTEGRATION) |
| Environment mismatch (no cross-env join) | partial correlate | Explicit pipeline env isolation | Pipeline scenario (INTEGRATION) |
| Change correlation ≠ causation | `correlate/test`, API | Rank preserved in contract fixture | Pipeline + frontend contract (INTEGRATION/UNIT) |
| Ground-truth isolation | detect/incident/API | Pipeline JSON leak scan | Pipeline + Vitest (INTEGRATION/UNIT) |
| Frontend: Pulse unavailable, graph OK | Playwright `pulse.spec.js` | Contract fixture from Go pipeline | `pulse.pipeline.contract.json` + Vitest (UNIT) + existing BROWSER |
| Sidebar overview / investigation digests | `pulse-sidebar.spec.js`, Vitest | Bind to pipeline fixture digests | Vitest contract (UNIT); Playwright reuse (BROWSER) |
| Screenshot baselines | `web/e2e/__screenshots__` | — | Keep; no redundant shots |
| Live Astronomy Shop smoke | `verify-live.sh` (local) | GHA manual workflow | `.github/workflows/astronomy-smoke.yml` (LIVE SMOKE, dispatch only) |

## Gates

| Gate | Workflow / command | Docker? | Required on PR? |
|------|--------------------|---------|-----------------|
| Authoritative verify | `make verify` via `ci.yml` job **Go** | No | Yes |
| Browser / screenshots | `web-ui.yml` Playwright | No (Chromium only) | Yes when `web/**` (or workflow) changes |
| Focused Perch regression | `make perch-regression` (subset of `go test`) | No | Covered by `go test ./...` in verify |
| Pulse pipeline integration | `make pulse-integration` | No | Covered by `go test ./...` in verify |
| Astronomy live smoke | `astronomy-smoke.yml` | Yes | **No** (workflow_dispatch only) |

## Known limitations

1. **Probe latency:** Active probes do not currently record elapsed milliseconds on `NodeReport`. Tests assert success/timeout/failure semantics, not latency histograms.
2. **Playwright path filter:** `web-ui.yml` skips when only Go files change. Pulse API contract is still covered by Go INTEGRATION + Vitest fixture; browser overlays need a `web/` touch or manual workflow run.
3. **Live smoke capacity:** Full Astronomy Shop may exceed free GitHub-hosted disk/RAM after image pulls. The smoke workflow preflights resources and fails with diagnostics rather than claiming success.
4. **No Databricks / external AI / paid monitoring** in any required gate.
5. Detector thresholds are **not** tuned for tests; failing honest scenarios are reported as gaps.

## Commands

```bash
make verify              # PR gate
make perch-regression    # original Perch suite (fast Go subset)
make pulse-integration   # deterministic Pulse pipeline
make web-e2e             # Playwright (separate; check disk first)
# Live (manual GHA or local Docker — never required for merge):
./examples/astronomy-shop/scripts/clone.sh
./examples/astronomy-shop/scripts/start.sh
./examples/astronomy-shop/scripts/verify-live.sh
./examples/astronomy-shop/scripts/ci-smoke.sh   # GHA-oriented wrapper
```
