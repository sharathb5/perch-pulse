# Frontend verification process

Use this checklist for any change under `web/` that can affect appearance or graph behavior.  
Reference design: [`DESIGN.md`](DESIGN.md). Agent skills: `.cursor/skills/perch-ui-implementation/`, `.cursor/skills/perch-ui-review/`.

Visual differences are **not** automatic bugs — they must be **explained** (intentional vs accidental).

---

## Agent / human loop

1. Implement the code change (reuse existing components/styles; no redesign).
2. Run unit/contract tests: `cd web && npm test`
3. Build the SPA: `cd web && npm run build`
4. Start the app with **deterministic fixtures** (Playwright does this via route mocks; locally you can also run `npm run preview` after build and mock APIs).
5. Run Playwright: `cd web && npm run test:e2e`
6. Capture / compare screenshots (Playwright `toHaveScreenshot` vs `e2e/__screenshots__/`).
7. Inspect visual diffs (HTML report + `test-results/` on failure).
8. Investigate unexpected differences against `DESIGN.md` baselines.
9. Run Codex (or structured secondary review) with the **perch-ui-review** skill — read-only.
10. Address legitimate findings.
11. Run `make verify` from repo root.
12. Open a PR linked to the relevant issue / [#17](https://github.com/sharathb5/perch-pulse/issues/17).

---

## Commands

| Step | Command |
|------|---------|
| Install JS deps | `cd web && npm ci` |
| Unit + contract tests | `cd web && npm test` |
| Lint | `cd web && npm run lint` |
| Install Chromium only | `cd web && npx playwright install chromium` |
| Browser + screenshots | `cd web && npm run test:e2e` |
| Update baselines **intentionally** | `cd web && npm run test:e2e:update` |
| Full Go+web gate | `make verify` (does **not** run Playwright; CI has a separate job) |
| Local Playwright gate | `make web-e2e` |

### Disk / safety

Playwright Chromium is ~200–300 MiB installed. Check free space before install. **Hard stop if &lt; 15 GiB free.** Do not Docker prune or delete unrelated caches.

---

## Deterministic fixtures

Fixtures live in `web/fixtures/`:

| File | Use |
|------|-----|
| `graph.ok.json` / `status.ok.json` | Happy-path topology |
| `graph.empty.json` / `status.empty.json` | Empty graph |
| `status.stale-unknown.json` | Nodes without matching health → `unknown` |

E2E tests intercept `/api/graph` and `/api/status` (and optionally fail them). They never call Astronomy Shop, Databricks, or production credentials.

---

## Screenshots

| Baseline | Viewport | What it covers |
|----------|----------|----------------|
| `main-graph` | 1280×720 | Graph with fixture nodes/edges |
| `selected-node` | 1280×720 | Node selected + detail panel |
| `api-error` | 1280×720 | Amber error banner (mock retained) |
| `empty-graph` | 1280×720 | Successful empty API response |
| `main-graph-narrow` | 390×844 | Narrow viewport sanity |

### Regenerating baselines

```bash
cd web
npm ci
npx playwright install chromium
npm run test:e2e:update
```

Review the git diff of `web/e2e/__screenshots__/**`. Commit only when the change is intentional and described in the PR.

### Comparing / inspecting diffs

- Local failure: open `web/playwright-report/index.html` (or the path printed by Playwright).
- CI failure: download the `playwright-report` / `test-results` workflow artifacts.
- Diffs show expected vs actual vs diff image.

### Approval policy

- **CI never updates baselines.**
- Failed screenshot assertions fail the job.
- Approving a visual change = explicit local `--update-snapshots` + human/agent review + PR explanation.

### OS / Chromium note

Baselines are PNGs from Chromium. Font rasterization can differ between macOS and Linux CI. If CI fails only on screenshots with small AA diffs (no layout change):

1. Download the `playwright-report` / `test-results` artifact.
2. Confirm the diff is AA/font-only, not a real regression.
3. Regenerate on the same OS as CI (`ubuntu-latest`): run `npm run test:e2e:update` in that environment, commit the Linux baselines, and explain in the PR.

Do **not** raise `maxDiffPixelRatio` casually to hide real layout regressions.

---

## Distinguishing intentional vs accidental visuals

| Signal | Treat as |
|--------|----------|
| New Pulse overlay / panel documented in the PR | Intentional — update baselines + DESIGN.md notes |
| Unrelated spacing, font, palette, or card size drift | Accidental — fix code, do not update baselines |
| Animation / anti-alias flake with tiny pixel delta | Investigate; tighten fixture / disable motion before raising threshold |

---

## Mock data honesty

Current SPA seeds mock topology and keeps it on API failure (`usePerchData` + banner saying “last known or demo data”). Tests assert this behavior explicitly. Future milestones must not present mock as live Pulse/probe data without labeling.
