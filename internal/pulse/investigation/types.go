package investigation

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/correlate"
	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/incident"
)

const (
	// SchemaVersion is the persisted investigation schema.
	SchemaVersion = "pulse.investigation.v1"
	// FeedbackSchemaVersion is the persisted feedback schema.
	FeedbackSchemaVersion = "pulse.investigation.feedback.v1"
	// GenerationMethodDeterministic is the F1 rules engine.
	GenerationMethodDeterministic = "deterministic_rules_v1"
	// MaxInvestigationBytes bounds persisted JSON size.
	MaxInvestigationBytes = 256 * 1024
	// MaxFeedbackRationale runes.
	MaxFeedbackRationale = 2000
	// MaxHypotheses caps generated hypotheses.
	MaxHypotheses = 5
	// MaxTopologyNeighbors bounds direct dependency expansion.
	MaxTopologyNeighbors = 12
	// MaxChangeCandidates embedded in a snapshot.
	MaxChangeCandidates = 5
	// MaxRetentionInvestigations soft retention when listing (newest kept).
	MaxRetentionInvestigations = 200
)

// Classification separates evidence quality for agents and UI.
type Classification string

const (
	ClassObserved Classification = "OBSERVED"
	ClassInferred Classification = "INFERRED"
	ClassUnknown  Classification = "UNKNOWN"
)

// IncidentStateAtInvestigation mirrors lifecycle labels without claiming diagnosis.
const (
	IncidentStateActive     = "active"
	IncidentStateRecovered  = "recovered"
	IncidentStateHistorical = "historical"
)

// EvidenceFreshness labels whether the snapshot is historical-only or mixed.
const (
	FreshnessIncidentSnapshot = "incident_snapshot"
	FreshnessMixedCurrent     = "mixed_current_supplement"
)

// EvidenceRef is a non-secret pointer to supporting artifacts.
type EvidenceRef struct {
	Ref            string         `json:"ref"`
	Summary        string         `json:"summary"`
	Classification Classification `json:"classification"`
}

// Symptom is an observed behavioral signal during the incident window.
type Symptom struct {
	ID             string            `json:"id"`
	ServiceID      string            `json:"service_id"`
	SignalType     detect.SignalType `json:"signal_type"`
	Statement      string            `json:"statement"`
	Classification Classification    `json:"classification"`
	EvidenceRefs   []string          `json:"evidence_refs"`
	DuringMedian   *float64          `json:"during_median,omitempty"`
	BeforeMedian   *float64          `json:"before_median,omitempty"`
}

// BaselineComparison contrasts before vs during digests when both exist.
type BaselineComparison struct {
	ServiceID      string            `json:"service_id"`
	SignalType     detect.SignalType `json:"signal_type"`
	BeforeMedian   *float64          `json:"before_median,omitempty"`
	DuringMedian   *float64          `json:"during_median,omitempty"`
	BeforeCount    int               `json:"before_count"`
	DuringCount    int               `json:"during_count"`
	Direction      string            `json:"direction,omitempty"` // increase | decrease | flat | unknown
	Classification Classification    `json:"classification"`
	Limitation     string            `json:"limitation,omitempty"`
}

// TopologyNode is a bounded graph neighbor.
type TopologyNode struct {
	ServiceID      string         `json:"service_id"`
	GraphNode      string         `json:"graph_node,omitempty"`
	Relation       string         `json:"relation"` // primary | upstream | downstream | affected | unmapped
	Anomalous      bool           `json:"anomalous"`
	Classification Classification `json:"classification"`
	Note           string         `json:"note,omitempty"`
}

// TopologyContext bounds static topology expansion around the incident.
type TopologyContext struct {
	PrimaryServiceID string         `json:"primary_service_id"`
	Nodes            []TopologyNode `json:"nodes"`
	Depth            int            `json:"expansion_depth"`
	Limitations      []string       `json:"limitations,omitempty"`
}

// ChangeCandidateRef embeds ranked correlation without claiming causation.
type ChangeCandidateRef struct {
	ChangeID             string                    `json:"change_id"`
	Rank                 int                       `json:"rank"`
	Score                float64                   `json:"score"`
	ScorerVersion        string                    `json:"scorer_version"`
	ServiceOverlap       string                    `json:"service_overlap"`
	TemporalDistanceSecs float64                   `json:"temporal_distance_secs"`
	EnvironmentMatch     bool                      `json:"environment_match"`
	RankingBasis         string                    `json:"ranking_basis"`
	Classification       Classification            `json:"classification"`
	ScoreBreakdown       *correlate.ScoreBreakdown `json:"score_breakdown,omitempty"`
	Observations         []string                  `json:"observations,omitempty"`
	Inferences           []string                  `json:"inferences,omitempty"`
	Limitations          []string                  `json:"limitations,omitempty"`
}

// LinkedRunPulse is an explicitly linked diagnostic run (never auto-started).
type LinkedRunPulse struct {
	RunID          string         `json:"run_id"`
	WorkflowID     string         `json:"workflow_id,omitempty"`
	Status         string         `json:"status,omitempty"`
	Classification Classification `json:"classification"`
	Note           string         `json:"note"`
}

// Hypothesis is an INFERRED explanation with citations.
type Hypothesis struct {
	HypothesisID           string         `json:"hypothesis_id"`
	Statement              string         `json:"statement"`
	Classification         Classification `json:"classification"` // always INFERRED in F1
	SupportingEvidenceRefs []string       `json:"supporting_evidence_refs"`
	CounterEvidenceRefs    []string       `json:"counter_evidence_refs,omitempty"`
	MissingEvidence        []string       `json:"missing_evidence,omitempty"`
	SuggestedVerification  string         `json:"suggested_verification"`
	Kind                   string         `json:"kind"` // error_rate_regression | latency_regression | call_rate_drop | change_correlation | dependency | insufficient
}

// Investigation is a versioned, immutable evidence snapshot for one incident.
type Investigation struct {
	SchemaVersion         string                 `json:"schema_version"`
	InvestigationID       string                 `json:"investigation_id"`
	IncidentID            string                 `json:"incident_id"`
	Environment           string                 `json:"environment,omitempty"`
	AffectedServiceIDs    []string               `json:"affected_service_ids"`
	IncidentState         string                 `json:"incident_state"`
	GeneratedAt           time.Time              `json:"generated_at"`
	EvidenceCollectedAt   time.Time              `json:"evidence_collected_at"`
	EvidenceFreshness     string                 `json:"evidence_freshness"`
	GenerationMethod      string                 `json:"generation_method"`
	ContentFingerprint    string                 `json:"content_fingerprint"`
	ObservedSymptoms      []Symptom              `json:"observed_symptoms"`
	BaselineComparisons   []BaselineComparison   `json:"baseline_comparisons"`
	Before                incident.PhaseSnapshot `json:"before"`
	During                incident.PhaseSnapshot `json:"during"`
	After                 incident.PhaseSnapshot `json:"after"`
	Topology              TopologyContext        `json:"topology"`
	ChangeCandidates      []ChangeCandidateRef   `json:"change_candidates"`
	StrongChangeCandidate bool                   `json:"strong_change_candidate"`
	LinkedRunPulse        []LinkedRunPulse       `json:"linked_run_pulse,omitempty"`
	MissingEvidence       []string               `json:"missing_evidence"`
	Hypotheses            []Hypothesis           `json:"hypotheses"`
	SuggestedVerification []string               `json:"suggested_verification_steps"`
	Limitations           []string               `json:"limitations"`
	SourceReferences      []EvidenceRef          `json:"source_references"`
	Unknowns              []string               `json:"unknowns"`
	Diagnosed             bool                   `json:"diagnosed"` // always false in F1
}

// FeedbackAssessment values for human evaluation.
const (
	AssessmentUseful               = "useful"
	AssessmentPartiallyUseful      = "partially_useful"
	AssessmentNotUseful            = "not_useful"
	AssessmentInsufficientEvidence = "insufficient_evidence"
)

// Feedback is a separate record linked to an immutable investigation.
type Feedback struct {
	SchemaVersion          string    `json:"schema_version"`
	FeedbackID             string    `json:"feedback_id"`
	InvestigationID        string    `json:"investigation_id"`
	IncidentID             string    `json:"incident_id"`
	Timestamp              time.Time `json:"timestamp"`
	Assessment             string    `json:"assessment"`
	UsefulHypothesisIDs    []string  `json:"useful_hypothesis_ids,omitempty"`
	RejectedHypothesisIDs  []string  `json:"rejected_hypothesis_ids,omitempty"`
	Rationale              string    `json:"rationale,omitempty"`
	ReviewerConfirmedCause string    `json:"reviewer_confirmed_cause,omitempty"`
	ConfirmationMethod     string    `json:"confirmation_method,omitempty"`
	Limitations            []string  `json:"limitations,omitempty"`
}

var forbiddenFieldSubstrings = []string{
	"secret", "token", "password", "credential", "apikey", "api_key", "auth",
}

// ValidateReportsSecretFieldNames guards exported shapes.
func ValidateReportsSecretFieldNames() error {
	for _, t := range []reflect.Type{
		reflect.TypeOf(Investigation{}),
		reflect.TypeOf(Feedback{}),
		reflect.TypeOf(Hypothesis{}),
		reflect.TypeOf(Symptom{}),
	} {
		for i := 0; i < t.NumField(); i++ {
			name := strings.ToLower(t.Field(i).Name)
			for _, bad := range forbiddenFieldSubstrings {
				if strings.Contains(name, bad) {
					return fmt.Errorf("investigation: type %s field %s looks like a secret holder", t.Name(), t.Field(i).Name)
				}
			}
		}
	}
	return nil
}

// Validate checks investigation invariants.
func (inv Investigation) Validate() error {
	if inv.SchemaVersion != SchemaVersion {
		return fmt.Errorf("investigation: schema_version must be %q", SchemaVersion)
	}
	if strings.TrimSpace(inv.InvestigationID) == "" {
		return fmt.Errorf("investigation: investigation_id is required")
	}
	if strings.TrimSpace(inv.IncidentID) == "" {
		return fmt.Errorf("investigation: incident_id is required")
	}
	if inv.GeneratedAt.IsZero() || inv.EvidenceCollectedAt.IsZero() {
		return fmt.Errorf("investigation: timestamps required")
	}
	if inv.GeneratedAt.Location() != time.UTC || inv.EvidenceCollectedAt.Location() != time.UTC {
		return fmt.Errorf("investigation: timestamps must be UTC")
	}
	if inv.GenerationMethod != GenerationMethodDeterministic {
		return fmt.Errorf("investigation: unsupported generation_method %q", inv.GenerationMethod)
	}
	if inv.Diagnosed {
		return fmt.Errorf("investigation: diagnosed must be false (report ≠ confirmed root cause)")
	}
	for _, h := range inv.Hypotheses {
		if h.Classification != ClassInferred {
			return fmt.Errorf("investigation: hypothesis %q must be INFERRED", h.HypothesisID)
		}
		if strings.TrimSpace(h.HypothesisID) == "" || strings.TrimSpace(h.Statement) == "" {
			return fmt.Errorf("investigation: hypothesis requires id and statement")
		}
	}
	return nil
}

// ValidAssessment reports whether a is a supported feedback assessment.
func ValidAssessment(a string) bool {
	switch a {
	case AssessmentUseful, AssessmentPartiallyUseful, AssessmentNotUseful, AssessmentInsufficientEvidence:
		return true
	default:
		return false
	}
}
