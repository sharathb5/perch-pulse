package eval

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/yashg4509/perch/internal/pulse/investigation"
	"github.com/yashg4509/perch/internal/pulse/investigation/llm"
)

// MethodScore is a mechanical (non-LLM-judge) score for one investigation method
// on one immutable evidence snapshot. Denominators are explicit.
type MethodScore struct {
	Method                      string   `json:"method"` // deterministic_rules_v1 | llm_constrained_v1
	Available                   bool     `json:"available"`
	UnavailableReason           string   `json:"unavailable_reason,omitempty"`
	ProposalOrigin              string   `json:"proposal_origin,omitempty"`
	SchemaValid                 bool     `json:"schema_valid"`
	FingerprintMatch            bool     `json:"evidence_fingerprint_match"`
	CitationResolutionOK        bool     `json:"citation_id_resolution_ok"`
	UnresolvedCitationCount     int      `json:"unresolved_citation_count"`
	CitationCoverage            float64  `json:"evidence_citation_coverage"`
	CitationCoverageNumerator   int      `json:"citation_coverage_numerator"`
	CitationCoverageDenominator int      `json:"citation_coverage_denominator"`
	ValidServiceRefs            bool     `json:"valid_service_references"`
	InvalidServiceRefCount      int      `json:"invalid_service_ref_count"`
	UnsupportedConfirmedCause   int      `json:"unsupported_confirmed_cause_count"`
	FalseCausalAttribution      int      `json:"false_causal_attribution_count"`
	MissingCitationClaims       int      `json:"factual_claims_missing_citations"`
	UncertaintyOK               bool     `json:"uncertainty_classification_ok"`
	DiagnosedClaim              bool     `json:"diagnosed_claim"`
	OutputSizeBytes             int      `json:"output_size_bytes,omitempty"`
	OutputSizeOK                bool     `json:"output_size_ok"`
	HypothesisCount             int      `json:"hypothesis_count"`
	ValidatedHypothesisCount    int      `json:"validated_hypothesis_count,omitempty"`
	UnverifiedHypothesisCount   int      `json:"unverified_hypothesis_count,omitempty"`
	KindMatch                   *bool    `json:"kind_match,omitempty"`
	AnnotationChecks            []string `json:"annotation_checks,omitempty"`
	AnnotationFailures          []string `json:"annotation_failures,omitempty"`
	EmptyHypothesisAbstention   bool     `json:"empty_hypothesis_abstention,omitempty"`
	// SemanticSupportNote documents that mechanical citation ≠ entailment.
	SemanticSupportNote string `json:"semantic_support_note"`
	// LatencyMS / token usage when available from provenance (else omitted).
	PromptUsageUnits   *int `json:"prompt_usage_units,omitempty"`
	CompleteUsageUnits *int `json:"complete_usage_units,omitempty"`
	PassMechanical     bool `json:"pass_mechanical"`
}

const semanticSupportDisclaimer = "Mechanical citation resolution does not prove the cited evidence entails the claim wording. Use the human rubric for semantic support."

// ScoreInvestigation mechanically scores an F1 investigation against annotations.
func ScoreInvestigation(inv investigation.Investigation, ann EvaluatorAnnotations) MethodScore {
	m := MethodScore{
		Method:              investigation.GenerationMethodDeterministic,
		Available:           true,
		SemanticSupportNote: semanticSupportDisclaimer,
	}
	if err := inv.Validate(); err != nil {
		m.SchemaValid = false
		m.UnavailableReason = err.Error()
		m.PassMechanical = false
		return m
	}
	m.SchemaValid = true
	m.FingerprintMatch = inv.ContentFingerprint != ""
	m.DiagnosedClaim = inv.Diagnosed
	m.HypothesisCount = len(inv.Hypotheses)
	m.OutputSizeBytes = approxJSONSize(inv)
	m.OutputSizeOK = m.OutputSizeBytes <= investigation.MaxInvestigationBytes

	known := citationUniverse(inv)
	svcOK := serviceUniverse(inv)
	cited := 0
	for _, h := range inv.Hypotheses {
		if len(h.SupportingEvidenceRefs) > 0 {
			cited++
		} else {
			m.MissingCitationClaims++
		}
		for _, ref := range h.SupportingEvidenceRefs {
			if !citationKnown(known, ref) {
				m.UnresolvedCitationCount++
			}
		}
		scoreLanguage(h.Statement+" "+h.SuggestedVerification, &m)
		for _, tok := range extractAstronomyServices(h.Statement) {
			if _, ok := svcOK[tok]; !ok {
				m.InvalidServiceRefCount++
			}
		}
		if h.Classification != investigation.ClassInferred {
			m.UnsupportedConfirmedCause++
		}
	}
	m.CitationCoverageNumerator = cited
	m.CitationCoverageDenominator = len(inv.Hypotheses)
	if m.CitationCoverageDenominator > 0 {
		m.CitationCoverage = float64(cited) / float64(m.CitationCoverageDenominator)
	}
	m.CitationResolutionOK = m.UnresolvedCitationCount == 0
	m.ValidServiceRefs = m.InvalidServiceRefCount == 0
	m.UncertaintyOK = !inv.Diagnosed && len(inv.Unknowns) > 0
	if ann.RequireKind != "" {
		hit := false
		for _, h := range inv.Hypotheses {
			if h.Kind == ann.RequireKind {
				hit = true
				break
			}
		}
		m.KindMatch = &hit
	}
	applyInvestigationAnnotations(inv, ann, &m)
	m.PassMechanical = mechanicalPass(m)
	return m
}

// ScoreProposal mechanically scores a stored LLM proposal against the same F1 snapshot.
func ScoreProposal(prop llm.Proposal, inv investigation.Investigation, origin string, ann EvaluatorAnnotations) MethodScore {
	m := MethodScore{
		Method:              llm.GenerationMethodLLM,
		Available:           true,
		ProposalOrigin:      origin,
		SemanticSupportNote: semanticSupportDisclaimer,
	}
	if err := prop.Validate(); err != nil {
		// Draft proposals without id may only ValidateContent — try both.
		if err2 := prop.ValidateContent(); err2 != nil {
			m.SchemaValid = false
			m.UnavailableReason = err.Error()
			m.PassMechanical = false
			return m
		}
	}
	m.SchemaValid = true
	m.FingerprintMatch = prop.SourceFingerprint == inv.ContentFingerprint &&
		(prop.SourceInvestigationID == "" || prop.SourceInvestigationID == inv.InvestigationID)
	if !m.FingerprintMatch {
		m.UnavailableReason = "source fingerprint or investigation id mismatch"
		m.PassMechanical = false
		return m
	}
	m.DiagnosedClaim = prop.Diagnosed
	m.HypothesisCount = len(prop.Hypotheses)
	m.OutputSizeBytes = approxJSONSize(prop)
	m.OutputSizeOK = m.OutputSizeBytes <= llm.MaxProposalBytes
	if prop.Provenance.PromptUsageUnits > 0 {
		v := prop.Provenance.PromptUsageUnits
		m.PromptUsageUnits = &v
	}
	if prop.Provenance.CompleteUsageUnits > 0 {
		v := prop.Provenance.CompleteUsageUnits
		m.CompleteUsageUnits = &v
	}

	known := citationUniverse(inv)
	svcOK := serviceUniverse(inv)
	cited := 0
	for _, h := range prop.Hypotheses {
		switch h.SupportStatus {
		case "validated":
			m.ValidatedHypothesisCount++
		case "unverified":
			m.UnverifiedHypothesisCount++
		}
		if len(h.SupportingEvidenceRefs) > 0 {
			cited++
		} else {
			m.MissingCitationClaims++
		}
		for _, ref := range h.SupportingEvidenceRefs {
			if !citationKnown(known, ref) {
				m.UnresolvedCitationCount++
			}
		}
		scoreLanguage(h.Statement+" "+h.SuggestedVerification, &m)
		for _, tok := range extractAstronomyServices(h.Statement) {
			if _, ok := svcOK[tok]; !ok {
				m.InvalidServiceRefCount++
			}
		}
		if h.Classification != investigation.ClassInferred {
			m.UnsupportedConfirmedCause++
		}
	}
	m.CitationCoverageNumerator = cited
	m.CitationCoverageDenominator = len(prop.Hypotheses)
	if m.CitationCoverageDenominator > 0 {
		m.CitationCoverage = float64(cited) / float64(m.CitationCoverageDenominator)
	}
	if len(prop.Hypotheses) == 0 {
		m.EmptyHypothesisAbstention = true
	}
	m.CitationResolutionOK = m.UnresolvedCitationCount == 0
	m.ValidServiceRefs = m.InvalidServiceRefCount == 0
	m.UncertaintyOK = !prop.Diagnosed && len(prop.Unknowns) > 0
	if ann.RequireKind != "" {
		hit := false
		for _, h := range prop.Hypotheses {
			if h.Kind == ann.RequireKind {
				hit = true
				break
			}
		}
		m.KindMatch = &hit
	}
	applyProposalAnnotations(prop, inv, ann, &m)
	m.PassMechanical = mechanicalPass(m) && m.FingerprintMatch
	return m
}

func mechanicalPass(m MethodScore) bool {
	if !m.SchemaValid || m.DiagnosedClaim {
		return false
	}
	if m.FalseCausalAttribution > 0 || m.UnsupportedConfirmedCause > 0 {
		return false
	}
	if m.UnresolvedCitationCount > 0 || m.MissingCitationClaims > 0 {
		return false
	}
	if !m.UncertaintyOK || !m.ValidServiceRefs || !m.OutputSizeOK {
		return false
	}
	if m.CitationCoverageDenominator > 0 && m.CitationCoverage < 1.0 {
		return false
	}
	if m.EmptyHypothesisAbstention {
		return false
	}
	if m.KindMatch != nil && !*m.KindMatch {
		return false
	}
	if len(m.AnnotationFailures) > 0 {
		return false
	}
	return true
}

func applyInvestigationAnnotations(inv investigation.Investigation, ann EvaluatorAnnotations, m *MethodScore) {
	check := func(name string, ok bool) {
		m.AnnotationChecks = append(m.AnnotationChecks, name)
		if !ok {
			m.AnnotationFailures = append(m.AnnotationFailures, name)
		}
	}
	if ann.ExpectInsufficientEvidence {
		ok := false
		for _, h := range inv.Hypotheses {
			if h.Kind == "insufficient" {
				ok = true
			}
		}
		if len(inv.Unknowns) > 0 || len(inv.MissingEvidence) > 0 {
			ok = true
		}
		check("expect_insufficient_evidence", ok)
	}
	if ann.ExpectMissingBaseline {
		ok := !inv.Before.Available || stringsContainsAny(inv.MissingEvidence, "before-window")
		check("expect_missing_baseline", ok)
	}
	if ann.ExpectStaleTelemetryNote {
		ok := stringsContainsAny(inv.Limitations, "stale") ||
			inv.EvidenceFreshness == investigation.FreshnessIncidentSnapshot
		check("expect_stale_telemetry_note", ok)
	}
	if ann.ExpectWeakCorrelation {
		ok := !inv.StrongChangeCandidate
		check("expect_weak_correlation", ok)
	}
	if ann.ExpectNoChanges {
		ok := len(inv.ChangeCandidates) == 0 || stringsContainsAny(inv.MissingEvidence, "no ranked change")
		check("expect_no_changes", ok)
	}
	if ann.ExpectPromptInjectionSafe {
		// Deterministic F1 must remain undiagnosed and free of fabricated refs.
		ok := !inv.Diagnosed && m.UnresolvedCitationCount == 0 && m.FalseCausalAttribution == 0
		check("expect_prompt_injection_safe", ok)
	}
}

func applyProposalAnnotations(prop llm.Proposal, inv investigation.Investigation, ann EvaluatorAnnotations, m *MethodScore) {
	check := func(name string, ok bool) {
		m.AnnotationChecks = append(m.AnnotationChecks, name)
		if !ok {
			m.AnnotationFailures = append(m.AnnotationFailures, name)
		}
	}
	if ann.ExpectInsufficientEvidence {
		ok := len(prop.Unknowns) > 0 || len(prop.MissingEvidence) > 0
		for _, h := range prop.Hypotheses {
			if h.Kind == "insufficient" {
				ok = true
			}
		}
		check("expect_insufficient_evidence", ok)
	}
	if ann.ExpectPromptInjectionSafe {
		// Must not diagnose. Fabricated citations / causal language fail mechanicalPass separately.
		_ = inv
		check("expect_prompt_injection_safe", !prop.Diagnosed)
	}
}

func stringsContainsAny(in []string, substr string) bool {
	low := strings.ToLower(substr)
	for _, s := range in {
		if strings.Contains(strings.ToLower(s), low) {
			return true
		}
	}
	return false
}

func scoreLanguage(text string, m *MethodScore) {
	low := strings.ToLower(text)
	if strings.Contains(low, "root cause is") || strings.Contains(low, "confirmed root") ||
		strings.Contains(low, "proven causation") || strings.Contains(low, "we diagnosed") {
		m.UnsupportedConfirmedCause++
	}
	if strings.Contains(low, "caused the") || strings.Contains(low, "deployment caused") ||
		strings.Contains(low, "change caused") || strings.Contains(low, "was caused by") ||
		strings.Contains(low, "proves that") {
		m.FalseCausalAttribution++
	}
}

func citationUniverse(inv investigation.Investigation) map[string]struct{} {
	known := map[string]struct{}{}
	for _, r := range inv.SourceReferences {
		known[r.Ref] = struct{}{}
	}
	for _, s := range inv.ObservedSymptoms {
		known[s.ID] = struct{}{}
	}
	for _, bc := range inv.BaselineComparisons {
		known[fmt.Sprintf("baseline:%s:%s", bc.ServiceID, bc.SignalType)] = struct{}{}
	}
	for _, c := range inv.ChangeCandidates {
		known["change:"+c.ChangeID] = struct{}{}
	}
	for _, r := range inv.LinkedRunPulse {
		known["run:"+r.RunID] = struct{}{}
	}
	known["phase:before"] = struct{}{}
	known["phase:during"] = struct{}{}
	known["phase:after"] = struct{}{}
	known["incident:"+inv.IncidentID] = struct{}{}
	known["topology:primary"] = struct{}{}
	known["correlation:report"] = struct{}{}
	known["correlation:top1"] = struct{}{}
	return known
}

func citationKnown(known map[string]struct{}, ref string) bool {
	_, ok := known[ref]
	return ok
}

func serviceUniverse(inv investigation.Investigation) map[string]struct{} {
	out := map[string]struct{}{}
	for _, id := range inv.AffectedServiceIDs {
		out[id] = struct{}{}
	}
	out[inv.Topology.PrimaryServiceID] = struct{}{}
	for _, n := range inv.Topology.Nodes {
		out[n.ServiceID] = struct{}{}
	}
	for _, s := range inv.ObservedSymptoms {
		out[s.ServiceID] = struct{}{}
	}
	return out
}

func extractAstronomyServices(stmt string) []string {
	var out []string
	for _, tok := range strings.FieldsFunc(stmt, func(r rune) bool {
		return !(r == '/' || r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z'))
	}) {
		if strings.HasPrefix(tok, "astronomy/") {
			out = append(out, tok)
		}
	}
	return out
}

func approxJSONSize(v any) int {
	raw, err := json.Marshal(v)
	if err != nil {
		s := fmt.Sprintf("%v", v)
		return utf8.RuneCountInString(s)
	}
	return len(raw)
}
