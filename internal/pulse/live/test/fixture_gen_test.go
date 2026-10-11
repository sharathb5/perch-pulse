package live_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	pulseapi "github.com/yashg4509/perch/internal/pulse/api"
	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/live"
	"github.com/yashg4509/perch/internal/pulse/pipeline"
)

// TestWriteLiveUIFixtures regenerates web/fixtures/pulse.live.*.json from the
// real Go API envelopes. Run with UPDATE_LIVE_FIXTURES=1.
func TestWriteLiveUIFixtures(t *testing.T) {
	if os.Getenv("UPDATE_LIVE_FIXTURES") != "1" {
		t.Skip("set UPDATE_LIVE_FIXTURES=1 to regenerate web/fixtures")
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "web", "fixtures"))

	write := func(name string, v any) {
		raw, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("wrote", path)
	}

	h := &pulseapi.Handler{Reader: mustReader(t)}
	mux := http.NewServeMux()
	h.Register(mux)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/pulse/live/status", nil))
	var disabled any
	if err := json.Unmarshal(rr.Body.Bytes(), &disabled); err != nil {
		t.Fatal(err)
	}
	write("pulse.live.status.disabled.json", disabled)

	base := time.Date(2026, 10, 11, 22, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := base
	nowFn := func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	advance := func(d time.Duration) { mu.Lock(); clock = clock.Add(d); mu.Unlock() }

	src := &scriptedSource{}
	for i := 0; i < 12; i++ {
		src.batches = append(src.batches, paymentTriple(80, 0.01, 1.0))
	}
	for i := 0; i < 6; i++ {
		src.batches = append(src.batches, paymentTriple(5000, 0.01, 1.0))
	}
	mapper, err := pulseapi.NewAstronomyMapper("local")
	if err != nil {
		t.Fatal(err)
	}
	rt, err := live.NewRuntime(live.Options{
		Config:    enabledCfg(),
		PulseDir:  t.TempDir(),
		Mapper:    mapper,
		DetectCfg: pipeline.DemoDetectConfig(),
		Clock:     nowFn,
		Source:    src,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(src.batches); i++ {
		rt.PollOnce(context.Background())
		advance(15 * time.Second)
	}
	h2 := &pulseapi.Handler{Live: rt, Reader: mustReader(t)}
	mux2 := http.NewServeMux()
	h2.Register(mux2)
	for path, name := range map[string]string{
		"/api/pulse/live/status":    "pulse.live.status.collecting.json",
		"/api/pulse/live/services":  "pulse.live.services.regression.json",
		"/api/pulse/live/incidents": "pulse.live.incidents.active.json",
	} {
		rr := httptest.NewRecorder()
		mux2.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		var body any
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		write(name, body)
	}

	// Warming / error-ish status: empty services after one warm poll.
	src2 := &scriptedSource{batches: [][]detect.Sample{paymentTriple(80, 0.01, 1.0)}}
	rt2, err := live.NewRuntime(live.Options{
		Config:    enabledCfg(),
		PulseDir:  t.TempDir(),
		Mapper:    mapper,
		DetectCfg: pipeline.DemoDetectConfig(),
		Clock:     nowFn,
		Source:    src2,
	})
	if err != nil {
		t.Fatal(err)
	}
	rt2.PollOnce(context.Background())
	h3 := &pulseapi.Handler{Live: rt2, Reader: mustReader(t)}
	mux3 := http.NewServeMux()
	h3.Register(mux3)
	rr = httptest.NewRecorder()
	mux3.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/pulse/live/status", nil))
	var warm any
	if err := json.Unmarshal(rr.Body.Bytes(), &warm); err != nil {
		t.Fatal(err)
	}
	write("pulse.live.status.warming.json", warm)
}
