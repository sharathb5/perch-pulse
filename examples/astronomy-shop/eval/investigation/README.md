# Investigation evaluation (Milestones F2b + F2c)

Offline harness comparing F1 deterministic investigations with stored F2a LLM
proposals on **identical** evidence fingerprints. F2c adds provider compatibility
smoke and a fixed evaluation corpus.

## Offline evaluation (default — no provider calls)

```bash
# F2c fixed corpus (default): 9 synthetic Astronomy Shop fixtures
go run ./cmd/perch pulse investigation-eval
go run ./cmd/perch pulse investigation-eval --dataset f2c --out-dir /tmp/inv-eval --json

# Full F2b builtin set
go run ./cmd/perch pulse investigation-eval --dataset f2b --out-dir /tmp/inv-eval-f2b
```

Datasets:

| ID | Constant | Notes |
|----|----------|-------|
| `f2c` (default) | `pulse.investigation.eval.f2c_provider_corpus.v1` | Fixed 9-case corpus for F2c |
| `f2b` | `pulse.investigation.eval.astronomy_fixtures.v1` | Full builtin fixture set |

- Synthetic fixture proposals are labeled `origin=synthetic_fixture`.
- Missing proposals are reported as unavailable — performance is **not** invented.
- Hidden scenario ground truth never enters generation inputs.
- Mechanical metrics ≠ semantic entailment; see the human rubric in the report.
- All corpus cases are **synthetic evaluation fixtures**, not production evidence.

### F2c corpus case IDs

| Case ID | Covers |
|---------|--------|
| `case_payment_error` | Payment error-rate regression |
| `case_shipping_latency` | Latency regression |
| `case_call_rate_decline` | Call-rate decline |
| `case_recovered` | Recovered incident |
| `case_weak_correlation` | Weak / absent change correlation |
| `case_stale_telemetry` | Stale / incomplete evidence |
| `case_multiple_changes` | Multiple plausible hypotheses |
| `case_misleading_metadata` | Unsupported / misleading change description |
| `case_insufficient_evidence` | Insufficient evidence |

## Provider compatibility smoke (F2c)

Documented compatible configuration:

- Provider: `openai_compatible` (`POST {base}/chat/completions`)
- Model: `gpt-4o-mini`
- Env: `PERCH_PULSE_LLM_ENABLED`, `PERCH_PULSE_LLM_API_KEY`, `PERCH_PULSE_LLM_BASE_URL`, optional model/timeout/token caps

```bash
# Offline (default): fixture response through the real validation pipeline — no network
go run ./cmd/perch pulse llm-provider-smoke --case-id case_payment_error --json

# Real provider (billable) — both flags required; credentials from env only
export PERCH_PULSE_LLM_ENABLED=1
export PERCH_PULSE_LLM_API_KEY=...          # never commit
export PERCH_PULSE_LLM_BASE_URL=https://api.openai.com/v1
export PERCH_PULSE_LLM_MODEL=gpt-4o-mini

go run ./cmd/perch pulse llm-provider-smoke \
  --case-id case_payment_error \
  --real-provider \
  --i-authorize-provider-calls \
  --json

# Optional: persist a valid proposal under PERCH_PULSE_DIR / --pulse-dir
go run ./cmd/perch pulse llm-provider-smoke \
  --case-id case_payment_error \
  --real-provider --i-authorize-provider-calls --persist --json
```

**Honesty:** Offline smoke PASS does **not** mean live model validation.
Report `real_provider_executed=false` unless the authorized real path ran.

Recommended live sequence (when authorized):

1. One minimal smoke (`case_payment_error`) and confirm schema/citations.
2. Only then run additional cases or `llm-propose` against persisted F1 ids.
3. Re-run `investigation-eval` after storing `recorded_offline` proposals.

## Human review rubric (F2c)

Rubric version: `pulse.investigation.rubric.f2c.v1`

Questions:

1. Are observed facts accurate?
2. Do references support the claims?
3. Does the report distinguish hypotheses from confirmed facts?
4. Are important unknowns acknowledged?
5. Are verification steps actionable?
6. Is the report more useful than deterministic F1?

Attach review JSON files (schema `pulse.investigation.human_review.v1`) via:

```bash
go run ./cmd/perch pulse investigation-eval --dataset f2c --reviews-dir ./reviews --out-dir /tmp/inv-eval
```

Do **not** invent reviews. Absent reviews → dimensions `not_evaluated`.
Reviews are subjective; they are never independently verified root cause.

See [docs/pulse-investigation.md](../../../../docs/pulse-investigation.md) and ADR-028.
