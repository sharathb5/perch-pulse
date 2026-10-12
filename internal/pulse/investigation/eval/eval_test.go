package eval_test

import (
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/pulse/astronomy"
	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/incident"
	"github.com/yashg4509/perch/internal/pulse/investigation"
	"github.com/yashg4509/perch/internal/pulse/investigation/eval"
)

func TestEvalCasesDeterministic(t *testing.T) {
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		sig  detect.SignalType
		obs  float64
		base float64
		kind string
	}{
		{"payment_error", detect.SignalErrorRate, 0.41, 0.0, "error_rate_regression"},
		{"shipping_latency", detect.SignalLatencyMS, 900, 50, "latency_regression"},
		{"call_rate_drop", detect.SignalCallRate, 0.0, 0.2, "call_rate_drop"},
	}
	mapping, err := astronomy.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := "astronomy/local/payment"
			if tc.sig == detect.SignalLatencyMS {
				svc = "astronomy/local/shipping"
			}
			f := detect.Finding{
				SchemaVersion: detect.SchemaVersion, FindingID: detect.FindingIDFor(svc, tc.sig, at),
				DetectorVersion: detect.DetectorVersion, ServiceID: svc, SignalType: tc.sig,
				ObservedWindow: detect.Window{Start: at.Add(-40 * time.Second), End: at},
				BaselineWindow: detect.Window{Start: at.Add(-3 * time.Minute), End: at.Add(-40 * time.Second)},
				ObservedValue:  tc.obs, BaselineValue: tc.base, Threshold: 0.05, Score: 2,
				Severity: detect.SeverityCritical, FirstDetectedAt: at, LastObservedAt: at.Add(30 * time.Second),
				Summary: "eval",
			}
			var samples []detect.Sample
			for i := 0; i < 5; i++ {
				samples = append(samples, detect.Sample{ServiceID: svc, Signal: tc.sig, Value: tc.base,
					ObservedAt: at.Add(-90 * time.Second).Add(time.Duration(i) * 10 * time.Second), Source: "test"})
			}
			for i := 0; i < 5; i++ {
				samples = append(samples, detect.Sample{ServiceID: svc, Signal: tc.sig, Value: tc.obs,
					ObservedAt: at.Add(time.Duration(i) * 5 * time.Second), Source: "test"})
			}
			inc, err := incident.Build(incident.BuildInput{Findings: []detect.Finding{f}, Samples: samples, Environment: "local"})
			if err != nil {
				t.Fatal(err)
			}
			inv1, err := investigation.Assemble(investigation.AssembleInput{Incident: inc, Mapping: mapping, Now: at})
			if err != nil {
				t.Fatal(err)
			}
			inv2, err := investigation.Assemble(investigation.AssembleInput{Incident: inc, Mapping: mapping, Now: at})
			if err != nil {
				t.Fatal(err)
			}
			if inv1.ContentFingerprint != inv2.ContentFingerprint {
				t.Fatal("non-deterministic fingerprint")
			}
			m := eval.Score(inv1, eval.Expectation{Name: tc.name, RequireKind: tc.kind})
			if !eval.Pass(m) {
				t.Fatalf("eval failed: %+v", m)
			}
			if m.HypothesisRelevance != 1 {
				t.Fatalf("relevance %v", m.HypothesisRelevance)
			}
		})
	}
}
