// Package eval provides deterministic scoring for investigation quality (F1).
// Scenario ground-truth labels must not be used as investigation input.
package eval

import (
	"strings"

	"github.com/yashg4509/perch/internal/pulse/investigation"
)

// Metrics are factual-correctness oriented counters for a single investigation.
type Metrics struct {
	EvidenceCitationCoverage float64 `json:"evidence_citation_coverage"`
	UnsupportedClaimCount    int     `json:"unsupported_claim_count"`
	HypothesisRelevance      float64 `json:"hypothesis_relevance"`
	UncertaintyOK            bool    `json:"correct_uncertainty_classification"`
	FalseCausalAttribution   int     `json:"false_causal_attribution_count"`
	DiagnosedClaim           bool    `json:"diagnosed_claim"`
	UnresolvedCitationCount  int     `json:"unresolved_citation_count"`
}

// Expectation describes what a fixture case should exhibit.
type Expectation struct {
	Name        string
	RequireKind string // optional hypothesis kind
}

// Score compares an investigation against a lightweight expectation.
// Phrase checks are necessary but not sufficient — see Pass and docs.
func Score(inv investigation.Investigation, exp Expectation) Metrics {
	known := map[string]struct{}{}
	for _, r := range inv.SourceReferences {
		known[r.Ref] = struct{}{}
	}
	for _, s := range inv.ObservedSymptoms {
		known[s.ID] = struct{}{}
	}
	known["phase:before"] = struct{}{}
	known["phase:during"] = struct{}{}
	known["phase:after"] = struct{}{}

	m := Metrics{
		UncertaintyOK:  !inv.Diagnosed && len(inv.Unknowns) > 0,
		DiagnosedClaim: inv.Diagnosed,
	}
	cited := 0
	total := 0
	for _, h := range inv.Hypotheses {
		total++
		if len(h.SupportingEvidenceRefs) > 0 {
			cited++
		}
		for _, ref := range h.SupportingEvidenceRefs {
			if _, ok := known[ref]; !ok && !strings.HasPrefix(ref, "baseline:") {
				m.UnresolvedCitationCount++
			}
		}
		low := strings.ToLower(h.Statement + " " + h.SuggestedVerification)
		if strings.Contains(low, "caused the") || strings.Contains(low, "proven root") ||
			(strings.Contains(low, "confirmed cause") && !strings.Contains(low, "not")) {
			m.FalseCausalAttribution++
			m.UnsupportedClaimCount++
		}
		if h.Classification != investigation.ClassInferred {
			m.UnsupportedClaimCount++
		}
	}
	if total > 0 {
		m.EvidenceCitationCoverage = float64(cited) / float64(total)
	}
	if exp.RequireKind != "" {
		hit := 0.0
		for _, h := range inv.Hypotheses {
			if h.Kind == exp.RequireKind {
				hit = 1
				break
			}
		}
		m.HypothesisRelevance = hit
	} else if total > 0 {
		m.HypothesisRelevance = 1
	}
	if inv.Diagnosed {
		m.FalseCausalAttribution++
	}
	return m
}

// Pass reports whether metrics meet F1 honesty bars.
func Pass(m Metrics) bool {
	if m.DiagnosedClaim || m.FalseCausalAttribution > 0 {
		return false
	}
	if m.UnsupportedClaimCount > 0 {
		return false
	}
	if !m.UncertaintyOK {
		return false
	}
	if m.EvidenceCitationCoverage < 1.0 {
		return false
	}
	if m.UnresolvedCitationCount > 0 {
		return false
	}
	if m.HypothesisRelevance < 1 {
		return false
	}
	return true
}
