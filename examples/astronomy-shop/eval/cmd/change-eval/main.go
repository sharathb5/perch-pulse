// Command change-eval runs:
//
//	baseline → simulated deploy marker → scenario fault → detect →
//	incident snapshot → correlate → recover → changeeval
//
// Ground-truth scenario labels are loaded only for post-hoc changeeval
// expectations (expected service for the marker), never into correlate scoring.
// The deployment marker is SIMULATED — not a real cloud deploy.
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
	"github.com/yashg4509/perch/internal/pulse/change"
	"github.com/yashg4509/perch/internal/pulse/changeeval"
	"github.com/yashg4509/perch/internal/pulse/correlate"
	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/evaluate"
	"github.com/yashg4509/perch/internal/pulse/incident"
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
	pulseDir := envOr("PERCH_PULSE_DIR", filepath.Join(root, "examples", "astronomy-shop", "change", "results"))
	scenarioResults := envOr("PERCH_SCENARIO_RESULTS_DIR", filepath.Join(root, "examples", "astronomy-shop", "scenarios", "results"))
	promURL := envOr("PERCH_PROM_URL", "http://127.0.0.1:9090")
	flagdBase := envOr("PERCH_FLAGD_API_BASE", "http://127.0.0.1:8080/feature/api")
	baselineSecs := envInt("PERCH_BASELINE_SECS", 90)
	faultSecs := envInt("PERCH_FAULT_SECS", 120)
	recoverSecs := envInt("PERCH_RECOVER_SECS", 90)
	pollSecs := envInt("PERCH_POLL_SECS", 10)

	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return err
	}
	if err := os.MkdirAll(pulseDir, 0o750); err != nil {
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
	cfg.BaselineWindow = 75 * time.Second
	cfg.CurrentWindow = 40 * time.Second
	cfg.MinBaselineSamples = 3
	cfg.MinCurrentSamples = 2
	cfg.LatencyRobustZ = 0
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

	var changeResults []changeeval.CaseResult
	var detResults []evaluate.ScenarioResult
	ctx := context.Background()
	changeStore := change.NewFileStore(pulseDir)
	incidentStore := incident.NewFileStore(pulseDir)

	for _, id := range scenarios {
		fmt.Printf("\n===== scenario %s =====\n", id)
		det := detect.New(cfg)
		var allFindings []detect.Finding
		var allSamples []detect.Sample

		fmt.Printf("INFO: collecting baseline for %ds\n", baselineSecs)
		if err := collect(ctx, det, src, time.Duration(baselineSecs)*time.Second, time.Duration(pollSecs)*time.Second, &allFindings, &allSamples, mapping); err != nil {
			return err
		}

		// Look up target service from catalog ONLY to place the synthetic marker
		// and later evaluate — never passed into correlate.Correlate.
		sc, err := catalog.Get(id)
		if err != nil {
			return err
		}
		targetID, err := astronomy.ServiceID(catalog.Environment, sc.TargetOTEL)
		if err != nil {
			return err
		}
		var marker *change.Event
		if sc.Classification != scenario.ClassificationControl {
			deployAt := time.Now().UTC()
			ev, err := change.Record(change.RecordInput{
				Type:        change.ChangeDeployment,
				ServiceIDs:  []string{targetID},
				Environment: catalog.Environment,
				CommitSHA:   fmt.Sprintf("sim-%s-%d", id, deployAt.Unix()),
				Simulated:   true,
				Source:      "change-eval-harness",
				Title:       "Simulated deploy before fault " + id,
				Summary:     "Simulated deployment marker for correlation demo (not a real deploy)",
				DeployedAt:  &deployAt,
				ObservedAt:  deployAt,
				CreatedAt:   deployAt,
			})
			if err != nil {
				return err
			}
			if err := changeStore.Append(ev); err != nil {
				return err
			}
			marker = &ev
			fmt.Printf("OK: recorded SIMULATED deploy marker %s for %s\n", ev.ChangeID, targetID)
		} else {
			fmt.Println("INFO: control scenario — no deploy marker (neutral change-without-fault covered in unit tests)")
		}

		rec, err := h.Start(ctx, id)
		if err != nil {
			return err
		}
		fmt.Printf("OK: started %s at %s\n", id, rec.StartTime.Format(time.RFC3339Nano))

		fmt.Printf("INFO: fault window %ds\n", faultSecs)
		if err := collect(ctx, det, src, time.Duration(faultSecs)*time.Second, time.Duration(pollSecs)*time.Second, &allFindings, &allSamples, mapping); err != nil {
			_, _ = h.Stop(ctx, id)
			return err
		}

		stopped, err := h.Stop(ctx, id)
		if err != nil {
			return err
		}
		fmt.Printf("OK: stopped %s recovered_flag=%v\n", id, stopped.RecoveryVerified)

		fmt.Printf("INFO: recovery window %ds\n", recoverSecs)
		if err := collect(ctx, det, src, time.Duration(recoverSecs)*time.Second, time.Duration(pollSecs)*time.Second, &allFindings, &allSamples, mapping); err != nil {
			return err
		}

		findPath := filepath.Join(outDir, stopped.RunID+"-findings.json")
		if err := writeJSON(findPath, allFindings); err != nil {
			return err
		}

		// Detector eval (existing path) — GT after findings.
		gtPath := filepath.Join(scenarioResults, stopped.RunID+".json")
		gtBytes, err := os.ReadFile(gtPath)
		if err != nil {
			return err
		}
		var gt scenario.Record
		if err := json.Unmarshal(gtBytes, &gt); err != nil {
			return err
		}
		detRes, err := evaluate.EvaluateScores(evaluate.Input{Record: gt, Findings: allFindings})
		if err != nil {
			return err
		}
		detResults = append(detResults, detRes)

		// Incident from findings only (no GT). Prefer findings that start after the
		// simulated deploy / scenario start so ambient baseline findings do not make
		// the marker look post-incident.
		relevant := findingsForService(allFindings, targetID)
		cutoff := stopped.StartTime.UTC()
		if marker != nil {
			cutoff = marker.EffectiveTime()
		}
		relevant = findingsAfter(relevant, cutoff)
		if len(relevant) == 0 && sc.Classification != scenario.ClassificationControl {
			// Fall back to target-service findings in the fault window only.
			relevant = findingsAfter(findingsForService(allFindings, targetID), stopped.StartTime.UTC())
		}
		if len(relevant) == 0 && sc.Classification != scenario.ClassificationControl {
			relevant = findingsAfter(allFindings, stopped.StartTime.UTC())
		}
		var changeCase changeeval.Case
		changeCase.Name = id
		changeCase.Notes = id

		if len(relevant) == 0 {
			fmt.Println("INFO: no findings after deploy/fault cutoff — cannot build incident")
			if sc.Classification == scenario.ClassificationControl {
				changeCase.ExpectNoStrongCandidate = true
				changeCase.IncidentID = "none"
				cr, _ := changeeval.Evaluate(changeCase, correlate.Report{
					IncidentID:        "none",
					NoStrongCandidate: true,
				})
				changeResults = append(changeResults, cr)
				continue
			}
			// Fault with no findings: attribution miss (do not score as successful no-candidate).
			cr := changeeval.CaseResult{
				Name:        id,
				IncidentID:  "none",
				Top1Correct: false,
				Top3Correct: false,
				ExpectNone:  false,
				Notes:       "fault produced no post-deploy findings; attribution miss",
			}
			if marker != nil {
				cr.Notes += "; expected change " + marker.ChangeID
			}
			changeResults = append(changeResults, cr)
			continue
		}

		inc, err := incident.Build(incident.BuildInput{
			Findings:    relevant,
			Samples:     allSamples,
			Environment: "local",
		})
		if err != nil {
			return err
		}
		if err := incidentStore.Append(inc); err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(outDir, stopped.RunID+"-incident.json"), inc); err != nil {
			return err
		}

		// Correlate: ONLY incident + this run's change events (no scenario record).
		// Do not pass prior scenarios' markers — avoids cross-run pollution.
		var changes []change.Event
		if marker != nil {
			// Unrelated decoy: same env, different service, slightly older than marker.
			// Keep it older than the marker but not competing via topology on the target.
			decoyAt := marker.EffectiveTime().Add(-20 * time.Second)
			decoy, err := change.Record(change.RecordInput{
				Type:        change.ChangeDeployment,
				ServiceIDs:  []string{"astronomy/local/flagd"},
				Environment: catalog.Environment,
				CommitSHA:   fmt.Sprintf("decoy-%s-%d", id, decoyAt.Unix()),
				Simulated:   true,
				Source:      "change-eval-harness",
				Title:       "Simulated unrelated decoy deploy",
				Summary:     "Simulated unrelated service deploy marker (not a real deploy)",
				DeployedAt:  &decoyAt,
				ObservedAt:  decoyAt,
				CreatedAt:   decoyAt,
			})
			if err != nil {
				return err
			}
			changes = []change.Event{*marker, decoy}
		}
		rep, err := correlate.Correlate(inc, changes, correlate.DefaultConfig())
		if err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(outDir, stopped.RunID+"-correlation.json"), rep); err != nil {
			return err
		}
		var topEv *change.Event
		if rep.Top1 != nil {
			for i := range changes {
				if changes[i].ChangeID == rep.Top1.ChangeID {
					topEv = &changes[i]
					break
				}
			}
		}
		fmt.Println(correlate.FormatAgentContext(inc, rep, topEv))

		if sc.Classification == scenario.ClassificationControl {
			changeCase.ExpectNoStrongCandidate = true
			changeCase.IncidentID = inc.IncidentID
		} else if marker != nil {
			changeCase.IncidentID = inc.IncidentID
			changeCase.ExpectedTop1ChangeID = marker.ChangeID
		} else {
			changeCase.ExpectNoStrongCandidate = true
			changeCase.IncidentID = inc.IncidentID
		}
		cr, err := changeeval.Evaluate(changeCase, rep)
		if err != nil {
			return err
		}
		changeResults = append(changeResults, cr)
		if err := writeJSON(filepath.Join(outDir, stopped.RunID+"-changeeval.json"), cr); err != nil {
			return err
		}
		fmt.Printf("OK: changeeval top1=%v top3=%v no_candidate=%v strong=%v\n",
			cr.Top1Correct, cr.Top3Correct, cr.NoCandidateCorrect, cr.StrongCandidate)
	}

	detSummary := evaluate.Aggregate(detResults, time.Now().UTC())
	chSummary := changeeval.Summarize(changeResults)
	if err := writeJSON(filepath.Join(outDir, "change-summary.json"), chSummary); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outDir, "detector-summary.json"), detSummary); err != nil {
		return err
	}
	fmt.Printf("\n=== change attribution ===\n")
	fmt.Printf("top1 %d/%d  top3 %d/%d  no-candidate %d/%d\n",
		chSummary.Top1Correct, chSummary.AttributionEligible,
		chSummary.Top3Correct, chSummary.AttributionEligible,
		chSummary.NoCandidateCorrect, chSummary.NoCandidateEligible)
	fmt.Println("OK: results in", outDir)
	fmt.Println("NOTE: deploy markers are SIMULATED (not real cloud deploys)")
	return nil
}

func findingsForService(all []detect.Finding, service string) []detect.Finding {
	var out []detect.Finding
	for _, f := range all {
		if f.ServiceID == service {
			out = append(out, f)
		}
	}
	return out
}

func findingsAfter(all []detect.Finding, cutoff time.Time) []detect.Finding {
	var out []detect.Finding
	for _, f := range all {
		// Strictly after cutoff so a deploy marker at T cannot be treated as
		// post-incident when a finding also timestamps at T.
		if f.FirstDetectedAt.After(cutoff) {
			out = append(out, f)
		}
	}
	return out
}

func collect(ctx context.Context, det *detect.Detector, src *telem.Prometheus, total, every time.Duration, all *[]detect.Finding, samples *[]detect.Sample, mapping astronomy.Mapping) error {
	deadline := time.Now().Add(total)
	for {
		now := time.Now().UTC()
		batch, err := src.Collect(ctx, now)
		if err != nil {
			return err
		}
		*samples = append(*samples, batch...)
		if err := det.Ingest(batch); err != nil {
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
