# Perch viz SPA

Embedded React UI for `perch viz` (localhost). Built with Vite, React 19, `@xyflow/react`, and Tailwind.

## Docs

- [`DESIGN.md`](DESIGN.md) — record of the **existing** interface (not a redesign)
- [`UI_VERIFICATION.md`](UI_VERIFICATION.md) — unit/browser/screenshot verification loop
- Fixtures: [`fixtures/`](fixtures/)

## Scripts

```bash
npm ci
npm run dev          # Vite HMR (needs viz APIs or mocks)
npm test             # Vitest contract/unit tests
npm run build        # production bundle → dist/ (embedded by Go)
npm run test:e2e     # build + Playwright (Chromium)
npm run test:e2e:update  # intentionally refresh screenshot baselines
```

Install browsers once (check free disk ≥ 15 GiB first):

```bash
npx playwright install chromium
```

## Agent skills

- `.cursor/skills/perch-ui-implementation/`
- `.cursor/skills/perch-ui-review/`
