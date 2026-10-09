# Astronomy Shop scenario harness (ground truth)

Reproducible **controlled fault injection** and **negative-control** scenarios for OpenTelemetry Astronomy Shop (pin **3.1.0**), with versioned ground-truth records for later Pulse detector evaluation.

This is **not** a detector. Labels stay out of Pulse `observation` / `store`.

## Quick start

Astronomy Shop must already be running (minimal + o11y):

```bash
./examples/astronomy-shop/scripts/start.sh
./examples/astronomy-shop/scripts/verify-live.sh
```

Then:

```bash
./examples/astronomy-shop/scenarios/scenario.sh list
./examples/astronomy-shop/scenarios/scenario.sh start error-payment
# …workload continues; dwell as needed…
./examples/astronomy-shop/scenarios/scenario.sh stop
./examples/astronomy-shop/scenarios/scenario.sh status
```

Live verify all catalog scenarios (activation + recovery; no LLM):

```bash
./examples/astronomy-shop/scenarios/verify-scenarios.sh
```

## Catalog

| ID | Type | Class | Flag (flagd) | Active |
|----|------|-------|--------------|--------|
| `latency-shipping-intl` | latency | fault | `intlShippingSlowdown` | `5sec` |
| `error-payment` | error_rate | fault | `paymentFailure` | `50%` |
| `outage-payment` | dependency_outage | fault | `paymentUnreachable` | `on` |
| `control-emit-raw-pii` | neutral | control | `emitRawPii` | `on` |

Mechanisms use the demo **flagd-ui** HTTP API (`/feature/api/read`, `/feature/api/write`) — no Astronomy Shop fork.

## Ground truth

Schema: `pulse.scenario.v1` (`internal/pulse/scenario`).

Persisted under `results/` (gitignored):

- `active.json` — currently active run (if any)
- `<run_id>.json` — full ground-truth record
- `live-verify-summary.json` — optional live harness summary

Records capture scenario id, type, classification, target Pulse `service_id`, UTC start/end, injected flag parameters, expected affected services / dependency path, activation/recovery verification, and evidence refs.

## Boundaries

| Layer | Location |
|-------|----------|
| Ground truth labels | `internal/pulse/scenario` + `results/*.json` |
| Observed telemetry | existing collectors / future observation ingest |
| Detector output | **not implemented** |

Do not feed ground-truth records into detector inputs as unlabeled features.

## Tests (CI)

```bash
go test ./internal/pulse/scenario/... -count=1
```

Included in `make verify`. Docker / live flagd is **not** required for CI.
