---
name: perch-ui-review
description: >-
  Independently reviews Perch React viz frontend changes for behavior and
  visual regressions against the existing design. Use when reviewing web/
  PRs, screenshot diffs, Playwright failures, or Milestone B/C/D UI work.
---

# Perch UI review

Independent review against the **existing** Perch design ([`web/DESIGN.md`](../../../web/DESIGN.md)).  
Do not depend on external SaaS visual tools — use repo Playwright artifacts, git diffs, and code inspection.

## Standard

The original Perch appearance is the source of truth. Intentional product additions are allowed; unexplained drift is not.

## Required checks

- [ ] Unexpected layout changes (navbar, graph flex, 300px detail panel)
- [ ] Styling drift (palette, borders, radii, shadows)
- [ ] Typography or spacing changes vs DESIGN.md
- [ ] Clipped text / truncated labels without titles
- [ ] Overlapping graph nodes
- [ ] Unreadable graph edges (contrast, missing arrows)
- [ ] Broken click and hover interactions (node select, close, Escape, refresh)
- [ ] Responsive rendering (desktop + narrow baseline)
- [ ] Loading and empty states
- [ ] Stale / unknown status representation (never green-as-healthy)
- [ ] Active health vs Pulse intelligence separation
- [ ] Accessibility / keyboard (Escape closes panel; controls labeled)
- [ ] Misleading severity colors
- [ ] Mock data shown as live data without disclosure
- [ ] Runtime errors in console / failed network handling
- [ ] Network/API failure banner behavior

## Process

1. Diff `web/` and screenshot baselines (`web/e2e/__screenshots__/`).
2. Read Playwright report / `test-results` if CI failed.
3. Classify each visual delta: **intentional** (must be explained in PR) vs **regression** (must fix).
4. Confirm contract tests cover identity, edges, selection, empty/error/unknown, mock honesty.
5. Report findings as Critical / Suggestion / Nice-to-have. Do not “approve” unexplained baseline churn.

## Out of scope for this skill

- Redesign proposals
- Rewriting the SPA
- Weakening CI security gates
