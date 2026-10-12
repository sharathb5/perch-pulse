package export

import "time"

const (
	// ExportSchemaVersion versions the analytical row envelope and dataset set.
	ExportSchemaVersion = "pulse.export.analytics.v1"
	// CheckpointSchemaVersion versions the incremental checkpoint file.
	CheckpointSchemaVersion = "pulse.export.checkpoint.v1"
	// ManifestSchemaVersion versions export-manifest.json.
	ManifestSchemaVersion = "pulse.export.manifest.v1"
	// QualityReportSchemaVersion versions the offline quality report.
	QualityReportSchemaVersion = "pulse.export.quality_report.v1"

	// DefaultMaxBatchRecords bounds rows written per dataset per run.
	DefaultMaxBatchRecords = 500
	// DefaultMaxOutputBytes soft-bounds total new bytes written in one run.
	DefaultMaxOutputBytes = 32 * 1024 * 1024
)

// Dataset names for JSONL files (<name>.jsonl).
const (
	DatasetIncidents             = "incidents"
	DatasetIncidentSignals       = "incident_signals"
	DatasetInvestigations        = "investigations"
	DatasetHypotheses            = "hypotheses"
	DatasetCitations             = "citations"
	DatasetInvestigationFeedback = "investigation_feedback"
	DatasetProposals             = "proposals"
	DatasetEvalRuns              = "eval_runs"
	DatasetEvalCaseResults       = "eval_case_results"
)

// SupportedDatasets is the ordered list of analytical datasets this exporter emits.
var SupportedDatasets = []string{
	DatasetIncidents,
	DatasetIncidentSignals,
	DatasetInvestigations,
	DatasetHypotheses,
	DatasetCitations,
	DatasetInvestigationFeedback,
	DatasetProposals,
	DatasetEvalRuns,
	DatasetEvalCaseResults,
}

// ProvenanceClass separates synthetic, observed, human, and model material.
type ProvenanceClass string

const (
	ProvenanceSyntheticFixture ProvenanceClass = "synthetic_fixture"
	ProvenanceHistoricalDemo   ProvenanceClass = "historical_demo"
	ProvenanceObservedLocal    ProvenanceClass = "observed_local"
	ProvenanceHumanAssessment  ProvenanceClass = "human_assessment"
	ProvenanceEvalAnnotation   ProvenanceClass = "evaluation_annotation"
	ProvenanceModelGenerated   ProvenanceClass = "model_generated"
	ProvenanceUnknown          ProvenanceClass = "unknown"
)

// PrivacyClass documents field sensitivity for allowlisted exports.
type PrivacyClass string

const (
	PrivacyPublicMetadata PrivacyClass = "public_metadata"
	PrivacyOperational    PrivacyClass = "operational_telemetry_summary"
	PrivacySubjective     PrivacyClass = "subjective_human"
	PrivacySynthetic      PrivacyClass = "synthetic_only"
	PrivacyRedactedOmit   PrivacyClass = "redacted_omitted"
)

// Envelope is shared by every analytical JSONL row.
type Envelope struct {
	ExportSchemaVersion string          `json:"export_schema_version"`
	Dataset             string          `json:"dataset"`
	DedupeKey           string          `json:"dedupe_key"`
	ContentFingerprint  string          `json:"content_fingerprint"`
	SourceSchemaVersion string          `json:"source_schema_version"`
	ProvenanceClass     ProvenanceClass `json:"provenance_class"`
	ExportedAt          time.Time       `json:"exported_at"`              // UTC
	ObservationAt       *time.Time      `json:"observation_at,omitempty"` // UTC event/observation time when known
}

// IncidentRow is allowlisted incident metadata (no raw logs/traces).
type IncidentRow struct {
	Envelope
	IncidentID         string     `json:"incident_id"`
	PrimaryServiceID   string     `json:"primary_service_id"`
	Environment        string     `json:"environment,omitempty"`
	FindingIDs         []string   `json:"finding_ids"`
	SignalTypes        []string   `json:"signal_types,omitempty"`
	FirstDetectedAt    time.Time  `json:"first_detected_at"`
	LastObservedAt     time.Time  `json:"last_observed_at"`
	RecoveredAt        *time.Time `json:"recovered_at,omitempty"`
	AffectedServiceIDs []string   `json:"affected_service_ids,omitempty"`
	ObservationCount   int        `json:"observation_count"`
	InferenceCount     int        `json:"inference_count"`
	LimitationCount    int        `json:"limitation_count"`
	EvidenceRefCount   int        `json:"evidence_ref_count"`
	SummaryPresent     bool       `json:"summary_present"`
	// DuringDigestCount is observed telemetry digest count in the during window (unit: digests).
	DuringDigestCount int `json:"during_digest_count"`
}

// IncidentSignalRow flattens during-phase sample digests (observed).
type IncidentSignalRow struct {
	Envelope
	IncidentID       string    `json:"incident_id"`
	PrimaryServiceID string    `json:"primary_service_id"`
	Environment      string    `json:"environment,omitempty"`
	Phase            string    `json:"phase"` // before|during|after
	Signal           string    `json:"signal"`
	SampleCount      int       `json:"sample_count"` // unit: samples summarized
	Median           float64   `json:"median"`       // unit: signal-native (rate or ms)
	Min              float64   `json:"min"`
	Max              float64   `json:"max"`
	FirstAt          time.Time `json:"first_at"`
	LastAt           time.Time `json:"last_at"`
}

// InvestigationRow is allowlisted F1 investigation metadata.
type InvestigationRow struct {
	Envelope
	InvestigationID       string    `json:"investigation_id"`
	IncidentID            string    `json:"incident_id"`
	Environment           string    `json:"environment,omitempty"`
	AffectedServiceIDs    []string  `json:"affected_service_ids"`
	IncidentState         string    `json:"incident_state"`
	GeneratedAt           time.Time `json:"generated_at"`
	EvidenceCollectedAt   time.Time `json:"evidence_collected_at"`
	EvidenceFreshness     string    `json:"evidence_freshness"`
	GenerationMethod      string    `json:"generation_method"`
	SourceFingerprint     string    `json:"source_content_fingerprint"`
	SymptomCount          int       `json:"symptom_count"`
	HypothesisCount       int       `json:"hypothesis_count"`
	CitationRefCount      int       `json:"source_reference_count"`
	ChangeCandidateCount  int       `json:"change_candidate_count"`
	StrongChangeCandidate bool      `json:"strong_change_candidate"`
	LinkedRunCount        int       `json:"linked_run_pulse_count"`
	Diagnosed             bool      `json:"diagnosed"` // always false for valid F1
	MissingEvidenceCount  int       `json:"missing_evidence_count"`
	LimitationCount       int       `json:"limitation_count"`
}

// HypothesisRow separates inferred hypotheses from observed evidence.
type HypothesisRow struct {
	Envelope
	HypothesisID                 string   `json:"hypothesis_id"`
	ParentKind                   string   `json:"parent_kind"` // investigation | proposal
	ParentID                     string   `json:"parent_id"`
	IncidentID                   string   `json:"incident_id,omitempty"`
	InvestigationID              string   `json:"investigation_id,omitempty"`
	ProposalID                   string   `json:"proposal_id,omitempty"`
	GenerationMethod             string   `json:"generation_method"`
	Classification               string   `json:"classification"` // INFERRED
	Kind                         string   `json:"kind,omitempty"`
	StatementHash                string   `json:"statement_hash"` // sha256 hex; statement text not exported
	StatementRuneCount           int      `json:"statement_rune_count"`
	SupportingEvidenceRefs       []string `json:"supporting_evidence_refs"`
	CounterEvidenceRefs          []string `json:"counter_evidence_refs,omitempty"`
	MissingEvidenceCount         int      `json:"missing_evidence_count"`
	SuggestedVerificationPresent bool     `json:"suggested_verification_present"`
	SupportStatus                string   `json:"support_status,omitempty"` // proposal only
	Rejected                     bool     `json:"rejected"`
}

// CitationRow is an observed/inferred evidence pointer (ref + classification only).
type CitationRow struct {
	Envelope
	InvestigationID string `json:"investigation_id"`
	IncidentID      string `json:"incident_id"`
	Ref             string `json:"ref"`
	Classification  string `json:"classification"`
	SummaryPresent  bool   `json:"summary_present"`
}

// FeedbackRow covers investigation and proposal human assessments.
type FeedbackRow struct {
	Envelope
	FeedbackID            string    `json:"feedback_id"`
	TargetKind            string    `json:"target_kind"` // investigation | proposal
	TargetID              string    `json:"target_id"`
	InvestigationID       string    `json:"investigation_id,omitempty"`
	IncidentID            string    `json:"incident_id,omitempty"`
	ProposalID            string    `json:"proposal_id,omitempty"`
	Timestamp             time.Time `json:"timestamp"`
	Assessment            string    `json:"assessment"`
	UsefulHypothesisIDs   []string  `json:"useful_hypothesis_ids,omitempty"`
	RejectedHypothesisIDs []string  `json:"rejected_hypothesis_ids,omitempty"`
	RationalePresent      bool      `json:"rationale_present"`
	RationaleRuneCount    int       `json:"rationale_rune_count,omitempty"`
	ReviewerCausePresent  bool      `json:"reviewer_confirmed_cause_present"`
	ConfirmationMethod    string    `json:"confirmation_method,omitempty"`
	LimitationCount       int       `json:"limitation_count"`
}

// ProposalRow is allowlisted LLM proposal metadata (no prompts/responses).
type ProposalRow struct {
	Envelope
	ProposalID              string    `json:"proposal_id"`
	SourceInvestigationID   string    `json:"source_investigation_id"`
	SourceIncidentID        string    `json:"source_incident_id"`
	SourceFingerprint       string    `json:"source_fingerprint"`
	GenerationMethod        string    `json:"generation_method"`
	GeneratedAt             time.Time `json:"generated_at"`
	Diagnosed               bool      `json:"diagnosed"`
	HypothesisCount         int       `json:"hypothesis_count"`
	RejectedHypothesisCount int       `json:"rejected_hypothesis_count"`
	MissingEvidenceCount    int       `json:"missing_evidence_count"`
	UnknownCount            int       `json:"unknown_count"`
	LimitationCount         int       `json:"limitation_count"`
	Provider                string    `json:"provider,omitempty"`
	Model                   string    `json:"model,omitempty"`
	EndpointHost            string    `json:"endpoint_host,omitempty"`
	FinishReason            string    `json:"finish_reason,omitempty"`
	PromptUsageUnits        int       `json:"prompt_usage_units,omitempty"`
	CompleteUsageUnits      int       `json:"complete_usage_units,omitempty"`
	RequestID               string    `json:"request_id,omitempty"`
}

// EvalRunRow summarizes one offline evaluation report.
type EvalRunRow struct {
	Envelope
	EvalRunID              string    `json:"eval_run_id"`
	DatasetID              string    `json:"dataset_id"`
	GeneratedAt            time.Time `json:"generated_at"`
	Mode                   string    `json:"mode"`
	CaseCount              int       `json:"case_count"`
	DeterministicPassCount int       `json:"deterministic_pass_count"`
	DeterministicDenom     int       `json:"deterministic_denominator"`
	LLMAvailableCount      int       `json:"llm_available_count"`
	LLMPassCount           int       `json:"llm_pass_count"`
	LLMPassDenom           int       `json:"llm_pass_denominator"`
	SyntheticProposalCount int       `json:"synthetic_proposal_count"`
	RecordedProposalCount  int       `json:"recorded_offline_proposal_count"`
	MissingLLMCount        int       `json:"missing_llm_proposal_count"`
	HonestyLabelCount      int       `json:"honesty_label_count"`
}

// EvalCaseResultRow is one mechanical case comparison (no hidden ground truth text).
type EvalCaseResultRow struct {
	Envelope
	EvalRunID                   string   `json:"eval_run_id"`
	CaseID                      string   `json:"case_id"`
	Title                       string   `json:"title"`
	Tags                        []string `json:"tags,omitempty"`
	IncidentID                  string   `json:"incident_id"`
	InvestigationID             string   `json:"investigation_id"`
	EvidenceFingerprint         string   `json:"evidence_fingerprint"`
	DeterministicPass           bool     `json:"deterministic_pass_mechanical"`
	DeterministicFalseCausal    int      `json:"deterministic_false_causal_count"`
	DeterministicUnresolvedCite int      `json:"deterministic_unresolved_citation_count"`
	LLMAvailable                bool     `json:"llm_available"`
	LLMProposalOrigin           string   `json:"llm_proposal_origin,omitempty"`
	LLMPass                     bool     `json:"llm_pass_mechanical"`
	LLMFalseCausal              int      `json:"llm_false_causal_count"`
	LLMUnresolvedCite           int      `json:"llm_unresolved_citation_count"`
	FeedbackCount               int      `json:"feedback_count,omitempty"`
}

// DatasetFileSchema documents one JSONL dataset for inventory/docs.
type DatasetFileSchema struct {
	Name          string
	RowType       string
	PrimaryKey    string
	JoinKeys      []string
	SourceSchemas []string
	NullableNotes []string
	Units         map[string]string
	Privacy       PrivacyClass
	SeparatesFrom []string
}
