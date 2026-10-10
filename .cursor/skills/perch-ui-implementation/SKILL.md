---
name: perch-ui-implementation
description: >-
  Adds functionality to the existing Perch React viz UI while preserving
  appearance, graph interactions, and health semantics. Use when modifying
  web/, perch viz frontend, graph nodes, DetailPanel, StatusPill, mappers,
  or Milestone B/C/D Pulse UI integration.
---

# Perch UI implementation

Preserve the current Perch interface. This is **not** a redesign.

## Before any UI change

1. Read [`web/DESIGN.md`](../../../web/DESIGN.md) — existing colors, typography, spacing, graph, panels, mock fallback.
2. Read [`web/UI_VERIFICATION.md`](../../../web/UI_VERIFICATION.md) — verification loop.
3. Inspect existing components under `web/src/components/`, `web/src/graph/`, `web/src/hooks/`, `web/src/lib/` **before** adding new files.
4. Prefer extending mappers/hooks over parallel data paths.

## Hard requirements

- Reuse established Tailwind patterns; minimize CSS/layout modifications.
- Do not change the color palette, fonts, global spacing, layouts, or animations.
- Do not introduce a new component library or rewrite the frontend.
- Preserve graph interactions (select, drag, fitView, env reset, refresh, poll).
- Preserve service identity: graph node `id` = config node name = status `name`.
- Distinguish **active probe health** (`/api/status`, `StatusPill`) from **Pulse intelligence** (`/api/pulse/*`). Never collapse them into one misleading indicator.
- Preserve `unknown` / missing / unavailable states — **stale ≠ healthy**.
- Do not invent telemetry, incidents, or healthy status from empty data.
- Do not silently use mock data as live data. If mock/`data/mock.js` remains visible, label it or keep the existing error banner semantics; document honesty in tests.
- Avoid new npm dependencies without justification in the PR / ADR.
- Add deterministic frontend tests (Vitest contract tests and/or Playwright).
- Verify actual browser behavior with Playwright fixtures (no live Astronomy Shop, no cloud credentials).

## Implementation checklist

```
- [ ] Read web/DESIGN.md
- [ ] Inspect neighboring components before adding new ones
- [ ] Reuse StatusPill / ServiceCard / DetailPanel patterns
- [ ] Active health vs Pulse intelligence visually and in copy
- [ ] unknown/stale/unavailable never styled as healthy
- [ ] No mock presented as live without disclosure
- [ ] Vitest contracts updated
- [ ] Playwright e2e + screenshots reviewed
- [ ] make verify
```

## After implementation

Follow the full loop in [`web/UI_VERIFICATION.md`](../../../web/UI_VERIFICATION.md). Use the **perch-ui-review** skill for independent review.
