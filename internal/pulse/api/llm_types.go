package api

import (
	"time"

	"github.com/yashg4509/perch/internal/pulse/investigation/llm"
)

const (
	SchemaLLMStatus                 = "pulse.api.investigation.llm_status.v1"
	SchemaLLMPropose                = "pulse.api.investigation.llm_propose.v1"
	SchemaLLMProposal               = "pulse.api.investigation.proposal.v1"
	SchemaLLMProposalList           = "pulse.api.investigation.proposal.list.v1"
	SchemaLLMProposalFeedbackCreate = "pulse.api.investigation.proposal.feedback.create.v1"
	SchemaLLMProposalFeedbackList   = "pulse.api.investigation.proposal.feedback.list.v1"
)

// LLMProposeRequest is POST .../llm-propose body.
type LLMProposeRequest struct {
	ExpectedFingerprint string `json:"expected_fingerprint,omitempty"`
}

// LLMProposeResponse wraps a validated proposal.
type LLMProposeResponse struct {
	SchemaVersion string       `json:"schema_version"`
	GeneratedAt   time.Time    `json:"generated_at"`
	Proposal      llm.Proposal `json:"proposal"`
	Limitations   []string     `json:"limitations"`
}

// LLMProposalResponse is GET proposal.
type LLMProposalResponse struct {
	SchemaVersion string       `json:"schema_version"`
	GeneratedAt   time.Time    `json:"generated_at"`
	Proposal      llm.Proposal `json:"proposal"`
	Limitations   []string     `json:"limitations"`
}

// LLMProposalListResponse lists proposals for an investigation.
type LLMProposalListResponse struct {
	SchemaVersion string         `json:"schema_version"`
	GeneratedAt   time.Time      `json:"generated_at"`
	Count         int            `json:"count"`
	Proposals     []llm.Proposal `json:"proposals"`
	Limitations   []string       `json:"limitations"`
}

// LLMStatusResponse is GET /api/pulse/llm/status.
type LLMStatusResponse struct {
	SchemaVersion string     `json:"schema_version"`
	GeneratedAt   time.Time  `json:"generated_at"`
	Status        llm.Status `json:"status"`
	Limitations   []string   `json:"limitations"`
}

// LLMProposalFeedbackRequest is POST proposal feedback.
type LLMProposalFeedbackRequest struct {
	Assessment            string   `json:"assessment"`
	UsefulHypothesisIDs   []string `json:"useful_hypothesis_ids,omitempty"`
	RejectedHypothesisIDs []string `json:"rejected_hypothesis_ids,omitempty"`
	Rationale             string   `json:"rationale,omitempty"`
}

// LLMProposalFeedbackResponse wraps stored proposal feedback.
type LLMProposalFeedbackResponse struct {
	SchemaVersion string       `json:"schema_version"`
	GeneratedAt   time.Time    `json:"generated_at"`
	Feedback      llm.Feedback `json:"feedback"`
	Limitations   []string     `json:"limitations"`
}

// LLMProposalFeedbackListResponse lists proposal feedback.
type LLMProposalFeedbackListResponse struct {
	SchemaVersion string         `json:"schema_version"`
	GeneratedAt   time.Time      `json:"generated_at"`
	Count         int            `json:"count"`
	Feedback      []llm.Feedback `json:"feedback"`
	Limitations   []string       `json:"limitations"`
}
