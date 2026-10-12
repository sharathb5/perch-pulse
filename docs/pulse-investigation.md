# Pulse Investigation (Milestone F1)

Evidence-backed investigation snapshots for Pulse incidents. **No LLM required.**
Investigations assemble bounded facts, explicit hypotheses, unknowns, and verification
steps. They do **not** confirm root causes or perform remediation.

See [ADR-023](DECISIONS.md).

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

## Future LLM seam

```text
EvidenceSnapshot → Proposer.Propose → Proposal
```

F1 ships only `DeterministicProposer`. No OpenAI/Anthropic/LangChain. Future
eval can compare LLM proposals against this baseline + human feedback, optionally
storing artifacts in Databricks/MLflow without making them required.

## Evaluation

Package `internal/pulse/investigation/eval` scores:

- evidence citation coverage (every hypothesis must cite refs)
- unresolved citation count (refs must resolve to source_references / symptoms / phases)
- unsupported-claim count (phrase + classification checks; not exhaustive NLP)
- hypothesis relevance (expected kind present)
- uncertainty classification (`diagnosed=false`, unknowns present)
- false causal attribution count
- rerun consistency is asserted in unit tests via content fingerprint equality (not a `Pass` field)

## Security

- Mutating POSTs: loopback + Origin checks (same pattern as Run Pulse)
- Not production multi-user auth
- Redaction of bearer/key-shaped substrings
- Path traversal rejected via `ValidateResourceID` + filename sanitization
- Bounded body (4 KiB) and snapshot size (256 KiB)
