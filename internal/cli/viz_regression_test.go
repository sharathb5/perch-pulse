package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/provider"
	pulseapi "github.com/yashg4509/perch/internal/pulse/api"
	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/incident"
	"github.com/yashg4509/perch/internal/stackstatus"
)

// UNIT/INTEGRATION: viz graph/status remain usable when Pulse is absent or corrupt.

func TestVizRegression_GraphAndStatusWithoutPulse(t *testing.T) {
	stackDir := writeVizTestStack(t)
	t.Chdir(stackDir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PERCH_PULSE_DIR", filepath.Join(t.TempDir(), "does-not-exist-yet"))
	prevTok := stackstatus.SetDeployableTokenFnForTest(func(*provider.Spec) (string, bool) {
		return "", false
	})
	t.Cleanup(func() { stackstatus.SetDeployableTokenFnForTest(prevTok) })

	mux := http.NewServeMux()
	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		serveGraphJSON(w, r, "dev")
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		serveStatusJSON(w, r, "dev")
	})
	if rt, err := registerPulseAPI(mux, "dev", context.Background()); err == nil {
		t.Cleanup(rt.Stop)
	}

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/graph?env=dev", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("graph=%d %s", rr.Code, rr.Body.String())
	}
	var graph map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &graph); err != nil {
		t.Fatal(err)
	}
	if graph["nodes"] == nil {
		t.Fatalf("graph missing nodes: %s", rr.Body.String())
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/status?env=dev", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d %s", rr.Code, rr.Body.String())
	}
	var status map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status["env"] != "dev" {
		t.Fatalf("status env=%v", status["env"])
	}
	nodes, _ := status["nodes"].([]any)
	if len(nodes) == 0 {
		t.Fatal("expected status nodes")
	}
}

func TestVizRegression_CorruptPulseArtifactsDoNotCrashCore(t *testing.T) {
	stackDir := writeVizTestStack(t)
	t.Chdir(stackDir)
	pulseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(pulseDir, "incidents"), 0o750); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(pulseDir, "incidents", "broken.json")
	if err := os.WriteFile(bad, []byte(`{"not": "an incident"`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PERCH_PULSE_DIR", pulseDir)
	prevTok := stackstatus.SetDeployableTokenFnForTest(func(*provider.Spec) (string, bool) {
		return "", false
	})
	t.Cleanup(func() { stackstatus.SetDeployableTokenFnForTest(prevTok) })

	mux := http.NewServeMux()
	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		serveGraphJSON(w, r, "dev")
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		serveStatusJSON(w, r, "dev")
	})
	rt, err := registerPulseAPI(mux, "dev", context.Background())
	if err != nil {
		t.Fatalf("pulse mount: %v", err)
	}
	t.Cleanup(rt.Stop)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/graph?env=dev", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("graph crashed under corrupt pulse: %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/status?env=dev", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status crashed under corrupt pulse: %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/pulse/incidents", nil))
	if rr.Code != http.StatusOK && rr.Code < 400 {
		t.Fatalf("unexpected pulse status %d %s", rr.Code, rr.Body.String())
	}
	if rr.Code == http.StatusOK && pulseapi.ContainsGroundTruthLeak(rr.Body.String()) {
		t.Fatal("gt leak")
	}
	if strings.Contains(rr.Body.String(), "scenario_id") {
		t.Fatal("scenario_id leak")
	}
}

func TestVizRegression_PulseIntelligenceDoesNotReplaceStatus(t *testing.T) {
	stackDir := writeVizTestStack(t)
	t.Chdir(stackDir)
	pulseDir := t.TempDir()
	first := time.Date(2026, 10, 10, 22, 0, 0, 0, time.UTC)
	inc := incident.Incident{
		SchemaVersion:      incident.SchemaVersion,
		IncidentID:         incident.IncidentIDFor("astronomy/local/shipping", first),
		PrimaryServiceID:   "astronomy/local/shipping",
		Environment:        "local",
		FindingIDs:         []string{"f1"},
		SignalTypes:        []string{"latency_ms"},
		FirstDetectedAt:    first,
		LastObservedAt:     first.Add(time.Minute),
		AffectedServiceIDs: []string{"astronomy/local/shipping"},
		Before:             incident.PhaseSnapshot{Phase: "before", Window: incident.Window{Start: first.Add(-time.Minute), End: first}},
		During: incident.PhaseSnapshot{
			Phase: "during", Window: incident.Window{Start: first, End: first.Add(time.Minute)}, Available: true,
			Digests: []incident.SampleDigest{{Signal: detect.SignalLatencyMS, Count: 1, Median: 10, Min: 10, Max: 10, FirstAt: first, LastAt: first}},
		},
		After:        incident.PhaseSnapshot{Phase: "after", Window: incident.Window{Start: first.Add(time.Minute), End: first.Add(2 * time.Minute)}},
		Observations: []string{"observed latency"},
		Inferences:   []string{"not proven causation"},
		Limitations:  []string{"fixture"},
		Summary:      "open shipping",
	}
	if err := incident.NewFileStore(pulseDir).Append(inc); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PERCH_PULSE_DIR", pulseDir)
	prevTok := stackstatus.SetDeployableTokenFnForTest(func(*provider.Spec) (string, bool) {
		return "", false
	})
	t.Cleanup(func() { stackstatus.SetDeployableTokenFnForTest(prevTok) })

	rr := httptest.NewRecorder()
	serveStatusJSON(rr, httptest.NewRequest(http.MethodGet, "/api/status?env=dev", nil), "dev")
	if rr.Code != http.StatusOK {
		t.Fatal(rr.Body.String())
	}
	var status struct {
		Nodes []struct {
			Name    string `json:"name"`
			Healthy bool   `json:"healthy"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	for _, n := range status.Nodes {
		if n.Name == "web" && !n.Healthy {
			t.Fatal("active /api/status probe must remain healthy despite open Pulse incident")
		}
	}
}
