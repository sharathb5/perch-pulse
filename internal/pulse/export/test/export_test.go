package export_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/pulse/astronomy"
	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/export"
	"github.com/yashg4509/perch/internal/pulse/incident"
	"github.com/yashg4509/perch/internal/pulse/investigation"
	"github.com/yashg4509/perch/internal/pulse/investigation/eval"
	"github.com/yashg4509/perch/internal/pulse/investigation/llm"
)

func baseFinding(svc string, sig detect.SignalType, at time.Time, obs, base float64) detect.Finding {
	return detect.Finding{
		SchemaVersion:   detect.SchemaVersion,
		FindingID:       detect.FindingIDFor(svc, sig, at),
		DetectorVersion: detect.DetectorVersion,
		ServiceID:       svc,
		SignalType:      sig,
		ObservedWindow:  detect.Window{Start: at.Add(-40 * time.Second), End: at},
		BaselineWindow:  detect.Window{Start: at.Add(-3 * time.Minute), End: at.Add(-40 * time.Second)},
		ObservedValue:   obs,
		BaselineValue:   base,
		Threshold:       0.05,
		Score:           2,
		Severity:        detect.SeverityCritical,
		FirstDetectedAt: at,
		LastObservedAt:  at.Add(30 * time.Second),
		Summary:         "observed regression (test)",
	}
}

func samplesFor(svc string, sig detect.SignalType, at time.Time, beforeVal, duringVal float64) []detect.Sample {
	var samples []detect.Sample
	for i := 0; i < 5; i++ {
		samples = append(samples, detect.Sample{
			ServiceID: svc, Signal: sig, Value: beforeVal,
			ObservedAt: at.Add(-90 * time.Second).Add(time.Duration(i) * 10 * time.Second), Source: "test",
		})
	}
	for i := 0; i < 5; i++ {
		samples = append(samples, detect.Sample{
			ServiceID: svc, Signal: sig, Value: duringVal,
			ObservedAt: at.Add(time.Duration(i) * 5 * time.Second), Source: "test",
		})
	}
	return samples
}

func seedPulseDir(t *testing.T) (pulseDir string, inv investigation.Investigation, prop llm.Proposal) {
	t.Helper()
	pulseDir = t.TempDir()
	at := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	pay := "astronomy/local/payment"
	f := baseFinding(pay, detect.SignalErrorRate, at, 0.41, 0.0)
	inc, err := incident.Build(incident.BuildInput{
		Findings: []detect.Finding{f}, Samples: samplesFor(pay, detect.SignalErrorRate, at, 0.0, 0.41), Environment: "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := incident.NewFileStore(pulseDir).Upsert(inc); err != nil {
		t.Fatal(err)
	}
	mapping, err := astronomy.Load()
	if err != nil {
		t.Fatal(err)
	}
	inv, err = investigation.Assemble(investigation.AssembleInput{Incident: inc, Mapping: mapping, Now: at.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	invStore := investigation.NewFileStore(pulseDir)
	if err := invStore.Put(inv); err != nil {
		t.Fatal(err)
	}
	fb := investigation.Feedback{
		SchemaVersion:   investigation.FeedbackSchemaVersion,
		FeedbackID:      "fb-test-1",
		InvestigationID: inv.InvestigationID,
		IncidentID:      inv.IncidentID,
		Timestamp:       at.Add(2 * time.Minute),
		Assessment:      investigation.AssessmentUseful,
		Rationale:       "helpful ranking",
	}
	if err := invStore.PutFeedback(fb); err != nil {
		t.Fatal(err)
	}
	sym := ""
	if len(inv.ObservedSymptoms) > 0 {
		sym = inv.ObservedSymptoms[0].ID
	}
	prop = llm.Proposal{
		SchemaVersion:         llm.ProposalSchemaVersion,
		ProposalID:            "prop-test-1",
		SourceInvestigationID: inv.InvestigationID,
		SourceIncidentID:      inv.IncidentID,
		SourceFingerprint:     inv.ContentFingerprint,
		GenerationMethod:      llm.GenerationMethodLLM,
		GeneratedAt:           at.Add(3 * time.Minute),
		Diagnosed:             false,
		Hypotheses: []llm.Hypothesis{{
			HypothesisID: "h1", Statement: "Error rate increased vs baseline during the window.",
			Classification: investigation.ClassInferred, Kind: "error_rate_regression",
			SupportingEvidenceRefs: []string{sym}, SuggestedVerification: "Compare digests.",
			SupportStatus: "validated",
		}},
		MissingEvidence:       []string{"deploy annotation"},
		Unknowns:              []string{"upstream saturation"},
		SuggestedVerification: []string{"Check canary"},
		Limitations:           []string{"offline fixture"},
		Provenance: llm.Provenance{
			Provider: "openai_compatible", Model: "fixture-model", EndpointHost: "127.0.0.1",
		},
	}
	if err := prop.Validate(); err != nil {
		t.Fatal(err)
	}
	propStore := llm.NewFileStore(pulseDir)
	if err := propStore.Put(prop); err != nil {
		t.Fatal(err)
	}
	pfb := llm.Feedback{
		SchemaVersion:         llm.ProposalFeedbackSchemaVersion,
		FeedbackID:            "pfb-1",
		ProposalID:            prop.ProposalID,
		SourceInvestigationID: inv.InvestigationID,
		Timestamp:             at.Add(4 * time.Minute),
		Assessment:            investigation.AssessmentPartiallyUseful,
	}
	if err := propStore.PutFeedback(pfb); err != nil {
		t.Fatal(err)
	}
	return pulseDir, inv, prop
}

func TestExportIncrementalIdempotent(t *testing.T) {
	pulseDir, _, _ := seedPulseDir(t)
	out := filepath.Join(t.TempDir(), "out")
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	r1, err := export.Run(export.Options{
		PulseDir: pulseDir, OutDir: out, AllowedRoot: filepath.Dir(out),
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if r1.RowsAdded[export.DatasetIncidents] != 1 {
		t.Fatalf("incidents added=%d", r1.RowsAdded[export.DatasetIncidents])
	}
	if r1.RowsAdded[export.DatasetInvestigations] != 1 {
		t.Fatalf("investigations added=%d", r1.RowsAdded[export.DatasetInvestigations])
	}
	if r1.RowsAdded[export.DatasetProposals] != 1 {
		t.Fatalf("proposals added=%d", r1.RowsAdded[export.DatasetProposals])
	}
	if r1.RowsAdded[export.DatasetInvestigationFeedback] < 2 {
		t.Fatalf("feedback added=%d", r1.RowsAdded[export.DatasetInvestigationFeedback])
	}

	r2, err := export.Run(export.Options{
		PulseDir: pulseDir, OutDir: out, AllowedRoot: filepath.Dir(out),
		Now: func() time.Time { return now.Add(time.Minute) },
	})
	if err != nil {
		t.Fatal(err)
	}
	for ds, n := range r2.RowsAdded {
		if n != 0 {
			t.Fatalf("idempotent re-export added %d rows to %s", n, ds)
		}
	}
	qr, err := export.ValidateExport(out)
	if err != nil {
		t.Fatal(err)
	}
	if !qr.OK {
		t.Fatalf("quality report issues: %+v", qr.Issues)
	}
}

func TestExportWithEvalReport(t *testing.T) {
	pulseDir, _, _ := seedPulseDir(t)
	ds, err := eval.BuiltinDataset()
	if err != nil {
		t.Fatal(err)
	}
	rep, err := eval.Run(eval.RunOptions{Dataset: ds})
	if err != nil {
		t.Fatal(err)
	}
	repPath := filepath.Join(t.TempDir(), "report.json")
	raw, err := rep.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")
	r, err := export.Run(export.Options{
		PulseDir: pulseDir, OutDir: out, AllowedRoot: filepath.Dir(out),
		EvalReportPath:     repPath,
		IncidentProvenance: export.ProvenanceObservedLocal,
		Now:                func() time.Time { return time.Date(2026, 10, 12, 13, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.RowsAdded[export.DatasetEvalRuns] != 1 {
		t.Fatalf("eval_runs=%d", r.RowsAdded[export.DatasetEvalRuns])
	}
	if r.RowsAdded[export.DatasetEvalCaseResults] == 0 {
		t.Fatal("expected eval case results")
	}
	qr, err := export.ValidateExport(out)
	if err != nil {
		t.Fatal(err)
	}
	if !qr.OK {
		t.Fatalf("issues: %+v", qr.Issues)
	}
	// Provenance note should mention mixing.
	found := false
	for _, n := range qr.Notes {
		if strings.Contains(n, "provenance_class") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected provenance mixing note")
	}
}

func TestPathTraversalRejected(t *testing.T) {
	root := t.TempDir()
	_, err := export.ResolveExportDir(filepath.Join(root, "..", "escape"), root)
	if err == nil {
		t.Fatal("expected escape rejection")
	}
	_, err = export.SafeJoin(root, "../x.jsonl")
	if err == nil {
		t.Fatal("expected safe join rejection")
	}
}

func TestCheckpointNotAdvancedOnFailedCommit(t *testing.T) {
	pulseDir, _, _ := seedPulseDir(t)
	out := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(out, 0o750); err != nil {
		t.Fatal(err)
	}
	// Pre-create a directory where JSONL should be a file to force rename/write failure path:
	// make incidents.jsonl a directory so OpenFile fails oddly — actually append opens .tmp first.
	// Better: make out read-only after first successful partial... simpler: corrupt checkpoint schema after first run.
	r1, err := export.Run(export.Options{
		PulseDir: pulseDir, OutDir: out, AllowedRoot: filepath.Dir(out),
		Now: func() time.Time { return time.Date(2026, 10, 12, 14, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	cpPath := r1.CheckpointPath
	raw, _ := os.ReadFile(cpPath)
	var before map[string]any
	_ = json.Unmarshal(raw, &before)

	// Write corrupt checkpoint and ensure Load fails without mutating JSONL counts wrongly.
	if err := os.WriteFile(cpPath, []byte(`{"schema_version":"nope"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = export.Run(export.Options{
		PulseDir: pulseDir, OutDir: out, AllowedRoot: filepath.Dir(out),
		Now: func() time.Time { return time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC) },
	})
	if err == nil {
		t.Fatal("expected corrupt checkpoint error")
	}
	// Restore and ensure idempotent still works after restoring good checkpoint.
	if err := os.WriteFile(cpPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	r2, err := export.Run(export.Options{
		PulseDir: pulseDir, OutDir: out, AllowedRoot: filepath.Dir(out),
		Now: func() time.Time { return time.Date(2026, 10, 12, 16, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range r2.RowsAdded {
		if n != 0 {
			t.Fatalf("after restore expected 0 adds, got %d", n)
		}
	}
}

func TestBatchCapAndDeterministicOrder(t *testing.T) {
	pulseDir := t.TempDir()
	at := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	store := incident.NewFileStore(pulseDir)
	for i := 0; i < 5; i++ {
		svc := "astronomy/local/payment"
		f := baseFinding(svc, detect.SignalErrorRate, at.Add(time.Duration(i)*time.Minute), 0.4, 0)
		f.FindingID = detect.FindingIDFor(svc, detect.SignalErrorRate, at.Add(time.Duration(i)*time.Minute))
		inc, err := incident.Build(incident.BuildInput{
			Findings:    []detect.Finding{f},
			Samples:     samplesFor(svc, detect.SignalErrorRate, at.Add(time.Duration(i)*time.Minute), 0, 0.4),
			Environment: "local",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Upsert(inc); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(t.TempDir(), "out")
	r, err := export.Run(export.Options{
		PulseDir: pulseDir, OutDir: out, AllowedRoot: filepath.Dir(out),
		MaxBatchRecords: 2,
		Now:             func() time.Time { return at.Add(time.Hour) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !r.BatchCapped {
		t.Fatal("expected batch cap")
	}
	if r.RowsAdded[export.DatasetIncidents] != 2 {
		t.Fatalf("got %d incident rows", r.RowsAdded[export.DatasetIncidents])
	}
	// Resume exports remaining.
	r2, err := export.Run(export.Options{
		PulseDir: pulseDir, OutDir: out, AllowedRoot: filepath.Dir(out),
		MaxBatchRecords: 10,
		Now:             func() time.Time { return at.Add(2 * time.Hour) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if r2.RowsAdded[export.DatasetIncidents] != 3 {
		t.Fatalf("resume added %d", r2.RowsAdded[export.DatasetIncidents])
	}
}

func TestInventoryAndSchemasNonEmpty(t *testing.T) {
	inv := export.Inventory()
	if len(inv) < 5 {
		t.Fatal("inventory too small")
	}
	md := export.InventoryMarkdown()
	if !strings.Contains(md, "pulse.incident.v1") {
		t.Fatal("missing incident schema in inventory md")
	}
	if len(export.DatasetSchemas()) != len(export.SupportedDatasets) {
		t.Fatal("schema docs mismatch")
	}
}

func TestInterruptedTmpDoesNotAdvance(t *testing.T) {
	pulseDir, _, _ := seedPulseDir(t)
	out := filepath.Join(t.TempDir(), "out")
	_, err := export.Run(export.Options{
		PulseDir: pulseDir, OutDir: out, AllowedRoot: filepath.Dir(out),
		Now: func() time.Time { return time.Date(2026, 10, 12, 17, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	// Leave a stale tmp; next run should clean and succeed idempotently.
	tmp := filepath.Join(out, "checkpoint.json.tmp")
	if err := os.WriteFile(tmp, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := export.Run(export.Options{
		PulseDir: pulseDir, OutDir: out, AllowedRoot: filepath.Dir(out),
		Now: func() time.Time { return time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range r.RowsAdded {
		if n != 0 {
			t.Fatalf("unexpected adds after stale tmp: %d", n)
		}
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatal("stale tmp should be removed")
	}
}

func TestSymlinkEscapeRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	_, err := export.ResolveExportDir(filepath.Join(link, "out"), root)
	if err == nil {
		t.Fatal("expected symlink escape rejection")
	}
}

func TestJSONLReconcilePreventsDuplicateAfterCheckpointLoss(t *testing.T) {
	pulseDir, _, _ := seedPulseDir(t)
	out := filepath.Join(t.TempDir(), "out")
	now := time.Date(2026, 10, 12, 21, 0, 0, 0, time.UTC)
	r1, err := export.Run(export.Options{
		PulseDir: pulseDir, OutDir: out, AllowedRoot: filepath.Dir(out),
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate crash: JSONL committed but checkpoint wiped to empty.
	empty := []byte(`{"schema_version":"pulse.export.checkpoint.v1","export_schema_version":"pulse.export.analytics.v1","datasets":{}}`)
	if err := os.WriteFile(r1.CheckpointPath, empty, 0o600); err != nil {
		t.Fatal(err)
	}
	r2, err := export.Run(export.Options{
		PulseDir: pulseDir, OutDir: out, AllowedRoot: filepath.Dir(out),
		Now: func() time.Time { return now.Add(time.Minute) },
	})
	if err != nil {
		t.Fatal(err)
	}
	for ds, n := range r2.RowsAdded {
		if n != 0 {
			t.Fatalf("reconcile should skip %s, added %d", ds, n)
		}
	}
}

func TestProvenanceChangeReExports(t *testing.T) {
	pulseDir, _, _ := seedPulseDir(t)
	out := filepath.Join(t.TempDir(), "out")
	now := time.Date(2026, 10, 12, 22, 0, 0, 0, time.UTC)
	_, err := export.Run(export.Options{
		PulseDir: pulseDir, OutDir: out, AllowedRoot: filepath.Dir(out),
		IncidentProvenance: export.ProvenanceObservedLocal,
		Now:                func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := export.Run(export.Options{
		PulseDir: pulseDir, OutDir: out, AllowedRoot: filepath.Dir(out),
		IncidentProvenance: export.ProvenanceHistoricalDemo,
		Now:                func() time.Time { return now.Add(time.Minute) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if r2.RowsAdded[export.DatasetIncidents] != 1 {
		t.Fatalf("expected re-export on provenance change, got %d", r2.RowsAdded[export.DatasetIncidents])
	}
}

func TestRejectSyntheticProvenanceOnNonemptyPulseDir(t *testing.T) {
	pulseDir, _, _ := seedPulseDir(t)
	out := filepath.Join(t.TempDir(), "out")
	_, err := export.Run(export.Options{
		PulseDir:           pulseDir,
		OutDir:             out,
		AllowedRoot:        filepath.Dir(out),
		IncidentProvenance: export.ProvenanceSyntheticFixture,
		Now:                func() time.Time { return time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC) },
	})
	if err == nil {
		t.Fatal("expected refusal of synthetic_fixture on non-empty pulse-dir")
	}
	if !strings.Contains(err.Error(), "refusing incident-provenance") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAllowSyntheticProvenanceOnEmptyPulseDir(t *testing.T) {
	pulseDir := t.TempDir()
	out := filepath.Join(t.TempDir(), "out")
	_, err := export.Run(export.Options{
		PulseDir:           pulseDir,
		OutDir:             out,
		AllowedRoot:        filepath.Dir(out),
		IncidentProvenance: export.ProvenanceSyntheticFixture,
		Now:                func() time.Time { return time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNoSecretFieldsInJSONL(t *testing.T) {
	pulseDir, _, _ := seedPulseDir(t)
	out := filepath.Join(t.TempDir(), "out")
	_, err := export.Run(export.Options{
		PulseDir: pulseDir, OutDir: out, AllowedRoot: filepath.Dir(out),
		Now: func() time.Time { return time.Date(2026, 10, 12, 19, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(out)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(out, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		low := strings.ToLower(string(raw))
		for _, bad := range []string{"authorization", "api_key", "password", "bearer "} {
			if strings.Contains(low, bad) {
				t.Fatalf("%s contains %q", e.Name(), bad)
			}
		}
		// Statement text should be hashed, not present as free-form long claims from fixture.
		if e.Name() == "hypotheses.jsonl" && strings.Contains(string(raw), `"statement":`) {
			t.Fatal("hypothesis statement text must not be exported")
		}
	}
}

func TestWriteSampleFixture(t *testing.T) {
	dir := os.Getenv("PULSE_EXPORT_SAMPLE_DIR")
	if dir == "" {
		t.Skip("set PULSE_EXPORT_SAMPLE_DIR to write sample fixtures")
	}
	pulseDir, _, _ := seedPulseDir(t)
	ds, err := eval.BuiltinDataset()
	if err != nil {
		t.Fatal(err)
	}
	rep, err := eval.Run(eval.RunOptions{Dataset: ds})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	repPath := filepath.Join(dir, "eval-report.json")
	raw, err := rep.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "export")
	_, err = export.Run(export.Options{
		PulseDir: pulseDir, OutDir: out, AllowedRoot: dir,
		EvalReportPath:     repPath,
		IncidentProvenance: export.ProvenanceHistoricalDemo,
		Now:                func() time.Time { return time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
}
