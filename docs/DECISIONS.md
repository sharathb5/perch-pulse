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
