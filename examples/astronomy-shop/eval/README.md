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

## Change / deployment correlation

```bash
./examples/astronomy-shop/eval/run-change-eval.sh
```

Sequence: baseline → **simulated** deploy marker → fault → detect → incident snapshot → correlate → recover → `changeeval`.

Correlation scoring uses only incident + change events (no scenario labels). Markers are modeled change events, not real cloud deploys. See [`../change/README.md`](../change/README.md).

## CI

```bash
go test ./internal/pulse/detect/... ./internal/pulse/evaluate/... ./internal/pulse/telem/... \
  ./internal/pulse/change/... ./internal/pulse/incident/... ./internal/pulse/correlate/... \
  ./internal/pulse/changeeval/... -count=1
```

Live Docker evaluation is **not** part of `make verify`.
