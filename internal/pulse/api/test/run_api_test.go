package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pulseapi "github.com/yashg4509/perch/internal/pulse/api"
	"github.com/yashg4509/perch/internal/pulse/runpulse"
)

func TestWorkflows_UnavailableWithoutEngineConfig(t *testing.T) {
	eng, err := runpulse.NewEngine(runpulse.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestHandler(t, t.TempDir(), nil, fixedNow())
	h.Runs = eng
	rr := doGET(t, h, "/api/pulse/workflows")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp pulseapi.WorkflowsResponse
	mustDecode(t, rr, &resp)
	if resp.SchemaVersion != pulseapi.SchemaWorkflows {
		t.Fatalf("schema=%s", resp.SchemaVersion)
	}
	if len(resp.Workflows) != 1 || resp.Workflows[0].Available {
		t.Fatalf("workflows=%+v", resp.Workflows)
	}
	if resp.Workflows[0].UnavailabilityReason == "" {
		t.Fatal("expected unavailability reason")
	}
}

func TestCreateRun_RequiresLoopback(t *testing.T) {
	demo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer demo.Close()
	jaeger := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer jaeger.Close()

	eng, err := runpulse.NewEngine(runpulse.Config{
		DemoBaseURL:   demo.URL,
		JaegerBaseURL: jaeger.URL,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestHandler(t, t.TempDir(), nil, fixedNow())
	h.Runs = eng
	mux := http.NewServeMux()
	h.Register(mux)

	body := []byte(`{"workflow_id":"astronomy.recommendations-browse.v1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/pulse/runs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "8.8.8.8:1234"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestCreateRun_RejectsForwardedFor(t *testing.T) {
	eng, err := runpulse.NewEngine(runpulse.Config{
		DemoBaseURL:   "http://127.0.0.1:9",
		JaegerBaseURL: "http://127.0.0.1:9",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestHandler(t, t.TempDir(), nil, fixedNow())
	h.Runs = eng
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/pulse/runs", strings.NewReader(`{"workflow_id":"astronomy.recommendations-browse.v1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:1"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestCreateRun_RejectsCrossOrigin(t *testing.T) {
	eng, err := runpulse.NewEngine(runpulse.Config{
		DemoBaseURL:   "http://127.0.0.1:9",
		JaegerBaseURL: "http://127.0.0.1:9",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestHandler(t, t.TempDir(), nil, fixedNow())
	h.Runs = eng
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/pulse/runs", strings.NewReader(`{"workflow_id":"astronomy.recommendations-browse.v1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	req.Host = "127.0.0.1:3131"
	req.RemoteAddr = "127.0.0.1:1"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestCreateRun_RejectsNonJSONContentType(t *testing.T) {
	eng, err := runpulse.NewEngine(runpulse.Config{
		DemoBaseURL:   "http://127.0.0.1:9",
		JaegerBaseURL: "http://127.0.0.1:9",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestHandler(t, t.TempDir(), nil, fixedNow())
	h.Runs = eng
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/pulse/runs", strings.NewReader(`{"workflow_id":"astronomy.recommendations-browse.v1"}`))
	req.Header.Set("Content-Type", "text/plain")
	req.RemoteAddr = "127.0.0.1:1"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestCreateRun_MalformedJSON(t *testing.T) {
	eng, err := runpulse.NewEngine(runpulse.Config{
		DemoBaseURL:   "http://127.0.0.1:9",
		JaegerBaseURL: "http://127.0.0.1:9",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestHandler(t, t.TempDir(), nil, fixedNow())
	h.Runs = eng
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/pulse/runs", strings.NewReader(`{`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:1"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestGetRun_NotFound(t *testing.T) {
	eng, err := runpulse.NewEngine(runpulse.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestHandler(t, t.TempDir(), nil, fixedNow())
	h.Runs = eng
	rr := doGET(t, h, "/api/pulse/runs/run_deadbeefdeadbeefdeadbeefdeadbeef")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestCreateAndGetRun_NoSecretsInBody(t *testing.T) {
	demo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer demo.Close()
	jaeger := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "services") {
			_, _ = w.Write([]byte(`{"data":["frontend"]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer jaeger.Close()

	eng, err := runpulse.NewEngine(runpulse.Config{
		DemoBaseURL:    demo.URL,
		JaegerBaseURL:  jaeger.URL,
		OverallTimeout: 2 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestHandler(t, t.TempDir(), nil, fixedNow())
	h.Runs = eng
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/pulse/runs", strings.NewReader(`{"workflow_id":"astronomy.recommendations-browse.v1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:9999"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var created pulseapi.CreateRunResponse
	mustDecode(t, rr, &created)
	if created.Run == nil || created.Run.ID == "" {
		t.Fatal("missing run")
	}
	body := rr.Body.String()
	for _, bad := range []string{"password", "authorization", "api_key", "bearer "} {
		if strings.Contains(strings.ToLower(body), bad) {
			t.Fatalf("secret-like %q in create response", bad)
		}
	}

	// Wait briefly then GET
	deadline := time.Now().Add(5 * time.Second)
	var getRR *httptest.ResponseRecorder
	for time.Now().Before(deadline) {
		getRR = doGET(t, h, "/api/pulse/runs/"+created.Run.ID)
		if getRR.Code != http.StatusOK {
			t.Fatalf("get status=%d", getRR.Code)
		}
		var got pulseapi.RunResponse
		mustDecode(t, getRR, &got)
		if got.Run.Status == runpulse.StatusCompleted || got.Run.Status == runpulse.StatusFailed || got.Run.Status == runpulse.StatusTimedOut {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pulseapi.ContainsGroundTruthLeak(getRR.Body.String()) {
		t.Fatal("ground truth leak")
	}
	// Historical pulse routes still OK
	if rr := doGET(t, h, "/api/pulse/services"); rr.Code != http.StatusOK {
		t.Fatalf("services broken: %d", rr.Code)
	}
}

func TestCreateRun_UnsupportedWorkflow(t *testing.T) {
	eng, err := runpulse.NewEngine(runpulse.Config{
		DemoBaseURL:   "http://127.0.0.1:9",
		JaegerBaseURL: "http://127.0.0.1:9",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestHandler(t, t.TempDir(), nil, fixedNow())
	h.Runs = eng
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/pulse/runs", strings.NewReader(`{"workflow_id":"nope"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:1"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestGraphAndStatusUnaffected_CompileSurface(t *testing.T) {
	// Ensure Register still mounts historical GET routes alongside runs.
	h := newTestHandler(t, t.TempDir(), nil, fixedNow())
	eng, _ := runpulse.NewEngine(runpulse.Config{}, nil)
	h.Runs = eng
	mux := http.NewServeMux()
	h.Register(mux)
	for _, path := range []string{
		"/api/pulse/services",
		"/api/pulse/incidents",
		"/api/pulse/changes",
		"/api/pulse/workflows",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s -> %d", path, rr.Code)
		}
	}
}

// silence unused json import if helper uses it
var _ = json.Marshal
