package eval

import (
	"fmt"
	"time"

	"github.com/yashg4509/perch/internal/pulse/investigation"
	"github.com/yashg4509/perch/internal/pulse/investigation/llm"
)

// FeedbackSource optionally supplies human feedback for report enrichment.
type FeedbackSource struct {
	Investigation func(investigationID string) ([]investigation.Feedback, error)
	Proposal      func(proposalID string) ([]llm.Feedback, error)
}

// RunOptions configure an offline evaluation run.
type RunOptions struct {
	Dataset  Dataset
	Feedback FeedbackSource
	Now      func() time.Time
	// RequireLLM when true fails cases that lack stored proposals (default false).
	RequireLLM bool
}

// Run executes a fully offline evaluation: no provider calls.
func Run(opts RunOptions) (Report, error) {
	if err := opts.Dataset.Validate(); err != nil {
		return Report{}, err
	}
	now := time.Now().UTC()
	if opts.Now != nil {
		now = opts.Now().UTC()
	}

	var results []CaseResult
	for _, c := range opts.Dataset.Cases {
		if err := AssertNoAnnotationLeakage(c); err != nil {
			return Report{}, fmt.Errorf("eval: case %s: %w", c.CaseID, err)
		}
		// Prove generation path cannot see annotations/GT by scoring only GenerationOnly().
		gen := c.GenerationOnly()
		inv := gen.Investigation

		det := ScoreInvestigation(inv, c.Annotations)
		llmScore := MethodScore{
			Method:              llm.GenerationMethodLLM,
			Available:           false,
			UnavailableReason:   "no stored proposal for this evidence fingerprint",
			SemanticSupportNote: semanticSupportDisclaimer,
		}
		var notes []string
		var fbSummary *FeedbackSummary

		if len(gen.StoredProposals) == 0 {
			if opts.RequireLLM {
				return Report{}, fmt.Errorf("eval: case %s missing stored LLM proposal", c.CaseID)
			}
			notes = append(notes, "LLM proposal unavailable — not scored; performance not fabricated")
		} else {
			// Use first matching fingerprint proposal.
			var chosen *StoredProposal
			for i := range gen.StoredProposals {
				sp := &gen.StoredProposals[i]
				if sp.Proposal.SourceFingerprint == "" || sp.Proposal.SourceFingerprint == inv.ContentFingerprint {
					chosen = sp
					break
				}
			}
			if chosen == nil {
				notes = append(notes, "stored proposals present but none matched fingerprint")
			} else {
				llmScore = ScoreProposal(chosen.Proposal, inv, chosen.Origin, c.Annotations)
				if opts.Feedback.Proposal != nil && chosen.Proposal.ProposalID != "" {
					if rows, err := opts.Feedback.Proposal(chosen.Proposal.ProposalID); err == nil {
						fbSummary = SummarizeProposalFeedback(rows)
					}
				}
			}
		}

		if opts.Feedback.Investigation != nil {
			if rows, err := opts.Feedback.Investigation(inv.InvestigationID); err == nil {
				if sum := SummarizeInvestigationFeedback(rows); sum != nil {
					if fbSummary == nil {
						fbSummary = sum
					} else {
						notes = append(notes, "investigation and proposal feedback both present; proposal summary shown")
					}
				}
			}
		}

		results = append(results, CaseResult{
			CaseID:              c.CaseID,
			Title:               c.Title,
			Tags:                append([]string{}, c.Tags...),
			IncidentID:          inv.IncidentID,
			InvestigationID:     inv.InvestigationID,
			EvidenceFingerprint: inv.ContentFingerprint,
			Deterministic:       det,
			LLM:                 llmScore,
			FeedbackSummary:     fbSummary,
			Notes:               notes,
		})
	}

	rep := Report{
		SchemaVersion: ReportSchemaVersion,
		GeneratedAt:   now,
		DatasetID:     opts.Dataset.DatasetID,
		Mode:          "offline",
		Cases:         results,
		Aggregate:     BuildAggregate(results),
		HumanRubric:   HumanRubric(),
		Limitations:   DefaultLimitations(),
	}
	return rep, nil
}
