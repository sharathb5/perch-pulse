package eval

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CaseResult is the per-case comparison output.
type CaseResult struct {
	CaseID              string           `json:"case_id"`
	Title               string           `json:"title"`
	Tags                []string         `json:"tags,omitempty"`
	IncidentID          string           `json:"incident_id"`
	InvestigationID     string           `json:"investigation_id"`
	EvidenceFingerprint string           `json:"evidence_fingerprint"`
	Deterministic       MethodScore      `json:"deterministic"`
	LLM                 MethodScore      `json:"llm"`
	FeedbackSummary     *FeedbackSummary `json:"feedback_summary,omitempty"`
	Notes               []string         `json:"notes,omitempty"`
}

// FeedbackSummary aggregates human feedback without treating it as ground truth.
type FeedbackSummary struct {
	Count              int            `json:"count"`
	AssessmentCounts   map[string]int `json:"assessment_counts"`
	SubjectiveOnlyNote string         `json:"subjective_only_note"`
}

// AggregateMetrics rolls up mechanical checks with explicit denominators.
type AggregateMetrics struct {
	CaseCount int `json:"case_count"`

	DeterministicPassCount int `json:"deterministic_pass_count"`
	DeterministicDenom     int `json:"deterministic_denominator"`

	LLMAvailableCount int `json:"llm_available_count"`
	LLMAvailableDenom int `json:"llm_available_denominator"`
	LLMPassCount      int `json:"llm_pass_count"`
	LLMPassDenom      int `json:"llm_pass_denominator"` // only among available

	SyntheticProposalCount int `json:"synthetic_proposal_count"`
	SyntheticPassCount     int `json:"synthetic_fixture_pass_count"`
	SyntheticPassDenom     int `json:"synthetic_fixture_pass_denominator"`
	RecordedProposalCount  int `json:"recorded_offline_proposal_count"`
	RecordedPassCount      int `json:"recorded_offline_pass_count"`
	RecordedPassDenom      int `json:"recorded_offline_pass_denominator"`
	MissingLLMCount        int `json:"missing_llm_proposal_count"`

	FalseCausalDeterministic    int `json:"false_causal_deterministic_total"`
	FalseCausalLLM              int `json:"false_causal_llm_total"`
	UnresolvedCiteDeterministic int `json:"unresolved_citations_deterministic_total"`
	UnresolvedCiteLLM           int `json:"unresolved_citations_llm_total"`

	// HonestyLabels prevent overclaiming.
	HonestyLabels []string `json:"honesty_labels"`
}

// Report is the offline evaluation report.
type Report struct {
	SchemaVersion string           `json:"schema_version"`
	GeneratedAt   time.Time        `json:"generated_at"`
	DatasetID     string           `json:"dataset_id"`
	Mode          string           `json:"mode"` // offline
	Cases         []CaseResult     `json:"cases"`
	Aggregate     AggregateMetrics `json:"aggregate"`
	HumanRubric   []string         `json:"human_rubric"`
	RubricVersion string           `json:"rubric_version,omitempty"`
	ReviewSummary *ReviewSummary   `json:"human_review_summary,omitempty"`
	CorpusMeta    []F2cCaseMeta    `json:"corpus_meta,omitempty"`
	Limitations   []string         `json:"limitations"`
}

// BuildAggregate computes denominators from case results.
func BuildAggregate(cases []CaseResult) AggregateMetrics {
	a := AggregateMetrics{
		CaseCount:          len(cases),
		DeterministicDenom: len(cases),
		LLMAvailableDenom:  len(cases),
		HonestyLabels: []string{
			"No statistical significance is claimed for this dataset size.",
			"LLM pass rates exclude unavailable/missing proposals from the pass denominator.",
			"Synthetic fixture pass rates measure harness/validator behavior — not live model quality.",
			"Missing LLM proposals do not invent model performance.",
			"Human feedback usefulness is subjective and separate from mechanical correctness.",
			"Citation resolution ≠ semantic entailment.",
		},
	}
	for _, c := range cases {
		if c.Deterministic.PassMechanical {
			a.DeterministicPassCount++
		}
		a.FalseCausalDeterministic += c.Deterministic.FalseCausalAttribution
		a.UnresolvedCiteDeterministic += c.Deterministic.UnresolvedCitationCount

		if c.LLM.Available {
			a.LLMAvailableCount++
			a.LLMPassDenom++
			if c.LLM.PassMechanical {
				a.LLMPassCount++
			}
			switch c.LLM.ProposalOrigin {
			case "synthetic_fixture":
				a.SyntheticProposalCount++
				a.SyntheticPassDenom++
				if c.LLM.PassMechanical {
					a.SyntheticPassCount++
				}
			case "recorded_offline":
				a.RecordedProposalCount++
				a.RecordedPassDenom++
				if c.LLM.PassMechanical {
					a.RecordedPassCount++
				}
			}
			a.FalseCausalLLM += c.LLM.FalseCausalAttribution
			a.UnresolvedCiteLLM += c.LLM.UnresolvedCitationCount
		} else {
			a.MissingLLMCount++
		}
	}
	return a
}

// Markdown renders a human-readable report.
func (r Report) Markdown() string {
	var b strings.Builder
	b.WriteString("# Pulse Investigation Evaluation Report\n\n")
	b.WriteString(fmt.Sprintf("- schema: `%s`\n", r.SchemaVersion))
	b.WriteString(fmt.Sprintf("- dataset: `%s`\n", r.DatasetID))
	b.WriteString(fmt.Sprintf("- generated_at: %s\n", r.GeneratedAt.UTC().Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("- mode: `%s`\n\n", r.Mode))

	a := r.Aggregate
	b.WriteString("## Aggregate (explicit denominators)\n\n")
	b.WriteString(fmt.Sprintf("| Metric | Value |\n|---|---|\n"))
	b.WriteString(fmt.Sprintf("| Cases | %d |\n", a.CaseCount))
	b.WriteString(fmt.Sprintf("| Deterministic mechanical pass | %d / %d |\n", a.DeterministicPassCount, a.DeterministicDenom))
	b.WriteString(fmt.Sprintf("| LLM proposals available | %d / %d |\n", a.LLMAvailableCount, a.LLMAvailableDenom))
	b.WriteString(fmt.Sprintf("| LLM mechanical pass (among available) | %d / %d |\n", a.LLMPassCount, a.LLMPassDenom))
	b.WriteString(fmt.Sprintf("| Synthetic fixture harness pass | %d / %d |\n", a.SyntheticPassCount, a.SyntheticPassDenom))
	b.WriteString(fmt.Sprintf("| Recorded offline proposal pass | %d / %d |\n", a.RecordedPassCount, a.RecordedPassDenom))
	b.WriteString(fmt.Sprintf("| Synthetic proposals | %d |\n", a.SyntheticProposalCount))
	b.WriteString(fmt.Sprintf("| Recorded offline proposals | %d |\n", a.RecordedProposalCount))
	b.WriteString(fmt.Sprintf("| Missing LLM proposals | %d |\n", a.MissingLLMCount))
	b.WriteString(fmt.Sprintf("| False causal (deterministic total) | %d |\n", a.FalseCausalDeterministic))
	b.WriteString(fmt.Sprintf("| False causal (LLM total) | %d |\n", a.FalseCausalLLM))
	b.WriteString("\n")

	b.WriteString("### Honesty labels\n\n")
	for _, h := range a.HonestyLabels {
		b.WriteString("- " + h + "\n")
	}
	b.WriteString("\n## Per-case\n\n")
	for _, c := range r.Cases {
		b.WriteString(fmt.Sprintf("### %s — %s\n\n", c.CaseID, c.Title))
		b.WriteString(fmt.Sprintf("- fingerprint: `%s`\n", c.EvidenceFingerprint))
		b.WriteString(fmt.Sprintf("- deterministic: pass=%v citations=%d/%d unresolved=%d false_causal=%d\n",
			c.Deterministic.PassMechanical,
			c.Deterministic.CitationCoverageNumerator,
			c.Deterministic.CitationCoverageDenominator,
			c.Deterministic.UnresolvedCitationCount,
			c.Deterministic.FalseCausalAttribution,
		))
		if c.LLM.Available {
			b.WriteString(fmt.Sprintf("- llm (%s): pass=%v citations=%d/%d unresolved=%d false_causal=%d\n",
				c.LLM.ProposalOrigin,
				c.LLM.PassMechanical,
				c.LLM.CitationCoverageNumerator,
				c.LLM.CitationCoverageDenominator,
				c.LLM.UnresolvedCitationCount,
				c.LLM.FalseCausalAttribution,
			))
		} else {
			b.WriteString(fmt.Sprintf("- llm: **unavailable** (%s)\n", c.LLM.UnavailableReason))
		}
		if c.FeedbackSummary != nil {
			b.WriteString(fmt.Sprintf("- feedback rows: %d (subjective usefulness only)\n", c.FeedbackSummary.Count))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Human rubric (semantic support)\n\n")
	for _, line := range r.HumanRubric {
		b.WriteString("- " + line + "\n")
	}
	b.WriteString("\n## Limitations\n\n")
	for _, line := range r.Limitations {
		b.WriteString("- " + line + "\n")
	}
	return b.String()
}

// JSON returns indented report JSON.
func (r Report) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}
