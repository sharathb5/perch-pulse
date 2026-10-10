# Deterministic viz fixtures

JSON payloads shaped like live viz APIs for Vitest and Playwright.

| File | API shape |
|------|-----------|
| `graph.*.json` / `status.*.json` | `/api/graph`, `/api/status` |
| `pulse.services.*.json` | `/api/pulse/services` |
| `pulse.incidents.*.json` / `pulse.incident.detail.json` | `/api/pulse/incidents` |
| `pulse.changes*.json` | `/api/pulse/changes` |

- No live network, Astronomy Shop Docker, Databricks, or credentials.
- Pulse fixtures use explicit `graph_node` joins (`api` / `web` / `db`) — not display-name heuristics.
- `process_memory_unavailable` fixtures must never be presented as live telemetry.
- Used by `web/e2e/*` via Playwright route mocking and by `web/src/**/*.test.js`.
