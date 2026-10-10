package correlate_test

import (
	"strings"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/pulse/change"
	"github.com/yashg4509/perch/internal/pulse/changeeval"
	"github.com/yashg4509/perch/internal/pulse/correlate"
	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/incident"
)

func mustIncident(t *testing.T, service string, at time.Time, affected ...string) incident.Incident {
	t.Helper()
	f := detect.Finding{
		SchemaVersion:   detect.SchemaVersion,
		FindingID:       detect.FindingIDFor(service, detect.SignalErrorRate, at),
		DetectorVersion: detect.DetectorVersion,
		ServiceID:       service,
		SignalType:      detect.SignalErrorRate,
		ObservedWindow:  detect.Window{Start: at.Add(-40 * time.Second), End: at},
		BaselineWindow:  detect.Window{Start: at.Add(-3 * time.Minute), End: at.Add(-40 * time.Second)},
		ObservedValue:   0.5,
		BaselineValue:   0.01,
		Threshold:       0.05,
		Score:           2,
		Severity:        detect.SeverityCritical,
		FirstDetectedAt: at,
		LastObservedAt:  at.Add(20 * time.Second),
		Summary:         "observed elevated error rate (correlation only, not causation)",
	}
	for i, a := range affected {
		f.CandidateImpact = append(f.CandidateImpact, detect.ImpactCandidate{
			ServiceID: a, Rank: i + 2, Score: 0.4, Relation: "downstream_of_primary",
			Note: "correlation, not causation",
		})
	}
	inc, err := incident.Build(incident.BuildInput{Findings: []detect.Finding{f}, Environment: "local"})
	if err != nil {
		t.Fatal(err)
	}
	return inc
}

func deploy(t *testing.T, idHint, service string, at time.Time) change.Event {
	t.Helper()
	ev, err := change.Record(change.RecordInput{
		Type:        change.ChangeDeployment,
		ServiceIDs:  []string{service},
		Environment: "local",
		CommitSHA:   idHint,
		Title:       "deploy " + idHint,
		DeployedAt:  &at,
		ObservedAt:  at,
		CreatedAt:   at,
		Simulated:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestSameServiceCloserOutranksUnrelated(t *testing.T) {
	at := time.Date(2026, 10, 9, 16, 0, 0, 0, time.UTC)
	inc := mustIncident(t, "astronomy/local/payment", at)
	related := deploy(t, "pay1", "astronomy/local/payment", at.Add(-40*time.Second))
	unrelated := deploy(t, "ship1", "astronomy/local/shipping", at.Add(-10*time.Second))
	rep, err := correlate.Correlate(inc, []change.Event{unrelated, related}, correlate.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Top1 == nil || rep.Top1.ChangeID != related.ChangeID {
		t.Fatalf("top1=%v want %s", rep.Top1, related.ChangeID)
	}
	if !rep.StrongCandidate {
		t.Fatal("expected strong candidate")
	}
}

func TestRecentRelatedBeatsOlderSameService(t *testing.T) {
	at := time.Date(2026, 10, 9, 16, 0, 0, 0, time.UTC)
	inc := mustIncident(t, "astronomy/local/payment", at, "astronomy/local/frontend")
	old := deploy(t, "oldpay", "astronomy/local/payment", at.Add(-25*time.Minute))
	recentDown := deploy(t, "fe1", "astronomy/local/frontend", at.Add(-30*time.Second))
	rep, err := correlate.Correlate(inc, []change.Event{old, recentDown}, correlate.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	// Primary overlap on old should still beat topology-only recent if scores allow —
	// but old is far: temporal~6.7 vs recent topology 15+~39. Document expected:
	// primary+far vs topology+near: compute roughly
	// old: temporal 40*(1-25/30)=6.67 + primary 35 + env 10 = 51.67
	// recentDown: temporal 40*(1-30/1800)=39.3 + topology 15 + env 10 = 64.3
	if rep.Top1 == nil || rep.Top1.ChangeID != recentDown.ChangeID {
		t.Fatalf("top1=%v want recent topology %s (got score breakdown)", rep.Top1, recentDown.ChangeID)
	}
	_ = old
}

func TestPostIncidentExcluded(t *testing.T) {
	at := time.Date(2026, 10, 9, 16, 0, 0, 0, time.UTC)
	inc := mustIncident(t, "astronomy/local/payment", at)
	post := deploy(t, "late", "astronomy/local/payment", at.Add(30*time.Second))
	rep, err := correlate.Correlate(inc, []change.Event{post}, correlate.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Candidates) != 0 || !rep.NoStrongCandidate {
		t.Fatalf("post-incident should be excluded: %+v", rep)
	}
}

func TestUnrelatedEnvironmentExcluded(t *testing.T) {
	at := time.Date(2026, 10, 9, 16, 0, 0, 0, time.UTC)
	inc := mustIncident(t, "astronomy/local/payment", at)
	ev, err := change.Record(change.RecordInput{
		Type: change.ChangeDeployment, ServiceIDs: []string{"astronomy/local/payment"},
		Environment: "staging", CommitSHA: "stg1",
		DeployedAt: ptr(at.Add(-20 * time.Second)), ObservedAt: at.Add(-20 * time.Second), CreatedAt: at.Add(-20 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := correlate.Correlate(inc, []change.Event{ev}, correlate.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Candidates) != 0 {
		t.Fatalf("env mismatch should exclude: %+v", rep.Candidates)
	}
}

func TestDeterministicTies(t *testing.T) {
	at := time.Date(2026, 10, 9, 16, 0, 0, 0, time.UTC)
	inc := mustIncident(t, "astronomy/local/payment", at)
	t0 := at.Add(-60 * time.Second)
	a := deploy(t, "aaa", "astronomy/local/payment", t0)
	b := deploy(t, "bbb", "astronomy/local/payment", t0)
	rep1, _ := correlate.Correlate(inc, []change.Event{a, b}, correlate.DefaultConfig())
	rep2, _ := correlate.Correlate(inc, []change.Event{b, a}, correlate.DefaultConfig())
	if rep1.Top1.ChangeID != rep2.Top1.ChangeID {
		t.Fatalf("tie unstable: %s vs %s", rep1.Top1.ChangeID, rep2.Top1.ChangeID)
	}
	// Lexicographic ChangeID: aaa vs bbb — ChangeIDFor uses type|key|time so "aaa" < "bbb"
	if rep1.Top1.ChangeID != a.ChangeID {
		t.Fatalf("want aaa first by ChangeID, got %s", rep1.Top1.ChangeID)
	}
}

func TestUnrelatedServiceRejection(t *testing.T) {
	at := time.Date(2026, 10, 9, 16, 0, 0, 0, time.UTC)
	inc := mustIncident(t, "astronomy/local/payment", at)
	unrel := deploy(t, "cart", "astronomy/local/cart", at.Add(-15*time.Second))
	rep, err := correlate.Correlate(inc, []change.Event{unrel}, correlate.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	// May appear as weak candidate (temporal+env) but not strong (no overlap).
	if rep.StrongCandidate {
		t.Fatal("unrelated must not be strong")
	}
}

func TestNoChangeIncident(t *testing.T) {
	at := time.Date(2026, 10, 9, 16, 0, 0, 0, time.UTC)
	inc := mustIncident(t, "astronomy/local/payment", at)
	rep, err := correlate.Correlate(inc, nil, correlate.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !rep.NoStrongCandidate {
		t.Fatal("expected no strong candidate")
	}
	cr, err := changeeval.Evaluate(changeeval.Case{
		Name: "no-change", IncidentID: inc.IncidentID, ExpectNoStrongCandidate: true, Notes: "no-change",
	}, rep)
	if err != nil || !cr.NoCandidateCorrect {
		t.Fatalf("eval: %+v %v", cr, err)
	}
}

func TestChangeWithoutIncidentNoCorrelation(t *testing.T) {
	// No incident → callers must not invent correlation; package requires incident.Validate.
	_, err := correlate.Correlate(incident.Incident{}, []change.Event{}, correlate.DefaultConfig())
	if err == nil {
		t.Fatal("empty incident must fail")
	}
}

func TestMultipleCandidatesRanking(t *testing.T) {
	at := time.Date(2026, 10, 9, 16, 0, 0, 0, time.UTC)
	inc := mustIncident(t, "astronomy/local/payment", at)
	c1 := deploy(t, "c1", "astronomy/local/payment", at.Add(-90*time.Second))
	c2 := deploy(t, "c2", "astronomy/local/payment", at.Add(-30*time.Second))
	c3 := deploy(t, "c3", "astronomy/local/payment", at.Add(-120*time.Second))
	rep, err := correlate.Correlate(inc, []change.Event{c1, c2, c3}, correlate.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Top3) != 3 || rep.Top1.ChangeID != c2.ChangeID {
		t.Fatalf("rank: top1=%v top3=%v", rep.Top1, rep.Top3)
	}
}

func TestObservationInferenceAndNoGTLeak(t *testing.T) {
	at := time.Date(2026, 10, 9, 16, 0, 0, 0, time.UTC)
	inc := mustIncident(t, "astronomy/local/payment", at)
	ch := deploy(t, "pay", "astronomy/local/payment", at.Add(-38*time.Second))
	rep, err := correlate.Correlate(inc, []change.Event{ch}, correlate.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	blob := strings.ToLower(strings.Join(rep.Observations, " ") + strings.Join(rep.Inferences, " ") + strings.Join(rep.Limitations, " "))
	for _, bad := range []string{"scenario_id", "ground_truth", "flagd", "paymentfailure", "caused the"} {
		if strings.Contains(blob, bad) {
			t.Fatalf("leak/causation: %s in %s", bad, blob)
		}
	}
	if !strings.Contains(blob, "not proven causation") {
		t.Fatal("missing causation caveat")
	}
	top := ch
	text := correlate.FormatAgentContext(inc, rep, &top)
	if strings.Contains(strings.ToLower(text), "caused") && !strings.Contains(strings.ToLower(text), "not proven") {
		t.Fatal("agent context causation")
	}
	if len(text) > 2000 {
		t.Fatalf("agent context too large: %d", len(text))
	}
	brief := correlate.Brief(inc, rep)
	if !brief.StrongCandidate || brief.Caveat == "" {
		t.Fatalf("brief: %+v", brief)
	}
}

func TestScoreBreakdownDocumented(t *testing.T) {
	at := time.Date(2026, 10, 9, 16, 0, 0, 0, time.UTC)
	inc := mustIncident(t, "astronomy/local/payment", at)
	ch := deploy(t, "pay", "astronomy/local/payment", at.Add(-60*time.Second))
	rep, _ := correlate.Correlate(inc, []change.Event{ch}, correlate.DefaultConfig())
	bd := rep.Top1.ScoreBreakdown
	sum := bd.TemporalPoints + bd.PrimaryPoints + bd.TopologyPoints + bd.EnvironmentPoints
	if sum != rep.Top1.Score {
		t.Fatalf("breakdown %v != score %v", sum, rep.Top1.Score)
	}
	if bd.PrimaryPoints <= 0 || bd.TemporalPoints <= 0 {
		t.Fatalf("expected primary+temporal points: %+v", bd)
	}
}

func TestChangeEvalTop1Top3(t *testing.T) {
	at := time.Date(2026, 10, 9, 16, 0, 0, 0, time.UTC)
	inc := mustIncident(t, "astronomy/local/payment", at)
	want := deploy(t, "want", "astronomy/local/payment", at.Add(-20*time.Second))
	other := deploy(t, "other", "astronomy/local/payment", at.Add(-100*time.Second))
	rep, _ := correlate.Correlate(inc, []change.Event{other, want}, correlate.DefaultConfig())
	cr, err := changeeval.Evaluate(changeeval.Case{
		Name: "attr", IncidentID: inc.IncidentID, ExpectedTop1ChangeID: want.ChangeID,
	}, rep)
	if err != nil || !cr.Top1Correct || !cr.Top3Correct {
		t.Fatalf("%+v %v", cr, err)
	}
	sum := changeeval.Summarize([]changeeval.CaseResult{cr})
	if sum.Top1Correct != 1 || sum.AttributionEligible != 1 {
		t.Fatalf("%+v", sum)
	}
}

func TestImportBoundaryNoScenario(t *testing.T) {
	// Compile-time / package doc guarantee: correlate must not import scenario.
	// Runtime string guard on scorer version.
	if correlate.ScorerVersion == "" || strings.Contains(correlate.ScorerVersion, "scenario") {
		t.Fatal(correlate.ScorerVersion)
	}
}

func ptr(t time.Time) *time.Time { return &t }
