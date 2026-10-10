# Astronomy Shop local target (Phase 2)

Reproducible helpers to run the OpenTelemetry Astronomy Shop next to Perch Pulse and map its topology into Pulse service IDs.

**Full documentation:** [`docs/astronomy-shop.md`](../../docs/astronomy-shop.md)  
**Authoritative mapping:** [`internal/pulse/astronomy/service-mapping.yaml`](../../internal/pulse/astronomy/service-mapping.yaml)

## Quick start

```bash
# From perch-pulse repository root
./examples/astronomy-shop/scripts/clone.sh
./examples/astronomy-shop/scripts/start.sh          # make start-minimal
./examples/astronomy-shop/scripts/verify-live.sh
./examples/astronomy-shop/scripts/stop.sh
```

Override clone location:

```bash
export PERCH_ASTRONOMY_DEMO_DIR=/path/to/opentelemetry-demo
./examples/astronomy-shop/scripts/clone.sh
```

## Files

| Path | Purpose |
|------|---------|
| `scripts/clone.sh` | Shallow-clone git tag **3.1.0** |
| `scripts/start.sh` | `make start-minimal` (shop + observability) |
| `scripts/stop.sh` | Compose `down` without `--volumes` (safer than upstream `make stop`) |
| `scripts/verify-live.sh` | Frontend + container + optional Jaeger checks |
| `perch.yaml` | Optional Perch graph for localhost / Docker health |
| `.demo/` | Gitignored clone (default) |

## Modes

Default is **minimal + observability** (~3 GB RAM). For Kafka / accounting / fraud-detection:

```bash
ASTRONOMY_MODE=full ./examples/astronomy-shop/scripts/start.sh
```

## Scenario harness (ground truth)

```bash
./examples/astronomy-shop/scenarios/scenario.sh list
./examples/astronomy-shop/scenarios/scenario.sh start error-payment
./examples/astronomy-shop/scenarios/scenario.sh stop
./examples/astronomy-shop/scenarios/verify-scenarios.sh
```

See [`scenarios/README.md`](scenarios/README.md). Labels are evaluation-only (ADR-010).

## Detector evaluation

```bash
./examples/astronomy-shop/eval/run-detector-eval.sh
```

## Change / deployment correlation

```bash
# Simulated deploy marker (not a real cloud deploy)
./examples/astronomy-shop/change/deploy-marker.sh astronomy/local/payment abc123

# Live: marker → fault → detect → incident → correlate → changeeval
./examples/astronomy-shop/eval/run-change-eval.sh
```

See [`change/README.md`](change/README.md) and ADR-013.

See [`eval/README.md`](eval/README.md) and ADR-012.

## Validation without Docker

```bash
go test ./internal/pulse/astronomy/... ./internal/pulse/scenario/... -count=1
```

Live Docker verification is intentionally **not** part of `make verify` (too heavy for CI).