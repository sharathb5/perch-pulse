# Perch Pulse — recording runbook (LIVE)

Practical workflow to record a polished **4–5 minute** walkthrough for Databricks
AI Forward Deployed Engineer audiences using the **existing** OpenTelemetry
Astronomy Shop — not a separate demo app.

Orchestration: [`scripts/demo/recording.sh`](../scripts/demo/recording.sh) via Make:

```bash
make demo-preflight   # optional
make demo-up          # no fault injection
make demo-status      # read-only
make demo-fault       # error-payment ON
make demo-recover     # error-payment OFF
make demo-down        # stop demo-owned viz only; leave shop up
```

Related: narrative script [`demo-script.md`](demo-script.md), broader orchestrator
[`demo-runbook.md`](demo-runbook.md) (`pulse-demo.sh` mock/replay/offline).

---

## A. Pre-recording checklist

- [ ] Free disk **≥ 15 GiB** on `/` (`df -h /`)
- [ ] Docker Desktop running; Astronomy Shop healthy (or approve start)
- [ ] No active `error-payment` fault (`make demo-status` → Fault injection: OFF)
- [ ] No unpaid LLM / Databricks calls planned for the take (use prior verified records)
- [ ] Browser windows pre-sized; screen recorder ready
- [ ] Optional: Databricks workspace bookmarked (read-only Delta + MLflow from 2026-10-13)

---

## B. Browser / window layout

| Window | Role |
|--------|------|
| **Main** | Perch viz — Astronomy Shop graph + Pulse sidebar (`http://127.0.0.1:3131/?env=local`) |
| **Secondary** | Payment metrics view (5s) **or** Prometheus graphs; Grafana only if healthy |
| **Optional** | Astronomy Shop storefront `http://localhost:8080/` |
| **Later** | Databricks catalog tables + MLflow experiment (read-only) |

**Critical:** Navbar must show environment **`local`**. If it is `production`, the SPA keeps the GitHub/Vercel/Render/Supabase **mock seed** after `/api/graph?env=production` returns 400. Always open `/?env=local` (demo-up prints this URL; viz auto-opens it).

Graph edges are **static dependencies** from `examples/astronomy-shop/perch.yaml` — not live trace inference. Live Pulse overlays regressions from Prometheus spanmetrics.

Preferred secondary metrics (real Prom samples, 5s refresh):

`http://127.0.0.1:3132/`  (started by `demo-up`; proxies allowlisted payment queries)

If Grafana is responsive, Spanmetrics with 5s refresh:

`http://localhost:8080/grafana/d/W2gX2zHVk48/spanmetrics-demo-dashboard?orgId=1&refresh=5s&from=now-15m&to=now&var-service=payment`

Grafana “No data” / upstream timeout often means Grafana is CPU-starved — **not** missing telemetry. Confirm with Prometheus or `:3132` before troubleshooting collectors.

### Prometheus queries (verified nonempty under load-generator)

Call rate:

```promql
sum by (service_name) (rate(traces_span_metrics_calls_total{service_name="payment"}[2m]))
```

Error rate (matches Live Pulse signal semantics):

```promql
sum by (service_name) (
  increase(traces_span_metrics_calls_total{service_name="payment",status_code="STATUS_CODE_ERROR"}[2m])
) / clamp_min(
  sum by (service_name) (increase(traces_span_metrics_calls_total{service_name="payment"}[2m])),
  1
)
```

Optional p99 latency:

```promql
histogram_quantile(0.99,
  sum by (service_name, le) (
    rate(traces_span_metrics_duration_milliseconds_bucket{service_name="payment"}[2m])
  )
)
```

Direct Prometheus UI: `http://localhost:9090/`  
(Envoy does not proxy Prometheus; host port **9090** is the demo surface.)

**Honesty:** these are live OpenTelemetry spanmetrics from the running shop. Do not
replay fixtures into Grafana/Prometheus and call them LIVE.

**Limitation:** adding a custom Grafana dashboard into vendor `.demo` provisioning
requires a stack recreate. Prefer the existing Spanmetrics dashboard + PromQL above.

---

## C. Exact commands

```bash
# 0) Optional read-only checks
make demo-preflight
make demo-status

# 1) Start / reuse shop + Live Pulse viz (NO fault)
make demo-up
# If shop is down and you authorize container start:
# DEMO_APPROVE_SHOP_START=1 make demo-up

# 2) Wait until Baseline: READY (warming may take ~2–3 minutes)
make demo-status

# 3) Inject payment regression
make demo-fault

# 4) After ACTIVE incident + Investigate demo — recover
make demo-recover

# 5) Cleanup (fault OFF, demo viz stopped, shop left running)
make demo-down
```

Equivalent script form: `./scripts/demo/recording.sh <preflight|up|status|fault|recover|down>`.

---

## D. Expected readiness indicators

From `make demo-status`:

| Line | Healthy recording start |
|------|-------------------------|
| Astronomy Shop | READY |
| Synthetic traffic | ACTIVE |
| Prometheus | READY |
| Payment telemetry | FRESH |
| Baseline | READY (WARMING is wait; UNKNOWN if viz down) |
| Payment error rate | ~0 before fault; rises after `demo-fault` |
| Perch viz | READY |
| Pulse API | READY (collecting) |
| Grafana | READY |
| Jaeger | READY (needed for Run Pulse) |
| Fault injection | OFF before demo; ON during fault |

Never treat “container exists” alone as READY — probes use HTTP + metrics + APIs.

---

## E. Where to click in Perch

1. **Service graph** — payment node in the Astronomy Shop topology.
2. **Pulse sidebar → Incidents** — wait for **ACTIVE** after `make demo-fault`.
3. Open the **ACTIVE** payment incident (`astronomy/local/payment`, signal `error_rate`).
4. **Investigate** — F1 immutable evidence (OBSERVED / HYPOTHESIS / UNKNOWN).
5. Expand **Evidence** digests (before / during).
6. Walk one **Hypothesis** + “Verify: …” step — say correlation ≠ causation.
7. Optional **Run Pulse** tab if Jaeger is READY (cross-service diagnostics on demand).
8. After `make demo-recover`, same incident → **RECOVERED** (historical, not a current outage).

UI polls Pulse/live ≈ **8s**; Live collector poll default **15s**.

---

## F. Databricks read-only presentation

Dated **2026-10-13** demo sample (see [`verification-matrix.md`](verification-matrix.md#2026-10-13--live-databricks-delta--mlflow-demo-sample)):

| Surface | What to show |
|---------|----------------|
| Delta | Catalog `workspace`, domain schemas under `analytics_*` — **10 tables**, **37 rows** |
| Idempotent rerun | Same load → **37 → 37** (no duplicate key+fingerprint rows) |
| MLflow | Experiment `/Users/sharathjb23@gmail.com/perch-pulse-investigation-eval` |
| MLflow run | `193935ec502f4496a5a7a1fc44118f6f` — artifact `eval/eval_summary.json` |

**No new writes needed for the recording.**  
Label out loud: **demo/synthetic fixture sample** — not production telemetry quality, not live-model performance.

---

## G. Expected timing and edit points

| Beat | Approx. wall | Edit tip |
|------|--------------|----------|
| Architecture + graph | 0:00–0:30 | Cut dead air |
| Show live payment metrics | 0:30–0:50 | Grafana/Prom secondary window |
| `demo-fault` + wait for ACTIVE | 0:50–2:00 | Speed-ramp wait; one measured detection ≈**70.3 s** |
| Investigate evidence | 2:00–3:00 | |
| Optional Run Pulse / honesty on LLM | 3:00–3:20 | |
| `demo-recover` + RECOVERED | 3:20–4:20 | One measured recovery ≈**130 s** — speed-ramp |
| Databricks Delta/MLflow slides | 4:20–4:50 | Read-only |
| Takeaway | 4:50–5:10 | |

Detection/recovery times are **not guarantees** — print warnings exist in `demo-fault` / `demo-recover`.

---

## H. Troubleshooting

| Symptom | What to do |
|---------|------------|
| No payment metrics | Confirm load-generator healthy; Prom label `service_name=payment`; wait 1–2m |
| Baseline not ready | Wait for Live Pulse `warming_up` → `collecting`; `make demo-status` |
| No incident yet | Keep fault ON; wait multiple 15s poll windows; confirm error rate rising in Prom |
| Fault remains active | `make demo-recover` or `./examples/astronomy-shop/scenarios/scenario.sh stop` |
| Viz is stale | Confirm `PERCH_LIVE_PULSE_*` via demo-up; check `/api/pulse/live/status` |
| Incident recovered before Investigate | Re-run `demo-fault` after baseline READY; Investigate while ACTIVE |
| Run Pulse unavailable | Need Jaeger READY + Run Pulse env from demo-up; not required for Live incident |
| Shop unhealthy | `./examples/astronomy-shop/scripts/verify-live.sh`; approve `DEMO_APPROVE_SHOP_START=1 make demo-up` |

---

## I. Cleanup and shutdown checklist

- [ ] `make demo-recover` (or confirm Fault injection: OFF)
- [ ] `make demo-down` — stops **demo-owned** perch viz only
- [ ] Shop containers still running unless you separately run `./examples/astronomy-shop/scripts/stop.sh`
- [ ] Do **not** Docker prune/purge; do **not** delete Pulse incident history under `.perch/demo-recording/pulse-dir`
- [ ] Do **not** modify Databricks resources

---

## J. Facts we can accurately claim

Label as **single live run / demo integration measurements** — not SLOs.

| Claim | Source |
|-------|--------|
| Detection latency ≈ **70.3 s** (`error-payment`) | 2026-10-15 live-fault-smoke |
| Recovery latency ≈ **130.0 s** | same |
| Payment `error_rate` **0 → 0.5** under `paymentFailure=50%` | same |
| Delta **37 rows / 10 tables** | 2026-10-13 Databricks demo sample |
| Idempotent rerun maintained **37 rows** | same |
| Live MLflow evaluation artifact persisted | same run id above |

Never portray synthetic evaluation fixtures as live-model quality.  
Never claim browser sidebar was verified during the 2026-10-15 ACTIVE window (API was).

---

## Verified local URLs (this environment)

| Surface | URL |
|---------|-----|
| Astronomy Shop | http://localhost:8080/ |
| Grafana | http://localhost:8080/grafana/ |
| Spanmetrics dashboard | http://localhost:8080/grafana/d/W2gX2zHVk48/spanmetrics-demo-dashboard?var-service=payment |
| Prometheus | http://localhost:9090/ |
| Jaeger | http://localhost:8080/jaeger/ui/ |
| Perch viz (Astronomy Shop) | http://127.0.0.1:3131/?env=local |
| Payment metrics (5s) | http://127.0.0.1:3132/ |

Override viz port with `DEMO_VIZ_PORT`. Reload safely: `make demo-down && make demo-up` (rebuilds embedded UI when sources change; leaves shop containers running).
