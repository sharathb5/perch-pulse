# Architectural decision log

## How to record decisions

Add a new ADR at the **top** (newest first) when you make a non-obvious choice that future agents must not reverse casually.

```markdown
## ADR-NNN: Short title

**Date:** YYYY-MM-DD
**Status:** Proposed | Accepted | Superseded by ADR-XXX

**Context:** What problem or constraint forced a choice?

**Decision:** What we chose.

**Consequences:** Benefits, costs, follow-ups.

**Alternatives considered:** Brief list (optional but preferred).
```

Rules:

- Prefer one decision per ADR.
- Link PRs or commits when useful.
- Do not invent ADRs for routine bugfixes.
- Supersede instead of silently rewriting history.

---

## ADR-012: Explainable rolling-window baseline detector with post-hoc evaluation

**Date:** 2026-10-09  
**Status:** Accepted

**Context:** Issue #13 needs the first Pulse regression detector against Astronomy Shop telemetry, with measurable evaluation against the Phase 2 ground-truth harness (#11), without label leakage, ML, Databricks, or causation claims.

**Decision:** Add three packages: `internal/pulse/telem` (Prometheus HTTP `Source` for windowed spanmetrics: `latency_ms` as **p99** `histogram_quantile` over duration buckets — mean dilutes sparse 10s faults — plus `error_rate`/`call_rate` via ~2m `increase()`; emit explicit `0` error_rate/call_rate when Prometheus omits a series so healthy baselines exist), `internal/pulse/detect` (rolling median baseline vs current window with relative/absolute/MAD gates; typed `Finding` `pulse.finding.v1`; topology attribution via astronomy mapping only; latency gated by concurrent call_rate and max-ms sanity), and `internal/pulse/evaluate` (the **only** package that reads scenario records, and only after findings exist). Latency scenario uses `intlShippingSlowdown=10sec`→shipping. Control false positives count only latency/error findings on the labeled target (demo `call_rate` burstiness alone is not a control FP). Attribution scoring prefers findings on the labeled target when present. Evaluation credits only findings whose `first_detected_at` falls in the fault window (pre-existing ambient findings do not count). Baseline defaults: ~75s–3m history, ~40s current window, latency +100% with ≥1000ms absolute floor (p99 scale), error-rate +0.10 absolute with floor 0.05, outage via error_rate ≥0.35 or call_rate drop ≥55%. Live harness: `examples/astronomy-shop/eval/run-detector-eval.sh`. CI uses synthetic fixtures only.

**Consequences:** Detector inputs are telemetry-only; evaluation can measure detection delay, attribution, control false positives, and recovery. Thresholds are demo-oriented and documented — not tuned inside a run from that run’s labels. Missing Prom series do not invent call_rate zeros; error_rate zeros require a concurrent call observation. Open findings are not marked recovered when current-window stats are unavailable (stale ≠ healthy). Future backends (file/Databricks) can implement `telem.Source`.

**Alternatives considered:** ML anomaly models (rejected: opacity/overkill); embedding labels into observation evidence (rejected: leakage); cumulative counter averages (rejected: polluted by prior faults); requiring Astronomy Shop in CI (rejected: disk/time).

---

## ADR-011: Bump Go toolchain to 1.27.2 for stdlib govulncheck gate

**Date:** 2026-10-09  
**Status:** Accepted

**Context:** `make verify` runs govulncheck against the active toolchain. Host Go **1.27.1** (and the prior `toolchain go1.25.9` directive under `GOTOOLCHAIN=auto`) reported multiple fixed-in-1.27.2 standard-library CVEs (HTTP/2 HPACK race, MIME/Range limits, TLS ECH, etc.), failing the security gate on both main and this branch before any scenario code ran.

**Decision:** Set `toolchain go1.27.2` in `go.mod` so analysis uses a patched toolchain. Keep the language `go` version at **1.24.4** unless a separate migration requires raising it. CI `actions/setup-go` installs **1.27.2** explicitly (not `go-version-file: go.mod`, which only reads the language line). `make security` sets `GOTOOLCHAIN=go1.27.2` so `go run` for gosec/govulncheck builds scanners with the same Go that loads this module.

**Consequences:** Local/CI verify use 1.27.2 for scanners and package load. Future stdlib CVE waves need deliberate toolchain + CI + Makefile bumps together (same spirit as ADR-005 scanner pins).

**Alternatives considered:** Weaken/skip govulncheck (rejected); pin older Go without the new vuln DB findings (rejected: leaves known CVEs); rely on `go-version-file` alone (rejected after CI: host 1.24.4 + tool min 1.26 caused govulncheck to run on go1.26.9 and fail parsing go1.27.2 stdlib).

---

## ADR-010: Ground-truth scenario harness via Astronomy Shop flagd (not a detector)

**Date:** 2026-10-09  
**Status:** Accepted

**Context:** Issue #11 needs a reproducible evaluation substrate: inject known failures into Astronomy Shop, record hidden labels (what/when/who/type/path/recovery), and keep those labels out of Pulse observations so future detectors can be scored without label leakage. Disk/Docker safety forbids heavy CI pulls of the demo stack.

**Decision:** Add `internal/pulse/scenario` with a versioned ground-truth `Record` (`pulse.scenario.v1`) and an embedded catalog of four scenarios driven only by OpenTelemetry Demo **3.1.0** flagd feature flags (`imageSlowLoad` / formerly intl shipping latency, `paymentFailure`, `paymentUnreachable`, `emitRawPii` as mandatory negative control). Harness start/stop uses the flagd-ui HTTP API (read-modify-write `defaultVariant`) with explicit timeouts; persists records under `examples/astronomy-shop/scenarios/results/` (gitignored). Shell wrappers provide demo UX. CI covers schema/lifecycle/identity/secret/label-separation tests only; live verification is a separate script and not part of `make verify`. The package does **not** implement detection, does **not** write to `observation`/`store`, and must not be treated as an observation producer.

**Consequences:** Detector evaluation can load ground-truth explicitly. Flag-only injection avoids forking Astronomy Shop. Only one active run at a time (active.json lock). Upstream flag semantics / image drift under pin 3.1.0 remain an external risk.

**Alternatives considered:** Custom service chaos sidecars (rejected: fork risk); embedding labels into observation evidence (rejected: label leakage); pulling Astronomy Shop in CI (rejected: disk/time); LLM-based success checks (rejected: nondeterministic).

---

## ADR-009: Astronomy Shop as Phase 2 local target with `astronomy/<env>/<name>` IDs

**Date:** 2026-10-07  
**Status:** Accepted

**Context:** Issue #9 needs a realistic local distributed system for Phase 2 without Databricks, fault injection, or changing Pulse observation/store semantics. The OpenTelemetry Astronomy Shop is the chosen target; identities must be stable for future collectors.

**Decision:** Treat Astronomy Shop as an **external** pinned dependency (git tag **3.1.0**, not vendored). Default local run is upstream `make start-minimal` (core shop + observability; no Kafka group). Document topology/telemetry from that pin. Pulse `service_id` values use `astronomy/<environment>/<name>` with `environment=local` for Compose; app names match `OTEL_SERVICE_NAME` (including `frontend-web`); infra without a demo OTEL name uses `infra/<component>`. Authoritative table lives in `internal/pulse/astronomy/service-mapping.yaml` and is validated by Go tests. Live Docker verify is scripted but not part of `make verify`.

**Consequences:** Setup is reproducible via `examples/astronomy-shop/scripts/*`. CI stays light. Future collectors can adopt IDs without renaming. Upstream `DEMO_VERSION=latest` images may still move under a git pin.

**Alternatives considered:** Vendor the full demo (rejected: huge tree, license/churn); full `make start` as default (rejected: higher RAM/disk for Phase 2 setup); reuse bare OTEL names as ServiceIDs (rejected: collide with other envs / lack product prefix).

---

## ADR-008: In-memory local Pulse observation store

**Date:** 2026-10-07  
**Status:** Accepted

**Context:** Issue #7 needs the smallest local historical store so Pulse can retain observations over time after `stackstatus` → observation adapter, without Databricks or a background collector.

**Decision:** Add `internal/pulse/store` with a small `Store` interface (`Append`, `List`, `Latest`) and a process-local `Memory` implementation. Validate on write via `observation.Validate`. Order and latest-selection reuse `observation.Compare` / `observation.Latest` (never arrival order). Compare-equal duplicates for a service are idempotent no-ops. `List` returns ascending Compare order. Values are cloned on write/read so callers cannot mutate stored rows. No new dependencies; no file/SQLite/Databricks in this step.

**Consequences:** History is lost on process exit. File or warehouse backends can implement the same interface later. Basic Perch collectors remain unchanged and Databricks-independent. Secret content scanning remains a producer responsibility.

**Alternatives considered:** Immediate file persistence (deferred: path/locking/lifecycle decisions without a caller yet); embed history in `stackstatus` (rejected: couples live probes to Pulse retention); require SQLite (rejected: new dependency for Phase 1).

---

## ADR-007: Map stackstatus rows to Pulse observations

**Date:** 2026-10-07  
**Status:** Accepted

**Context:** Issue #5 needs the smallest adapter from live `stackstatus` results to `observation.Observation` without changing `perch status` output or emitting producer `StatusStale`.

**Decision:** Add pure package `internal/pulse/stackstatusadapt` with `FromNodeReport` / `FromEnvReport`. Map live measured health (`Healthy==true` from `shell`, `api`, or successful `probe`) to `StatusHealthy`, except when `ErrorRate > 0` then `StatusDegraded`. Map config-only or unprobed rows (`app_env`, `unchecked` including Perch’s “no HTTP probe” healthy shortcut) to `StatusUnavailable` even when `NodeReport.Healthy` is true (Pulse fail-closed vs `perch status` display). Map missing config/credentials, placeholders, probe setup failures, transport/timeouts, and other no-usable-signal probe errors to `StatusUnavailable`. Map vendor-confirmed failures (HTTP/status detail after a response) to `StatusUnhealthy`. Prefix `ServiceID` with `Env/` when adapting `EnvReport`. Set `Source` to `"stackstatus"`; evidence uses redacted detail (URLs and auth query params stripped) plus provider and numeric `error_rate` only (no `recent_errors`). Timestamps are caller-supplied via `Options`.

**Consequences:** Staleness remains derived in `observation`; adapters never store `StatusStale`. `StatusUnknown` is not produced by this adapter today (unconfigured/unchecked rows are unavailable, not unknown). Callers must pass explicit `ObservedAt`.

**Alternatives considered:** Embed mapping in `stackstatus.Collect` (rejected: couples Pulse to collectors); map unchecked rows to `StatusUnknown` (rejected: product treats them as no usable signal, aligned with TUI “check pending”).

---

## ADR-006: Typed Pulse observation with derived staleness

**Date:** 2026-10-07  
**Status:** Accepted

**Context:** Issue #3 needs a first Pulse data primitive that preserves observation time and never treats stale or missing telemetry as current healthy status. Live collectors (`stackstatus`, etc.) remain unchanged.

**Decision:** Add `internal/pulse/observation` as an optional, I/O-free contract package. Producer `Status` is a constrained string type (`healthy`, `degraded`, `unhealthy`, `unavailable`, plus zero-value unknown). `StatusStale` is derived-only (`Validate` rejects it as a stored producer status) via `IsStale` / `EffectiveStatus` from `ObservedAt` and an explicit freshness threshold (age >= threshold, non-positive threshold, zero `ObservedAt`, or future `ObservedAt` → stale). `Latest` / `Compare` order by `ObservedAt`, then `IngestedAt`, then identity fields, then **severity** (worse/uncertain wins equal-time conflicts so lexical `"healthy" > "degraded"` cannot win), then evidence strings—never by arrival/slice order. Evidence carries only non-secret `Ref` / `Summary`; field-name guards exist in tests, but string-content redaction stays at the producer boundary.

**Consequences:** Callers must pass an explicit `now` and freshness duration; wiring collectors into observations is a follow-up. Historical storage and UI are out of scope. Content-level secret scanning is not implemented here.

**Alternatives considered:** Overload `stackstatus.NodeReport` (rejected: mixes live probe rows with Pulse history semantics); store staleness as the only status field (rejected: loses underlying healthy/degraded signal); lexical status tie-break (rejected after Codex review: preferred healthy over degraded).

---

## ADR-005: Pin gosec and govulncheck versions in `make security`

**Date:** 2026-10-07  
**Status:** Accepted

**Context:** Codex review flagged `@latest` security tools as non-reproducible: the same commit can fail when scanners release breaking changes.

**Decision:** Pin `GOSEC_VERSION` (default `v2.29.0`) and `GOVULNCHECK_VERSION` (default `v1.8.0`) in the Makefile. Bump deliberately with a PR when upgrading scanners.

**Consequences:** Reproducible CI; occasional manual version bumps required.

---

## ADR-004: Single `make verify` entrypoint aligned with CI

**Date:** 2026-10-07  
**Status:** Accepted

**Context:** Go embed requires `web/dist` before Go compile. Partial local checks caused false confidence. CI and agents need one reproducible gate.

**Decision:** `make verify` runs frontend lockfile install, embed build, lint, tests, `gofmt` check, `go vet`, `go test`, provider validation, and existing gosec/govulncheck security targets. CI invokes `make verify` under the existing job display name `Go` so branch-protection check names stay stable. Individual Makefile targets remain for iteration. Keep the pre-existing `G304` gosec exclude (CLI reads user project files by design); do not broaden excludes without a new ADR.

**Consequences:** Longer full runs; fail-fast Make semantics; security scanning stays mandatory; humans must update branch protection only if they rename the CI job.

**Alternatives considered:** Keep CI steps duplicated without a Makefile target; drop security from local verify (rejected); rename CI job to `Verify` (rejected for protection-name churn).

---

## ADR-003: Frontend lint unblocked with targeted eslint disable

**Date:** 2026-10-07  
**Status:** Accepted (temporary)

**Context:** `eslint-plugin-react-hooks` flags intentional `setCopyWhich(null)` reset in `DetailPanel.jsx`, blocking `npm run lint` and thus `make verify`.

**Decision:** Keep behavior; add a single-line `eslint-disable-next-line` with comment. Defer DetailPanel state redesign.

**Consequences:** Verify can pass. Future key-based remount is preferred long-term.

---

## ADR-002: Databricks is optional for Pulse 2.0

**Date:** 2026-10-07  
**Status:** Accepted

**Context:** Historical analytics may eventually use Databricks, but requiring it would break local-first Perch.

**Decision:** Databricks is an optional analytical backend. Basic Perch must work without it. Missing Databricks is never reported as stack outage.

**Consequences:** Feature flags / graceful degradation required for any future Databricks code paths.

---

## ADR-001: Bootstrap from upstream Perch with full history

**Date:** 2026-10-06  
**Status:** Accepted

**Context:** `perch-pulse` started empty; Pulse extends Perch rather than rewriting it.

**Decision:** Import `yashg4509/perch` onto `main` with full history; `origin` = perch-pulse; `upstream` = original remote. Preserve MIT license. Module path remains `github.com/yashg4509/perch` until a dedicated migration ADR.

**Consequences:** Upstream syncs remain feasible; renaming the module is a separate decision.
