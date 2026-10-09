package detect_test

import (
	"encoding/json"
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
)

func TestInsufficientHistoryNoFinding(t *testing.T) {
	d := detect.New(detect.DefaultConfig())
	base := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	d.SetClock(func() time.Time { return base })
	svc := astronomy.MustLocalServiceID("shipping")
	// Only 2 samples — below MinBaselineSamples.
	mustIngest(t, d, synth(svc, detect.SignalLatencyMS, base, 20, 10, 2))
	findings, err := d.Evaluate(base.Add(30 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

func TestLatencyRegressionDetection(t *testing.T) {
	cfg := detect.DefaultConfig()
	cfg.BaselineWindow = 2 * time.Minute
	cfg.CurrentWindow = 30 * time.Second
	cfg.MinBaselineSamples = 4
	cfg.MinCurrentSamples = 2
	cfg.LatencyRobustZ = 0 // disable MAD gate for fixture simplicity
	cfg.MinCallRateForLatency = 0.05
	d := detect.New(cfg)
	base := time.Date(2026, 10, 9, 13, 0, 0, 0, time.UTC)
	svc := astronomy.MustLocalServiceID("shipping")

	// Baseline: ~200ms every 20s for 2m (p99-scale)
	var samples []detect.Sample
	for i := 0; i < 6; i++ {
		t0 := base.Add(time.Duration(i) * 20 * time.Second)
		samples = append(samples,
			detect.Sample{ServiceID: svc, Signal: detect.SignalLatencyMS, Value: 200, ObservedAt: t0, Source: "test"},
			detect.Sample{ServiceID: svc, Signal: detect.SignalCallRate, Value: 0.5, ObservedAt: t0, Source: "test"},
		)
	}
	// Current: ~5000ms (demo-scale injected delay)
	for i := 0; i < 3; i++ {
		t0 := base.Add(2*time.Minute + time.Duration(i)*10*time.Second)
		samples = append(samples,
			detect.Sample{ServiceID: svc, Signal: detect.SignalLatencyMS, Value: 5000, ObservedAt: t0, Source: "test"},
			detect.Sample{ServiceID: svc, Signal: detect.SignalCallRate, Value: 0.5, ObservedAt: t0, Source: "test"},
		)
	}
	mustIngest(t, d, samples)
	now := base.Add(2*time.Minute + 25*time.Second)
	findings, err := d.Evaluate(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) == 0 {
		t.Fatal("expected latency finding")
	}
	f := findings[0]
	if f.ServiceID != svc || f.SignalType != detect.SignalLatencyMS {
		t.Fatalf("unexpected finding %+v", f)
	}
	if f.Score < 1 {
		t.Fatalf("score too low: %v", f.Score)
	}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestErrorRateAndOutageDetection(t *testing.T) {
	cfg := detect.DefaultConfig()
	cfg.BaselineWindow = 2 * time.Minute
	cfg.CurrentWindow = 30 * time.Second
	cfg.MinBaselineSamples = 4
	cfg.MinCurrentSamples = 2
	d := detect.New(cfg)
	base := time.Date(2026, 10, 9, 14, 0, 0, 0, time.UTC)
	pay := astronomy.MustLocalServiceID("payment")

	var samples []detect.Sample
	for i := 0; i < 6; i++ {
		t0 := base.Add(time.Duration(i) * 20 * time.Second)
		samples = append(samples,
			detect.Sample{ServiceID: pay, Signal: detect.SignalErrorRate, Value: 0.01, ObservedAt: t0, Source: "test"},
			detect.Sample{ServiceID: pay, Signal: detect.SignalCallRate, Value: 1.0, ObservedAt: t0, Source: "test"},
		)
	}
	for i := 0; i < 3; i++ {
		t0 := base.Add(2*time.Minute + time.Duration(i)*10*time.Second)
		samples = append(samples,
			detect.Sample{ServiceID: pay, Signal: detect.SignalErrorRate, Value: 0.55, ObservedAt: t0, Source: "test"},
			detect.Sample{ServiceID: pay, Signal: detect.SignalCallRate, Value: 0.2, ObservedAt: t0, Source: "test"},
		)
	}
	mustIngest(t, d, samples)
	findings, err := d.Evaluate(base.Add(2*time.Minute + 25*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	var sawErr, sawCall bool
	for _, f := range findings {
		if f.SignalType == detect.SignalErrorRate {
			sawErr = true
			if f.Severity != detect.SeverityCritical {
				t.Fatalf("expected critical for high error rate, got %s", f.Severity)
			}
		}
		if f.SignalType == detect.SignalCallRate {
			sawCall = true
		}
	}
	if !sawErr {
		t.Fatal("missing error_rate finding")
	}
	if !sawCall {
		t.Fatal("missing call_rate (outage) finding")
	}
}

func TestRecoveryDetection(t *testing.T) {
	cfg := detect.DefaultConfig()
	cfg.BaselineWindow = 2 * time.Minute
	cfg.CurrentWindow = 30 * time.Second
	cfg.MinBaselineSamples = 4
	cfg.MinCurrentSamples = 2
	cfg.LatencyRobustZ = 0
	cfg.MinCallRateForLatency = 0.05
	d := detect.New(cfg)
	base := time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)
	svc := astronomy.MustLocalServiceID("shipping")
	var samples []detect.Sample
	for i := 0; i < 6; i++ {
		t0 := base.Add(time.Duration(i) * 20 * time.Second)
		samples = append(samples,
			detect.Sample{ServiceID: svc, Signal: detect.SignalLatencyMS, Value: 200, ObservedAt: t0, Source: "test"},
			detect.Sample{ServiceID: svc, Signal: detect.SignalCallRate, Value: 0.5, ObservedAt: t0, Source: "test"},
		)
	}
	for i := 0; i < 3; i++ {
		t0 := base.Add(2*time.Minute + time.Duration(i)*10*time.Second)
		samples = append(samples,
			detect.Sample{ServiceID: svc, Signal: detect.SignalLatencyMS, Value: 5000, ObservedAt: t0, Source: "test"},
			detect.Sample{ServiceID: svc, Signal: detect.SignalCallRate, Value: 0.5, ObservedAt: t0, Source: "test"},
		)
	}
	mustIngest(t, d, samples)
	now := base.Add(2*time.Minute + 25*time.Second)
	findings, _ := d.Evaluate(now)
	if len(findings) == 0 || !findings[0].Open() {
		t.Fatalf("expected open finding: %+v", findings)
	}
	// Recover: more healthy samples filling current window.
	var recover []detect.Sample
	for i := 0; i < 4; i++ {
		t0 := now.Add(time.Duration(i+1) * 10 * time.Second)
		recover = append(recover,
			detect.Sample{ServiceID: svc, Signal: detect.SignalLatencyMS, Value: 210, ObservedAt: t0, Source: "test"},
			detect.Sample{ServiceID: svc, Signal: detect.SignalCallRate, Value: 0.5, ObservedAt: t0, Source: "test"},
		)
	}
	mustIngest(t, d, recover)
	recNow := now.Add(45 * time.Second)
	findings, _ = d.Evaluate(recNow)
	var recovered bool
	for _, f := range findings {
		if f.RecoveredAt != nil {
			recovered = true
			if f.RecoveredAt.Before(now) {
				t.Fatal("recovered_at before detection")
			}
		}
	}
	if !recovered {
		t.Fatalf("expected recovery, got %+v", findings)
	}
}

func TestNoisyButHealthyNoFire(t *testing.T) {
	cfg := detect.DefaultConfig()
	cfg.BaselineWindow = 2 * time.Minute
	cfg.CurrentWindow = 30 * time.Second
	cfg.MinBaselineSamples = 4
	cfg.MinCurrentSamples = 2
	cfg.LatencyRobustZ = 0
	d := detect.New(cfg)
	base := time.Date(2026, 10, 9, 16, 0, 0, 0, time.UTC)
	svc := astronomy.MustLocalServiceID("shipping")
	var samples []detect.Sample
	for i := 0; i < 10; i++ {
		v := 40.0 + float64(i%3) // 40-42 jitter
		samples = append(samples, detect.Sample{
			ServiceID: svc, Signal: detect.SignalLatencyMS, Value: v,
			ObservedAt: base.Add(time.Duration(i) * 15 * time.Second), Source: "test",
		})
	}
	mustIngest(t, d, samples)
	findings, _ := d.Evaluate(base.Add(2*time.Minute + 20*time.Second))
	if len(findings) != 0 {
		t.Fatalf("noisy healthy should not fire: %+v", findings)
	}
}

func TestNeutralControlSamplesNoFire(t *testing.T) {
	// emitRawPii-like: stable latency/error/call rates.
	cfg := detect.DefaultConfig()
	cfg.BaselineWindow = 2 * time.Minute
	cfg.CurrentWindow = 30 * time.Second
	cfg.MinBaselineSamples = 4
	cfg.MinCurrentSamples = 2
	d := detect.New(cfg)
	base := time.Date(2026, 10, 9, 17, 0, 0, 0, time.UTC)
	chk := astronomy.MustLocalServiceID("checkout")
	var samples []detect.Sample
	for i := 0; i < 10; i++ {
		t0 := base.Add(time.Duration(i) * 15 * time.Second)
		samples = append(samples,
			detect.Sample{ServiceID: chk, Signal: detect.SignalLatencyMS, Value: 30, ObservedAt: t0, Source: "test"},
			detect.Sample{ServiceID: chk, Signal: detect.SignalErrorRate, Value: 0.0, ObservedAt: t0, Source: "test"},
			detect.Sample{ServiceID: chk, Signal: detect.SignalCallRate, Value: 0.5, ObservedAt: t0, Source: "test"},
		)
	}
	mustIngest(t, d, samples)
	findings, _ := d.Evaluate(base.Add(2*time.Minute + 20*time.Second))
	if len(findings) != 0 {
		t.Fatalf("control-like telemetry fired: %+v", findings)
	}
}

func TestDuplicateAndOutOfOrder(t *testing.T) {
	d := detect.New(detect.DefaultConfig())
	svc := astronomy.MustLocalServiceID("payment")
	t1 := time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC)
	s := detect.Sample{ServiceID: svc, Signal: detect.SignalErrorRate, Value: 0.1, ObservedAt: t1, Source: "test"}
	mustIngest(t, d, []detect.Sample{s})
	mustIngest(t, d, []detect.Sample{s}) // duplicate
	later := s
	later.ObservedAt = t1.Add(-time.Minute)
	later.Value = 0.05
	mustIngest(t, d, []detect.Sample{later})
	if d.SeriesCount() != 1 {
		t.Fatalf("series=%d", d.SeriesCount())
	}
	// Conflicting value at same timestamp replaces prior (no double-count).
	conflict := s
	conflict.Value = 0.9
	mustIngest(t, d, []detect.Sample{conflict})
	if d.SeriesCount() != 1 {
		t.Fatalf("series after conflict=%d", d.SeriesCount())
	}
}

func TestStaleTelemetryDoesNotMarkRecovered(t *testing.T) {
	cfg := detect.DefaultConfig()
	cfg.BaselineWindow = 2 * time.Minute
	cfg.CurrentWindow = 30 * time.Second
	cfg.MinBaselineSamples = 4
	cfg.MinCurrentSamples = 2
	cfg.LatencyRobustZ = 0
	cfg.MinCallRateForLatency = 0.05
	d := detect.New(cfg)
	base := time.Date(2026, 10, 9, 15, 30, 0, 0, time.UTC)
	svc := astronomy.MustLocalServiceID("shipping")
	var samples []detect.Sample
	for i := 0; i < 6; i++ {
		t0 := base.Add(time.Duration(i) * 20 * time.Second)
		samples = append(samples,
			detect.Sample{ServiceID: svc, Signal: detect.SignalLatencyMS, Value: 200, ObservedAt: t0, Source: "test"},
			detect.Sample{ServiceID: svc, Signal: detect.SignalCallRate, Value: 0.5, ObservedAt: t0, Source: "test"},
		)
	}
	for i := 0; i < 3; i++ {
		t0 := base.Add(2*time.Minute + time.Duration(i)*10*time.Second)
		samples = append(samples,
			detect.Sample{ServiceID: svc, Signal: detect.SignalLatencyMS, Value: 5000, ObservedAt: t0, Source: "test"},
			detect.Sample{ServiceID: svc, Signal: detect.SignalCallRate, Value: 0.5, ObservedAt: t0, Source: "test"},
		)
	}
	mustIngest(t, d, samples)
	now := base.Add(2*time.Minute + 25*time.Second)
	findings, _ := d.Evaluate(now)
	if len(findings) == 0 || !findings[0].Open() {
		t.Fatalf("expected open finding: %+v", findings)
	}
	// Jump far ahead with no new samples — insufficient current window must not recover.
	staleNow := now.Add(10 * time.Minute)
	findings, _ = d.Evaluate(staleNow)
	for _, f := range findings {
		if f.SignalType == detect.SignalLatencyMS && f.RecoveredAt != nil {
			t.Fatalf("stale evaluate must not recover: %+v", f)
		}
	}
	if len(d.OpenFindings()) == 0 {
		t.Fatal("expected finding to remain open under stale telemetry")
	}
}

func TestFindingIDDeterminism(t *testing.T) {
	t0 := time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC)
	a := detect.FindingIDFor("astronomy/local/payment", detect.SignalErrorRate, t0)
	b := detect.FindingIDFor("astronomy/local/payment", detect.SignalErrorRate, t0)
	if a != b || a == "" {
		t.Fatalf("%q vs %q", a, b)
	}
}

func TestAttributionRanking(t *testing.T) {
	mapping, err := astronomy.Load()
	if err != nil {
		t.Fatal(err)
	}
	pay := astronomy.MustLocalServiceID("payment")
	chk := astronomy.MustLocalServiceID("checkout")
	now := time.Date(2026, 10, 9, 19, 0, 0, 0, time.UTC)
	findings := []detect.Finding{
		{
			SchemaVersion: detect.SchemaVersion, FindingID: "f-pay", DetectorVersion: detect.DetectorVersion,
			ServiceID: pay, SignalType: detect.SignalErrorRate, ObservedValue: 0.5, BaselineValue: 0.0,
			Threshold: 0.15, Score: 0.5, Severity: detect.SeverityCritical,
			FirstDetectedAt: now, LastObservedAt: now,
			Summary: "observed elevated error rate on payment",
		},
		{
			SchemaVersion: detect.SchemaVersion, FindingID: "f-chk", DetectorVersion: detect.DetectorVersion,
			ServiceID: chk, SignalType: detect.SignalErrorRate, ObservedValue: 0.2, BaselineValue: 0.0,
			Threshold: 0.15, Score: 0.2, Severity: detect.SeverityWarning,
			FirstDetectedAt: now, LastObservedAt: now,
			Summary: "observed elevated error rate on checkout",
		},
	}
	out := detect.Attribute(findings, mapping)
	if len(out[0].CandidateImpact) == 0 {
		t.Fatal("expected candidates")
	}
	if out[0].CandidateImpact[0].ServiceID != pay || out[0].CandidateImpact[0].Relation != "primary" {
		t.Fatalf("primary=%+v", out[0].CandidateImpact[0])
	}
	// checkout depends on payment ⇒ payment finding should list checkout as downstream.
	var sawDown bool
	for _, c := range out[0].CandidateImpact {
		if c.ServiceID == chk && (c.Relation == "also_degraded" || c.Relation == "downstream_of_primary") {
			sawDown = true
		}
	}
	if !sawDown {
		t.Fatalf("expected checkout in impact: %+v", out[0].CandidateImpact)
	}
}

func TestNoGroundTruthLeakageImports(t *testing.T) {
	assertNoImport(t, "internal/pulse/detect", "internal/pulse/scenario")
	assertNoImport(t, "internal/pulse/telem", "internal/pulse/scenario")
	// Findings JSON must not carry scenario fields.
	f := detect.Finding{
		SchemaVersion: detect.SchemaVersion, FindingID: "x", DetectorVersion: "v",
		ServiceID: "astronomy/local/payment", SignalType: detect.SignalErrorRate,
		ObservedValue: 1, BaselineValue: 0, Threshold: 0.1, Score: 1,
		Severity: detect.SeverityWarning, FirstDetectedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		LastObservedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Summary:        "observed degradation",
	}
	b, _ := json.Marshal(f)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, bad := range []string{"scenario_id", "classification", "ground_truth", "run_id", "injected_parameters"} {
		if _, ok := m[bad]; ok {
			t.Fatalf("finding leaked %s", bad)
		}
	}
}

func TestCausationLanguageRejected(t *testing.T) {
	f := detect.Finding{
		SchemaVersion: detect.SchemaVersion, FindingID: "x", DetectorVersion: "v",
		ServiceID: "astronomy/local/payment", SignalType: detect.SignalErrorRate,
		Severity: detect.SeverityWarning, FirstDetectedAt: time.Now().UTC(), LastObservedAt: time.Now().UTC(),
		Summary: "root cause is payment failure",
	}
	if err := f.Validate(); err == nil {
		t.Fatal("expected causation reject")
	}
	f.Summary = "observed elevated error rate (correlation only, not causation)"
	f.FirstDetectedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f.LastObservedAt = f.FirstDetectedAt
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestZeroDivisionEmptyBaseline(t *testing.T) {
	// RelativeIncrease / RobustZ should not panic on zero baseline.
	if detect.RelativeIncrease(1, 0) <= 0 {
		t.Fatal("expected positive relative increase from zero baseline")
	}
	_ = detect.RobustZ(1, 0, 0)
}

func mustIngest(t *testing.T, d *detect.Detector, samples []detect.Sample) {
	t.Helper()
	if err := d.Ingest(samples); err != nil {
		t.Fatal(err)
	}
}

func synth(svc string, sig detect.SignalType, start time.Time, value float64, stepSec, n int) []detect.Sample {
	out := make([]detect.Sample, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, detect.Sample{
			ServiceID: svc, Signal: sig, Value: value,
			ObservedAt: start.Add(time.Duration(i*stepSec) * time.Second), Source: "test",
		})
	}
	return out
}

func assertNoImport(t *testing.T, pkgPath, forbidden string) {
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
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(p, forbidden) {
				t.Fatalf("%s imports %s", pkgPath, p)
			}
		}
	}
}
