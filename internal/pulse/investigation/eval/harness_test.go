package eval_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/pulse/investigation"
	"github.com/yashg4509/perch/internal/pulse/investigation/eval"
	"github.com/yashg4509/perch/internal/pulse/investigation/llm"
)

func TestBuiltinDatasetOfflineRun(t *testing.T) {
	ds, err := eval.BuiltinDataset()
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Cases) < 15 {
		t.Fatalf("want >=15 cases, got %d", len(ds.Cases))
	}
	rep, err := eval.Run(eval.RunOptions{
		Dataset: ds,
		Now:     func() time.Time { return time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Mode != "offline" {
		t.Fatalf("mode %s", rep.Mode)
	}
	if rep.Aggregate.CaseCount != len(ds.Cases) {
		t.Fatalf("case count %d", rep.Aggregate.CaseCount)
	}
	if rep.Aggregate.DeterministicDenom != len(ds.Cases) {
		t.Fatal("deterministic denominator")
	}
	if rep.Aggregate.LLMAvailableDenom != len(ds.Cases) {
		t.Fatal("llm available denominator")
	}
	if rep.Aggregate.LLMPassDenom != rep.Aggregate.LLMAvailableCount {
		t.Fatal("llm pass denom must equal available count")
	}
	if rep.Aggregate.SyntheticProposalCount+rep.Aggregate.RecordedProposalCount+rep.Aggregate.MissingLLMCount != len(ds.Cases) {
		t.Fatalf("proposal accounting broken: %+v", rep.Aggregate)
	}
	// No fabricated LLM score when missing.
	for _, c := range rep.Cases {
		if !c.LLM.Available && c.LLM.PassMechanical {
			t.Fatalf("%s missing LLM but pass=true", c.CaseID)
		}
	}
	md := rep.Markdown()
	if !strings.Contains(md, "explicit denominators") && !strings.Contains(md, "Denominator") && !strings.Contains(md, "denominators") {
		// Markdown uses "explicit denominators" in header
		if !strings.Contains(md, "Aggregate") {
			t.Fatal("markdown missing aggregate")
		}
	}
	raw, err := rep.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "sk-") {
		t.Fatal("report leaked key material")
	}
	// Stable rerun
	rep2, err := eval.Run(eval.RunOptions{
		Dataset: ds,
		Now:     func() time.Time { return rep.GeneratedAt },
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Aggregate.DeterministicPassCount != rep2.Aggregate.DeterministicPassCount ||
		rep.Aggregate.LLMPassCount != rep2.Aggregate.LLMPassCount {
		t.Fatal("unstable aggregate across reruns")
	}
}

func TestNoAnnotationLeakage(t *testing.T) {
	ds, err := eval.BuiltinDataset()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range ds.Cases {
		if err := eval.AssertNoAnnotationLeakage(c); err != nil {
			t.Fatalf("%s: %v", c.CaseID, err)
		}
		gen, _ := json.Marshal(c.GenerationOnly())
		if strings.Contains(string(gen), "hidden_ground_truth") {
			t.Fatalf("%s leaked GT field", c.CaseID)
		}
		if c.HiddenGroundTruth != nil && c.HiddenGroundTruth.ScenarioID != "" {
			if strings.Contains(string(gen), c.HiddenGroundTruth.ScenarioID) {
				t.Fatalf("%s leaked scenario id into generation", c.CaseID)
			}
		}
	}
}

func TestFingerprintMismatchRejected(t *testing.T) {
	ds, err := eval.BuiltinDataset()
	if err != nil {
		t.Fatal(err)
	}
	c := ds.Cases[0]
	inv := c.Generation.Investigation
	bad := c.Generation.StoredProposals[0]
	bad.Proposal.SourceFingerprint = "deadbeef"
	score := eval.ScoreProposal(bad.Proposal, inv, bad.Origin, c.Annotations)
	if score.PassMechanical || score.FingerprintMatch {
		t.Fatalf("mismatch must fail: %+v", score)
	}
}

func TestInventedCitationAndInvalidService(t *testing.T) {
	ds, err := eval.BuiltinDataset()
	if err != nil {
		t.Fatal(err)
	}
	var inv investigation.Investigation
	for _, c := range ds.Cases {
		if c.CaseID == "case_payment_error" {
			inv = c.Generation.Investigation
			break
		}
	}
	prop := llm.Proposal{
		SchemaVersion:         llm.ProposalSchemaVersion,
		ProposalID:            "prop|bad",
		SourceInvestigationID: inv.InvestigationID,
		SourceIncidentID:      inv.IncidentID,
		SourceFingerprint:     inv.ContentFingerprint,
		GenerationMethod:      llm.GenerationMethodLLM,
		GeneratedAt:           time.Now().UTC(),
		Diagnosed:             false,
		Hypotheses: []llm.Hypothesis{{
			HypothesisID:           "h1",
			Statement:              "astronomy/local/not-real-service showed error_rate issues during the window.",
			Classification:         investigation.ClassInferred,
			SupportingEvidenceRefs: []string{"fabricated:cite"},
			SuggestedVerification:  "x",
			Kind:                   "llm_hypothesis",
			SupportStatus:          "validated",
		}},
		Unknowns: []string{"Root cause is UNKNOWN"},
	}
	score := eval.ScoreProposal(prop, inv, "synthetic_fixture", eval.EvaluatorAnnotations{})
	if score.UnresolvedCitationCount == 0 {
		t.Fatal("expected unresolved citation")
	}
	if score.InvalidServiceRefCount == 0 {
		t.Fatal("expected invalid service")
	}
	if score.PassMechanical {
		t.Fatal("must not pass")
	}
}

func TestFalseCausalClaims(t *testing.T) {
	ds, err := eval.BuiltinDataset()
	if err != nil {
		t.Fatal(err)
	}
	inv := ds.Cases[0].Generation.Investigation
	prop := llm.Proposal{
		SchemaVersion: llm.ProposalSchemaVersion, ProposalID: "prop|causal",
		SourceInvestigationID: inv.InvestigationID, SourceIncidentID: inv.IncidentID,
		SourceFingerprint: inv.ContentFingerprint, GenerationMethod: llm.GenerationMethodLLM,
		GeneratedAt: time.Now().UTC(), Diagnosed: false,
		Hypotheses: []llm.Hypothesis{{
			HypothesisID: "h", Statement: "The deployment caused the error_rate spike during the incident window.",
			Classification: investigation.ClassInferred, SupportingEvidenceRefs: []string{"phase:during"},
			SuggestedVerification: "x", Kind: "change_correlation", SupportStatus: "validated",
		}},
		Unknowns: []string{"Root cause is UNKNOWN"},
	}
	score := eval.ScoreProposal(prop, inv, "synthetic_fixture", eval.EvaluatorAnnotations{})
	if score.FalseCausalAttribution == 0 || score.PassMechanical {
		t.Fatalf("causal must fail: %+v", score)
	}
}

func TestPromptInjectionCaseFailsLLMMechanical(t *testing.T) {
	ds, err := eval.BuiltinDataset()
	if err != nil {
		t.Fatal(err)
	}
	rep, err := eval.Run(eval.RunOptions{Dataset: ds})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range rep.Cases {
		if c.CaseID == "case_prompt_injection" {
			found = true
			if !c.LLM.Available {
				t.Fatal("expected synthetic bad proposal present")
			}
			if c.LLM.PassMechanical {
				t.Fatal("injection/fabricated citation proposal must fail mechanical pass")
			}
		}
	}
	if !found {
		t.Fatal("missing injection case")
	}
}

func TestSameInputComparison(t *testing.T) {
	ds, err := eval.BuiltinDataset()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range ds.Cases {
		for _, sp := range c.Generation.StoredProposals {
			if sp.Proposal.SourceFingerprint != "" &&
				sp.Proposal.SourceFingerprint != c.Generation.Investigation.ContentFingerprint &&
				c.CaseID != "case_prompt_injection" {
				// injection case may still match fingerprint with bad content
				t.Fatalf("%s proposal fingerprint diverges from investigation", c.CaseID)
			}
		}
	}
}

func TestFeedbackSummaryAndDuplicates(t *testing.T) {
	rows := []investigation.Feedback{
		{Assessment: "useful"},
		{Assessment: "useful"},
		{Assessment: "not_useful"},
	}
	sum := eval.SummarizeInvestigationFeedback(rows)
	if sum.Count != 3 || sum.AssessmentCounts["useful"] != 2 {
		t.Fatalf("%+v", sum)
	}
	if eval.SummarizeInvestigationFeedback(nil) != nil {
		t.Fatal("empty")
	}
	// Missing feedback labeled unavailable in report notes path covered by cases without feedback.
}

func TestMalformedDataset(t *testing.T) {
	ds := eval.Dataset{SchemaVersion: "bad", DatasetID: "x", Cases: nil}
	if err := ds.Validate(); err == nil {
		t.Fatal("expected invalid")
	}
	_, err := eval.Run(eval.RunOptions{Dataset: ds})
	if err == nil {
		t.Fatal("run must reject malformed")
	}
}

func TestNoModelCallsInOfflineMode(t *testing.T) {
	// Offline Run never constructs an llm.Service / ChatClient — ensured by API surface.
	// Guard: BuiltinDataset stored proposals are synthetic_fixture or absent.
	ds, err := eval.BuiltinDataset()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range ds.Cases {
		for _, sp := range c.Generation.StoredProposals {
			if sp.Origin != "synthetic_fixture" && sp.Origin != "recorded_offline" {
				t.Fatalf("bad origin %s", sp.Origin)
			}
			if sp.Proposal.Provenance.Model == "" {
				continue
			}
			if strings.Contains(strings.ToLower(sp.Proposal.Provenance.Model), "gpt-4") && sp.Origin == "synthetic_fixture" {
				// synthetic may name model; ensure note clarifies
				if sp.Note == "" {
					t.Fatal("synthetic fixture should note offline nature")
				}
			}
		}
	}
}

func TestReportWriteRedacted(t *testing.T) {
	ds, err := eval.BuiltinDataset()
	if err != nil {
		t.Fatal(err)
	}
	rep, err := eval.Run(eval.RunOptions{Dataset: ds})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	raw, err := rep.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), "PERCH_PULSE_LLM_API_KEY") || strings.Contains(string(got), "Bearer ") {
		t.Fatal("secrets in report")
	}
}
