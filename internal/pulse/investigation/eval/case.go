package eval

import (
	"fmt"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/investigation"
	"github.com/yashg4509/perch/internal/pulse/investigation/llm"
)

const (
	// CaseSchemaVersion is the versioned evaluation-case schema.
	CaseSchemaVersion = "pulse.investigation.eval_case.v1"
	// DatasetSchemaVersion versions a bundled case set.
	DatasetSchemaVersion = "pulse.investigation.eval_dataset.v1"
	// ReportSchemaVersion versions offline runner output.
	ReportSchemaVersion = "pulse.investigation.eval_report.v1"
)

// GenerationInput is the only material generators may see.
// It must not include Annotations or HiddenGroundTruth.
type GenerationInput struct {
	// Investigation is an immutable F1 snapshot shared by F1 scoring and LLM proposals.
	Investigation investigation.Investigation `json:"investigation"`
	// StoredProposals are optional pre-persisted F2a proposals for offline comparison.
	// Origin must be "synthetic_fixture" or "recorded_offline" — never invented at score time.
	StoredProposals []StoredProposal `json:"stored_proposals,omitempty"`
}

// StoredProposal wraps a proposal with provenance about how it was obtained.
type StoredProposal struct {
	Origin   string       `json:"origin"` // synthetic_fixture | recorded_offline
	Proposal llm.Proposal `json:"proposal"`
	Note     string       `json:"note,omitempty"`
}

// EvaluatorAnnotations are scoring expectations — never passed to generators.
type EvaluatorAnnotations struct {
	RequireKind                string   `json:"require_kind,omitempty"`
	ExpectInsufficientEvidence bool     `json:"expect_insufficient_evidence,omitempty"`
	ExpectMissingBaseline      bool     `json:"expect_missing_baseline,omitempty"`
	ExpectStaleTelemetryNote   bool     `json:"expect_stale_telemetry_note,omitempty"`
	ExpectWeakCorrelation      bool     `json:"expect_weak_correlation,omitempty"`
	ExpectNoChanges            bool     `json:"expect_no_changes,omitempty"`
	ExpectPromptInjectionSafe  bool     `json:"expect_prompt_injection_safe,omitempty"`
	HumanRubricHints           []string `json:"human_rubric_hints,omitempty"`
	Notes                      string   `json:"notes,omitempty"`
}

// HiddenGroundTruth holds scenario labels that must never enter generation prompts.
type HiddenGroundTruth struct {
	ScenarioID    string `json:"scenario_id,omitempty"`
	InjectedFault string `json:"injected_fault,omitempty"`
	TrueService   string `json:"true_service,omitempty"`
	LabelNote     string `json:"label_note,omitempty"`
}

// Case is one versioned evaluation case.
type Case struct {
	SchemaVersion     string               `json:"schema_version"`
	CaseID            string               `json:"case_id"`
	Title             string               `json:"title"`
	Tags              []string             `json:"tags,omitempty"`
	Generation        GenerationInput      `json:"generation"`
	Annotations       EvaluatorAnnotations `json:"annotations"`
	HiddenGroundTruth *HiddenGroundTruth   `json:"hidden_ground_truth,omitempty"`
}

// Dataset bundles cases.
type Dataset struct {
	SchemaVersion string    `json:"schema_version"`
	DatasetID     string    `json:"dataset_id"`
	CreatedAt     time.Time `json:"created_at"`
	Description   string    `json:"description,omitempty"`
	Cases         []Case    `json:"cases"`
}

// Validate checks case structural invariants and generation/annotation separation.
func (c Case) Validate() error {
	if c.SchemaVersion != CaseSchemaVersion {
		return fmt.Errorf("eval: case schema_version must be %q", CaseSchemaVersion)
	}
	if strings.TrimSpace(c.CaseID) == "" {
		return fmt.Errorf("eval: case_id required")
	}
	if err := c.Generation.Investigation.Validate(); err != nil {
		return fmt.Errorf("eval: case %s investigation: %w", c.CaseID, err)
	}
	if c.Generation.Investigation.Diagnosed {
		return fmt.Errorf("eval: case %s generation investigation must have diagnosed=false", c.CaseID)
	}
	for i, sp := range c.Generation.StoredProposals {
		switch sp.Origin {
		case "synthetic_fixture", "recorded_offline":
		default:
			return fmt.Errorf("eval: case %s stored_proposals[%d] origin %q invalid", c.CaseID, i, sp.Origin)
		}
		if sp.Proposal.SourceFingerprint != "" &&
			sp.Proposal.SourceFingerprint != c.Generation.Investigation.ContentFingerprint {
			return fmt.Errorf("eval: case %s stored proposal fingerprint mismatch", c.CaseID)
		}
		if sp.Proposal.SourceInvestigationID != "" &&
			sp.Proposal.SourceInvestigationID != c.Generation.Investigation.InvestigationID {
			return fmt.Errorf("eval: case %s stored proposal investigation id mismatch", c.CaseID)
		}
	}
	return nil
}

// Validate checks dataset invariants.
func (d Dataset) Validate() error {
	if d.SchemaVersion != DatasetSchemaVersion {
		return fmt.Errorf("eval: dataset schema_version must be %q", DatasetSchemaVersion)
	}
	if strings.TrimSpace(d.DatasetID) == "" {
		return fmt.Errorf("eval: dataset_id required")
	}
	if len(d.Cases) == 0 {
		return fmt.Errorf("eval: dataset has no cases")
	}
	seen := map[string]struct{}{}
	for _, c := range d.Cases {
		if err := c.Validate(); err != nil {
			return err
		}
		if _, ok := seen[c.CaseID]; ok {
			return fmt.Errorf("eval: duplicate case_id %q", c.CaseID)
		}
		seen[c.CaseID] = struct{}{}
	}
	return nil
}

// GenerationOnly returns a copy of generation input with no annotation/GT fields.
// Used to prove leakage barriers in tests.
func (c Case) GenerationOnly() GenerationInput {
	out := GenerationInput{
		Investigation: c.Generation.Investigation,
	}
	if len(c.Generation.StoredProposals) > 0 {
		out.StoredProposals = append([]StoredProposal{}, c.Generation.StoredProposals...)
	}
	return out
}
