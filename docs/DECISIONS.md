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
