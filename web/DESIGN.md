# Perch viz UI — existing design record

**Purpose:** Document the *current* embedded React interface served by `perch viz`.  
This is a baseline for Milestones B–D, not a redesign proposal.

**Source of truth:** The live components under `web/src/` and the screenshot baselines under `web/e2e/__screenshots__/`.  
If docs and screenshots diverge, trust the code + baselines and update this file.

**Last surveyed:** 2026-10-10 (main after Milestone A / PR #19).

---

## Stack (actual)

| Layer | Choice |
|-------|--------|
| Framework | React 19 (`react`, `react-dom`) |
| Build | Vite 8 (`web/vite.config.js`), output `web/dist` embedded via `web/embed.go` |
| Routing | `react-router-dom` v7 — `/` → `/stack/default`; `/stack/:stackName`; `/stack/:stackName/:nodeId` |
| Graph | `@xyflow/react` v12 + `dagre` LR layout (`web/src/graph/layout.js`) |
| Icons | `lucide-react` (RefreshCw, X, Loader2) |
| CSS | Tailwind CSS 3 utility classes; no custom design-token file |
| Fonts | Browser / system defaults (`antialiased` on `body`); mono via `font-mono` |
| Data | `fetch` to localhost viz APIs; poll every 10s |
| Unit tests | Vitest (`web/src/**/*.test.js`) |
| Browser tests | Playwright (see `web/UI_VERIFICATION.md`) |

Serving: Go `perch viz` binds loopback and serves `web/dist` plus `/api/graph`, `/api/status`, `/api/logs`, `/api/credentials`, and Milestone A `/api/pulse/*`.

---

## Color palette (as used)

No CSS variables. Colors are Tailwind class names in components:

| Role | Classes / hex |
|------|----------------|
| Page background | `bg-white` |
| Primary text | `text-black`, `text-gray-900` |
| Secondary text | `text-gray-500`, `text-gray-600`, `text-gray-400` |
| Borders | `border-gray-200`, `border-gray-300` |
| Surfaces | `bg-white`, `bg-gray-50`, `bg-gray-100` |
| Health healthy | `bg-green-500` dot |
| Health degraded | `bg-amber-400` dot |
| Health down | `bg-red-500` dot |
| Health source | `bg-gray-400` dot |
| Health unknown | `bg-slate-300` dot |
| Meta success / warning / danger | `text-green-600` / `text-amber-500` / `text-red-500` |
| Error banner | `bg-amber-50`, `border-amber-200`, `text-amber-900` |
| Links | `text-blue-600` |
| Graph edges | stroke `#94a3b8` (slate-400) |
| Graph dots background | `#e5e7eb` |
| Provider badges | Vercel black; GitHub `#24292e`; Supabase `#3ecf8e`; Render `#46e3b7`; custom `bg-gray-200` |
| Logs terminal | `bg-gray-900` / `text-gray-100` |

---

## Typography

| Element | Classes |
|---------|---------|
| Brand wordmark | `text-sm font-bold text-black` (“perch”) |
| Stack name pill | `text-sm text-black` |
| Service card title | `text-[16px] font-medium` |
| Service URL | `font-mono text-[11px] text-gray-400` |
| Meta rows | `font-mono text-xs` |
| Detail panel title | `text-[13px] font-medium` |
| Detail meta labels | `text-[10px] uppercase tracking-wide text-gray-400` |
| Tabs / pills | `text-xs` |

No custom `@font-face` or Google Fonts.

---

## Spacing and layout

- Full-viewport column: `h-screen flex flex-col`.
- Navbar height: `h-12`, horizontal `px-4`.
- Main row: graph `flex-1` + optional detail panel fixed `w-[300px]`.
- Service cards: fixed visual width `210px` (`NODE_WIDTH`); Dagre height budget `NODE_HEIGHT = 300`.
- Card internal padding: mostly `px-2.5` / `py-2`.
- Dagre: `rankdir: LR`, `ranksep: 120`, `nodesep: 88`, margins 32.
- Snap-to-grid: `[12, 12]`.
- Border radius: cards `rounded-xl`; controls/minimap `rounded-md`; env pill `rounded-full`.

---

## Component hierarchy

```
App (BrowserRouter)
└── StackView
    ├── error banner (optional)
    ├── Navbar
    ├── ReactFlowProvider → PerchGraph
    │     └── ServiceCard (node type `serviceCard`)
    └── DetailPanel (when :nodeId present)
          ├── DeployRow
          └── logs / credentials setup UI
```

Supporting: `StatusPill`, `ProviderBadge`, `PulseIndicator`, `PulseSection`, hooks `usePerchData` / `useNodeLogs` / `usePulseData` / `usePulseIncident`, mappers in `lib/mappers.js`, Pulse adapters in `lib/pulse.js`, AI handoff in `lib/aiHandoff.js`.

---

## Graph appearance

**Nodes (`ServiceCard`):**

- White card, gray border; selected = `border-gray-400 ring-2 ring-gray-200`.
- Header: provider badge + provider name + `StatusPill`.
- Body: label + mono URL.
- Meta key/value rows.
- Footer tab strip (provider-specific labels; clicks navigate to detail, do not switch in-card tabs).
- Optional “Open in” AI menu when node is errored (`degraded`/`down` or `recentErrors`).
- Handles are invisible (transparent) on left/right for edges.

**Edges:**

- Animated by default.
- Stroke `#94a3b8`, closed arrow markers same color.
- Built from graph JSON `from` → `to` as React Flow `source` → `target`.

**Chrome:**

- Dot `Background`, `MiniMap` (bottom-right), `Controls` (bottom-left).
- `fitView` on node-count change.
- Drag preserves positions across poll; `layoutResetKey` (environment) clears user positions.
- Overlap separation after drag (`separateOverlappingNodes`).

---

## Navigation and interactions

| Action | Behavior |
|--------|----------|
| Click node / card regions | Navigate to `/stack/:stackName/:nodeId` |
| Close detail / Escape | Navigate to `/stack/:stackName` |
| Environment select | Local state: `production` \| `staging` \| `dev` (hardcoded options) |
| Refresh button | Re-fetch graph + status |
| Poll | Every 10s while mounted |
| Node drag | Snap + de-overlap; positions kept until env change |

Navbar brand is static text “perch”; stack title comes from graph `appName` or route `:stackName`.

---

## Health indicators

`deriveStatus` in `lib/mappers.js`:

| Status | Meaning |
|--------|---------|
| `source` | GitHub non-deployable |
| `unknown` | No matching status row |
| `down` | `healthy === false` |
| `degraded` | healthy and `error_rate >= 0.01` |
| `healthy` | otherwise |

**Invariant reminder:** unknown/missing must not look like healthy. Dot for unknown is slate, not green.

Active probe health (`/api/status`) is separate from Pulse intelligence (`/api/pulse/*`).

**Pulse overlay (Milestone B):** `PulseIndicator` on `ServiceCard` and `PulseSection` in the detail panel consume `/api/pulse/services` (+ incidents when needed). Join is via backend `graph_node` only. Kinds: `open` / `historical` / `stale` / `unavailable` / `unknown`. Recovered incidents use a historical (slate) indicator — never force probe `StatusPill` red/green. `process_memory_unavailable` is shown as unavailable/unknown, never healthy. Incident deep-link: `?incident=<url-encoded-id>` on the existing `/stack/:stackName/:nodeId` route.

---

## Panels

**Detail panel (300px):**

- Header: label, provider · environment, close.
- Summary grid: region / status / branch (often “—” for live custom nodes).
- Extra meta: project, service, error rate, daily $, recent errors.
- Pulse section: intelligence availability, incident counts, freshness, signals, summary, incident reference (Milestone B — not full investigation).
- Tabs: `deployments` | `logs`.
- Custom providers: copyable status/logs shell commands instead of provider log fetch.
- Missing node id: “No node exists for this id in the stack graph.”

---

## Loading / empty / error states

| State | Current behavior |
|-------|------------------|
| Initial load | `usePerchData` starts with **mock graph** (`data/mock.js`); `loading` true but **StackView does not render a loading UI** |
| API success | Replaces nodes/edges with mapped live data |
| API failure | Keeps **last known** graph (initially mock); amber banner: “Could not reach perch — showing last known or demo data” (dismissible until error message changes) |
| Empty graph JSON | Would render empty React Flow (no dedicated empty illustration) |
| Selected node missing from live data | Detail panel may still resolve via **mockNodes** fallback in `StackView` |
| Logs loading | Spinner + “Fetching logs...” in logs tab |
| Logs error | Red message + Retry |

### Known: mock data fallback

The SPA **does** seed and retain mock topology when the viz API is unreachable. Banner copy says “last known or demo data” so first-paint / total-failure states are not presented as live stack health. After a successful fetch, a later failure still shows the last successful payload under the same banner.

Milestone B+ work must keep demo fixtures distinguishable from live data. Do not silently treat mock as production health.

---

## Responsive behavior

- Desktop-first: full-height flex layout.
- Detail panel is fixed 300px; on narrow viewports it shrinks the graph rather than stacking (no dedicated mobile layout).
- Cards truncate long labels/URLs (`truncate` + `title` tooltips).
- No separate mobile navigation.

Baselines capture desktop (1280×720) and a narrower (390×844) viewport for regression awareness.

---

## Known UI limitations / inconsistencies

Documented, **not fixed** in the verification-prep milestone:

1. Mock seed + mock selected-node fallback (see above).
2. `loading` from `usePerchData` unused in `StackView`.
3. Environment options hardcoded; Astronomy Shop uses `local`, which is **not** in the select list.
4. Service card “tabs” only navigate; they do not switch panel tabs.
5. Provider-specific card tabs differ from detail panel tabs (`deployments`/`logs` only).
6. Animated edges + fitView can cause minor screenshot variance (E2E disables motion where possible).
7. Vite template README / default assets (`hero.png`, vite/react SVGs) remain in the package but are unused by the viz SPA.
8. Pulse Milestone B adds indicators + compact detail section only; full incident investigation is Milestone C.

---

## Screenshot baselines

See `web/UI_VERIFICATION.md` and `web/e2e/__screenshots__/`.

Baselines are the visual contract for “do not redesign.” Intentional product changes that alter pixels must update baselines **explicitly** (`--update-snapshots`), never via silent CI overwrite.
