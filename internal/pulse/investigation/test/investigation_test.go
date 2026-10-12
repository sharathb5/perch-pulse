package investigation_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/pulse/astronomy"
	"github.com/yashg4509/perch/internal/pulse/change"
	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/incident"
	"github.com/yashg4509/perch/internal/pulse/investigation"
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

func mustInc(t *testing.T, f detect.Finding, samples []detect.Sample, recovered bool) incident.Incident {
	t.Helper()
	if recovered {
		rec := f.LastObservedAt.Add(30 * time.Second)
		f.RecoveredAt = &rec
		for i := 0; i < 3; i++ {
			samples = append(samples, detect.Sample{
				ServiceID: f.ServiceID, Signal: f.SignalType, Value: f.BaselineValue,
				ObservedAt: rec.Add(time.Duration(i) * 10 * time.Second), Source: "test",
			})
		}
	}
	inc, err := incident.Build(incident.BuildInput{Findings: []detect.Finding{f}, Samples: samples, Environment: "local"})
	if err != nil {
		t.Fatal(err)
	}
	return inc
}

func TestAssemblePaymentErrorRegression(t *testing.T) {
	if err := investigation.ValidateReportsSecretFieldNames(); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 10, 5, 0, 0, 0, time.UTC)
	pay := "astronomy/local/payment"
	f := baseFinding(pay, detect.SignalErrorRate, at, 0.41, 0.0)
	inc := mustInc(t, f, samplesFor(pay, detect.SignalErrorRate, at, 0.0, 0.41), false)
	mapping, err := astronomy.Load()
	if err != nil {
		t.Fatal(err)
	}
	inv, err := investigation.Assemble(investigation.AssembleInput{
		Incident: inc,
		Mapping:  mapping,
		Now:      at.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Diagnosed {
		t.Fatal("diagnosed must be false")
	}
	if inv.IncidentState != investigation.IncidentStateActive {
		t.Fatalf("state %s", inv.IncidentState)
	}
	found := false
	for _, h := range inv.Hypotheses {
		if h.Kind == "error_rate_regression" {
			found = true
			if h.Classification != investigation.ClassInferred {
				t.Fatal("hypothesis must be INFERRED")
			}
			if strings.Contains(strings.ToLower(h.Statement), "caused") {
				t.Fatal("causation language")
			}
		}
	}
	if !found {
		t.Fatal("expected error_rate_regression hypothesis")
	}
	assertNoSecrets(t, inv)
}

func TestAssembleShippingLatency(t *testing.T) {
	at := time.Date(2026, 10, 10, 4, 0, 0, 0, time.UTC)
	svc := "astronomy/local/shipping"
	f := baseFinding(svc, detect.SignalLatencyMS, at, 800, 40)
	inc := mustInc(t, f, samplesFor(svc, detect.SignalLatencyMS, at, 40, 800), true)
	mapping, _ := astronomy.Load()
	inv, err := investigation.Assemble(investigation.AssembleInput{Incident: inc, Mapping: mapping, Now: at.Add(2 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if inv.IncidentState != investigation.IncidentStateRecovered {
		t.Fatalf("state %s", inv.IncidentState)
	}
	ok := false
	for _, h := range inv.Hypotheses {
		if h.Kind == "latency_regression" {
			ok = true
		}
	}
	if !ok {
		t.Fatal("expected latency hypothesis")
	}
}

func TestAssembleCallRateDrop(t *testing.T) {
	at := time.Date(2026, 10, 10, 5, 20, 0, 0, time.UTC)
	svc := "astronomy/local/payment"
	f := baseFinding(svc, detect.SignalCallRate, at, 0.0, 0.16)
	inc := mustInc(t, f, samplesFor(svc, detect.SignalCallRate, at, 0.16, 0.0), true)
	mapping, _ := astronomy.Load()
	inv, err := investigation.Assemble(investigation.AssembleInput{Incident: inc, Mapping: mapping, Now: at.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	ok := false
	for _, h := range inv.Hypotheses {
		if h.Kind == "call_rate_drop" {
			ok = true
		}
	}
	if !ok {
		t.Fatal("expected call_rate_drop")
	}
}

func TestMissingBaselineAndNoChanges(t *testing.T) {
	at := time.Date(2026, 10, 10, 6, 0, 0, 0, time.UTC)
	f := baseFinding("astronomy/local/payment", detect.SignalErrorRate, at, 0.5, 0.0)
	inc, err := incident.Build(incident.BuildInput{Findings: []detect.Finding{f}, Environment: "local"})
	if err != nil {
		t.Fatal(err)
	}
	mapping, _ := astronomy.Load()
	inv, err := investigation.Assemble(investigation.AssembleInput{Incident: inc, Mapping: mapping, Now: at})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(inv.MissingEvidence, " ")
	if !strings.Contains(joined, "before-window") && !strings.Contains(joined, "no ranked change") {
		t.Fatalf("missing evidence incomplete: %v", inv.MissingEvidence)
	}
	for _, u := range inv.Unknowns {
		if strings.Contains(strings.ToLower(u), "root cause is unknown") {
			return
		}
	}
	t.Fatal("expected UNKNOWN root cause note")
}

func TestChangeCorrelationNotCausation(t *testing.T) {
	at := time.Date(2026, 10, 10, 7, 0, 0, 0, time.UTC)
	pay := "astronomy/local/payment"
	f := baseFinding(pay, detect.SignalErrorRate, at, 0.5, 0.0)
	inc := mustInc(t, f, samplesFor(pay, detect.SignalErrorRate, at, 0.0, 0.5), false)
	dep := at.Add(-2 * time.Minute)
	chg := change.Event{
		SchemaVersion:  change.SchemaVersion,
		ChangeID:       "chg|test|payment",
		ChangeType:     change.ChangeDeployment,
		Source:         "test",
		ServiceIDs:     []string{pay},
		ServiceMapping: change.MappingExplicit,
		Environment:    "local",
		Title:          "deploy payment",
		CreatedAt:      dep,
		DeployedAt:     &dep,
		ObservedAt:     dep,
		Summary:        "simulated marker",
		Metadata:       map[string]string{"simulated": "true"},
	}
	if err := chg.Validate(); err != nil {
		t.Fatal(err)
	}
	mapping, _ := astronomy.Load()
	inv, err := investigation.Assemble(investigation.AssembleInput{
		Incident: inc,
		Changes:  []change.Event{chg},
		Mapping:  mapping,
		Now:      at,
	})
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := json.Marshal(inv)
	low := strings.ToLower(string(blob))
	for _, bad := range []string{"caused the outage", "is the root cause"} {
		if strings.Contains(low, bad) {
			t.Fatalf("forbidden claim %q", bad)
		}
	}
	// Allow "not a confirmed root-cause" / "does not confirm" phrasing only.
	if strings.Contains(low, "confirmed root cause") &&
		!strings.Contains(low, "not a confirmed") &&
		!strings.Contains(low, "does not confirm") {
		t.Fatal("unqualified confirmed root cause claim")
	}
	if strings.Contains(low, "proven causation") && !strings.Contains(low, "not proven causation") {
		t.Fatal("causation claimed without negation")
	}
	if !strings.Contains(low, "root cause is unknown") {
		t.Fatal("expected explicit UNKNOWN root-cause classification")
	}
	if inv.StrongChangeCandidate {
		for _, h := range inv.Hypotheses {
			if h.Kind == "change_correlation" && !strings.Contains(h.Statement, "not proven causation") && !strings.Contains(h.Statement, "correlation, not proven") {
				if !strings.Contains(strings.ToLower(h.Statement), "not proven causation") {
					t.Fatalf("change hyp must deny causation: %s", h.Statement)
				}
			}
		}
	}
}

func TestPersistenceRoundTripAndImmutability(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	pay := "astronomy/local/payment"
	f := baseFinding(pay, detect.SignalErrorRate, at, 0.4, 0.0)
	inc := mustInc(t, f, samplesFor(pay, detect.SignalErrorRate, at, 0.0, 0.4), false)
	incStore := incident.NewFileStore(dir)
	if err := incStore.Append(inc); err != nil {
		t.Fatal(err)
	}
	svc := &investigation.Service{
		Incidents: incStore,
		Changes:   change.NewFileStore(dir),
		Store:     investigation.NewFileStore(dir),
		Now:       func() time.Time { return at.Add(time.Minute) },
	}
	inv1, err := svc.Create(investigation.CreateOptions{IncidentID: inc.IncidentID, ReuseIdentical: true})
	if err != nil {
		t.Fatal(err)
	}
	inv2, err := svc.Create(investigation.CreateOptions{IncidentID: inc.IncidentID, ReuseIdentical: true})
	if err != nil {
		t.Fatal(err)
	}
	if inv1.InvestigationID != inv2.InvestigationID {
		t.Fatalf("reuse identical failed: %s vs %s", inv1.InvestigationID, inv2.InvestigationID)
	}
	got, err := svc.Get(inv1.InvestigationID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ContentFingerprint != inv1.ContentFingerprint {
		t.Fatal("fingerprint mismatch")
	}
	// Mutating incident must not rewrite old investigation.
	inc2 := inc
	inc2.Summary = "updated summary after investigation"
	_ = incStore.Upsert(inc2)
	got2, err := svc.Get(inv1.InvestigationID)
	if err != nil {
		t.Fatal(err)
	}
	if got2.ContentFingerprint != inv1.ContentFingerprint {
		t.Fatal("snapshot mutated")
	}
}

func TestFeedbackValidationAndNoIncidentMutation(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	pay := "astronomy/local/payment"
	f := baseFinding(pay, detect.SignalErrorRate, at, 0.4, 0.0)
	inc := mustInc(t, f, samplesFor(pay, detect.SignalErrorRate, at, 0.0, 0.4), false)
	incStore := incident.NewFileStore(dir)
	_ = incStore.Append(inc)
	svc := &investigation.Service{
		Incidents: incStore,
		Changes:   change.NewFileStore(dir),
		Store:     investigation.NewFileStore(dir),
		Now:       func() time.Time { return at.Add(time.Minute) },
	}
	inv, err := svc.Create(investigation.CreateOptions{IncidentID: inc.IncidentID})
	if err != nil {
		t.Fatal(err)
	}
	hypID := inv.Hypotheses[0].HypothesisID
	fb, err := svc.AddFeedback(investigation.Feedback{
		InvestigationID:     inv.InvestigationID,
		Assessment:          investigation.AssessmentUseful,
		UsefulHypothesisIDs: []string{hypID},
		Rationale:           "helpful leads",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fb.FeedbackID == "" {
		t.Fatal("feedback id")
	}
	_, err = svc.AddFeedback(investigation.Feedback{
		InvestigationID:     inv.InvestigationID,
		Assessment:          investigation.AssessmentUseful,
		UsefulHypothesisIDs: []string{"hyp|does-not-exist"},
	})
	if err == nil {
		t.Fatal("expected unknown hypothesis rejection")
	}
	before, _, _ := incStore.Get(inc.IncidentID)
	list, err := svc.ListFeedback(inv.InvestigationID)
	if err != nil || len(list) != 1 {
		t.Fatalf("list feedback: %v %d", err, len(list))
	}
	after, _, _ := incStore.Get(inc.IncidentID)
	if before.Summary != after.Summary || before.LastObservedAt != after.LastObservedAt {
		t.Fatal("feedback mutated incident")
	}
	invAfter, _ := svc.Get(inv.InvestigationID)
	if invAfter.ContentFingerprint != inv.ContentFingerprint {
		t.Fatal("feedback mutated investigation")
	}
}

func TestAgentContextStableAndRedacted(t *testing.T) {
	at := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	pay := "astronomy/local/payment"
	f := baseFinding(pay, detect.SignalErrorRate, at, 0.4, 0.0)
	f.Summary = "leak bearer sk-abcdefghijklmnopqrstuvwxyz1234"
	inc := mustInc(t, f, samplesFor(pay, detect.SignalErrorRate, at, 0.0, 0.4), false)
	mapping, _ := astronomy.Load()
	inv, err := investigation.Assemble(investigation.AssembleInput{Incident: inc, Mapping: mapping, Now: at})
	if err != nil {
		t.Fatal(err)
	}
	ctx := investigation.BuildAgentContext(inv)
	if ctx.SchemaVersion != investigation.AgentContextSchema {
		t.Fatal(ctx.SchemaVersion)
	}
	if ctx.Diagnosed {
		t.Fatal("diagnosed")
	}
	raw, _ := json.Marshal(ctx)
	if strings.Contains(string(raw), "sk-abcdefghijklmnopqrstuvwxyz1234") {
		t.Fatal("secret leaked into agent context")
	}
	text := investigation.FormatAgentContextText(inv)
	for _, sec := range []string{"FACTS:", "HYPOTHESES:", "UNKNOWN:", "NEXT STEPS:"} {
		if !strings.Contains(text, sec) {
			t.Fatalf("missing section %s", sec)
		}
	}
}

func TestConcurrencyIdempotentCreate(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 10, 10, 11, 0, 0, 0, time.UTC)
	pay := "astronomy/local/payment"
	f := baseFinding(pay, detect.SignalErrorRate, at, 0.4, 0.0)
	inc := mustInc(t, f, samplesFor(pay, detect.SignalErrorRate, at, 0.0, 0.4), false)
	incStore := incident.NewFileStore(dir)
	_ = incStore.Append(inc)
	svc := &investigation.Service{
		Incidents: incStore,
		Changes:   change.NewFileStore(dir),
		Store:     investigation.NewFileStore(dir),
		Now:       func() time.Time { return at.Add(time.Minute) },
	}
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			inv, err := svc.Create(investigation.CreateOptions{IncidentID: inc.IncidentID, ReuseIdentical: true})
			if err != nil {
				t.Errorf("create: %v", err)
				return
			}
			ids <- inv.InvestigationID
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]struct{}{}
	for id := range ids {
		seen[id] = struct{}{}
	}
	if len(seen) != 1 {
		t.Fatalf("expected single investigation id under reuse, got %d", len(seen))
	}
}

func TestUnknownIncident(t *testing.T) {
	dir := t.TempDir()
	svc := &investigation.Service{
		Incidents: incident.NewFileStore(dir),
		Store:     investigation.NewFileStore(dir),
	}
	_, err := svc.Create(investigation.CreateOptions{IncidentID: "inc|missing"})
	if err == nil {
		t.Fatal("expected not found")
	}
}

func TestFixtureIncidentsFromAstronomyShop(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "examples", "astronomy-shop", "change", "results"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "incidents")); err != nil {
		t.Skip("fixture dir missing")
	}
	incStore := incident.NewFileStore(root)
	list, err := incStore.List()
	if err != nil || len(list) == 0 {
		t.Fatalf("fixtures: %v %d", err, len(list))
	}
	dir := t.TempDir()
	// Copy one payment incident into temp dir via Append.
	var pick *incident.Incident
	for i := range list {
		if strings.Contains(list[i].PrimaryServiceID, "payment") {
			pick = &list[i]
			break
		}
	}
	if pick == nil {
		pick = &list[0]
	}
	tmpInc := incident.NewFileStore(dir)
	if err := tmpInc.Append(*pick); err != nil {
		t.Fatal(err)
	}
	// Also copy changes if present.
	chgStore := change.NewFileStore(root)
	chgs, _ := chgStore.List()
	tmpChg := change.NewFileStore(dir)
	for _, c := range chgs {
		_ = tmpChg.Append(c)
	}
	svc := &investigation.Service{
		Incidents: tmpInc,
		Changes:   tmpChg,
		Store:     investigation.NewFileStore(dir),
		Now:       func() time.Time { return time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC) },
	}
	inv, err := svc.Create(investigation.CreateOptions{IncidentID: pick.IncidentID, ReuseIdentical: true})
	if err != nil {
		t.Fatal(err)
	}
	if inv.SchemaVersion != investigation.SchemaVersion {
		t.Fatal(inv.SchemaVersion)
	}
	assertNoSecrets(t, inv)
}

func assertNoSecrets(t *testing.T, inv investigation.Investigation) {
	t.Helper()
	raw, _ := json.Marshal(inv)
	low := strings.ToLower(string(raw))
	for _, bad := range []string{"bearer ", "api_key", "password"} {
		if strings.Contains(low, bad) {
			t.Fatalf("secret-shaped content %q", bad)
		}
	}
}
