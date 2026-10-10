// Package evaluate scores detector findings against completed scenario
// ground-truth records. It is the only Pulse package that combines detect
// outputs with scenario labels — and only after detection is finished.
package evaluate

import (
	"fmt"
	"time"

	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/scenario"
)

const ReportSchemaVersion = "pulse.eval.v1"

// ScenarioResult is per-scenario evaluation against one ground-truth record.
type ScenarioResult struct {
	ScenarioID         string   `json:"scenario_id"`
	RunID              string   `json:"run_id"`
	Classification     string   `json:"classification"`
	TargetServiceID    string   `json:"target_service_id"`
	Detected           bool     `json:"detected"`
	DetectionDelaySecs *float64 `json:"detection_delay_secs,omitempty"`
	RecoveryDetected   bool     `json:"recovery_detected"`
	RecoveryDelaySecs  *float64 `json:"recovery_delay_secs,omitempty"`
	Top1Correct        bool     `json:"top1_correct"`
	Top3Correct        bool     `json:"top3_correct"`
	FalsePositive      bool     `json:"false_positive"`
	FalseAttribution   bool     `json:"false_attribution"`
	MatchedFindingIDs  []string `json:"matched_finding_ids,omitempty"`
	Notes              string   `json:"notes,omitempty"`
	ExpectedSignalHint string   `json:"expected_signal_hint,omitempty"`
}

// Summary aggregates a full evaluation run.
type Summary struct {
	SchemaVersion         string           `json:"schema_version"`
	GeneratedAt           time.Time        `json:"generated_at"`
	Scenarios             []ScenarioResult `json:"scenarios"`
	FaultDetected         int              `json:"fault_detected"`
	FaultTotal            int              `json:"fault_total"`
	ControlFalsePositives int              `json:"control_false_positives"`
	ControlTotal          int              `json:"control_total"`
	Top1Correct           int              `json:"top1_correct"`
	Top3Correct           int              `json:"top3_correct"`
	AttributionEligible   int              `json:"attribution_eligible"`
}

// Input is one completed scenario plus detector findings observed during/after it.
type Input struct {
	Record   scenario.Record
	Findings []detect.Finding
	// ActiveWindow optionally restricts which findings count (defaults to record start/end).
	Now time.Time
}

// EvaluateScores compares findings to ground truth. Findings must already be
// produced without access to Record labels.
func EvaluateScores(in Input) (ScenarioResult, error) {
	rec := in.Record
	if err := rec.Validate(); err != nil {
		return ScenarioResult{}, err
	}
	if rec.State != scenario.StateStopped && rec.EndTime == nil {
		return ScenarioResult{}, fmt.Errorf("evaluate: record %s not complete (need end_time)", rec.RunID)
	}
	start := rec.StartTime.UTC()
	end := start
	if rec.EndTime != nil {
		end = rec.EndTime.UTC()
	}
	// Prom increase() windows lag fault onset/offset; allow short post-stop grace.
	detectEnd := end.Add(2 * time.Minute)
	now := in.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}

	out := ScenarioResult{
		ScenarioID:         rec.ScenarioID,
		RunID:              rec.RunID,
		Classification:     string(rec.Classification),
		TargetServiceID:    rec.TargetServiceID,
		ExpectedSignalHint: hintFor(rec.ScenarioType),
	}

	var relevant []detect.Finding
	for _, f := range in.Findings {
		if err := f.Validate(); err != nil {
			return ScenarioResult{}, err
		}
		if overlapsFault(f, start, detectEnd) {
			relevant = append(relevant, f)
		}
	}

	isControl := rec.Classification == scenario.ClassificationControl
	if isControl {
		// Ambient multi-service demo noise is ignored; only regressions on the
		// labeled control target count as false positives for that change.
		var onTarget []detect.Finding
		for _, f := range relevant {
			if f.ServiceID != rec.TargetServiceID {
				continue
			}
			// Demo call_rate is bursty; do not treat throughput jitter alone as a control FP.
			if f.SignalType == detect.SignalCallRate {
				continue
			}
			onTarget = append(onTarget, f)
		}
		out.FalsePositive = len(onTarget) > 0
		if out.FalsePositive {
			out.FalseAttribution = true
			out.Notes = "neutral control produced detector findings on the labeled target"
		} else {
			out.Notes = "neutral control: no meaningful regression on labeled target"
		}
		return out, nil
	}

	// Fault scenario: detection success if any finding on target or expected affected,
	// preferring signal hint match.
	matched := matchFindings(relevant, rec)
	out.Detected = len(matched) > 0
	for _, f := range matched {
		out.MatchedFindingIDs = append(out.MatchedFindingIDs, f.FindingID)
	}
	if out.Detected {
		// Earliest FirstDetectedAt among matched.
		earliest := matched[0].FirstDetectedAt
		for _, f := range matched[1:] {
			if f.FirstDetectedAt.Before(earliest) {
				earliest = f.FirstDetectedAt
			}
		}
		delay := earliest.Sub(start).Seconds()
		if delay < 0 {
			delay = 0
		}
		out.DetectionDelaySecs = &delay

		// Attribution: among findings that were open near first detection, check candidates.
		top1, top3 := attributionCorrect(matched, rec.TargetServiceID)
		out.Top1Correct = top1
		out.Top3Correct = top3

		// Recovery: a matched finding that recovered after end.
		for _, f := range matched {
			if f.RecoveredAt != nil && !f.RecoveredAt.Before(end) {
				out.RecoveryDetected = true
				rd := f.RecoveredAt.Sub(end).Seconds()
				if rd < 0 {
					rd = 0
				}
				out.RecoveryDelaySecs = &rd
				break
			}
		}
		if !out.RecoveryDetected {
			// Also accept: no open findings on target after end among matched.
			stillOpen := false
			for _, f := range matched {
				if f.Open() && (f.LastObservedAt.After(end) || f.LastObservedAt.Equal(end)) {
					stillOpen = true
					break
				}
			}
			if !stillOpen && len(matched) > 0 {
				// If all matched findings recovered anytime after start, count recovery.
				for _, f := range matched {
					if f.RecoveredAt != nil {
						out.RecoveryDetected = true
						rd := f.RecoveredAt.Sub(end).Seconds()
						if rd < 0 {
							rd = 0
						}
						out.RecoveryDelaySecs = &rd
						break
					}
				}
			}
		}
	} else {
		out.Notes = "miss: no overlapping findings for target/expected affected services"
	}
	return out, nil
}

// Aggregate builds a summary from per-scenario results.
func Aggregate(results []ScenarioResult, at time.Time) Summary {
	at = at.UTC()
	s := Summary{SchemaVersion: ReportSchemaVersion, GeneratedAt: at, Scenarios: results}
	for _, r := range results {
		if r.Classification == string(scenario.ClassificationControl) {
			s.ControlTotal++
			if r.FalsePositive {
				s.ControlFalsePositives++
			}
			continue
		}
		s.FaultTotal++
		if r.Detected {
			s.FaultDetected++
			s.AttributionEligible++
			if r.Top1Correct {
				s.Top1Correct++
			}
			if r.Top3Correct {
				s.Top3Correct++
			}
		}
	}
	return s
}

// HumanReport renders a concise markdown summary.
func HumanReport(s Summary) string {
	out := fmt.Sprintf("# Pulse detector evaluation (%s)\n\n", s.GeneratedAt.Format(time.RFC3339))
	out += fmt.Sprintf("- Fault detection: %d / %d\n", s.FaultDetected, s.FaultTotal)
	out += fmt.Sprintf("- Control false positives: %d / %d\n", s.ControlFalsePositives, s.ControlTotal)
	if s.AttributionEligible > 0 {
		out += fmt.Sprintf("- Top-1 attribution: %d / %d\n", s.Top1Correct, s.AttributionEligible)
		out += fmt.Sprintf("- Top-3 attribution: %d / %d\n", s.Top3Correct, s.AttributionEligible)
	}
	out += "\n## Per scenario\n\n"
	for _, r := range s.Scenarios {
		out += fmt.Sprintf("### %s (%s)\n", r.ScenarioID, r.Classification)
		if r.Classification == string(scenario.ClassificationControl) {
			out += fmt.Sprintf("- false_positive: %v\n- false_attribution: %v\n", r.FalsePositive, r.FalseAttribution)
		} else {
			out += fmt.Sprintf("- detected: %v\n", r.Detected)
			if r.DetectionDelaySecs != nil {
				out += fmt.Sprintf("- detection_delay_secs: %.1f\n", *r.DetectionDelaySecs)
			}
			out += fmt.Sprintf("- top1: %v top3: %v\n", r.Top1Correct, r.Top3Correct)
			out += fmt.Sprintf("- recovery_detected: %v\n", r.RecoveryDetected)
			if r.RecoveryDelaySecs != nil {
				out += fmt.Sprintf("- recovery_delay_secs: %.1f\n", *r.RecoveryDelaySecs)
			}
		}
		if r.Notes != "" {
			out += fmt.Sprintf("- notes: %s\n", r.Notes)
		}
		out += "\n"
	}
	return out
}

func hintFor(t scenario.Type) string {
	switch t {
	case scenario.TypeLatency:
		return string(detect.SignalLatencyMS)
	case scenario.TypeErrorRate:
		return string(detect.SignalErrorRate)
	case scenario.TypeDependencyOutage:
		return string(detect.SignalErrorRate) + "|" + string(detect.SignalCallRate)
	default:
		return ""
	}
}

func overlapsFault(f detect.Finding, start, end time.Time) bool {
	// Credit only findings first detected during the fault (+ post-stop grace).
	// Pre-existing open findings must not count as detecting this scenario.
	return !f.FirstDetectedAt.Before(start) && !f.FirstDetectedAt.After(end)
}

func matchFindings(findings []detect.Finding, rec scenario.Record) []detect.Finding {
	want := map[string]struct{}{rec.TargetServiceID: {}}
	for _, id := range rec.ExpectedAffectedServiceIDs {
		want[id] = struct{}{}
	}
	hint := hintFor(rec.ScenarioType)
	var matched []detect.Finding
	for _, f := range findings {
		if _, ok := want[f.ServiceID]; !ok {
			continue
		}
		if hint != "" && rec.ScenarioType == scenario.TypeLatency && f.SignalType != detect.SignalLatencyMS {
			continue
		}
		if rec.ScenarioType == scenario.TypeErrorRate && f.SignalType != detect.SignalErrorRate {
			continue
		}
		if rec.ScenarioType == scenario.TypeDependencyOutage &&
			f.SignalType != detect.SignalErrorRate && f.SignalType != detect.SignalCallRate {
			continue
		}
		matched = append(matched, f)
	}
	// Fallback: any finding on target service if signal-strict match empty.
	if len(matched) == 0 {
		for _, f := range findings {
			if f.ServiceID == rec.TargetServiceID {
				matched = append(matched, f)
			}
		}
	}
	return matched
}

func attributionCorrect(matched []detect.Finding, target string) (top1, top3 bool) {
	// Prefer findings on the labeled target when present, then highest score.
	use := matched
	var onTarget []detect.Finding
	for _, f := range matched {
		if f.ServiceID == target {
			onTarget = append(onTarget, f)
		}
	}
	if len(onTarget) > 0 {
		use = onTarget
	}
	best := use[0]
	for _, f := range use[1:] {
		if f.Score > best.Score {
			best = f
		}
	}
	if len(best.CandidateImpact) == 0 {
		return best.ServiceID == target, best.ServiceID == target
	}
	for _, c := range best.CandidateImpact {
		if c.ServiceID != target {
			continue
		}
		if c.Rank == 1 {
			top1 = true
			top3 = true
			return top1, top3
		}
		if c.Rank <= 3 {
			top3 = true
		}
	}
	// Also: primary service_id match counts as top1.
	if best.ServiceID == target {
		return true, true
	}
	return top1, top3
}
