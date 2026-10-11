# Run Pulse — on-demand diagnostics (Milestone E)

User-triggered, allowlisted synthetic diagnostics that correlate a **safe** HTTP request with a Jaeger distributed trace and map observed services onto the Perch graph.

This is **not** continuous telemetry monitoring, incident detection, or automated remediation.

Parent issues: [#25](https://github.com/sharathb5/perch-pulse/issues/25), [#17](https://github.com/sharathb5/perch-pulse/issues/17). ADR: [ADR-020](DECISIONS.md).

## How it differs from other Pulse surfaces

| Surface | Source | Meaning |
|---------|--------|---------|
| Active health | `GET /api/status` | Probe reachability / StatusPill |
| Historical Pulse | `GET /api/pulse/services\|incidents\|changes` | Persisted intelligence snapshots |
| **Run Pulse** | `GET/POST /api/pulse/workflows\|runs` | Explicit on-demand diagnostic |
| Future continuous live telemetry | — | **Not implemented** |

A completed Run Pulse result must **not** overwrite probe health or historical incident state.

## Supported workflow (MVP)

| Field | Value |
|-------|-------|
| ID | `astronomy.recommendations-browse.v1` |
| Display name | Recommendations Browse |
| Environment | Local Astronomy Shop demo |
| Operation | `GET /api/recommendations?productIds=OLJCESPC7Z` |
| Validation identities | `frontend`, `recommendation`, `product-catalog` |

Observed services shown in the UI come from the **qualified trace**, not from the expected list alone. Expected identities are used only for qualification thresholds.

## Configuration

Explicit server-side env (loopback only):

```bash
export PERCH_RUN_PULSE_DEMO_BASE_URL=http://127.0.0.1:8080
# optional:
export PERCH_RUN_PULSE_JAEGER_BASE=http://127.0.0.1:8080/jaeger/ui

# From an Astronomy Shop checkout with demo running:
cd examples/astronomy-shop
PERCH_RUN_PULSE_DEMO_BASE_URL=http://127.0.0.1:8080 perch viz --env local --port 3131
```

If `PERCH_RUN_PULSE_DEMO_BASE_URL` is unset, the workflow is listed as **unavailable** with a clear reason. Non-loopback URLs are rejected (SSRF defense).

## API

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/pulse/workflows` | Allowlisted catalog + availability |
| POST | `/api/pulse/runs` | Start one diagnostic (`{"workflow_id":"…"}`) — **loopback clients only** |
| GET | `/api/pulse/runs/{id}` | Poll run status / bounded evidence |

Schemas: `pulse.api.workflows.v1`, `pulse.api.runs.create.v1`, `pulse.api.run.v1`.

### State machine

`pending` → `running` → `completed` | `failed` | `timed_out`

`completed` requires successful **trace qualification** (same-trace multi-service proof). HTTP 200 alone is insufficient.

### Execution limits (MVP)

- Max concurrent runs: 1
- Overall timeout: 90s (workflow default)
- Max probe GETs: 3
- Bounded Jaeger polling; context cancellation
- In-memory store: max 20 runs, 30m retention after finish
- Process restart clears runs (UI documents this)

## Security

- No arbitrary URLs from the browser — server allowlist only
- Demo/Jaeger bases must be http(s) **loopback** (DNS resolve + dial-time re-check); query/fragment rejected on configured bases
- Redirects disabled on diagnostic HTTP client
- `X-Forwarded-For` ignored / rejected on start
- POST start requires loopback `RemoteAddr`, `Content-Type: application/json`, and loopback `Origin` when present (cross-site browser POSTs rejected)
- `perch viz` binds `127.0.0.1` — do not expose a remotely reachable viz with this endpoint without additional auth design
- No secrets in responses; bounded redacted evidence only (URL query stripped except `productIds`)

## Trace qualification reuse

Shared package: `internal/pulse/astronomy/tracesmoke` (also used by Astronomy Shop CI smoke).

Authoritative pieces: Jaeger JSON parse, marker subgraph, qualification, evidence summaries, HTTP probe/query helpers.

## UI

Pulse sidebar tab **Run** (`?pulse=run`, optional `?run=<run_id>`).

Completed runs highlight **observed** graph nodes (amber). Nodes with error spans use a stronger amber ring. Unobserved nodes are unchanged (never shown as failed). Unmapped OTEL services are listed as evidence without inventing graph nodes.

## Local verification (deterministic)

```bash
go test ./internal/pulse/runpulse/... ./internal/pulse/api/test/ ./internal/pulse/astronomy/tracesmoke/...
cd web && npm test && npm run test:e2e -- e2e/pulse-run.spec.js
make verify
```

Live Astronomy Shop + Run Pulse API validation is optional and must not be required on every PR (see Astronomy Smoke `workflow_dispatch`).

The Astronomy Smoke workflow starts `perch viz` with `PERCH_RUN_PULSE_DEMO_BASE_URL`, then:

1. `cmd/trace-smoke` — library-level Jaeger qualification (unchanged)
2. `cmd/run-pulse-api-smoke` — real `POST /api/pulse/runs` + polled `GET /api/pulse/runs/{id}` with evidence assertions (`internal/pulse/runpulse/apismoke`)

Both stages share one Astronomy Shop stack; the second probe exists to prove the HTTP API path, not to replace the first.

## Known limitations

- Single workflow; Astronomy Shop demo only
- In-memory runs (lost on restart)
- No edge highlighting in MVP UI
- No critical-path / bottleneck inference
- Not a universal production workflow engine
