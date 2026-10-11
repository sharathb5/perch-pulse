# Live Pulse Runtime (backend)

Optional continuous telemetry ingestion inside `perch viz`. **Not** Run Pulse.
Does **not** trigger synthetic requests. Passive Prometheus scrape → existing
detector → incident lifecycle → read-only snapshot API.

See [ADR-022](DECISIONS.md).

## Enable (opt-in)

```bash
export PERCH_LIVE_PULSE_ENABLED=1
export PERCH_LIVE_PULSE_PROM_URL=http://127.0.0.1:9090
# optional:
# export PERCH_LIVE_PULSE_POLL_INTERVAL=15s
# export PERCH_LIVE_PULSE_TIMEOUT=10s
# export PERCH_LIVE_PULSE_ENVIRONMENT=local
# export PERCH_LIVE_PULSE_FRESHNESS=2m
# export PERCH_LIVE_PULSE_SAMPLE_WINDOW=2m
# export PERCH_PULSE_DIR=/path/to/.perch/pulse

perch viz --env local --port 3131
```

When disabled or misconfigured, original Perch, historical Pulse APIs, and Run
Pulse remain unchanged. Live endpoints report **unavailable**, never healthy.

## Security boundaries

| Rule | Detail |
|------|--------|
| Loopback only | `PERCH_LIVE_PULSE_PROM_URL` validated with the same helpers as Run Pulse (`ValidateAllowlistedHTTPURL` + `SafeHTTPClient`) |
| No browser PromQL | Fixed allowlisted queries in `internal/pulse/telem` only |
| No frontend secrets | Config is server-side env only |
| Bounded I/O | Timeouts, 4 MiB response cap, series cardinality cap |

## Metrics (Astronomy Shop spanmetrics)

| Signal | Prom series | Notes |
|--------|-------------|-------|
| `latency_ms` | `traces_span_metrics_duration_milliseconds_bucket` | p99 via `histogram_quantile` |
| `error_rate` | `traces_span_metrics_calls_total` | ERROR / total; 0 only when call_rate observed |
| `call_rate` | `traces_span_metrics_calls_total` | increase / window seconds |

Service IDs: `astronomy/<environment>/<otel_service_name>` (no fuzzy graph matching).

## API

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/pulse/live/status` | Runtime enabled/configured/freshness/errors |
| GET | `/api/pulse/live/services` | Per-service signals + regression flags |
| GET | `/api/pulse/live/incidents` | Active/recovered/historical lifecycle labels |

Handlers read a concurrency-safe **snapshot**. They never query Prometheus.

## Freshness semantics

- **fresh** — last successful sample within `PERCH_LIVE_PULSE_FRESHNESS`
- **stale** — last-known values retained after outage; not healthy
- **missing** — no series this poll; not zero errors
- **warming_up** — baseline windows not yet decidable; no regression claim from a single sample
- Missing telemetry is **not** recovery (detector leaves open findings when not decidable)

## Incident lifecycle

Reuses `detect.Detector` + `incident.Build`. Persistence uses
`incident.FileStore.Upsert` so evidence refresh and recovery can update the same
`incident_id` (Append alone rejected conflicting updates).

## Independence

| Concern | Surface |
|---------|---------|
| Active probes | `GET /api/status` |
| Historical Pulse | `GET /api/pulse/services\|incidents\|changes` |
| Live Pulse | `GET /api/pulse/live/*` |
| Run Pulse | `GET /api/pulse/workflows`, `POST /api/pulse/runs` |

## Limitations (MVP)

- Process restart clears rolling baselines; open files remain but a continuing
  regression may open a new incident after warmup.
- Demo-scale detector windows (same as pipeline harness defaults).
- Loopback Prometheus only.
- Sample `ObservedAt` uses the poll clock (Prometheus instant-vector timestamps
  are validated for finiteness but not used as the detector clock).
- Error-rate PromQL uses `clamp_min(..., 1)` on the call denominator (pre-existing
  spanmetrics query); very low traffic can understate ratios — documented, not
  silently treated as healthy zeros for unobserved services.
- Fixture/integration tests prove the chain; live Astronomy Shop Docker is not
  a PR-required gate (see smoke follow-up in UI PR).

## UI consumption (Part 2)

The React viz polls `GET /api/pulse/live/*` on the same interval as historical
Pulse (≈8s). Overview shows a **Live telemetry** section; graph nodes with a
fresh live regression set `data-live-pulse="regression"` and Pulse kind
`live_regression` (does not replace StatusPill or Run Pulse amber rings).

Fixtures under `web/fixtures/pulse.live.*.json` are regenerated from the Go API:

```bash
UPDATE_LIVE_FIXTURES=1 go test ./internal/pulse/live/test/ -run TestWriteLiveUIFixtures
```

## Local verification

```bash
go test ./internal/pulse/live/test/... ./internal/pulse/telem/... -count=1
make pulse-integration
cd web && npm test && npm run test:e2e
make verify
```
