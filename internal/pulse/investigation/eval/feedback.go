package eval

import (
	"github.com/yashg4509/perch/internal/pulse/investigation"
	"github.com/yashg4509/perch/internal/pulse/investigation/llm"
)

// SummarizeInvestigationFeedback aggregates F1 feedback without elevating it to truth.
func SummarizeInvestigationFeedback(rows []investigation.Feedback) *FeedbackSummary {
	if len(rows) == 0 {
		return nil
	}
	counts := map[string]int{}
	for _, fb := range rows {
		counts[fb.Assessment]++
	}
	return &FeedbackSummary{
		Count:              len(rows),
		AssessmentCounts:   counts,
		SubjectiveOnlyNote: "Feedback assessments rate perceived usefulness — not independently verified factual correctness or ground truth.",
	}
}

// SummarizeProposalFeedback aggregates LLM proposal feedback separately from F1.
func SummarizeProposalFeedback(rows []llm.Feedback) *FeedbackSummary {
	if len(rows) == 0 {
		return nil
	}
	counts := map[string]int{}
	for _, fb := range rows {
		counts[fb.Assessment]++
	}
	return &FeedbackSummary{
		Count:              len(rows),
		AssessmentCounts:   counts,
		SubjectiveOnlyNote: "Proposal feedback is linked to a specific proposal revision and is not mixed with F1 investigation feedback. Not verified ground truth.",
	}
}
