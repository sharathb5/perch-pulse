# Perch Pulse — 3–5 minute demo script

Audience: engineer / recruiter technical screen.  
Use **MOCK** or **REPLAY** by default. Use **LIVE** only when Astronomy Shop is already healthy and disk ≥ 15 GiB.

Props: `perch viz` with Pulse sidebar (fixture or live), terminal for one offline command, optional slide pointing at architecture.

---

### 0:00–0:30 — Problem and architecture

> Distributed checkouts fail across services. Dashboards show symptoms; they rarely assemble **bounded evidence**, separate **observation from inference**, or leave an **evaluable** trail for AI proposals.
>
> Perch Pulse sits on open-source Perch: local topology + health, plus Pulse incidents, F1 deterministic investigation, optional constrained LLM proposals, offline evaluation, JSONL export, and optional Databricks Delta/MLflow — **without requiring Databricks for core use**.

Point at: service graph → Pulse sidebar. Mention Astronomy Shop as the local OpenTelemetry target.

---

### 0:30–1:15 — Live incident / evidence

**If LIVE available:** show payment incident from `error-payment` (ACTIVE → later recovered).  
**If not:** open fixture incident labeled **RECOVERED / historical** — say so out loud.

> We do not inject fake incidents into the UI and call them detector-generated. Scenario labels are ground truth for eval harnesses — not detector inputs.

Show before/during/after digests. Emphasize: recovered ≠ current outage; stale ≠ healthy.

---

### 1:15–2:15 — Investigation and hypotheses

Click **Investigate**.

> F1 builds an **immutable evidence snapshot** and a deterministic report: OBSERVED symptoms, HYPOTHESIS with citation refs, UNKNOWN gaps. Generating a report is **not** a diagnosis.

Walk one hypothesis + “Verify: …” step. Note correlation panels never say “confirmed root cause.”

---

### 2:15–3:00 — Optional AI vs F1

If LLM unavailable (default): show unavailable state — **honesty win**.

If available: Generate AI Investigation.

> Same evidence fingerprint. Citations must resolve. Validated citations ≠ the model’s wording is entailed. F1 remains the baseline; AI is a separate proposal.

Optional terminal (offline — say “fixture smoke, not GPT quality proof”):

```bash
go run ./cmd/perch pulse llm-provider-smoke --case-id case_payment_error --json
```

Submit brief feedback (“useful”) — subjective, not ground truth.

Mention **Run Pulse** as explicit cross-service diagnostics on demand.

---

### 3:00–3:45 — Evaluation and Databricks

> Offline evaluation compares deterministic vs stored proposals with **explicit denominators**. We export versioned JSONL. Databricks tooling validates and plans append-only MERGE on `(dedupe_key, content_fingerprint)`; MLflow logs eval metrics when authorized.

Show one of:

```bash
./scripts/demo/pulse-demo.sh --mode mock --phase offline
# or
python -m perch_pulse_dbx dry-run --export-dir examples/pulse-export/sample/export --json
```

State clearly:

- **REAL LLM VALIDATION = NOT RUN** (unless you just authorized it)
- **LIVE DELTA INGESTION / LIVE MLFLOW TRACKING** — say **NOT RUN** unless this session authorized cloud; a dated demo PASS exists for 2026-10-13 ([`verification-matrix.md`](verification-matrix.md#2026-10-13--live-databricks-delta--mlflow-demo-sample)) and must still be labeled demo/synthetic, not production

Do not invent row counts or MLflow metrics.

---

### 3:45–4:30 — Takeaway and tradeoffs

> Evidence discipline over vibes: freshness, provenance isolation, citation validation, append-only analytics history, and dual-flag gates for paid calls. Correlation ≠ causation. Databricks is optional. The system is demo-credible and CI-verified offline — not a claim of production SaaS readiness.

Invite questions on: agent evaluation, lineage, or why F1 ships before LLM.

---

## Anti-patterns (do not do)

- Fake live telemetry or present MOCK as LIVE
- Pre-label ground truth into the investigator
- Claim Run Pulse or change correlation proved root cause
- Claim GPT-4o-mini passed from synthetic fixtures
- Leave `error-payment` enabled after a live demo
