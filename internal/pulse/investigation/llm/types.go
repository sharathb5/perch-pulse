package llm

import (
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yashg4509/perch/internal/pulse/investigation"
)

const (
	// ProposalSchemaVersion is the persisted LLM proposal schema.
	ProposalSchemaVersion = "pulse.investigation.proposal.v1"
	// ProposalFeedbackSchemaVersion is feedback linked to a proposal revision.
	ProposalFeedbackSchemaVersion = "pulse.investigation.proposal.feedback.v1"
	// GenerationMethodLLM is the generation_method for model-backed proposals.
	GenerationMethodLLM = "llm_constrained_v1"
	// MaxProposalBytes bounds persisted proposal JSON.
	MaxProposalBytes = 256 * 1024
	// MaxHypotheses caps validated hypotheses in a proposal.
	MaxHypotheses = investigation.MaxHypotheses
	// MaxStatementRunes bounds hypothesis statement length.
	MaxStatementRunes = 800
	// MaxVerificationRunes bounds suggested verification text.
	MaxVerificationRunes = 600
	// MaxMissingItemRunes bounds each missing/unknown string.
	MaxMissingItemRunes = 400
	// MaxProposalRetention soft-prunes oldest proposals.
	MaxProposalRetention = 100
	// EvidencePayloadSchemaVersion versions the model-facing evidence contract.
	EvidencePayloadSchemaVersion = "pulse.investigation.llm_evidence.v1"
	// ModelOutputSchemaVersion is the JSON object the model must emit.
	ModelOutputSchemaVersion = "pulse.investigation.llm_output.v1"
)

// Provenance records non-secret model/provider metadata.
type Provenance struct {
	Provider           string `json:"provider"` // openai_compatible
	Model              string `json:"model"`
	EndpointHost       string `json:"endpoint_host"` // host only; never full URL with secrets
	FinishReason       string `json:"finish_reason,omitempty"`
	PromptUsageUnits   int    `json:"prompt_usage_units,omitempty"`
	CompleteUsageUnits int    `json:"complete_usage_units,omitempty"`
	RequestID          string `json:"request_id,omitempty"`
}

// Hypothesis is a validated INFERRED claim with citations into the F1 snapshot.
type Hypothesis struct {
	HypothesisID           string                       `json:"hypothesis_id"`
	Statement              string                       `json:"statement"`
	Classification         investigation.Classification `json:"classification"` // always INFERRED
	SupportingEvidenceRefs []string                     `json:"supporting_evidence_refs"`
	CounterEvidenceRefs    []string                     `json:"counter_evidence_refs,omitempty"`
	MissingEvidence        []string                     `json:"missing_evidence,omitempty"`
	SuggestedVerification  string                       `json:"suggested_verification"`
	Kind                   string                       `json:"kind"`
	SupportStatus          string                       `json:"support_status"` // validated | unverified
	RejectedReason         string                       `json:"rejected_reason,omitempty"`
}

// Proposal is a versioned, immutable LLM investigation proposal.
// It does not replace the F1 investigation snapshot.
type Proposal struct {
	SchemaVersion         string       `json:"schema_version"`
	ProposalID            string       `json:"proposal_id"`
	SourceInvestigationID string       `json:"source_investigation_id"`
	SourceIncidentID      string       `json:"source_incident_id"`
	SourceFingerprint     string       `json:"source_fingerprint"`
	GenerationMethod      string       `json:"generation_method"`
	GeneratedAt           time.Time    `json:"generated_at"`
	Diagnosed             bool         `json:"diagnosed"` // always false
	Hypotheses            []Hypothesis `json:"hypotheses"`
	RejectedHypotheses    []Hypothesis `json:"rejected_hypotheses,omitempty"`
	MissingEvidence       []string     `json:"missing_evidence"`
	Unknowns              []string     `json:"unknowns"`
	SuggestedVerification []string     `json:"suggested_verification_steps"`
	Limitations           []string     `json:"limitations"`
	Provenance            Provenance   `json:"provenance"`
	ValidationNotes       []string     `json:"validation_notes,omitempty"`
}

// Feedback is human quality rating for a specific proposal revision.
// It is never mixed with F1 investigation feedback.
type Feedback struct {
	SchemaVersion         string    `json:"schema_version"`
	FeedbackID            string    `json:"feedback_id"`
	ProposalID            string    `json:"proposal_id"`
	SourceInvestigationID string    `json:"source_investigation_id"`
	Timestamp             time.Time `json:"timestamp"`
	Assessment            string    `json:"assessment"`
	UsefulHypothesisIDs   []string  `json:"useful_hypothesis_ids,omitempty"`
	RejectedHypothesisIDs []string  `json:"rejected_hypothesis_ids,omitempty"`
	Rationale             string    `json:"rationale,omitempty"`
	Limitations           []string  `json:"limitations,omitempty"`
}

// Status describes whether LLM investigation is available (no secrets).
type Status struct {
	SchemaVersion string `json:"schema_version"`
	Available     bool   `json:"available"`
	Enabled       bool   `json:"enabled"`
	Configured    bool   `json:"configured"`
	Reason        string `json:"reason,omitempty"`
	Model         string `json:"model,omitempty"`
	EndpointHost  string `json:"endpoint_host,omitempty"`
	Provider      string `json:"provider,omitempty"`
}

const StatusSchemaVersion = "pulse.investigation.llm_status.v1"

var forbiddenFieldSubstrings = []string{
	"secret", "token", "password", "credential", "apikey", "api_key", "auth",
}

// ValidateReportsSecretFieldNames guards exported proposal shapes.
func ValidateReportsSecretFieldNames() error {
	for _, t := range []reflect.Type{
		reflect.TypeOf(Proposal{}),
		reflect.TypeOf(Feedback{}),
		reflect.TypeOf(Hypothesis{}),
		reflect.TypeOf(Provenance{}),
		reflect.TypeOf(Status{}),
	} {
		for i := 0; i < t.NumField(); i++ {
			name := strings.ToLower(t.Field(i).Name)
			for _, bad := range forbiddenFieldSubstrings {
				if strings.Contains(name, bad) {
					return fmt.Errorf("llm: type %s field %s looks like a secret holder", t.Name(), t.Field(i).Name)
				}
			}
		}
	}
	return nil
}

// ValidateContent checks proposal content invariants (before persistence assigns id/time).
func (p Proposal) ValidateContent() error {
	if p.SchemaVersion != ProposalSchemaVersion {
		return fmt.Errorf("llm: schema_version must be %q", ProposalSchemaVersion)
	}
	if strings.TrimSpace(p.SourceInvestigationID) == "" {
		return fmt.Errorf("llm: source_investigation_id is required")
	}
	if strings.TrimSpace(p.SourceFingerprint) == "" {
		return fmt.Errorf("llm: source_fingerprint is required")
	}
	if p.GenerationMethod != GenerationMethodLLM {
		return fmt.Errorf("llm: unsupported generation_method %q", p.GenerationMethod)
	}
	if p.Diagnosed {
		return fmt.Errorf("llm: diagnosed must be false")
	}
	if len(p.Hypotheses) > MaxHypotheses {
		return fmt.Errorf("llm: too many hypotheses (%d > %d)", len(p.Hypotheses), MaxHypotheses)
	}
	for _, h := range p.Hypotheses {
		if err := validateHypothesis(h, true); err != nil {
			return err
		}
	}
	for _, h := range p.RejectedHypotheses {
		if err := validateHypothesis(h, false); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks proposal invariants for persistence.
func (p Proposal) Validate() error {
	if err := p.ValidateContent(); err != nil {
		return err
	}
	if strings.TrimSpace(p.ProposalID) == "" {
		return fmt.Errorf("llm: proposal_id is required")
	}
	if p.GeneratedAt.IsZero() || p.GeneratedAt.Location() != time.UTC {
		return fmt.Errorf("llm: generated_at must be UTC")
	}
	return nil
}

func validateHypothesis(h Hypothesis, requireValidated bool) error {
	if strings.TrimSpace(h.HypothesisID) == "" || strings.TrimSpace(h.Statement) == "" {
		return fmt.Errorf("llm: hypothesis requires id and statement")
	}
	if h.Classification != investigation.ClassInferred {
		return fmt.Errorf("llm: hypothesis %q must be INFERRED", h.HypothesisID)
	}
	if utf8.RuneCountInString(h.Statement) > MaxStatementRunes {
		return fmt.Errorf("llm: hypothesis %q statement too long", h.HypothesisID)
	}
	if utf8.RuneCountInString(h.SuggestedVerification) > MaxVerificationRunes {
		return fmt.Errorf("llm: hypothesis %q verification too long", h.HypothesisID)
	}
	if requireValidated && h.SupportStatus != "validated" && h.SupportStatus != "unverified" {
		return fmt.Errorf("llm: hypothesis %q has invalid support_status %q", h.HypothesisID, h.SupportStatus)
	}
	return nil
}

// FeedbackLimitations documents proposal feedback semantics.
func FeedbackLimitations() []string {
	return []string{
		"Proposal feedback rates model output quality — not independently verified ground truth.",
		"Feedback does not mutate F1 investigations, incidents, or detector thresholds.",
		"Feedback is linked to a specific proposal revision only.",
	}
}

// DefaultLimitations are always attached to validated proposals.
func DefaultLimitations() []string {
	return []string{
		"LLM proposal is optional inference — not a confirmed root-cause diagnosis.",
		"F1 deterministic investigation remains authoritative for OBSERVED facts.",
		"Every factual claim must cite evidence IDs from the source snapshot; fabricated IDs are rejected.",
		"Citation validity does not prove the cited evidence supports the claim wording.",
		"Correlation is never presented as proven causation.",
		"No credentials or raw provider transcripts are persisted.",
		"Provider compatibility is tested via OpenAI-style chat/completions fixtures; live provider behavior may differ.",
	}
}
