# Pulse Investigation (Milestone F1 + F2a)

Evidence-backed investigation snapshots for Pulse incidents. **No LLM required for F1.**
Investigations assemble bounded facts, explicit hypotheses, unknowns, and verification
steps. They do **not** confirm root causes or perform remediation.

Optional Milestone F2a adds a constrained LLM proposer that emits a **separate**
validated proposal (`pulse.investigation.proposal.v1`) without mutating F1 snapshots.
See [ADR-023](DECISIONS.md) and [ADR-025](DECISIONS.md).

## Concepts

| Term | Meaning |
|------|---------|
| Investigation | Immutable versioned snapshot (`pulse.investigation.v1`) |
| OBSERVED | Directly supported by telemetry / incident records |
| INFERRED | Plausible hypothesis — never proven causation |
| UNKNOWN | Insufficient evidence |
| Feedback | Optional human quality rating (`pulse.investigation.feedback.v1`) — not ground truth |

Hard rules:

- Investigation ≠ confirmed root cause (`diagnosed` is always `false` in F1)
- Feedback ≠ independently verified ground truth
- Hypotheses ≠ deployment causation
- Active probe health ≠ telemetry regression
- Run Pulse ≠ continuous monitoring (only included when explicitly linked)
- Persisted snapshot ≠ current live state

## Evidence sources

| Source | Fields used | Freshness | Persistence | Agent-safe? |
|--------|-------------|-----------|-------------|-------------|
| Incident (`pulse.incident.v1`) | digests, signals, lifecycle, obs/inf/lim | Snapshot at `last_observed_at` | FileStore | Yes (redacted) |
| Correlation (`pulse.correlation.v1`) | ranked candidates, score breakdown | On-read | Not persisted | Yes |
| Astronomy topology | depends_on neighbors (depth 1) | Static map | Embedded YAML | Yes |
| Run Pulse | only if `linked_run_ids` provided | Point-in-time / may be expired | In-memory TTL | Yes |
| Scenario GT | **not used** | n/a | n/a | **No** |

## API

| Method | Path | Notes |
|--------|------|-------|
| POST | `/api/pulse/investigations` | Body: `{ "incident_id", "reuse_identical?", "linked_run_ids?" }` — loopback only |
| GET | `/api/pulse/investigations/{id...}` | Persisted snapshot |
| GET | `/api/pulse/incidents/{id...}/investigation-context` | Read-only agent context for latest persisted investigation (404 until POST create) |
| POST | `/api/pulse/investigations/{id...}/feedback` | Loopback only; does not mutate incident |
| GET | `/api/pulse/investigations/{id...}/feedback` | List feedback rows |

Go’s ServeMux forbids `{…}` mid-path; `/investigation-context` and `/feedback` are
suffixes on the catch-all `{id...}` routes.

### POST create body (only)

```json
{
  "incident_id": "inc|astronomy/local/payment|…",
  "reuse_identical": true,
  "linked_run_ids": ["run_…"]
}
```

Rejected: arbitrary URLs, PromQL, shell, LLM prompts, tool execution.

## CLI

```bash
PERCH_PULSE_DIR=examples/astronomy-shop/change/results \
  perch pulse investigate 'inc|astronomy/local/payment|…' --reuse-identical

perch pulse investigate '…' --for-agent
perch pulse investigation-context '…' --json
```

## Agent context

Schema `pulse.investigation.agent_context.v1` with sections:

- **FACTS** — OBSERVED evidence
- **HYPOTHESES** — INFERRED statements with ids
- **UNKNOWN** — gaps / missing evidence
- **NEXT STEPS** — human-approved verification suggestions

No credentials, unbounded logs, or scenario labels.

## Deterministic rules (F1)

`deterministic_rules_v1` emits hypotheses from digests + correlation only, e.g.:

- error-rate / latency increases vs before-window
- call-rate declines
- change correlation (with explicit non-causation language)
- dependency neighbor warranting inspection (topology ≠ exercised edge)
- insufficient evidence when nothing reliable can be said

Prefer ≤5 hypotheses. Ranking basis is documented from the correlate score breakdown.

## Persistence

Under `$PERCH_PULSE_DIR` (or `.perch/pulse`):

- `investigations/{sanitized_id}.json` — atomic write, mode `0600`
- `investigation-feedback/{sanitized_id}.json`

Snapshots are immutable. Newer evidence ⇒ new investigation id. Same content
fingerprint + `reuse_identical` returns the existing row.

## Feedback assessments

`useful` | `partially_useful` | `not_useful` | `insufficient_evidence`

Feedback cannot modify incidents, recovery, hypotheses, or detector thresholds.

## Optional LLM investigator (F2a)

```text
F1 Investigation (immutable)
  → bounded EvidencePayload
  → OpenAI-compatible chat/completions (opt-in)
  → validate citations / fingerprint / language policy
  → pulse.investigation.proposal.v1 (separate persistence)
```

Disabled by default. Server-side env only:

| Variable | Purpose |
|----------|---------|
| `PERCH_PULSE_LLM_ENABLED` | Opt-in (`1`/`true`) |
| `PERCH_PULSE_LLM_API_KEY` | Bearer token (never logged or returned by status) |
| `PERCH_PULSE_LLM_BASE_URL` | Provider base ending before `/chat/completions` (HTTPS; loopback HTTP allowed for fakes) |
| `PERCH_PULSE_LLM_MODEL` | Model id (default `gpt-4o-mini`) |
| `PERCH_PULSE_LLM_TIMEOUT_SEC` | 1–120 (default 20) |
| `PERCH_PULSE_LLM_MAX_TOKENS` | 64–4096 (default 1200) |
| `PERCH_PULSE_LLM_MAX_CONCURRENT` | 1–4 (default 1) |

Provider contract used: OpenAI-style `POST {base}/chat/completions` with
`model`, `messages[{role,content}]`, `max_tokens`, `temperature`, optional
`response_format:{type:json_object}`. Response fields used: `choices[0].message.content`,
`finish_reason`, `usage.prompt_tokens`, `usage.completion_tokens`, `id`, `model`.

**Not tested against a live paid provider in CI.** Compatibility beyond this
documented subset is unverified.

**Usage bounds:** each request is bounded (`max_tokens`, payload/response size,
timeout, concurrency). There is **no aggregate spend/token budget** across
repeated POSTs — local loopback callers can invoke propose repeatedly. Operators
who need spend caps should gate credentials or terminate the viz process.

**Validation honesty:** `support_status=validated` means citation IDs resolve and
pass lexical policy checks. It does **not** prove the cited evidence entails the
claim wording. Causal attribution phrases are rejected; correlation remains
correlation.

| Method | Path | Notes |
|--------|------|-------|
| GET | `/api/pulse/llm/status` | Secret-free availability |
| POST | `/api/pulse/investigations/{id}/llm-propose` | Loopback only; no GET side effects |
| GET | `/api/pulse/investigations/{id}/proposals` | List persisted proposals |
| GET | `/api/pulse/proposals/{id}` | Load proposal |
| POST | `/api/pulse/proposals/{id}/feedback` | Proposal-only feedback (not F1 feedback) |

```bash
perch pulse llm-status
perch pulse llm-propose 'inv|…' --expected-fingerprint '…'
```

F1 still ships `DeterministicProposer`. Future eval (F2b) can compare LLM proposals
against this baseline + human feedback, optionally storing artifacts in
Databricks/MLflow without making them required.

## Evaluation (F1 unit + F2b harness)

Package `internal/pulse/investigation/eval` provides:

**F1 unit scoring** (`Score` / `Pass`):

- evidence citation coverage (every hypothesis must cite refs)
- unresolved citation count (refs must resolve to source_references / symptoms / phases)
- unsupported-claim count (phrase + classification checks; not exhaustive NLP)
- hypothesis relevance (expected kind present)
- uncertainty classification (`diagnosed=false`, unknowns present)
- false causal attribution count
- rerun consistency is asserted in unit tests via content fingerprint equality (not a `Pass` field)

**F2b offline harness** (`BuiltinDataset`, `Run`, `perch pulse investigation-eval`):

- versioned cases `pulse.investigation.eval_case.v1` with separated generation /
  annotations / hidden ground truth
- same-fingerprint comparison of deterministic F1 vs stored F2a proposals
- mechanical method scores with explicit aggregate denominators
- synthetic fixture proposals clearly labeled; missing proposals not scored as model quality
- human rubric for semantic support (no LLM judge by default)
- optional feedback summaries treated as subjective usefulness only

See [ADR-026](DECISIONS.md) and `examples/astronomy-shop/eval/investigation/`.

## Security

- Mutating POSTs: loopback + Origin checks (same pattern as Run Pulse)
- Not production multi-user auth
- Redaction of bearer/key-shaped substrings
- Path traversal rejected via `ValidateResourceID` + filename sanitization
- Bounded body (4 KiB) and snapshot size (256 KiB)
