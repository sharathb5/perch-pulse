# Perch Pulse 2.0 — Product specification

Perch Pulse 2.0 is a **production feedback system** for developers and coding agents. It extends open-source [Perch](https://github.com/yashg4509/perch): a local-first CLI and embedded UI for multi-service deployment stacks.

This document defines product intent. Capabilities marked **Planned** are not implemented unless code and tests prove otherwise.

## Positioning

| Audience | Need |
|----------|------|
| Developers | One trustworthy view of stack health, regressions, and blast radius |
| Coding agents | Structured, evidence-backed production context—not narrative guesswork |
| Humans in the loop | Keyboard TUI and local `perch viz` without a mandatory cloud dependency |

**Hard product rule:** Perch must remain useful **without Databricks**. Databricks is an optional analytical backend for history and heavy investigation—not a requirement for basic status, graph, logs, viz, or TUI.

---

## Existing functionality (imported Perch)

These capabilities ship in the current repository:

- **Local-first stack model** via committed `perch.yaml` (nodes, edges, environments).
- **Provider specs** as embedded YAML under `providers/` (hosting, data, SaaS, AI, observability, …).
- **CLI / TUI** (`perch`, Bubbletea) for interactive exploration.
- **Live status probes** (`perch status`, `internal/stackstatus`) where credentials/config allow; custom shell health checks.
- **Logs resolution** (`perch logs`, `internal/stacklogs`) with ordered credential strategies and setup hints.
- **Graph / topology** (`perch graph`, `internal/graph`) for environment-scoped dependency views.
- **Agent-oriented context** (`perch context`, `--json`, `--for-agent`) from current collectors.
- **Embedded React graph UI** (`perch viz` on localhost: `/api/graph`, `/api/status`, `/api/logs`, `/api/credentials`, plus read-only `/api/pulse/*` for persisted Pulse intelligence — see [`pulse-api.md`](pulse-api.md)).
- **Local credentials** in `~/.perch` plus optional project `.env` sync—no Perch cloud service.

Point-in-time probes and log fetches are the source of truth today. There is **no** warehouse-backed history, deploy-impact engine, or Pulse investigation API in-tree yet.

**Phase 2 local target:** OpenTelemetry Astronomy Shop can be run beside Perch via documented scripts; see [`astronomy-shop.md`](astronomy-shop.md). A **ground-truth scenario harness** lives under `examples/astronomy-shop/scenarios/`. An explainable **telemetry-only baseline detector** and post-hoc evaluator live under `internal/pulse/{telem,detect,evaluate}` with a live harness at `examples/astronomy-shop/eval/` — not an AI investigator. **Change/deployment correlation** (`internal/pulse/{change,incident,correlate,changeeval}`) ranks recent typed change events against incident evidence; correlation is never presented as proven causation. Simulated deploy markers for demos: `examples/astronomy-shop/change/`.

---

## Planned functionality (Pulse 2.0)

### 1. Historical service health and behavioral baselines

Retain health and behavior signals over time so “is this normal?” can be answered with windows and baselines—not only the latest poll. Baselines must state what was measured and over what interval.

### 2. Deployment impact and regression detection

Correlate deploys/config changes with shifts in status, errors, or log patterns across graph nodes. Prefer evidence links (deploy IDs, time ranges, affected nodes) over narrative-only summaries. **Correlation must never be presented as proven causation.**

### 3. Cross-service dependency analysis

Use topology plus observed failures to reason about blast radius and upstream/downstream impact. Explicitly separate configured edges from inferred runtime coupling.

### 4. Evidence-backed AI investigations

Investigations must cite concrete artifacts (status rows, log excerpts, probe errors, setup gaps, topology). AI diagnoses must **distinguish evidence from inference**. Missing data is not “healthy.”

### 5. Production-aware context for coding agents

Stable schemas for coding agents: current vs historical signals, uncertainty, credential/setup gaps, and enough topology for safe change suggestions. Secrets must never appear in agent context.

### 6. Optional Databricks analytical backend

Databricks may later power warehouse-scale history, baselines, and heavy joins. When unset, Pulse features degrade with explicit “history unavailable” semantics. **Absence of Databricks is never a stack outage.**

---

## Non-goals (near term)

- Replacing vendor observability products wholesale.
- Requiring a hosted Perch/Pulse control plane for local use.
- Implementing Databricks ingestion or production simulation in Phase 0.
- Storing customer secrets in this repository.

## Success criteria

| Signal | Meaning |
|--------|---------|
| Local trust | Basic Perch flows work without analytical backends |
| Agent clarity | Structured outputs name sources, gaps, and errors |
| Evidence over vibes | Investigations cite measurable signals |
| Optional scale-out | Databricks can be adopted later without rewriting core Perch |

## Related docs

- [`ARCHITECTURE.md`](ARCHITECTURE.md) — current system vs proposed Pulse modules
- [`DECISIONS.md`](DECISIONS.md) — decision log
- [`CODEBASE_GUIDE.md`](CODEBASE_GUIDE.md) — imported codebase orientation
- [`../AGENTS.md`](../AGENTS.md) — engineering rules for Cursor and Codex
