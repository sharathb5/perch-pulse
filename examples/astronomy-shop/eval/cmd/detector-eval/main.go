// Command detector-eval runs baseline → scenario → detect → recover → evaluate
// against a live Astronomy Shop + Prometheus. Ground-truth labels are loaded
// only after detector findings are produced for each scenario.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/astronomy"
	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/evaluate"
	"github.com/yashg4509/perch/internal/pulse/scenario"
	"github.com/yashg4509/perch/internal/pulse/telem"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	outDir := envOr("PERCH_EVAL_RESULTS_DIR", filepath.Join(root, "examples", "astronomy-shop", "eval", "results"))
	scenarioResults := envOr("PERCH_SCENARIO_RESULTS_DIR", filepath.Join(root, "examples", "astronomy-shop", "scenarios", "results"))
	promURL := envOr("PERCH_PROM_URL", "http://127.0.0.1:9090")
	flagdBase := envOr("PERCH_FLAGD_API_BASE", "http://127.0.0.1:8080/feature/api")
	baselineSecs := envInt("PERCH_BASELINE_SECS", 90)
	faultSecs := envInt("PERCH_FAULT_SECS", 75)
	recoverSecs := envInt("PERCH_RECOVER_SECS", 60)
	pollSecs := envInt("PERCH_POLL_SECS", 10)

	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return err
	}

	catalog, err := scenario.LoadCatalog()
	if err != nil {
		return err
	}
	catalog.FlagdAPIBase = flagdBase
	h, err := scenario.NewHarness(catalog, scenario.NewFlagdClient(flagdBase), scenarioResults, nil)
	if err != nil {
		return err
	}
	// Clear leftover active scenario.
	if active, _ := h.LoadActive(); active != nil {
		fmt.Println("INFO: stopping leftover active scenario", active.ScenarioID)
		if _, err := h.Stop(context.Background(), ""); err != nil {
			return fmt.Errorf("stop leftover: %w", err)
		}
	}

	mapping, err := astronomy.Load()
	if err != nil {
		return err
	}
	src := telem.NewPrometheus(promURL)
	src.Window = 2 * time.Minute
	cfg := detect.DefaultConfig()
	// Shorter windows for a demo-length run; keep DefaultConfig threshold floors
	// so ambient checkout jitter does not count as a regression.
	cfg.BaselineWindow = 75 * time.Second
	cfg.CurrentWindow = 40 * time.Second
	cfg.MinBaselineSamples = 3
	cfg.MinCurrentSamples = 2
	cfg.LatencyRobustZ = 0 // spanmetrics windows are already aggregated; MAD of few points is brittle
	cfg.MinCallRateForLatency = 0.02

	scenarios := []string{
		"latency-shipping-intl",
		"error-payment",
		"outage-payment",
		"control-emit-raw-pii",
	}
	if only := strings.TrimSpace(os.Getenv("PERCH_EVAL_SCENARIOS")); only != "" {
		scenarios = strings.Split(only, ",")
		for i := range scenarios {
			scenarios[i] = strings.TrimSpace(scenarios[i])
		}
	}

	var allResults []evaluate.ScenarioResult
	ctx := context.Background()

	for _, id := range scenarios {
		fmt.Printf("\n===== scenario %s =====\n", id)
		det := detect.New(cfg)
		var allFindings []detect.Finding

		fmt.Printf("INFO: collecting baseline for %ds\n", baselineSecs)
		if err := collect(ctx, det, src, time.Duration(baselineSecs)*time.Second, time.Duration(pollSecs)*time.Second, &allFindings, mapping); err != nil {
			return err
		}

		rec, err := h.Start(ctx, id)
		if err != nil {
			return err
		}
		fmt.Printf("OK: started %s at %s\n", id, rec.StartTime.Format(time.RFC3339Nano))

		fmt.Printf("INFO: fault window %ds\n", faultSecs)
		if err := collect(ctx, det, src, time.Duration(faultSecs)*time.Second, time.Duration(pollSecs)*time.Second, &allFindings, mapping); err != nil {
			_, _ = h.Stop(ctx, id)
			return err
		}

		stopped, err := h.Stop(ctx, id)
		if err != nil {
			return err
		}
		fmt.Printf("OK: stopped %s recovered_flag=%v\n", id, stopped.RecoveryVerified)

		fmt.Printf("INFO: recovery window %ds\n", recoverSecs)
		if err := collect(ctx, det, src, time.Duration(recoverSecs)*time.Second, time.Duration(pollSecs)*time.Second, &allFindings, mapping); err != nil {
			return err
		}

		// Persist findings (no ground-truth fields).
		findPath := filepath.Join(outDir, stopped.RunID+"-findings.json")
		if err := writeJSON(findPath, allFindings); err != nil {
			return err
		}

		// Evaluation AFTER detection — load ground truth explicitly here only.
		gtPath := filepath.Join(scenarioResults, stopped.RunID+".json")
		gtBytes, err := os.ReadFile(gtPath)
		if err != nil {
			return err
		}
		var gt scenario.Record
		if err := json.Unmarshal(gtBytes, &gt); err != nil {
			return err
		}
		result, err := evaluate.EvaluateScores(evaluate.Input{Record: gt, Findings: allFindings})
		if err != nil {
			return err
		}
		allResults = append(allResults, result)
		if err := writeJSON(filepath.Join(outDir, stopped.RunID+"-eval.json"), result); err != nil {
			return err
		}
		fmt.Printf("OK: eval detected=%v fp=%v top1=%v recovery=%v\n",
			result.Detected, result.FalsePositive, result.Top1Correct, result.RecoveryDetected)
	}

	summary := evaluate.Aggregate(allResults, time.Now().UTC())
	if err := writeJSON(filepath.Join(outDir, "summary.json"), summary); err != nil {
		return err
	}
	report := evaluate.HumanReport(summary)
	if err := os.WriteFile(filepath.Join(outDir, "summary.md"), []byte(report), 0o600); err != nil {
		return err
	}
	fmt.Println("\n" + report)
	fmt.Println("OK: results in", outDir)
	return nil
}

func collect(ctx context.Context, det *detect.Detector, src *telem.Prometheus, total, every time.Duration, all *[]detect.Finding, mapping astronomy.Mapping) error {
	deadline := time.Now().Add(total)
	for {
		now := time.Now().UTC()
		samples, err := src.Collect(ctx, now)
		if err != nil {
			return err
		}
		if err := det.Ingest(samples); err != nil {
			return err
		}
		findings, err := det.Evaluate(now)
		if err != nil {
			return err
		}
		findings = detect.Attribute(findings, mapping)
		*all = mergeFindings(*all, findings)
		if !time.Now().Before(deadline) {
			return nil
		}
		sleep := every
		if rem := time.Until(deadline); rem < sleep {
			sleep = rem
		}
		if sleep <= 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleep):
		}
	}
}

func mergeFindings(dst, src []detect.Finding) []detect.Finding {
	byID := map[string]detect.Finding{}
	for _, f := range dst {
		byID[f.FindingID] = f
	}
	for _, f := range src {
		byID[f.FindingID] = f
	}
	out := make([]detect.Finding, 0, len(byID))
	for _, f := range byID {
		out = append(out, f)
	}
	return out
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", wd)
		}
		dir = parent
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
		return def
	}
	return n
}
