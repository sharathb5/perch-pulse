package live_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	pulseapi "github.com/yashg4509/perch/internal/pulse/api"
	"github.com/yashg4509/perch/internal/pulse/live"
	"github.com/yashg4509/perch/internal/pulse/pipeline"
	"github.com/yashg4509/perch/internal/pulse/telem"
)

// INTEGRATION: httptest Prometheus → telem.Prometheus → detector → incident → live API.
func TestLive_EndToEndPrometheusFixture(t *testing.T) {
	base := time.Date(2026, 10, 11, 20, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := base
	nowFn := func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	advance := func(d time.Duration) { mu.Lock(); clock = clock.Add(d); mu.Unlock() }

	var phaseMu sync.Mutex
	phase := "warm" // warm | spike | healthy
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		phaseMu.Lock()
		p := phase
		phaseMu.Unlock()
		q := r.URL.Query().Get("query")
		lat, errRate, calls := "90", "0.01", "1.0"
		if p == "spike" {
			lat, errRate, calls = "6000", "0.01", "1.0"
		}
		if p == "healthy" {
			lat, errRate, calls = "95", "0.01", "1.0"
		}
		var results []map[string]any
		switch {
		case strings.Contains(q, "histogram_quantile"):
			results = []map[string]any{{"metric": map[string]string{"service_name": "payment"}, "value": []any{1.0, lat}}}
		case strings.Contains(q, `status_code="STATUS_CODE_ERROR"`):
			results = []map[string]any{{"metric": map[string]string{"service_name": "payment"}, "value": []any{1.0, errRate}}}
		default:
			results = []map[string]any{{"metric": map[string]string{"service_name": "payment"}, "value": []any{1.0, calls}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data":   map[string]any{"resultType": "vector", "result": results},
		})
	}))
	defer srv.Close()

	prom := telem.NewPrometheus(srv.URL)
	prom.Services = []string{"payment"}
	prom.Environment = "local"
	prom.Window = 2 * time.Minute

	cfg := enabledCfg()
	// httptest URL is loopback; Validate on cfg uses 127.0.0.1 — override URL for client only.
	cfg.PrometheusURL = "http://127.0.0.1:9090"
	dir := t.TempDir()
	mapper, err := pulseapi.NewAstronomyMapper("local")
	if err != nil {
		t.Fatal(err)
	}
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

	for i := 0; i < 12; i++ {
		rt.PollOnce(context.Background())
		advance(15 * time.Second)
	}
	phaseMu.Lock()
	phase = "spike"
	phaseMu.Unlock()
	for i := 0; i < 6; i++ {
		rt.PollOnce(context.Background())
		advance(15 * time.Second)
	}

	h := &pulseapi.Handler{Live: rt, Reader: mustReader(t)}
	mux := http.NewServeMux()
	h.Register(mux)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/pulse/live/incidents", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("incidents=%d %s", rr.Code, rr.Body.String())
	}
	var resp pulseapi.LiveIncidentsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, iv := range resp.Incidents {
		if iv.Lifecycle == "active" && iv.PrimaryService == "astronomy/local/payment" {
			active++
		}
	}
	if active < 1 {
		t.Fatalf("expected active live incident from Prom fixture, body=%s", rr.Body.String())
	}

	// Recovery from healthy measurements (not absence).
	phaseMu.Lock()
	phase = "healthy"
	phaseMu.Unlock()
	for i := 0; i < 10; i++ {
		rt.PollOnce(context.Background())
		advance(15 * time.Second)
	}
	snap := rt.Snapshot()
	stillActive := 0
	recovered := 0
	for _, iv := range snap.Incidents {
		if iv.PrimaryService != "astronomy/local/payment" {
			continue
		}
		if iv.Lifecycle == "active" {
			stillActive++
		}
		if iv.Lifecycle == "recovered" {
			recovered++
		}
	}
	if stillActive > 0 {
		t.Fatalf("expected recovery from healthy measurements; still active=%d snap=%+v", stillActive, snap.Status)
	}
	if recovered < 1 {
		t.Fatalf("expected recovered incident, got none")
	}
	rt.Stop()
}
