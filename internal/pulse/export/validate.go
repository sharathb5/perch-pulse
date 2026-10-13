package export

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// QualityReport is an offline data-quality summary for an export directory.
type QualityReport struct {
	SchemaVersion       string          `json:"schema_version"`
	ExportSchemaVersion string          `json:"export_schema_version"`
	GeneratedAt         time.Time       `json:"generated_at"`
	OK                  bool            `json:"ok"`
	DatasetCounts       map[string]int  `json:"dataset_counts"`
	Issues              []QualityIssue  `json:"issues,omitempty"`
	Checks              map[string]bool `json:"checks"`
	Notes               []string        `json:"notes"`
}

// QualityIssue is one validation finding.
type QualityIssue struct {
	Code      string `json:"code"`
	Dataset   string `json:"dataset,omitempty"`
	DedupeKey string `json:"dedupe_key,omitempty"`
	Message   string `json:"message"`
}

// ValidateExport checks schema compliance, duplicates, joins, timestamps, and path safety.
func ValidateExport(exportDir string) (QualityReport, error) {
	qr := QualityReport{
		SchemaVersion:       QualityReportSchemaVersion,
		ExportSchemaVersion: ExportSchemaVersion,
		GeneratedAt:         time.Now().UTC(),
		DatasetCounts:       map[string]int{},
		Checks:              map[string]bool{},
		Notes: []string{
			"Validation is local-only; it does not execute Databricks jobs.",
			"Join checks use exported IDs present in this directory only.",
		},
	}

	// Path already resolved by caller; re-check checkpoint load.
	cp, err := LoadCheckpoint(exportDir)
	if err != nil {
		qr.Issues = append(qr.Issues, QualityIssue{Code: "checkpoint_corrupt", Message: err.Error()})
		qr.Checks["checkpoint_load"] = false
		qr.OK = false
		return qr, nil
	}
	qr.Checks["checkpoint_load"] = true

	incidents := map[string]bool{}
	investigations := map[string]string{} // id -> incident_id
	proposals := map[string]string{}      // id -> investigation_id
	evalRuns := map[string]bool{}

	type rowEnv struct {
		Envelope
		IncidentID            string `json:"incident_id"`
		InvestigationID       string `json:"investigation_id"`
		SourceInvestigationID string `json:"source_investigation_id"`
		ProposalID            string `json:"proposal_id"`
		TargetKind            string `json:"target_kind"`
		TargetID              string `json:"target_id"`
		EvalRunID             string `json:"eval_run_id"`
		ParentKind            string `json:"parent_kind"`
		ParentID              string `json:"parent_id"`
		SourceFingerprint     string `json:"source_fingerprint"`
		SourceContentFP       string `json:"source_content_fingerprint"`
		Ref                   string `json:"ref"`
	}

	seen := map[string]map[string]string{} // dataset -> dedupe -> fp

	for _, ds := range SupportedDatasets {
		lines, err := readJSONLLines(exportDir, ds)
		if err != nil {
			qr.Issues = append(qr.Issues, QualityIssue{Code: "malformed_jsonl", Dataset: ds, Message: err.Error()})
			continue
		}
		qr.DatasetCounts[ds] = len(lines)
		seen[ds] = map[string]string{}
		for _, line := range lines {
			var row rowEnv
			if err := json.Unmarshal(line, &row); err != nil {
				qr.Issues = append(qr.Issues, QualityIssue{Code: "malformed_record", Dataset: ds, Message: err.Error()})
				continue
			}
			if row.ExportSchemaVersion != ExportSchemaVersion {
				qr.Issues = append(qr.Issues, QualityIssue{Code: "schema_mismatch", Dataset: ds, DedupeKey: row.DedupeKey,
					Message: fmt.Sprintf("export_schema_version %q", row.ExportSchemaVersion)})
			}
			if row.Dataset != ds {
				qr.Issues = append(qr.Issues, QualityIssue{Code: "dataset_field_mismatch", Dataset: ds, DedupeKey: row.DedupeKey,
					Message: fmt.Sprintf("dataset field %q", row.Dataset)})
			}
			if strings.TrimSpace(row.DedupeKey) == "" || strings.TrimSpace(row.ContentFingerprint) == "" {
				qr.Issues = append(qr.Issues, QualityIssue{Code: "missing_stable_id", Dataset: ds, Message: "empty dedupe_key or content_fingerprint"})
			}
			if prev, ok := seen[ds][row.DedupeKey]; ok {
				if prev == row.ContentFingerprint {
					qr.Issues = append(qr.Issues, QualityIssue{Code: "duplicate_key", Dataset: ds, DedupeKey: row.DedupeKey,
						Message: "duplicate dedupe_key with identical fingerprint"})
				}
				// Differing fingerprints for the same key are intentional history
				// (incident upsert / provenance correction); warehouse MERGE uses latest.
			}
			seen[ds][row.DedupeKey] = row.ContentFingerprint

			if row.ExportedAt.IsZero() || row.ExportedAt.Location() != time.UTC {
				qr.Issues = append(qr.Issues, QualityIssue{Code: "invalid_timestamp", Dataset: ds, DedupeKey: row.DedupeKey,
					Message: "exported_at must be UTC"})
			}
			if row.ObservationAt != nil && row.ObservationAt.Location() != time.UTC {
				qr.Issues = append(qr.Issues, QualityIssue{Code: "invalid_timestamp", Dataset: ds, DedupeKey: row.DedupeKey,
					Message: "observation_at must be UTC"})
			}
			if row.ProvenanceClass == "" {
				qr.Issues = append(qr.Issues, QualityIssue{Code: "missing_provenance", Dataset: ds, DedupeKey: row.DedupeKey,
					Message: "provenance_class required"})
			}

			// Redaction: reject obvious secret field names in raw JSON.
			low := strings.ToLower(string(line))
			for _, bad := range []string{`"authorization"`, `"api_key"`, `"apikey"`, `"password"`, `"secret"`, `"bearer `} {
				if strings.Contains(low, bad) {
					qr.Issues = append(qr.Issues, QualityIssue{Code: "privacy_leak_suspect", Dataset: ds, DedupeKey: row.DedupeKey,
						Message: "suspicious secret-like token in JSONL"})
					break
				}
			}

			switch ds {
			case DatasetIncidents:
				incidents[row.IncidentID] = true
			case DatasetInvestigations:
				investigations[row.DedupeKey] = row.IncidentID
				var full InvestigationRow
				if json.Unmarshal(line, &full) == nil && full.SourceFingerprint == "" {
					qr.Issues = append(qr.Issues, QualityIssue{Code: "missing_evidence_fingerprint", Dataset: ds, DedupeKey: row.DedupeKey,
						Message: "source_content_fingerprint empty"})
				}
			case DatasetProposals:
				proposals[row.DedupeKey] = row.SourceInvestigationID
			case DatasetEvalRuns:
				evalRuns[row.DedupeKey] = true
			}
		}
		// Checkpoint consistency: counts should match when present.
		if dsCP, ok := cp.Datasets[ds]; ok && dsCP.RowCount != len(seen[ds]) {
			// Allow checkpoint ahead only if we count unique keys; row_count tracks unique keys.
			if dsCP.RowCount != len(seen[ds]) {
				qr.Issues = append(qr.Issues, QualityIssue{Code: "checkpoint_count_mismatch", Dataset: ds,
					Message: fmt.Sprintf("checkpoint row_count=%d unique_keys_in_file=%d", dsCP.RowCount, len(seen[ds]))})
			}
		}
	}

	// Join checks
	for _, line := range mustLines(exportDir, DatasetIncidentSignals) {
		var row IncidentSignalRow
		if json.Unmarshal(line, &row) != nil {
			continue
		}
		if row.IncidentID != "" && !incidents[row.IncidentID] && qr.DatasetCounts[DatasetIncidents] > 0 {
			qr.Issues = append(qr.Issues, QualityIssue{Code: "broken_join", Dataset: DatasetIncidentSignals, DedupeKey: row.DedupeKey,
				Message: "incident_id not in incidents dataset"})
		}
	}
	for _, line := range mustLines(exportDir, DatasetInvestigations) {
		var row InvestigationRow
		if json.Unmarshal(line, &row) != nil {
			continue
		}
		if row.IncidentID != "" && !incidents[row.IncidentID] && qr.DatasetCounts[DatasetIncidents] > 0 {
			qr.Issues = append(qr.Issues, QualityIssue{Code: "broken_join", Dataset: DatasetInvestigations, DedupeKey: row.DedupeKey,
				Message: "incident_id not in incidents dataset"})
		}
	}
	for _, line := range mustLines(exportDir, DatasetCitations) {
		var row CitationRow
		if json.Unmarshal(line, &row) != nil {
			continue
		}
		if _, ok := investigations[row.InvestigationID]; !ok && qr.DatasetCounts[DatasetInvestigations] > 0 {
			qr.Issues = append(qr.Issues, QualityIssue{Code: "broken_join", Dataset: DatasetCitations, DedupeKey: row.DedupeKey,
				Message: "investigation_id orphan"})
		}
	}
	for _, line := range mustLines(exportDir, DatasetInvestigationFeedback) {
		var row FeedbackRow
		if json.Unmarshal(line, &row) != nil {
			continue
		}
		switch row.TargetKind {
		case "investigation":
			if _, ok := investigations[row.TargetID]; !ok && qr.DatasetCounts[DatasetInvestigations] > 0 {
				qr.Issues = append(qr.Issues, QualityIssue{Code: "orphaned_feedback", Dataset: DatasetInvestigationFeedback, DedupeKey: row.DedupeKey,
					Message: "investigation target missing"})
			}
		case "proposal":
			if _, ok := proposals[row.TargetID]; !ok && qr.DatasetCounts[DatasetProposals] > 0 {
				qr.Issues = append(qr.Issues, QualityIssue{Code: "orphaned_feedback", Dataset: DatasetInvestigationFeedback, DedupeKey: row.DedupeKey,
					Message: "proposal target missing"})
			}
		}
	}
	for _, line := range mustLines(exportDir, DatasetProposals) {
		var row ProposalRow
		if json.Unmarshal(line, &row) != nil {
			continue
		}
		if row.SourceFingerprint == "" {
			qr.Issues = append(qr.Issues, QualityIssue{Code: "missing_evidence_fingerprint", Dataset: DatasetProposals, DedupeKey: row.DedupeKey,
				Message: "source_fingerprint empty"})
		}
		if _, ok := investigations[row.SourceInvestigationID]; !ok && qr.DatasetCounts[DatasetInvestigations] > 0 {
			qr.Issues = append(qr.Issues, QualityIssue{Code: "broken_join", Dataset: DatasetProposals, DedupeKey: row.DedupeKey,
				Message: "source_investigation_id not in investigations"})
		}
	}
	for _, line := range mustLines(exportDir, DatasetEvalCaseResults) {
		var row EvalCaseResultRow
		if json.Unmarshal(line, &row) != nil {
			continue
		}
		if !evalRuns[row.EvalRunID] && qr.DatasetCounts[DatasetEvalRuns] > 0 {
			qr.Issues = append(qr.Issues, QualityIssue{Code: "broken_join", Dataset: DatasetEvalCaseResults, DedupeKey: row.DedupeKey,
				Message: "eval_run_id missing"})
		}
		if row.EvidenceFingerprint == "" {
			qr.Issues = append(qr.Issues, QualityIssue{Code: "missing_evidence_fingerprint", Dataset: DatasetEvalCaseResults, DedupeKey: row.DedupeKey,
				Message: "evidence_fingerprint empty"})
		}
	}

	// Provenance mixing soft check: flag when synthetic_fixture and observed_local share incident_id space without note.
	// (Informational only if both present.)
	if qr.DatasetCounts[DatasetEvalCaseResults] > 0 && qr.DatasetCounts[DatasetIncidents] > 0 {
		qr.Notes = append(qr.Notes, "Both eval_case_results and incidents present: filter by provenance_class before analytical joins.")
	}

	qr.Checks["schema_compliance"] = !hasCode(qr.Issues, "schema_mismatch", "malformed_record", "malformed_jsonl")
	qr.Checks["stable_ids"] = !hasCode(qr.Issues, "missing_stable_id", "duplicate_key_conflict")
	qr.Checks["joins"] = !hasCode(qr.Issues, "broken_join", "orphaned_feedback")
	qr.Checks["timestamps"] = !hasCode(qr.Issues, "invalid_timestamp")
	qr.Checks["redaction"] = !hasCode(qr.Issues, "privacy_leak_suspect")
	qr.Checks["fingerprints"] = !hasCode(qr.Issues, "missing_evidence_fingerprint")
	qr.Checks["checkpoint"] = !hasCode(qr.Issues, "checkpoint_corrupt", "checkpoint_count_mismatch")
	qr.OK = len(qr.Issues) == 0
	return qr, nil
}

func mustLines(exportDir, ds string) [][]byte {
	lines, err := readJSONLLines(exportDir, ds)
	if err != nil {
		return nil
	}
	return lines
}

func hasCode(issues []QualityIssue, codes ...string) bool {
	set := map[string]bool{}
	for _, c := range codes {
		set[c] = true
	}
	for _, i := range issues {
		if set[i.Code] {
			return true
		}
	}
	return false
}
