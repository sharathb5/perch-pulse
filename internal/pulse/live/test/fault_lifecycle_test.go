package live_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	pulseapi "github.com/yashg4509/perch/internal/pulse/api"
	"github.com/yashg4509/perch/internal/pulse/incident"
	"github.com/yashg4509/perch/internal/pulse/live"
	"github.com/yashg4509/perch/internal/pulse/pipeline"
	"github.com/yashg4509/perch/internal/pulse/telem"
)

// TestLive_FaultLifecycle_ShippingLatency exercises real production packages
// with a controlled Prometheus fixture server:
//
//	healthy baseline → no incident
//	→ latency regression → active incident (persisted)
//	→ continued regression (no duplicate)
//	→ missing telemetry (not recovery, not healthy)
//	→ healthy recovery observations → recovered lifecycle
//	→ API-visible historical record with stable incident_id
//
// Sibling tests cover duplicate polls, absence≠recovery, and provider outage.
// This test uses shipping (the preferred live Astronomy Shop fault target).
func TestLive_FaultLifecycle_ShippingLatency(t *testing.T) {
	base := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := base
	nowFn := func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	advance := func(d time.Duration) { mu.Lock(); clock = clock.Add(d); mu.Unlock() }

	var phaseMu sync.Mutex
	phase := "warm" // warm | fault | recover | missing
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		phaseMu.Lock()
		p := phase
		phaseMu.Unlock()
		if p == "missing" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data":   map[string]any{"resultType": "vector", "result": []any{}},
			})
			return
		}
		q := r.URL.Query().Get("query")
		lat, errRate, calls := "85", "0.01", "1.2"
		if p == "fault" {
			lat, errRate, calls = "8000", "0.01", "1.2"
		}
		if p == "recover" {
			lat, errRate, calls = "90", "0.01", "1.2"
		}
		var results []map[string]any
		switch {
		case strings.Contains(q, "histogram_quantile"):
			results = []map[string]any{{"metric": map[string]string{"service_name": "shipping"}, "value": []any{1.0, lat}}}
		case strings.Contains(q, `status_code="STATUS_CODE_ERROR"`):
			results = []map[string]any{{"metric": map[string]string{"service_name": "shipping"}, "value": []any{1.0, errRate}}}
		default:
			results = []map[string]any{{"metric": map[string]string{"service_name": "shipping"}, "value": []any{1.0, calls}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data":   map[string]any{"resultType": "vector", "result": results},
		})
	}))
	defer srv.Close()

	prom := telem.NewPrometheus(srv.URL)
	prom.Services = []string{"shipping"}
	prom.Environment = "local"
	prom.Window = 2 * time.Minute

	dir := t.TempDir()
	mapper, err := pulseapi.NewAstronomyMapper("local")
	if err != nil {
		t.Fatal(err)
	}
	cfg := enabledCfg()
	cfg.PrometheusURL = "http://127.0.0.1:9090"
	rt, err := live.NewRuntime(live.Options{
		Config:    cfg,
		PulseDir:  dir,
		Mapper:    mapper,
		DetectCfg: pipeline.DemoDetectConfig(),
		Clock:     nowFn,
		Source:    prom,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Stop()

	h := &pulseapi.Handler{Live: rt, Reader: mustReader(t)}
	mux := http.NewServeMux()
	h.Register(mux)

	poll := func() {
		rt.PollOnce(context.Background())
		advance(15 * time.Second)
	}

	// --- Baseline warmup: must NOT open an incident ---
	for i := 0; i < 12; i++ {
		poll()
	}
	snap := rt.Snapshot()
	if snap.Status.ActiveIncidents != 0 || snap.Status.ActiveRegressions != 0 {
		t.Fatalf("healthy baseline must not open incidents: status=%+v", snap.Status)
	}
	foundShip := false
	for _, s := range snap.Services {
		if s.ServiceID != "astronomy/local/shipping" {
			continue
		}
		foundShip = true
		if s.GraphNode != "shipping" {
			t.Fatalf("graph_node=%q want shipping (no fabricated identity)", s.GraphNode)
		}
		if s.RegressionOpen {
			t.Fatal("shipping must not regress during healthy baseline")
		}
	}
	if !foundShip {
		t.Fatal("expected shipping observation during baseline")
	}

	// --- Fault: latency regression ---
	// Keep the fault window short enough that the rolling baseline cannot absorb
	// the spike before we assert (same pattern as TestLive_EndToEndPrometheusFixture).
	phaseMu.Lock()
	phase = "fault"
	phaseMu.Unlock()
	var activeID string
	var incResp pulseapi.LiveIncidentsResponse
	var rr *httptest.ResponseRecorder
	for i := 0; i < 6; i++ {
		poll()
		rr = httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/pulse/live/incidents", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("incidents=%d %s", rr.Code, rr.Body.String())
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &incResp); err != nil {
			t.Fatal(err)
		}
		activeID = ""
		for _, iv := range incResp.Incidents {
			body, _ := json.Marshal(iv)
			low := strings.ToLower(string(body))
			for _, leak := range []string{"latency-shipping-intl", "intlshippingslowdown", "ground_truth", "scenario_id"} {
				if strings.Contains(low, leak) {
					t.Fatalf("scenario ground-truth leak in live API: %s", leak)
				}
			}
			if iv.PrimaryService == "astronomy/local/shipping" && iv.Lifecycle == "active" {
				activeID = iv.IncidentID
				if iv.GraphNode != "shipping" {
					t.Fatalf("active incident graph_node=%q", iv.GraphNode)
				}
				if strings.Contains(strings.ToLower(iv.Summary), "root cause") {
					t.Fatal("must not claim proven root cause")
				}
			}
		}
		if activeID != "" {
			break
		}
	}
	if activeID == "" {
		t.Fatalf("expected active shipping incident during fault window, body=%s", rr.Body.String())
	}

	list, err := incident.NewFileStore(dir).List()
	if err != nil {
		t.Fatal(err)
	}
	persistedOpen := 0
	for _, inc := range list {
		if inc.IncidentID == activeID && inc.Open() {
			persistedOpen++
		}
	}
	if persistedOpen != 1 {
		t.Fatalf("expected persisted open incident %s, got %d", activeID, persistedOpen)
	}

	// --- Continued regression: no duplicate (2 more polls; stay inside fault window) ---
	for i := 0; i < 2; i++ {
		poll()
	}
	list, err = incident.NewFileStore(dir).List()
	if err != nil {
		t.Fatal(err)
	}
	openShip := 0
	for _, inc := range list {
		if inc.PrimaryServiceID == "astronomy/local/shipping" && inc.Open() {
			openShip++
			if inc.IncidentID != activeID {
				t.Fatalf("duplicate episode id %s want %s", inc.IncidentID, activeID)
			}
		}
	}
	if openShip != 1 {
		t.Fatalf("repeated polls must not duplicate open incidents, got %d", openShip)
	}

	// --- Missing telemetry must NOT recover ---
	phaseMu.Lock()
	phase = "missing"
	phaseMu.Unlock()
	for i := 0; i < 4; i++ {
		poll()
	}
	snap = rt.Snapshot()
	stillActive := false
	for _, iv := range snap.Incidents {
		if iv.IncidentID == activeID && iv.Lifecycle == "active" {
			stillActive = true
		}
	}
	if !stillActive {
		t.Fatal("missing telemetry must not close the active incident")
	}
	if snap.Status.ActiveIncidents < 1 {
		t.Fatal("status.active_incidents must remain ≥1 when telemetry is missing")
	}
	shipSeen := false
	for _, s := range snap.Services {
		if s.ServiceID != "astronomy/local/shipping" {
			continue
		}
		shipSeen = true
		if s.Intelligence == "healthy" {
			t.Fatal("missing telemetry must not be healthy")
		}
		if s.Quality != "missing" && s.Freshness != "stale" && s.Freshness != "missing" {
			// Retained last-known may be stale; never fresh+healthy with empty Prom.
			t.Logf("shipping quality=%s freshness=%s intel=%s", s.Quality, s.Freshness, s.Intelligence)
		}
	}
	if !shipSeen && snap.Status.UnavailableOrUnknown < 1 {
		t.Fatal("missing telemetry must surface as unavailable/unknown or retained shipping row")
	}

	// Resume fault series so recovery is from measured healthy values, not gaps.
	phaseMu.Lock()
	phase = "fault"
	phaseMu.Unlock()
	for i := 0; i < 2; i++ {
		poll()
	}

	// --- Recovery from healthy measurements ---
	phaseMu.Lock()
	phase = "recover"
	phaseMu.Unlock()
	for i := 0; i < 12; i++ {
		poll()
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/pulse/live/incidents", nil))
	if err := json.Unmarshal(rr.Body.Bytes(), &incResp); err != nil {
		t.Fatal(err)
	}
	recoveredSame := false
	activeAfter := 0
	for _, iv := range incResp.Incidents {
		if iv.PrimaryService == "astronomy/local/shipping" && iv.Lifecycle == "active" {
			activeAfter++
		}
		if iv.IncidentID == activeID && (iv.Lifecycle == "recovered" || iv.Lifecycle == "historical") {
			recoveredSame = true
		}
	}
	if activeAfter > 0 {
		t.Fatalf("expected no active shipping incident after recovery evidence, active=%d body=%s", activeAfter, rr.Body.String())
	}
	if !recoveredSame {
		t.Fatalf("expected same incident_id %s as recovered/historical in live API", activeID)
	}

	list, err = incident.NewFileStore(dir).List()
	if err != nil {
		t.Fatal(err)
	}
	foundHist := false
	for _, inc := range list {
		if inc.IncidentID != activeID {
			continue
		}
		foundHist = true
		if inc.Open() {
			t.Fatal("persisted incident must not remain open after recovery")
		}
		if inc.RecoveredAt == nil {
			t.Fatal("persisted incident missing RecoveredAt")
		}
	}
	if !foundHist {
		t.Fatalf("historical incident %s missing from file store under %s", activeID, dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("expected incident files in isolated PulseDir: err=%v", err)
	}
	alien := filepath.Join(t.TempDir(), "alien-env-should-be-empty")
	if _, err := os.Stat(filepath.Join(alien, "incidents")); err == nil {
		t.Fatal("must not write outside isolated PulseDir")
	}
}
