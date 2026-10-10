package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	pulseapi "github.com/yashg4509/perch/internal/pulse/api"
	"github.com/yashg4509/perch/internal/pulse/change"
	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/incident"
)

func TestVizMux_PulseAndExistingRoutes(t *testing.T) {
	stackDir := writeVizTestStack(t)
	t.Chdir(stackDir)

	pulseDir := t.TempDir()
	t.Setenv("PERCH_PULSE_DIR", pulseDir)

	first := time.Date(2026, 10, 10, 5, 0, 0, 0, time.UTC)
	last := first.Add(time.Minute)
	inc := incident.Incident{
		SchemaVersion:      incident.SchemaVersion,
		IncidentID:         incident.IncidentIDFor("astronomy/local/shipping", first),
		PrimaryServiceID:   "astronomy/local/shipping",
		Environment:        "local",
		FindingIDs:         []string{"f1"},
		SignalTypes:        []string{"latency_ms"},
		FirstDetectedAt:    first,
		LastObservedAt:     last,
		AffectedServiceIDs: []string{"astronomy/local/shipping"},
		Before:             incident.PhaseSnapshot{Phase: "before", Window: incident.Window{Start: first.Add(-time.Minute), End: first}},
		During: incident.PhaseSnapshot{
			Phase: "during", Window: incident.Window{Start: first, End: last}, Available: true,
			Digests: []incident.SampleDigest{{Signal: detect.SignalLatencyMS, Count: 1, Median: 10, Min: 10, Max: 10, FirstAt: first, LastAt: last}},
		},
		After:        incident.PhaseSnapshot{Phase: "after", Window: incident.Window{Start: last, End: last}},
		Observations: []string{"observed latency regression"},
		Inferences:   []string{"possible deploy correlation; not proven causation"},
		Limitations:  []string{"fixture"},
		Summary:      "shipping latency fixture",
	}
	if err := incident.NewFileStore(pulseDir).Append(inc); err != nil {
		t.Fatal(err)
	}
	deployed := first.Add(-2 * time.Minute)
	ev, err := change.Record(change.RecordInput{
		Type: change.ChangeDeployment, ServiceIDs: []string{"astronomy/local/shipping"},
		Environment: "local", Title: "sim", Summary: "marker", DeployedAt: &deployed,
		Simulated: true, Source: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := change.NewFileStore(pulseDir).Append(ev); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		serveGraphJSON(w, r, "dev")
	})
	if err := registerPulseAPI(mux, "local"); err != nil {
		t.Fatal(err)
	}

	// Existing Perch graph route still works.
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/graph?env=dev", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("graph status=%d body=%s", rr.Code, rr.Body.String())
	}
	var graph map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &graph); err != nil {
		t.Fatal(err)
	}
	if graph["appName"] == nil && graph["nodes"] == nil {
		t.Fatalf("unexpected graph payload: %s", rr.Body.String())
	}

	// Pulse services.
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/pulse/services", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("services=%d %s", rr.Code, rr.Body.String())
	}
	if pulseapi.ContainsGroundTruthLeak(rr.Body.String()) {
		t.Fatal("gt leak")
	}

	// Incident detail with correlation.
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/pulse/incidents/"+inc.IncidentID, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("incident detail=%d %s", rr.Code, rr.Body.String())
	}
	var detail pulseapi.IncidentDetailResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Correlation == nil {
		t.Fatal("expected correlation")
	}
}

func TestVizPulseDataDir_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PERCH_PULSE_DIR", dir)
	got, err := vizPulseDataDir()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(got) != filepath.Clean(dir) {
		t.Fatalf("got %q want %q", got, dir)
	}
}
