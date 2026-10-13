# Pulse HTTP API (Milestone A + E + Live + F1 + F2a)

Localhost JSON for the Perch React UI. Mounted on the existing `perch viz` server (127.0.0.1). See [ADR-014](DECISIONS.md), [ADR-020](DECISIONS.md), [ADR-022](DECISIONS.md), and [ADR-023](DECISIONS.md).

Historical intelligence routes remain GET-only. Run Pulse adds an allowlisted POST for on-demand diagnostics — see [`pulse-run.md`](pulse-run.md). Live Pulse adds opt-in continuous snapshot routes — see [`pulse-live.md`](pulse-live.md). Investigations (F1) assemble evidence snapshots — see [`pulse-investigation.md`](pulse-investigation.md).

## Routes

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/pulse/services` | Pulse intelligence per known service ID |
| GET | `/api/pulse/incidents` | Recent incidents (`?limit=`, default 50, max 200) |
| GET | `/api/pulse/incidents/{id...}` | Incident detail + on-read correlation |
| GET | `/api/pulse/incidents/{id...}/investigation-context` | Agent-oriented investigation context (suffix on `{id...}`) |
| GET | `/api/pulse/changes` | Recorded changes (`?limit=`) |
| GET | `/api/pulse/changes/{id...}` | Change detail |
| GET | `/api/pulse/workflows` | Run Pulse allowlisted workflow catalog |
| POST | `/api/pulse/runs` | Start one Run Pulse diagnostic (loopback only) |
| GET | `/api/pulse/runs/{id}` | Poll Run Pulse run status / bounded evidence |
| GET | `/api/pulse/live/status` | Live Pulse runtime status (opt-in collector) |
| GET | `/api/pulse/live/services` | Live per-service telemetry snapshot |
| GET | `/api/pulse/live/incidents` | Live/active vs recovered incident labels |
| POST | `/api/pulse/investigations` | Assemble evidence investigation (loopback only) |
| GET | `/api/pulse/investigations/{id...}` | Persisted investigation snapshot |
| POST | `/api/pulse/investigations/{id...}/feedback` | Record investigation feedback (loopback only) |
| GET | `/api/pulse/investigations/{id...}/feedback` | List feedback for an investigation |
| GET | `/api/pulse/llm/status` | Optional LLM investigator availability (no secrets) |
| POST | `/api/pulse/investigations/{id...}/llm-propose` | Generate constrained LLM proposal (loopback; opt-in) |
| GET | `/api/pulse/investigations/{id...}/proposals` | List LLM proposals for an investigation |
| GET | `/api/pulse/proposals/{id...}` | Persisted LLM proposal |
| POST | `/api/pulse/proposals/{id...}/feedback` | Proposal-only feedback (not F1 feedback) |
| GET | `/api/pulse/llm/status` | Optional LLM investigator availability (no secrets) |
| POST | `/api/pulse/investigations/{id...}/llm-propose` | Generate constrained LLM proposal (loopback only; opt-in) |
| GET | `/api/pulse/investigations/{id...}/proposals` | List LLM proposals for an investigation |
| GET | `/api/pulse/proposals/{id...}` | Load a persisted LLM proposal |
| POST | `/api/pulse/proposals/{id...}/feedback` | Proposal-only feedback (not mixed with F1 feedback) |

`{id...}` is required because Pulse IDs embed `/` (e.g. `inc|astronomy/local/shipping|…`).

Existing Perch routes are unchanged: `/api/graph`, `/api/status`, `/api/logs`, `/api/credentials`.

## Data sources (explicit split)

| Kind | Backend | Notes |
|------|---------|-------|
| Incidents | `incident.FileStore` under `$PERCH_PULSE_DIR` or `<cwd>/.perch/pulse` | Persisted JSON snapshots |
| Changes | `change.FileStore` (same root) | Persisted JSON snapshots |
| Observations | Optional process-local `store.Store` | Wired when Live Pulse is enabled; otherwise `process_memory_unavailable` |
| Correlation | `correlate.Correlate` at request time | Embedded in incident detail; **not causation** |
| Live snapshot | `internal/pulse/live` collector | Opt-in; `/api/pulse/live/*` only |

Historical routes remain snapshot reads of persisted artifacts. Continuous updates require Live Pulse (`PERCH_LIVE_PULSE_ENABLED`). Empty Pulse dirs return empty lists / unavailable intelligence — never mock incidents.

## Active health vs Pulse intelligence

- **Active health** (`GET /api/status`): can the service be reached / probe latency.
- **Pulse intelligence** (`GET /api/pulse/services`): telemetry/regression state from observations or open persisted incidents.

These are never collapsed into one boolean. Missing or stale telemetry is never reported as healthy.

## Service ID ↔ graph node mapping

Graph node IDs are `perch.yaml` node names (e.g. `shipping`). Pulse IDs for Astronomy Shop are `astronomy/<env>/<name>` (ADR-009).

Milestone A resolves bindings via the embedded astronomy `compose_service` table (`mapping_source: astronomy_compose`). No display-name heuristics.

Response fields for Milestone B:

- `service_id` — Pulse identity
- `graph_node` — perch graph node when mapped
- `intelligence` / `freshness` / `open_incident_ids`
- `active_health_note` — reminds clients to keep using `/api/status`

## Milestone B consumption plan

1. Keep loading topology from `/api/graph` and active probes from `/api/status` (unchanged).
2. Fetch `/api/pulse/services` in parallel.
3. Join on `graph_node` (preferred) or map node name → `service_id` using the same astronomy table.
4. Overlay intelligence badges without replacing `deriveStatus` probe semantics; show `stale` / `unknown` / `unavailable` explicitly.
5. Incident deep-links use `/api/pulse/incidents/{id}` (correlation already included).

## Security

- Localhost bind preserved (`127.0.0.1`).
- Historical routes remain read-only.
- Run Pulse POST is loopback-only, allowlisted workflows only, no arbitrary URLs (see [`pulse-run.md`](pulse-run.md)).
- Resource IDs validated against path traversal / control characters.
- Secret-shaped metadata and bearer-like substrings redacted before JSON encode.
- No PromQL, Docker, credential dumps, or arbitrary filesystem reads outside the Pulse store root.
- Scenario ground-truth packages are not imported; evaluation labels must not appear in responses.

## Local verification

```bash
# Point at persisted change/incident fixtures (no Docker required):
PERCH_PULSE_DIR=examples/astronomy-shop/change/results perch viz --port 3131

curl -sS http://127.0.0.1:3131/api/graph?env=local | head
curl -sS http://127.0.0.1:3131/api/pulse/services | jq .
curl -sS http://127.0.0.1:3131/api/pulse/incidents | jq .
```
