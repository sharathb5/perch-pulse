package export

import (
	"fmt"
	"strings"
)

// SourceInventoryEntry documents one persisted Pulse source family.
type SourceInventoryEntry struct {
	Name                   string
	PathConvention         string
	SourceSchema           string
	RecordIdentity         string
	Timestamps             []string
	PrivacyClassification  PrivacyClass
	Persistence            string
	MissingOrOptional      []string
	AnalyticalDatasets     []string
	AvailableInRepoSources bool
}

// Inventory returns the F3a data inventory from inspected FileStores and eval schemas.
// Entries with AvailableInRepoSources=false are documented as unsupported for export.
func Inventory() []SourceInventoryEntry {
	return []SourceInventoryEntry{
		{
			Name:                   "incidents",
			PathConvention:         "<pulse-dir>/incidents/<sanitized-id>.json",
			SourceSchema:           "pulse.incident.v1",
			RecordIdentity:         "incident_id",
			Timestamps:             []string{"first_detected_at", "last_observed_at", "recovered_at?"},
			PrivacyClassification:  PrivacyOperational,
			Persistence:            "FileStore upsert; one JSON file per ID; local immutable for export readers",
			MissingOrOptional:      []string{"environment", "signal_types", "affected_service_ids", "recovered_at", "evidence[]"},
			AnalyticalDatasets:     []string{DatasetIncidents, DatasetIncidentSignals},
			AvailableInRepoSources: true,
		},
		{
			Name:                   "investigations",
			PathConvention:         "<pulse-dir>/investigations/<id>.json",
			SourceSchema:           "pulse.investigation.v1",
			RecordIdentity:         "investigation_id",
			Timestamps:             []string{"generated_at", "evidence_collected_at"},
			PrivacyClassification:  PrivacyOperational,
			Persistence:            "immutable Put; soft retention MaxRetentionInvestigations=200",
			MissingOrOptional:      []string{"environment", "linked_run_pulse", "change score_breakdown"},
			AnalyticalDatasets:     []string{DatasetInvestigations, DatasetHypotheses, DatasetCitations},
			AvailableInRepoSources: true,
		},
		{
			Name:                   "investigation_feedback",
			PathConvention:         "<pulse-dir>/investigations/feedback/<id>.json",
			SourceSchema:           "pulse.investigation.feedback.v1",
			RecordIdentity:         "feedback_id",
			Timestamps:             []string{"timestamp"},
			PrivacyClassification:  PrivacySubjective,
			Persistence:            "append-only feedback files linked to investigation_id",
			MissingOrOptional:      []string{"rationale", "useful/rejected hypothesis ids", "reviewer_confirmed_cause", "confirmation_method"},
			AnalyticalDatasets:     []string{DatasetInvestigationFeedback},
			AvailableInRepoSources: true,
		},
		{
			Name:                   "llm_proposals",
			PathConvention:         "<pulse-dir>/investigation-proposals/<id>.json",
			SourceSchema:           "pulse.investigation.proposal.v1",
			RecordIdentity:         "proposal_id",
			Timestamps:             []string{"generated_at"},
			PrivacyClassification:  PrivacyOperational,
			Persistence:            "immutable Put; soft retention MaxProposalRetention=100; separate from F1",
			MissingOrOptional:      []string{"rejected_hypotheses", "validation_notes", "usage units", "request_id"},
			AnalyticalDatasets:     []string{DatasetProposals, DatasetHypotheses},
			AvailableInRepoSources: true,
		},
		{
			Name:                   "proposal_feedback",
			PathConvention:         "<pulse-dir>/investigation-proposals/feedback/<id>.json",
			SourceSchema:           "pulse.investigation.proposal.feedback.v1",
			RecordIdentity:         "feedback_id",
			Timestamps:             []string{"timestamp"},
			PrivacyClassification:  PrivacySubjective,
			Persistence:            "append-only; never mixed with F1 feedback store",
			MissingOrOptional:      []string{"rationale", "hypothesis id lists", "limitations"},
			AnalyticalDatasets:     []string{DatasetInvestigationFeedback},
			AvailableInRepoSources: true,
		},
		{
			Name:                   "eval_cases_builtin",
			PathConvention:         "in-memory BuiltinDataset() (not FileStore)",
			SourceSchema:           "pulse.investigation.eval_case.v1",
			RecordIdentity:         "case_id",
			Timestamps:             []string{"dataset.created_at; investigation.generated_at"},
			PrivacyClassification:  PrivacySynthetic,
			Persistence:            "code fixtures; synthetic_fixture proposals optional",
			MissingOrOptional:      []string{"hidden_ground_truth", "stored_proposals", "human_rubric_hints"},
			AnalyticalDatasets:     []string{}, // cases themselves are not exported as warehouse rows in F3a
			AvailableInRepoSources: true,
		},
		{
			Name:                   "eval_reports",
			PathConvention:         "CLI --eval-report path (report.json from investigation-eval)",
			SourceSchema:           "pulse.investigation.eval_report.v1",
			RecordIdentity:         "eval_run_id = sha256(dataset_id|generated_at|mode)",
			Timestamps:             []string{"generated_at"},
			PrivacyClassification:  PrivacySynthetic,
			Persistence:            "optional offline artifact; not auto-written to pulse-dir",
			MissingOrOptional:      []string{"feedback_summary", "notes", "aggregate honesty_labels"},
			AnalyticalDatasets:     []string{DatasetEvalRuns, DatasetEvalCaseResults},
			AvailableInRepoSources: true,
		},
		{
			Name:                   "run_pulse",
			PathConvention:         "<pulse-dir>/runs/ (RunPulse store)",
			SourceSchema:           "runpulse types (linked via investigation.linked_run_pulse)",
			RecordIdentity:         "run_id",
			Timestamps:             []string{"run lifecycle timestamps when present"},
			PrivacyClassification:  PrivacyOperational,
			Persistence:            "retention-bounded; investigations only embed LinkedRunPulse refs",
			MissingOrOptional:      []string{"full run payloads not projected in F3a"},
			AnalyticalDatasets:     []string{}, // only linked_run_pulse_count on investigations
			AvailableInRepoSources: false,
		},
		{
			Name:                   "raw_observations_logs_traces",
			PathConvention:         "detector/telem stores (outside investigation export allowlist)",
			SourceSchema:           "varies",
			RecordIdentity:         "n/a",
			Timestamps:             []string{"sample timestamps"},
			PrivacyClassification:  PrivacyRedactedOmit,
			Persistence:            "not exported",
			MissingOrOptional:      []string{"full sample blobs", "provider prompts/responses", "auth headers"},
			AnalyticalDatasets:     []string{},
			AvailableInRepoSources: false,
		},
	}
}

// InventoryMarkdown renders the inventory for docs and CLI.
func InventoryMarkdown() string {
	var b strings.Builder
	b.WriteString("# Pulse investigation data inventory (F3a)\n\n")
	b.WriteString("Generated from inspected FileStores and eval schemas. ")
	b.WriteString("Unavailable families are listed but not fabricated as export rows.\n\n")
	for _, e := range Inventory() {
		avail := "export-supported"
		if !e.AvailableInRepoSources {
			avail = "documented-only / not exported"
		}
		_, _ = fmt.Fprintf(&b, "## %s (%s)\n\n", e.Name, avail)
		_, _ = fmt.Fprintf(&b, "- **Path:** `%s`\n", e.PathConvention)
		_, _ = fmt.Fprintf(&b, "- **Source schema:** `%s`\n", e.SourceSchema)
		_, _ = fmt.Fprintf(&b, "- **Identity:** `%s`\n", e.RecordIdentity)
		_, _ = fmt.Fprintf(&b, "- **Timestamps:** %s\n", strings.Join(e.Timestamps, ", "))
		_, _ = fmt.Fprintf(&b, "- **Privacy:** `%s`\n", e.PrivacyClassification)
		_, _ = fmt.Fprintf(&b, "- **Persistence:** %s\n", e.Persistence)
		if len(e.MissingOrOptional) > 0 {
			_, _ = fmt.Fprintf(&b, "- **Missing/optional:** %s\n", strings.Join(e.MissingOrOptional, "; "))
		}
		if len(e.AnalyticalDatasets) > 0 {
			_, _ = fmt.Fprintf(&b, "- **Analytical datasets:** %s\n", strings.Join(e.AnalyticalDatasets, ", "))
		} else {
			b.WriteString("- **Analytical datasets:** _(none in F3a)_\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// DatasetSchemas returns documentation for each analytical JSONL dataset.
func DatasetSchemas() []DatasetFileSchema {
	return []DatasetFileSchema{
		{
			Name: DatasetIncidents, RowType: "IncidentRow", PrimaryKey: "dedupe_key=incident_id",
			JoinKeys: []string{"incident_id"}, SourceSchemas: []string{"pulse.incident.v1"},
			NullableNotes: []string{"environment", "recovered_at", "signal_types", "affected_service_ids"},
			Units:         map[string]string{"during_digest_count": "digest rows"},
			Privacy:       PrivacyOperational,
			SeparatesFrom: []string{"hypotheses", "feedback", "eval labels"},
		},
		{
			Name: DatasetIncidentSignals, RowType: "IncidentSignalRow", PrimaryKey: "dedupe_key=incident_id|phase|signal",
			JoinKeys: []string{"incident_id"}, SourceSchemas: []string{"pulse.incident.v1"},
			NullableNotes: []string{"environment"},
			Units:         map[string]string{"sample_count": "samples", "median/min/max": "signal-native"},
			Privacy:       PrivacyOperational,
			SeparatesFrom: []string{"inferred hypotheses"},
		},
		{
			Name: DatasetInvestigations, RowType: "InvestigationRow", PrimaryKey: "dedupe_key=investigation_id",
			JoinKeys:      []string{"investigation_id", "incident_id", "source_content_fingerprint"},
			SourceSchemas: []string{"pulse.investigation.v1"},
			NullableNotes: []string{"environment"},
			Privacy:       PrivacyOperational,
			SeparatesFrom: []string{"proposal text", "human feedback"},
		},
		{
			Name: DatasetHypotheses, RowType: "HypothesisRow", PrimaryKey: "dedupe_key=parent_kind|parent_id|hypothesis_id|rejected",
			JoinKeys:      []string{"investigation_id", "proposal_id", "incident_id"},
			SourceSchemas: []string{"pulse.investigation.v1", "pulse.investigation.proposal.v1"},
			NullableNotes: []string{"kind", "support_status", "proposal_id", "counter_evidence_refs"},
			Units:         map[string]string{"statement_rune_count": "UTF-8 runes"},
			Privacy:       PrivacyOperational,
			SeparatesFrom: []string{"observed incident_signals", "evaluation annotations"},
		},
		{
			Name: DatasetCitations, RowType: "CitationRow", PrimaryKey: "dedupe_key=investigation_id|ref",
			JoinKeys:      []string{"investigation_id", "incident_id", "ref"},
			SourceSchemas: []string{"pulse.investigation.v1"},
			NullableNotes: []string{},
			Privacy:       PrivacyOperational,
			SeparatesFrom: []string{"hypothesis statements"},
		},
		{
			Name: DatasetInvestigationFeedback, RowType: "FeedbackRow", PrimaryKey: "dedupe_key=target_kind|feedback_id",
			JoinKeys:      []string{"investigation_id", "proposal_id", "incident_id"},
			SourceSchemas: []string{"pulse.investigation.feedback.v1", "pulse.investigation.proposal.feedback.v1"},
			NullableNotes: []string{"rationale fields as presence flags only", "confirmation_method"},
			Privacy:       PrivacySubjective,
			SeparatesFrom: []string{"mechanical eval_case_results", "model hypotheses"},
		},
		{
			Name: DatasetProposals, RowType: "ProposalRow", PrimaryKey: "dedupe_key=proposal_id",
			JoinKeys:      []string{"proposal_id", "source_investigation_id", "source_fingerprint"},
			SourceSchemas: []string{"pulse.investigation.proposal.v1"},
			NullableNotes: []string{"provider metadata optional"},
			Privacy:       PrivacyOperational,
			SeparatesFrom: []string{"raw prompts/responses (never exported)"},
		},
		{
			Name: DatasetEvalRuns, RowType: "EvalRunRow", PrimaryKey: "dedupe_key=eval_run_id",
			JoinKeys:      []string{"eval_run_id", "dataset_id"},
			SourceSchemas: []string{"pulse.investigation.eval_report.v1"},
			NullableNotes: []string{},
			Privacy:       PrivacySynthetic,
			SeparatesFrom: []string{"live observed_local incidents"},
		},
		{
			Name: DatasetEvalCaseResults, RowType: "EvalCaseResultRow", PrimaryKey: "dedupe_key=eval_run_id|case_id",
			JoinKeys:      []string{"eval_run_id", "case_id", "investigation_id", "incident_id"},
			SourceSchemas: []string{"pulse.investigation.eval_report.v1"},
			NullableNotes: []string{"llm_proposal_origin when unavailable", "feedback_count"},
			Privacy:       PrivacySynthetic,
			SeparatesFrom: []string{"hidden_ground_truth text (never exported)"},
		},
	}
}
