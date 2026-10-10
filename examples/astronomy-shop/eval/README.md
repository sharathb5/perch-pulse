# Detector evaluation harness

Telemetry-only baseline + regression detection evaluated against scenario ground truth **after** each run.

```text
OBSERVED TELEMETRY → BASELINE → DETECTOR → FINDINGS
                              ↘ (later) EVALUATION ← GROUND TRUTH
```

## Live run

Requires Astronomy Shop minimal + Prometheus already up (reuse containers):

```bash
./examples/astronomy-shop/eval/run-detector-eval.sh
# Optional: PERCH_EVAL_SCENARIOS=latency-shipping-intl,control-emit-raw-pii
```

Writes gitignored artifacts under `results/`:

- `*-findings.json` — detector outputs (no scenario labels)
- `*-eval.json` — per-scenario scores
- `summary.json` / `summary.md` — aggregate report

## Packages

| Package | Role |
|---------|------|
| `internal/pulse/telem` | Prometheus sample source (replaceable) |
| `internal/pulse/detect` | Baseline + findings + attribution (**no** scenario import) |
| `internal/pulse/evaluate` | Scores findings vs completed scenario records |

Control false positives count latency/error findings on the labeled target only; demo `call_rate` burstiness alone is not scored as a control FP. Attribution prefers findings on the labeled target when present.

## CI

```bash
go test ./internal/pulse/detect/... ./internal/pulse/evaluate/... ./internal/pulse/telem/... -count=1
```

Live Docker evaluation is **not** part of `make verify`.
