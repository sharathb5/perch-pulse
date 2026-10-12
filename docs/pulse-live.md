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

## Fault-to-recovery validation

Deterministic coverage (in `make verify` / `go test ./internal/pulse/live/test/...`):

- `TestLive_FaultLifecycle_ShippingLatency` — httptest Prometheus fixture →
  baseline → shipping latency spike → active incident → missing≠recovery →
  measured recovery → historical `incident_id` continuity via live API.
- Sibling tests: no duplicate incidents, absence≠recovery, provider outage
  retains snapshot (stale≠healthy).

### Live Astronomy Shop scenario (CI `live-fault`)

**Selected for CI (evidence-based):** `error-payment` — ambient Locust traffic
reliably exercises checkout→payment; `paymentFailure=50%` produces `error_rate`
above detector floors (`delta≥0.10`, current≥`0.05`) without changing thresholds.

| Field | Value |
|-------|-------|
| Scenario id (flagd only) | `error-payment` |
| Flag | `paymentFailure=50%` |
| Target Pulse service | `astronomy/local/payment` |
| Graph node | `payment` |
| Expected signal | `error_rate` |
| Traffic | Demo load-generator ambient checkout (no custom generator) |
| Baseline wait | minimum 180s wall clock **and** fresh target baseline |
| Fault window | ≤180s |
| Recovery window | ≤150s |
| Max job | ~45–50 min including stack start |

**Preferred product-wise but not used in CI live-fault:** `latency-shipping-intl`
(`intlShippingSlowdown=10sec` → `astronomy/local/shipping` / `latency_ms`). First
CI attempt ([run 38202773957](https://github.com/sharathb5/perch-pulse/actions/runs/38202773957))
ingested real shipping telemetry (baseline p99 ≈9.8 ms → fault ≈45 ms) but never
cleared `LatencyAbsMinMS=1000`. Sparse international addresses diluted spanmetrics
p99; thresholds were **not** lowered.

Scenario labels must **never** appear in Live Pulse API responses. Injection uses
`internal/pulse/scenario` harness only.

Live run (manual, not a PR gate):

```bash
# GitHub Actions → Astronomy Smoke → mode=live-fault, confirm=run
./examples/astronomy-shop/scripts/ci-live-fault.sh
```

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
- Fixture/integration tests prove the chain; live Astronomy Shop Docker fault
  lifecycle is `workflow_dispatch` only (`mode=live-fault`), never required for PRs.
- International shipping paths can be sparse; if the live run cannot qualify a
  shipping latency regression within the fault budget, fail honestly and keep
  artifacts — do not inject Prom fixtures into the live verifier.

## Local verification

```bash
go test ./internal/pulse/live/test/... ./internal/pulse/telem/... -count=1
make pulse-integration
make verify
```
