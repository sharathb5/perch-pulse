# Perch Pulse

**Evidence-backed AI infrastructure investigation** on top of open-source [Perch](https://github.com/yashg4509/perch).

Perch Pulse helps engineers investigate distributed-service regressions by combining **real telemetry**, **service topology**, **bounded diagnostics**, and **evidence-backed AI proposals** — with optional evaluation, JSONL analytics export, and Databricks Delta / MLflow lineage.

This fork extends Perch; core status/graph/logs/TUI remain usable **without Databricks** and **without paid LLM calls**.

**License:** [MIT](LICENSE) · **Contributing:** [CONTRIBUTING.md](CONTRIBUTING.md) · **Security:** [SECURITY.md](SECURITY.md)

---

## Problem

When payment or shipping misbehaves in a multi-service checkout, engineers face:

- Symptom dashboards without a bounded evidence pack
- AI summaries that blur **observation** vs **inference**
- No repeatable way to score investigation methods or export analytical history

Pulse targets that gap for local/demo stacks (OpenTelemetry Astronomy Shop) and keeps causality discipline: **correlation ≠ causation**, **stale ≠ healthy**, **report ≠ diagnosed**.

---

## Architecture overview

```
Astronomy Shop (OTEL) → Prometheus / Jaeger
        ↓
 Pulse live/detector → incidents (local store)
        ↓
 F1 deterministic investigation (immutable evidence snapshot)
        ↓ optional
 Constrained LLM proposal (citation-validated)
        ↓
 Human feedback · Run Pulse diagnostics
        ↓
 F2b/F2c offline evaluation
        ↓
 F3a versioned JSONL export
        ↓ optional / authorized
 F3b Databricks Delta MERGE + MLflow eval logging
```

| Layer | Role |
|-------|------|
| Perch core | `perch.yaml` topology, status, logs, TUI, `perch viz` |
| Pulse | Incidents, Live telemetry, Run Pulse, investigations |
| F1 | Deterministic evidence-backed report |
| F2a/F2c | Optional LLM + provider smoke (offline default) |
| F2b | Offline method comparison |
| F3a/F3b | JSONL export; optional Delta/MLflow |

Specs: [`docs/PRODUCT.md`](docs/PRODUCT.md) · [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) · [`docs/DECISIONS.md`](docs/DECISIONS.md)

---

## Supported demo

Primary scenario: **`error-payment`** (payment error-rate fault via flagd).

| Mode | Use when |
|------|----------|
| **MOCK** | Default offline fixtures — CI-safe, no Docker |
| **REPLAY** | Labeled historical sample export / UI fixtures |
| **LIVE** | Real shop telemetry + real fault (disk ≥ 15 GiB) |

Orchestration: [`docs/demo-runbook.md`](docs/demo-runbook.md) · spoken script: [`docs/demo-script.md`](docs/demo-script.md) · honesty matrix: [`docs/verification-matrix.md`](docs/verification-matrix.md)

```bash
./scripts/demo/pulse-demo.sh --mode mock --phase offline
```

---

## Getting started

```bash
git clone https://github.com/sharathb5/perch-pulse.git
cd perch-pulse
go build -o perch ./cmd/perch
make web-build-embed   # required before packages that embed web/
./perch --help
./perch viz            # localhost React graph + Pulse APIs
```

Requires the Go version in [`go.mod`](go.mod). Optional Homebrew for upstream Perch: see historical install notes below.

**Astronomy Shop (LIVE, heavy):** [`docs/astronomy-shop.md`](docs/astronomy-shop.md) — prefer GitHub-hosted smoke when disk is tight.

---

## Deterministic investigation (F1)

From an incident in the Pulse sidebar: **Investigate**.

- Assembles an **immutable** evidence snapshot + content fingerprint
- Labels **OBSERVED** / **HYPOTHESIS** / **UNKNOWN**
- Never claims diagnosed root cause from report generation alone

Docs: [`docs/pulse-investigation.md`](docs/pulse-investigation.md)

---

## Optional model-backed investigation (F2a / F2c)

Disabled unless the viz process opts in with credentials. CLI smoke is **offline by default**:

```bash
# Offline compatibility smoke (no network) — DEFAULT
go run ./cmd/perch pulse llm-provider-smoke --case-id case_payment_error --json

# Real provider (billable) — BOTH flags required
go run ./cmd/perch pulse llm-provider-smoke --case-id case_payment_error \
  --real-provider --i-authorize-provider-calls --json
```

| Item | Value |
|------|--------|
| Documented target | `openai_compatible` + `gpt-4o-mini` |
| Env (viz) | `PERCH_PULSE_LLM_ENABLED`, `PERCH_PULSE_LLM_API_KEY`, `PERCH_PULSE_LLM_BASE_URL`, `PERCH_PULSE_LLM_MODEL` |
| Real validation in this RC | **NOT RUN** unless you authorize it |

Fixture smoke ≠ live model quality.

---

## Evaluation methodology

```bash
go run ./cmd/perch pulse investigation-eval --dataset f2c --out-dir /tmp/inv-eval --json
```

Compares F1 vs stored LLM proposals on the same evidence fingerprint with **explicit denominators**. Missing human reviews → `not_evaluated`. Mechanical citation checks ≠ semantic entailment.

---

## Analytical export (F3a)

```bash
go run ./cmd/perch pulse export-analytics \
  --out-dir /tmp/pulse-export \
  --eval-report /tmp/inv-eval/report.json \
  --incident-provenance synthetic_fixture \
  --json
```

Sample fixture: `examples/pulse-export/sample/export/` (includes `historical_demo` rows for replay demos).

---

## Databricks Delta / MLflow (F3b, optional)

```bash
cd integrations/databricks && pip install -e ".[test]"
python -m perch_pulse_dbx validate --export-dir ../../examples/pulse-export/sample/export --json
python -m perch_pulse_dbx dry-run --export-dir ../../examples/pulse-export/sample/export --simulate-merge --json
python -m perch_pulse_dbx verify-lineage --export-dir ../../examples/pulse-export/sample/export --json
python -m perch_pulse_dbx mlflow-log --report ../../examples/pulse-export/sample/eval-report.json --json
```

Authorized live path (existing SQL warehouse only — no auto-create):

```bash
python -m perch_pulse_dbx live-load \
  --export-dir ../../examples/pulse-export/sample/export \
  --i-authorize-databricks-calls \
  --execute-merge \          # omit for connection-only
  --max-batch 50 --json
```

| Check | This RC default |
|-------|-----------------|
| LIVE DELTA INGESTION | **NOT RUN** |
| LIVE MLFLOW TRACKING | **NOT RUN** |

Connection check ≠ completed ingestion. Details: [`integrations/databricks/README.md`](integrations/databricks/README.md)

---

## Known limitations

- Not a hosted multi-tenant SaaS control plane
- Viz credential APIs are localhost-oriented
- Live Astronomy Shop is Docker-heavy; disk hard-stop at 15 GiB for LIVE demos
- Real LLM / Databricks / MLflow require explicit authorization and budget
- Change correlation and Run Pulse do **not** prove root cause
- Offline eval scores are not production model SLAs

---

## Verification evidence

| Gate | Command |
|------|---------|
| Merge gate | `make verify` |
| Databricks offline | `make databricks-integration-test` |
| UI e2e | `make web-e2e` |
| Demo offline | `./scripts/demo/pulse-demo.sh --mode mock --phase offline` |

Journey honesty table: [`docs/verification-matrix.md`](docs/verification-matrix.md)  
Prior LIVE shop evidence: **2026-10-09** in [`docs/astronomy-shop.md`](docs/astronomy-shop.md) (`error-payment` flagd activation/recovery + `verify-live.sh`).

---

## Upstream Perch (CLI / TUI / providers)

Perch remains a **local-first** CLI/TUI for multi-service stacks (`perch.yaml`, embedded provider YAML, `perch status|graph|context`).

**From source:** `go build -o perch ./cmd/perch`  
**Homebrew (upstream tap, optional):** `brew tap yashg4509/perch && brew install --HEAD perch`

| Command | Role |
|---------|------|
| `perch` | Interactive TUI |
| `perch init` | Scaffold `perch.yaml` |
| `perch status` / `graph` / `context` | Health, topology, agent context |
| `perch viz` | Embedded React UI + Pulse APIs |
| `perch pulse …` | Incidents, investigation, eval, export, LLM smoke |

Providers, examples, and contributor docs: [`docs/providers.md`](docs/providers.md) · [`examples/README.md`](examples/README.md) · [`CONTRIBUTING.md`](CONTRIBUTING.md)

**Remotes:** `origin` → `sharathb5/perch-pulse` · `upstream` → `yashg4509/perch`
