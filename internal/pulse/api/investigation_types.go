package api

import (
	"time"

	"github.com/yashg4509/perch/internal/pulse/investigation"
)

const (
	SchemaInvestigationCreate  = "pulse.api.investigation.create.v1"
	SchemaInvestigation        = "pulse.api.investigation.v1"
	SchemaInvestigationContext = "pulse.api.investigation_context.v1"
	SchemaFeedbackCreate       = "pulse.api.investigation.feedback.create.v1"
	SchemaFeedbackList         = "pulse.api.investigation.feedback.list.v1"
)

// CreateInvestigationRequest is the only accepted POST body.
type CreateInvestigationRequest struct {
	IncidentID     string   `json:"incident_id"`
	ReuseIdentical bool     `json:"reuse_identical,omitempty"`
	LinkedRunIDs   []string `json:"linked_run_ids,omitempty"`
}

// CreateInvestigationResponse is POST /api/pulse/investigations.
type CreateInvestigationResponse struct {
	SchemaVersion string                      `json:"schema_version"`
	GeneratedAt   time.Time                   `json:"generated_at"`
	Investigation investigation.Investigation `json:"investigation"`
	Limitations   []string                    `json:"limitations"`
}

// InvestigationResponse is GET /api/pulse/investigations/{id}.
type InvestigationResponse struct {
	SchemaVersion string                      `json:"schema_version"`
	GeneratedAt   time.Time                   `json:"generated_at"`
	Investigation investigation.Investigation `json:"investigation"`
	Limitations   []string                    `json:"limitations"`
}

// InvestigationContextResponse is GET /api/pulse/incidents/{id}/investigation-context.
type InvestigationContextResponse struct {
	SchemaVersion string                      `json:"schema_version"`
	GeneratedAt   time.Time                   `json:"generated_at"`
	Investigation investigation.Investigation `json:"investigation"`
	AgentContext  investigation.AgentContext  `json:"agent_context"`
	Limitations   []string                    `json:"limitations"`
}

// CreateFeedbackRequest is POST /api/pulse/investigations/{id}/feedback.
type CreateFeedbackRequest struct {
	Assessment             string   `json:"assessment"`
	UsefulHypothesisIDs    []string `json:"useful_hypothesis_ids,omitempty"`
	RejectedHypothesisIDs  []string `json:"rejected_hypothesis_ids,omitempty"`
	Rationale              string   `json:"rationale,omitempty"`
	ReviewerConfirmedCause string   `json:"reviewer_confirmed_cause,omitempty"`
	ConfirmationMethod     string   `json:"confirmation_method,omitempty"`
}

// CreateFeedbackResponse wraps a stored feedback row.
type CreateFeedbackResponse struct {
	SchemaVersion string                 `json:"schema_version"`
	GeneratedAt   time.Time              `json:"generated_at"`
	Feedback      investigation.Feedback `json:"feedback"`
	Limitations   []string               `json:"limitations"`
}

// FeedbackListResponse is GET feedback.
type FeedbackListResponse struct {
	SchemaVersion string                   `json:"schema_version"`
	GeneratedAt   time.Time                `json:"generated_at"`
	Count         int                      `json:"count"`
	Feedback      []investigation.Feedback `json:"feedback"`
	Limitations   []string                 `json:"limitations"`
}
