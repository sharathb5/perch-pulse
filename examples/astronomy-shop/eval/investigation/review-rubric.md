# F2c human review rubric

**Rubric version:** `pulse.investigation.rubric.f2c.v1`  
**Review schema:** `pulse.investigation.human_review.v1`

Answers: `yes` | `no` | `partial` | `not_evaluated`

| # | Field | Question |
|---|-------|----------|
| 1 | `observed_facts_accurate` | Are observed facts accurate? |
| 2 | `references_support_claims` | Do references support the claims? |
| 3 | `distinguishes_hypotheses_from_facts` | Does the report distinguish hypotheses from confirmed facts? |
| 4 | `acknowledges_unknowns` | Are important unknowns acknowledged? |
| 5 | `verification_steps_actionable` | Are verification steps actionable? |
| 6 | `more_useful_than_deterministic_f1` | Is the report more useful than deterministic F1? |

Each review must record:

- `proposal_id`
- `source_fingerprint`
- `evaluator_provenance`
- `timestamp` (UTC)
- `rubric_version`

Limitations (always attach):

- Human reviews are subjective usefulness judgments — not independently verified root cause.
- Absent reviews must be labeled `not_evaluated`; do not invent answers.
- Citation existence checks remain separate from semantic support judgments.
- Feedback/reviews cannot mutate F1 incident evidence or investigation snapshots.

Example shell (do not commit real reviews unless intentional):

```json
{
  "schema_version": "pulse.investigation.human_review.v1",
  "review_id": "review|example|1",
  "proposal_id": "prop|…",
  "source_fingerprint": "…",
  "evaluator_provenance": "human:alice",
  "timestamp": "2026-10-12T19:00:00Z",
  "rubric_version": "pulse.investigation.rubric.f2c.v1",
  "observed_facts_accurate": "not_evaluated",
  "references_support_claims": "not_evaluated",
  "distinguishes_hypotheses_from_facts": "not_evaluated",
  "acknowledges_unknowns": "not_evaluated",
  "verification_steps_actionable": "not_evaluated",
  "more_useful_than_deterministic_f1": "not_evaluated",
  "limitations": [
    "Human reviews are subjective usefulness judgments — not independently verified root cause."
  ]
}
```
