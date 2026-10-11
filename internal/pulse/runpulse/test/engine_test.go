package runpulse_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pulseapi "github.com/yashg4509/perch/internal/pulse/api"
	"github.com/yashg4509/perch/internal/pulse/astronomy/tracesmoke"
	"github.com/yashg4509/perch/internal/pulse/runpulse"
	"github.com/yashg4509/perch/internal/testutil"
)

func TestWorkflowCatalog_UnavailableWithoutConfig(t *testing.T) {
	t.Parallel()
	eng, err := runpulse.NewEngine(runpulse.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	list := eng.ListWorkflows()
	if len(list) != 1 {
		t.Fatalf("want 1 workflow, got %d", len(list))
	}
	if list[0].Available {
		t.Fatal("expected unavailable")
	}
	if list[0].ID != runpulse.WorkflowIDAstronomyRecommendationsBrowse {
		t.Fatalf("id=%s", list[0].ID)
	}
	if _, err := eng.StartRun(list[0].ID); err == nil {
		t.Fatal("start should fail when unconfigured")
	}
}

func TestStartRun_UnsupportedWorkflow(t *testing.T) {
	t.Parallel()
	eng := mustEngine(t, "http://127.0.0.1:9", "http://127.0.0.1:9")
	_, err := eng.StartRun("not.a.workflow")
	se, ok := err.(*runpulse.StartError)
	if !ok || se.Code != runpulse.FailUnsupportedWorkflow {
		t.Fatalf("got %v", err)
	}
}

func TestEngine_CompletedWithFixtureTrace(t *testing.T) {
	mapper, err := pulseapi.NewAstronomyMapper("local")
	if err != nil {
		t.Fatal(err)
	}

	var sawMarker atomic.Value
	demo, jaeger := newDemoAndJaeger(t, func(marker string) []byte {
		sawMarker.Store(marker)
		return stampedMultiServiceFixture(t, marker)
	})
	defer demo.Close()
	defer jaeger.Close()

	eng, err := runpulse.NewEngine(runpulse.Config{
		DemoBaseURL:       demo.URL,
		JaegerBaseURL:     jaeger.URL,
		MaxConcurrentRuns: 1,
		MaxRetainedRuns:   10,
		RetentionTTL:      time.Hour,
		OverallTimeout:    15 * time.Second,
	}, mapper)
	if err != nil {
		t.Fatal(err)
	}

	run, err := eng.StartRun(runpulse.WorkflowIDAstronomyRecommendationsBrowse)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != runpulse.StatusPending && run.Status != runpulse.StatusRunning {
		t.Fatalf("status=%s", run.Status)
	}

	final := waitRun(t, eng, run.ID, 20*time.Second)
	if final.Status != runpulse.StatusCompleted {
		t.Fatalf("status=%s code=%s msg=%s", final.Status, final.FailureCode, final.FailureMessage)
	}
	if final.Request == nil || !final.Request.OK || final.Request.StatusCode != 200 {
		t.Fatalf("request=%+v", final.Request)
	}
	if final.Trace == nil || final.Trace.TraceID == "" {
		t.Fatal("missing trace evidence")
	}
	if len(final.GraphNodes) < 2 {
		t.Fatalf("graph nodes=%v", final.GraphNodes)
	}
	appMapped := 0
	for _, n := range final.GraphNodes {
		switch n {
		case "frontend", "recommendation", "product-catalog":
			appMapped++
		case "frontend-proxy":
			// Observed infra hop is allowed when present in the marked subgraph.
		default:
			t.Fatalf("unexpected graph node %q", n)
		}
	}
	if appMapped < 2 {
		t.Fatalf("need >=2 app graph nodes, got %v", final.GraphNodes)
	}
	if m, _ := sawMarker.Load().(string); m == "" || !strings.HasPrefix(m, "perch-run-pulse/") {
		t.Fatalf("marker=%v", sawMarker.Load())
	}
	// Polling Get must not re-execute.
	again, ok := eng.GetRun(run.ID)
	if !ok || again.Status != runpulse.StatusCompleted {
		t.Fatal("get after complete failed")
	}
}

func TestEngine_ConcurrencyLimit(t *testing.T) {
	blocker := make(chan struct{})
	demo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocker
		w.WriteHeader(http.StatusOK)
	}))
	defer demo.Close()
	jaeger := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocker
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer jaeger.Close()

	eng, err := runpulse.NewEngine(runpulse.Config{
		DemoBaseURL:       demo.URL,
		JaegerBaseURL:     jaeger.URL,
		MaxConcurrentRuns: 1,
		OverallTimeout:    5 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.StartRun(runpulse.WorkflowIDAstronomyRecommendationsBrowse); err != nil {
		t.Fatal(err)
	}
	_, err = eng.StartRun(runpulse.WorkflowIDAstronomyRecommendationsBrowse)
	se, ok := err.(*runpulse.StartError)
	if !ok || se.Code != runpulse.FailConcurrencyLimit {
		t.Fatalf("got %v", err)
	}
	close(blocker)
}

func TestEngine_RejectsNonLoopbackConfig(t *testing.T) {
	t.Parallel()
	_, err := runpulse.NewEngine(runpulse.Config{DemoBaseURL: "http://example.com"}, nil)
	if err == nil {
		t.Fatal("expected reject")
	}
}

func TestEngine_TraceIncomplete(t *testing.T) {
	demo, jaeger := newDemoAndJaeger(t, func(marker string) []byte {
		return stampedSingleServiceFixture(t, marker)
	})
	defer demo.Close()
	defer jaeger.Close()

	eng, err := runpulse.NewEngine(runpulse.Config{
		DemoBaseURL:    demo.URL,
		JaegerBaseURL:  jaeger.URL,
		OverallTimeout: 4 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	run, err := eng.StartRun(runpulse.WorkflowIDAstronomyRecommendationsBrowse)
	if err != nil {
		t.Fatal(err)
	}
	final := waitRun(t, eng, run.ID, 10*time.Second)
	if final.Status == runpulse.StatusCompleted {
		t.Fatal("single-service must not complete")
	}
	if final.FailureCode != runpulse.FailTraceIncomplete && final.FailureCode != runpulse.FailTimedOut && final.FailureCode != runpulse.FailTraceNotFound {
		t.Fatalf("code=%s msg=%s", final.FailureCode, final.FailureMessage)
	}
}

func TestStore_RetentionExpiry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	clock := now
	s := runpulse.NewStore(2, time.Minute, func() time.Time { return clock })
	r1 := &runpulse.Run{ID: "run_a", Status: runpulse.StatusCompleted, StartedAt: now}
	end := now
	r1.EndedAt = &end
	s.Put(r1)
	r2 := &runpulse.Run{ID: "run_b", Status: runpulse.StatusCompleted, StartedAt: now.Add(time.Second)}
	end2 := now.Add(time.Second)
	r2.EndedAt = &end2
	s.Put(r2)
	r3 := &runpulse.Run{ID: "run_c", Status: runpulse.StatusCompleted, StartedAt: now.Add(2 * time.Second)}
	end3 := now.Add(2 * time.Second)
	r3.EndedAt = &end3
	s.Put(r3)
	if _, ok := s.Get("run_a"); ok {
		t.Fatal("oldest finished should be evicted by max keep")
	}
	clock = now.Add(2 * time.Hour)
	if _, ok := s.Get("run_b"); ok {
		t.Fatal("ttl should expire finished runs")
	}
}

func mustEngine(t *testing.T, demo, jaeger string) *runpulse.Engine {
	t.Helper()
	eng, err := runpulse.NewEngine(runpulse.Config{
		DemoBaseURL:   demo,
		JaegerBaseURL: jaeger,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return eng
}

func waitRun(t *testing.T, eng *runpulse.Engine, id string, budget time.Duration) *runpulse.Run {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		r, ok := eng.GetRun(id)
		if !ok {
			t.Fatal("run disappeared")
		}
		if r.Status == runpulse.StatusCompleted || r.Status == runpulse.StatusFailed || r.Status == runpulse.StatusTimedOut {
			return r
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timeout waiting for run")
	return nil
}

func newDemoAndJaeger(t *testing.T, traces func(marker string) []byte) (*httptest.Server, *httptest.Server) {
	t.Helper()
	demo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.Contains(r.URL.Path, "/api/recommendations") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	jaeger := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/services"):
			_, _ = w.Write([]byte(`{"data":["frontend","recommendation","product-catalog"]}`))
		case strings.Contains(r.URL.Path, "/api/traces"):
			marker := ""
			if tags := r.URL.Query().Get("tags"); tags != "" {
				var m map[string]string
				_ = json.Unmarshal([]byte(tags), &m)
				marker = m["user_agent"]
			}
			if marker == "" {
				// Fallback unfiltered query — still return stamped fixture with last probe marker unknown;
				// engine prefers tagged query first. Provide empty then tagged path handles it.
				_, _ = w.Write([]byte(`{"data":[]}`))
				return
			}
			_, _ = w.Write(traces(marker))
		default:
			http.NotFound(w, r)
		}
	}))
	return demo, jaeger
}

func stampedMultiServiceFixture(t *testing.T, marker string) []byte {
	t.Helper()
	raw := readFixture(t, "synthetic_multi_service.json")
	return stampFixture(t, raw, marker)
}

func stampedSingleServiceFixture(t *testing.T, marker string) []byte {
	t.Helper()
	raw := readFixture(t, "synthetic_single_service.json")
	return stampFixture(t, raw, marker)
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join(testutil.RepoRoot(t), "internal", "pulse", "astronomy", "tracesmoke", "testdata", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func stampFixture(t *testing.T, raw []byte, marker string) []byte {
	t.Helper()
	var resp tracesmoke.TracesResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	nowUs := time.Now().UTC().UnixMicro()
	for ti := range resp.Data {
		for si := range resp.Data[ti].Spans {
			span := &resp.Data[ti].Spans[si]
			span.StartTime = nowUs + int64(si)*1000
			for i := range span.Tags {
				if span.Tags[i].Key == "user_agent" || span.Tags[i].Key == "user_agent.original" {
					span.Tags[i].Value = marker
				}
			}
		}
	}
	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
