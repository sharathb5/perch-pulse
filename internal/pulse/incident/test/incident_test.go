package incident_test

import (
	"strings"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/incident"
)

func baseFinding(at time.Time) detect.Finding {
	return detect.Finding{
		SchemaVersion:   detect.SchemaVersion,
		FindingID:       detect.FindingIDFor("astronomy/local/payment", detect.SignalErrorRate, at),
		DetectorVersion: detect.DetectorVersion,
		ServiceID:       "astronomy/local/payment",
		SignalType:      detect.SignalErrorRate,
		ObservedWindow:  detect.Window{Start: at.Add(-40 * time.Second), End: at},
		BaselineWindow:  detect.Window{Start: at.Add(-3 * time.Minute), End: at.Add(-40 * time.Second)},
		ObservedValue:   0.48,
		BaselineValue:   0.004,
		Threshold:       0.05,
		Score:           2,
		Severity:        detect.SeverityCritical,
		FirstDetectedAt: at,
		LastObservedAt:  at.Add(30 * time.Second),
		Summary:         "observed elevated error rate (correlation only, not causation)",
		CandidateImpact: []detect.ImpactCandidate{{
			ServiceID: "astronomy/local/frontend",
			Rank:      2,
			Score:     0.5,
			Relation:  "downstream_of_primary",
			Note:      "configured dependent; correlation, not causation",
		}},
	}
}

func TestBuildBeforeDuringAfter(t *testing.T) {
	if err := incident.ValidateReportsSecretFieldNames(); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)
	f := baseFinding(at)
	var samples []detect.Sample
	for i := 0; i < 5; i++ {
		samples = append(samples, detect.Sample{
			ServiceID: "astronomy/local/payment", Signal: detect.SignalErrorRate,
			Value: 0.01, ObservedAt: at.Add(-90 * time.Second).Add(time.Duration(i) * 10 * time.Second), Source: "test",
		})
	}
	for i := 0; i < 5; i++ {
		samples = append(samples, detect.Sample{
			ServiceID: "astronomy/local/payment", Signal: detect.SignalErrorRate,
			Value: 0.45, ObservedAt: at.Add(time.Duration(i) * 5 * time.Second), Source: "test",
		})
	}
	rec := at.Add(60 * time.Second)
	f.RecoveredAt = &rec
	for i := 0; i < 3; i++ {
		samples = append(samples, detect.Sample{
			ServiceID: "astronomy/local/payment", Signal: detect.SignalErrorRate,
			Value: 0.02, ObservedAt: rec.Add(time.Duration(i) * 10 * time.Second), Source: "test",
		})
	}
	inc, err := incident.Build(incident.BuildInput{Findings: []detect.Finding{f}, Samples: samples})
	if err != nil {
		t.Fatal(err)
	}
	if !inc.Before.Available || !inc.During.Available || !inc.After.Available {
		t.Fatalf("phases: before=%v during=%v after=%v", inc.Before.Available, inc.During.Available, inc.After.Available)
	}
	if inc.Environment != "local" {
		t.Fatalf("env %q", inc.Environment)
	}
	joined := strings.ToLower(strings.Join(inc.Observations, " ") + strings.Join(inc.Inferences, " "))
	for _, bad := range []string{"scenario", "ground.truth", "ground_truth", "flagd"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("ground-truth leakage: %s", bad)
		}
	}
	if strings.Contains(joined, "caused by") {
		t.Fatal("causation language")
	}
}

func TestNoSamplesStillBuilds(t *testing.T) {
	at := time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)
	inc, err := incident.Build(incident.BuildInput{Findings: []detect.Finding{baseFinding(at)}})
	if err != nil {
		t.Fatal(err)
	}
	if inc.Before.Available {
		t.Fatal("expected unavailable before")
	}
	store := incident.NewMemory()
	if err := store.Append(inc); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(inc); err != nil {
		t.Fatal(err)
	}
}

func TestObservationInferenceSeparation(t *testing.T) {
	at := time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)
	inc, err := incident.Build(incident.BuildInput{Findings: []detect.Finding{baseFinding(at)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(inc.Observations) == 0 || len(inc.Inferences) == 0 || len(inc.Limitations) == 0 {
		t.Fatal("need obs/inf/lim")
	}
}

func TestPartialRecoveryKeepsIncidentOpen(t *testing.T) {
	at := time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)
	errF := baseFinding(at)
	lat := baseFinding(at)
	lat.SignalType = detect.SignalLatencyMS
	lat.FindingID = detect.FindingIDFor(lat.ServiceID, detect.SignalLatencyMS, at)
	lat.Summary = "observed elevated latency (correlation only, not causation)"
	rec := at.Add(30 * time.Second)
	errF.RecoveredAt = &rec
	// latency still open
	inc, err := incident.Build(incident.BuildInput{Findings: []detect.Finding{errF, lat}})
	if err != nil {
		t.Fatal(err)
	}
	if !inc.Open() || inc.RecoveredAt != nil {
		t.Fatalf("expected open incident when one finding unrecovered: recovered=%v", inc.RecoveredAt)
	}
}
