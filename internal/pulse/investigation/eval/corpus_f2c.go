package eval

import (
	"fmt"
	"time"
)

const (
	// F2cCorpusDatasetID identifies the fixed Milestone F2c evaluation corpus.
	F2cCorpusDatasetID = "pulse.investigation.eval.f2c_provider_corpus.v1"
)

// F2cCaseMeta documents corpus provenance without exposing hidden GT to models.
type F2cCaseMeta struct {
	CaseID               string   `json:"case_id"`
	EvidenceFingerprint  string   `json:"evidence_fingerprint"`
	ProvenanceClass      string   `json:"provenance_class"` // synthetic_evaluation_fixture | observed_local_demo
	Tags                 []string `json:"tags,omitempty"`
	HasSyntheticProposal bool     `json:"has_synthetic_proposal"`
	Notes                string   `json:"notes,omitempty"`
}

// F2cCorpus returns a fixed 9-case subset of BuiltinDataset covering the F2c
// representative incident classes. All cases are synthetic evaluation fixtures
// derived from Astronomy Shop topology — not production evidence.
//
// Hidden evaluator annotations and scenario GT remain on the Case but are never
// passed to generators (see GenerationOnly / AssertNoAnnotationLeakage).
func F2cCorpus() (Dataset, error) {
	base, err := BuiltinDataset()
	if err != nil {
		return Dataset{}, err
	}
	want := []string{
		"case_payment_error",       // payment error-rate regression
		"case_shipping_latency",    // latency regression
		"case_call_rate_decline",   // call-rate decline
		"case_recovered",           // recovered incident
		"case_weak_correlation",    // weak/absent change correlation
		"case_stale_telemetry",     // stale/incomplete evidence
		"case_multiple_changes",    // multiple plausible hypotheses
		"case_misleading_metadata", // unsupported/misleading change description
		"case_insufficient_evidence",
	}
	byID := map[string]Case{}
	for _, c := range base.Cases {
		byID[c.CaseID] = c
	}
	var cases []Case
	for _, id := range want {
		c, ok := byID[id]
		if !ok {
			return Dataset{}, fmt.Errorf("eval: F2c corpus missing builtin case %q", id)
		}
		cases = append(cases, c)
	}
	ds := Dataset{
		SchemaVersion: DatasetSchemaVersion,
		DatasetID:     F2cCorpusDatasetID,
		CreatedAt:     time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC),
		Description: "F2c fixed evaluation corpus (synthetic Astronomy Shop fixtures). " +
			"Not production evidence. Hidden GT never enters generation inputs.",
		Cases: cases,
	}
	if err := ds.Validate(); err != nil {
		return Dataset{}, err
	}
	return ds, nil
}

// F2cCorpusMeta returns redacted case metadata for documentation/reports.
func F2cCorpusMeta(ds Dataset) []F2cCaseMeta {
	out := make([]F2cCaseMeta, 0, len(ds.Cases))
	for _, c := range ds.Cases {
		meta := F2cCaseMeta{
			CaseID:               c.CaseID,
			EvidenceFingerprint:  c.Generation.Investigation.ContentFingerprint,
			ProvenanceClass:      "synthetic_evaluation_fixture",
			Tags:                 append([]string{}, c.Tags...),
			HasSyntheticProposal: len(c.Generation.StoredProposals) > 0,
			Notes:                "synthetic fixture — not observed production telemetry",
		}
		out = append(out, meta)
	}
	return out
}

// DatasetByID returns BuiltinDataset or F2cCorpus.
func DatasetByID(id string) (Dataset, error) {
	switch id {
	case "", BuiltinDatasetID, "builtin", "f2b":
		return BuiltinDataset()
	case F2cCorpusDatasetID, "f2c":
		return F2cCorpus()
	default:
		return Dataset{}, fmt.Errorf("eval: unknown dataset_id %q (use %s or %s)", id, BuiltinDatasetID, F2cCorpusDatasetID)
	}
}
