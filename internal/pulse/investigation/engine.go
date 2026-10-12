package investigation

import (
	"fmt"
	"sort"
	"strings"

	"github.com/yashg4509/perch/internal/pulse/detect"
)

// GenerateHypotheses produces a small set of INFERRED hypotheses from a snapshot.
// Prefer 2 useful hypotheses over many generic ones. Never asserts causation.
func GenerateHypotheses(inv *Investigation) []Hypothesis {
	if inv == nil {
		return nil
	}
	var out []Hypothesis
	refsBySignal := map[detect.SignalType]string{}
	for _, s := range inv.ObservedSymptoms {
		refsBySignal[s.SignalType] = s.ID
	}

	for _, bc := range inv.BaselineComparisons {
		if bc.Classification != ClassObserved {
			continue
		}
		if bc.BeforeMedian == nil || bc.DuringMedian == nil {
			continue
		}
		ref := refsBySignal[bc.SignalType]
		if ref == "" {
			ref = fmt.Sprintf("baseline:%s:%s", bc.ServiceID, bc.SignalType)
		}
		switch bc.SignalType {
		case detect.SignalErrorRate:
			if bc.Direction == "increase" {
				out = append(out, Hypothesis{
					HypothesisID:           "hyp|error_rate_regression|" + sanitizeID(bc.ServiceID),
					Kind:                   "error_rate_regression",
					Statement:              fmt.Sprintf("%s error rate increased relative to its observed before-window baseline (during median %.3f vs before %.3f).", shortService(bc.ServiceID), *bc.DuringMedian, *bc.BeforeMedian),
					Classification:         ClassInferred,
					SupportingEvidenceRefs: []string{ref, "phase:during", "phase:before"},
					MissingEvidence:        []string{"request-level traces confirming error class", "change confirmation independent of correlation score"},
					SuggestedVerification:  "Compare error-class traces or logs for " + bc.ServiceID + " in the during window; do not treat correlation alone as cause.",
				})
			}
		case detect.SignalLatencyMS:
			if bc.Direction == "increase" {
				out = append(out, Hypothesis{
					HypothesisID:           "hyp|latency_regression|" + sanitizeID(bc.ServiceID),
					Kind:                   "latency_regression",
					Statement:              fmt.Sprintf("%s latency increased relative to its observed before-window baseline (during median %.0fms vs before %.0fms).", shortService(bc.ServiceID), *bc.DuringMedian, *bc.BeforeMedian),
					Classification:         ClassInferred,
					SupportingEvidenceRefs: []string{ref, "phase:during", "phase:before"},
					MissingEvidence:        []string{"span breakdown for slow dependency vs local work"},
					SuggestedVerification:  "Inspect latency histograms/traces for " + bc.ServiceID + " during the incident window.",
				})
			}
		case detect.SignalCallRate:
			if bc.Direction == "decrease" {
				out = append(out, Hypothesis{
					HypothesisID:           "hyp|call_rate_drop|" + sanitizeID(bc.ServiceID),
					Kind:                   "call_rate_drop",
					Statement:              fmt.Sprintf("Observed request volume for %s declined during the incident window relative to the before-window baseline (during median %.3f vs before %.3f).", shortService(bc.ServiceID), *bc.DuringMedian, *bc.BeforeMedian),
					Classification:         ClassInferred,
					SupportingEvidenceRefs: []string{ref, "phase:during", "phase:before"},
					CounterEvidenceRefs:    callRateCounterRefs(inv),
					MissingEvidence:        []string{"whether upstream clients reduced traffic or the service became unreachable"},
					SuggestedVerification:  "Check upstream call rates and active probe reachability separately from Pulse telemetry.",
				})
			}
		}
	}

	if inv.StrongChangeCandidate && len(inv.ChangeCandidates) > 0 {
		top := inv.ChangeCandidates[0]
		out = append(out, Hypothesis{
			HypothesisID:           "hyp|change_correlation|" + sanitizeID(top.ChangeID),
			Kind:                   "change_correlation",
			Statement:              fmt.Sprintf("A recorded change (%s) overlaps the incident in time and affected-service scope (rank #%d, score %.1f via %s). This is correlation, not proven causation.", top.ChangeID, top.Rank, top.Score, top.ScorerVersion),
			Classification:         ClassInferred,
			SupportingEvidenceRefs: []string{"change:" + top.ChangeID, "correlation:top1"},
			CounterEvidenceRefs:    weakChangeCounters(inv),
			MissingEvidence:        []string{"independent confirmation that the change was deployed to the affected environment", "proof the change altered the failing code path"},
			SuggestedVerification:  "Verify the change actually rolled out to " + inv.Environment + " and inspect whether its service scope matches the anomalous signals.",
		})
	} else if len(inv.ChangeCandidates) > 0 {
		top := inv.ChangeCandidates[0]
		out = append(out, Hypothesis{
			HypothesisID:           "hyp|weak_change_correlation|" + sanitizeID(top.ChangeID),
			Kind:                   "change_correlation",
			Statement:              fmt.Sprintf("A weakly ranked change candidate (%s, score %.1f) exists; overlap is insufficient to treat as a strong lead.", top.ChangeID, top.Score),
			Classification:         ClassInferred,
			SupportingEvidenceRefs: []string{"change:" + top.ChangeID},
			MissingEvidence:        []string{"stronger temporal/service overlap", "additional change records if deployments were not logged"},
			SuggestedVerification:  "Treat absent or weak change correlation as incomplete change inventory — not proof that no deployment occurred.",
		})
	}

	// Dependency neighbor hypothesis only when we have anomalous primary + static neighbors.
	var neighbor string
	for _, n := range inv.Topology.Nodes {
		if n.Relation == "upstream" || n.Relation == "downstream" {
			neighbor = n.ServiceID
			break
		}
	}
	if neighbor != "" && len(out) < MaxHypotheses {
		out = append(out, Hypothesis{
			HypothesisID:           "hyp|dependency|" + sanitizeID(inv.Topology.PrimaryServiceID),
			Kind:                   "dependency",
			Statement:              fmt.Sprintf("Static topology lists neighbors of %s (e.g. %s); a dependency may warrant investigation, but no trace evidence in this snapshot confirms the edge was exercised.", shortService(inv.Topology.PrimaryServiceID), shortService(neighbor)),
			Classification:         ClassInferred,
			SupportingEvidenceRefs: []string{"topology:primary"},
			MissingEvidence:        []string{"distributed traces showing the dependency edge during the incident window"},
			SuggestedVerification:  "If available, inspect traces spanning " + inv.Topology.PrimaryServiceID + " and its neighbors; topology alone is not a causal chain.",
		})
	}

	// Local regression framing when we have symptoms but no strong change.
	if len(inv.ObservedSymptoms) > 0 && !inv.StrongChangeCandidate && len(out) < MaxHypotheses {
		out = append(out, Hypothesis{
			HypothesisID:           "hyp|service_local|" + sanitizeID(inv.Topology.PrimaryServiceID),
			Kind:                   "service_local",
			Statement:              fmt.Sprintf("Observed symptoms in this snapshot are recorded against %s; a service-local regression is one plausible explanation pending neighbor and change verification (neighbor digests were not compared here).", shortService(inv.Topology.PrimaryServiceID)),
			Classification:         ClassInferred,
			SupportingEvidenceRefs: symptomRefs(inv),
			MissingEvidence:        []string{"neighbor service digests for the same window", "change inventory completeness"},
			SuggestedVerification:  "Compare neighbor service digests for the same window and review whether change recording covers this environment.",
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].HypothesisID < out[j].HypothesisID
	})

	if len(out) == 0 {
		out = []Hypothesis{{
			HypothesisID:           "hyp|insufficient_evidence",
			Kind:                   "insufficient",
			Statement:              "No reliable hypothesis can be formed from the available evidence snapshot.",
			Classification:         ClassInferred,
			SupportingEvidenceRefs: []string{},
			MissingEvidence:        append([]string{}, inv.MissingEvidence...),
			SuggestedVerification:  "Collect additional telemetry digests, change events, or explicitly linked Run Pulse diagnostics before re-running investigation.",
		}}
	}

	if len(out) > MaxHypotheses {
		out = out[:MaxHypotheses]
	}
	return out
}

func callRateCounterRefs(inv *Investigation) []string {
	var refs []string
	for _, bc := range inv.BaselineComparisons {
		if bc.SignalType == detect.SignalErrorRate && bc.Direction == "increase" {
			refs = append(refs, fmt.Sprintf("baseline:%s:%s", bc.ServiceID, bc.SignalType))
		}
	}
	return refs
}

func weakChangeCounters(inv *Investigation) []string {
	if len(inv.ChangeCandidates) <= 1 {
		return nil
	}
	var refs []string
	for _, c := range inv.ChangeCandidates[1:] {
		refs = append(refs, "change:"+c.ChangeID)
	}
	return refs
}

func symptomRefs(inv *Investigation) []string {
	var refs []string
	for _, s := range inv.ObservedSymptoms {
		refs = append(refs, s.ID)
	}
	sort.Strings(refs)
	return refs
}

func shortService(id string) string {
	parts := strings.Split(id, "/")
	if len(parts) == 0 {
		return id
	}
	return parts[len(parts)-1]
}

func sanitizeID(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "|", "_")
	s = strings.ReplaceAll(s, " ", "_")
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// SuggestedSteps builds human-approved verification suggestions (not autonomous actions).
func SuggestedSteps(inv *Investigation) []string {
	steps := []string{
		"Treat this investigation as an evidence package — not a confirmed root-cause diagnosis.",
		"Keep active probe health (/api/status) separate from Pulse telemetry regressions.",
	}
	if inv.IncidentState == IncidentStateRecovered || inv.IncidentState == IncidentStateHistorical {
		steps = append(steps, "Incident is recovered/historical; do not treat the snapshot as a current outage.")
	}
	if len(inv.ChangeCandidates) == 0 {
		steps = append(steps, "No change events were correlated — absence of records is not proof that no deployment occurred.")
	}
	if inv.StrongChangeCandidate {
		steps = append(steps, "If pursuing the top change candidate, verify rollout independently; correlation score is not causation.")
	}
	for _, h := range inv.Hypotheses {
		if h.SuggestedVerification != "" && h.Kind != "insufficient" {
			steps = append(steps, h.SuggestedVerification)
		}
	}
	steps = append(steps, "Run Pulse diagnostics are optional and must be started explicitly; they are not continuous monitoring.")
	return uniquePreserveOrder(steps)
}

func uniquePreserveOrder(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
