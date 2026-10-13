# Investigation evaluation (Milestone F2b)

Offline harness comparing F1 deterministic investigations with stored F2a LLM
proposals on **identical** evidence fingerprints.

```bash
# From repo root — fully offline, no provider calls:
go run ./cmd/perch pulse investigation-eval
go run ./cmd/perch pulse investigation-eval --out-dir /tmp/inv-eval --json
```

Dataset: `pulse.investigation.eval.astronomy_fixtures.v1` (built in
`internal/pulse/investigation/eval.BuiltinDataset`).

- Synthetic fixture proposals are labeled `origin=synthetic_fixture`.
- Missing proposals are reported as unavailable — performance is **not** invented.
- Hidden scenario ground truth never enters generation inputs.
- Mechanical metrics ≠ semantic entailment; see the human rubric in the report.
