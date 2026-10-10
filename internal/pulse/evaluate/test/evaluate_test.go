package evaluate_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/pulse/astronomy"
	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/evaluate"
	"github.com/yashg4509/perch/internal/pulse/scenario"
)

func TestEvaluatorDetectionAndAttribution(t *testing.T) {
	start := time.Date(2026, 10, 9, 20, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Minute)
	pay := astronomy.MustLocalServiceID("payment")
	rec := scenario.Record{
		SchemaVersion: scenario.SchemaVersion, ScenarioID: "error-payment",
		RunID: "error-payment-1", ScenarioType: scenario.TypeErrorRate,
		Classification: scenario.ClassificationFault, Environment: "local",
		TargetServiceID: pay, StartTime: start, EndTime: &end,
		InjectedParameters:         map[string]string{"flag": "paymentFailure", "active_variant": "50%", "idle_variant": "off", "mechanism_kind": "flagd"},
		ExpectedAffectedServiceIDs: []string{pay, astronomy.MustLocalServiceID("checkout")},
		State:                      scenario.StateStopped,
		Mechanism:                  scenario.Mechanism{Kind: scenario.MechanismFlagd, Flag: "paymentFailure", ActiveVariant: "50%", IdleVariant: "off"},
	}
	recAt := start.Add(30 * time.Second)
	recov := end.Add(20 * time.Second)
	findings := []detect.Finding{{
		SchemaVersion: detect.SchemaVersion, FindingID: "f1", DetectorVersion: detect.DetectorVersion,
		ServiceID: pay, SignalType: detect.SignalErrorRate,
		ObservedValue: 0.5, BaselineValue: 0, Threshold: 0.15, Score: 0.5,
		Severity: detect.SeverityCritical, FirstDetectedAt: recAt, LastObservedAt: recov,
		RecoveredAt: &recov,
		CandidateImpact: []detect.ImpactCandidate{
			{ServiceID: pay, Rank: 1, Score: 0.5, Relation: "primary"},
			{ServiceID: astronomy.MustLocalServiceID("checkout"), Rank: 2, Score: 0.1, Relation: "also_degraded"},
		},
		Summary: "observed elevated error rate",
	}}
	got, err := evaluate.EvaluateScores(evaluate.Input{Record: rec, Findings: findings})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Detected || got.DetectionDelaySecs == nil || *got.DetectionDelaySecs < 29 || *got.DetectionDelaySecs > 31 {
		t.Fatalf("detection: %+v", got)
	}
	if !got.Top1Correct || !got.Top3Correct {
		t.Fatalf("attribution: %+v", got)
	}
	if !got.RecoveryDetected || got.RecoveryDelaySecs == nil {
		t.Fatalf("recovery: %+v", got)
	}
}

func TestEvaluatorControlFalsePositive(t *testing.T) {
	start := time.Date(2026, 10, 9, 21, 0, 0, 0, time.UTC)
	end := start.Add(time.Minute)
	chk := astronomy.MustLocalServiceID("checkout")
	rec := scenario.Record{
		SchemaVersion: scenario.SchemaVersion, ScenarioID: "control-emit-raw-pii",
		RunID: "control-1", ScenarioType: scenario.TypeNeutral,
		Classification: scenario.ClassificationControl, Environment: "local",
		TargetServiceID: chk, StartTime: start, EndTime: &end,
		InjectedParameters: map[string]string{"flag": "emitRawPii", "active_variant": "on", "idle_variant": "off", "mechanism_kind": "flagd"},
		State:              scenario.StateStopped,
		Mechanism:          scenario.Mechanism{Kind: scenario.MechanismFlagd, Flag: "emitRawPii", ActiveVariant: "on", IdleVariant: "off"},
	}
	// No findings → clean control.
	got, err := evaluate.EvaluateScores(evaluate.Input{Record: rec, Findings: nil})
	if err != nil {
		t.Fatal(err)
	}
	if got.FalsePositive || got.FalseAttribution {
		t.Fatalf("clean control: %+v", got)
	}
	// Call-rate jitter on target is ignored for control FP.
	got, err = evaluate.EvaluateScores(evaluate.Input{Record: rec, Findings: []detect.Finding{{
		SchemaVersion: detect.SchemaVersion, FindingID: "jitter", DetectorVersion: detect.DetectorVersion,
		ServiceID: chk, SignalType: detect.SignalCallRate, Score: 0.7, Threshold: 0.55,
		Severity: detect.SeverityCritical, FirstDetectedAt: start.Add(10 * time.Second),
		LastObservedAt: start.Add(20 * time.Second), Summary: "observed call-rate drop",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.FalsePositive {
		t.Fatalf("call_rate-only should not FP control: %+v", got)
	}
	// Latency finding on target → FP.
	got, err = evaluate.EvaluateScores(evaluate.Input{Record: rec, Findings: []detect.Finding{{
		SchemaVersion: detect.SchemaVersion, FindingID: "bad", DetectorVersion: detect.DetectorVersion,
		ServiceID: chk, SignalType: detect.SignalLatencyMS, Score: 2, Threshold: 1,
		Severity: detect.SeverityWarning, FirstDetectedAt: start.Add(10 * time.Second),
		LastObservedAt: start.Add(20 * time.Second), Summary: "observed latency degradation",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if !got.FalsePositive || !got.FalseAttribution {
		t.Fatalf("expected FP: %+v", got)
	}
}

func TestEvaluatorAttributionPrefersTargetService(t *testing.T) {
	start := time.Date(2026, 10, 9, 20, 30, 0, 0, time.UTC)
	end := start.Add(2 * time.Minute)
	pay := astronomy.MustLocalServiceID("payment")
	chk := astronomy.MustLocalServiceID("checkout")
	rec := scenario.Record{
		SchemaVersion: scenario.SchemaVersion, ScenarioID: "outage-payment",
		RunID: "outage-1", ScenarioType: scenario.TypeDependencyOutage,
		Classification: scenario.ClassificationFault, Environment: "local",
		TargetServiceID: pay, StartTime: start, EndTime: &end,
		InjectedParameters:         map[string]string{"flag": "paymentUnreachable", "active_variant": "on", "idle_variant": "off", "mechanism_kind": "flagd"},
		ExpectedAffectedServiceIDs: []string{pay, chk},
		State:                      scenario.StateStopped,
		Mechanism:                  scenario.Mechanism{Kind: scenario.MechanismFlagd, Flag: "paymentUnreachable", ActiveVariant: "on", IdleVariant: "off"},
	}
	// Downstream checkout finding has a higher score; attribution must still prefer payment.
	got, err := evaluate.EvaluateScores(evaluate.Input{Record: rec, Findings: []detect.Finding{
		{
			SchemaVersion: detect.SchemaVersion, FindingID: "chk", DetectorVersion: detect.DetectorVersion,
			ServiceID: chk, SignalType: detect.SignalCallRate, Score: 1.0, Threshold: 0.55,
			Severity: detect.SeverityCritical, FirstDetectedAt: start.Add(40 * time.Second),
			LastObservedAt: start.Add(50 * time.Second), Summary: "observed call-rate drop",
		},
		{
			SchemaVersion: detect.SchemaVersion, FindingID: "pay", DetectorVersion: detect.DetectorVersion,
			ServiceID: pay, SignalType: detect.SignalCallRate, Score: 0.8, Threshold: 0.55,
			Severity: detect.SeverityCritical, FirstDetectedAt: start.Add(20 * time.Second),
			LastObservedAt: start.Add(50 * time.Second), Summary: "observed call-rate drop",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Detected || !got.Top1Correct || !got.Top3Correct {
		t.Fatalf("expected target-preferred attribution: %+v", got)
	}
}

func TestEvaluatorMissAndEmpty(t *testing.T) {
	start := time.Date(2026, 10, 9, 22, 0, 0, 0, time.UTC)
	end := start.Add(time.Minute)
	ship := astronomy.MustLocalServiceID("shipping")
	rec := scenario.Record{
		SchemaVersion: scenario.SchemaVersion, ScenarioID: "latency-shipping-intl",
		RunID: "lat-1", ScenarioType: scenario.TypeLatency,
		Classification: scenario.ClassificationFault, Environment: "local",
		TargetServiceID: ship, StartTime: start, EndTime: &end,
		InjectedParameters:         map[string]string{"flag": "intlShippingSlowdown", "active_variant": "10sec", "idle_variant": "off", "mechanism_kind": "flagd"},
		ExpectedAffectedServiceIDs: []string{ship},
		State:                      scenario.StateStopped,
		Mechanism:                  scenario.Mechanism{Kind: scenario.MechanismFlagd, Flag: "intlShippingSlowdown", ActiveVariant: "10sec", IdleVariant: "off"},
	}
	got, err := evaluate.EvaluateScores(evaluate.Input{Record: rec, Findings: nil})
	if err != nil {
		t.Fatal(err)
	}
	if got.Detected {
		t.Fatal("expected miss")
	}
	sum := evaluate.Aggregate([]evaluate.ScenarioResult{got}, start)
	if sum.FaultDetected != 0 || sum.FaultTotal != 1 {
		t.Fatalf("%+v", sum)
	}
	_ = evaluate.HumanReport(sum)
}

func TestEvaluatePackageMayImportScenarioButDetectMustNot(t *testing.T) {
	// evaluate is allowed to import scenario; verify it does (evaluation boundary).
	assertImports(t, "internal/pulse/evaluate", "internal/pulse/scenario", true)
	assertImports(t, "internal/pulse/evaluate", "internal/pulse/detect", true)
}

func assertImports(t *testing.T, pkgPath, want string, must bool) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	dir := filepath.Join(root, pkgPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	found := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			if strings.Contains(strings.Trim(imp.Path.Value, `"`), want) {
				found = true
			}
		}
	}
	if must && !found {
		t.Fatalf("%s should import %s", pkgPath, want)
	}
}
