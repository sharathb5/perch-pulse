package apismoke_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/pulse/runpulse"
	"github.com/yashg4509/perch/internal/pulse/runpulse/apismoke"
	"github.com/yashg4509/perch/internal/testutil"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	p := filepath.Join(testutil.RepoRoot(t), "web", "fixtures", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func TestRequireWorkflowAvailable_OK(t *testing.T) {
	err := apismoke.RequireWorkflowAvailable(fixture(t, "pulse.workflows.available.json"), apismoke.DefaultWorkflowID)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRequireWorkflowAvailable_Unavailable(t *testing.T) {
	err := apismoke.RequireWorkflowAvailable(fixture(t, "pulse.workflows.unavailable.json"), apismoke.DefaultWorkflowID)
	if err == nil {
		t.Fatal("expected failure")
	}
	if !strings.Contains(err.Error(), "workflow_unavailable") {
		t.Fatalf("got %v", err)
	}
}

func TestRequireWorkflowAvailable_MissingWorkflow(t *testing.T) {
	body := []byte(`{"schema_version":"pulse.api.workflows.v1","generated_at":"2026-10-10T12:00:00Z","limitations":[],"count":0,"workflows":[]}`)
	err := apismoke.RequireWorkflowAvailable(body, apismoke.DefaultWorkflowID)
	if err == nil || !strings.Contains(err.Error(), "workflow_missing") {
		t.Fatalf("got %v", err)
	}
}

func TestParseCreateRun_OK(t *testing.T) {
	run, err := apismoke.ParseCreateRun(http.StatusAccepted, fixture(t, "pulse.run.create.json"), apismoke.DefaultWorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	if run.ID == "" || run.Status != runpulse.StatusPending {
		t.Fatalf("%+v", run)
	}
}

func TestParseCreateRun_Rejected(t *testing.T) {
	_, err := apismoke.ParseCreateRun(http.StatusForbidden, []byte(`{"error":"loopback_only"}`), apismoke.DefaultWorkflowID)
	if err == nil || !strings.Contains(err.Error(), "post_rejected") {
		t.Fatalf("got %v", err)
	}
}

func TestParseCreateRun_Malformed(t *testing.T) {
	_, err := apismoke.ParseCreateRun(http.StatusAccepted, []byte(`{"schema_version":"pulse.api.runs.create.v1","run":null}`), apismoke.DefaultWorkflowID)
	if err == nil || !strings.Contains(err.Error(), "malformed_create") {
		t.Fatalf("got %v", err)
	}
}

func TestParseCreateRun_InvalidRunID(t *testing.T) {
	body := []byte(`{"schema_version":"pulse.api.runs.create.v1","generated_at":"2026-10-10T12:00:00Z","run":{"id":"bad","workflow_id":"astronomy.recommendations-browse.v1","status":"pending","started_at":"2026-10-10T12:00:00Z"}}`)
	_, err := apismoke.ParseCreateRun(http.StatusAccepted, body, apismoke.DefaultWorkflowID)
	if err == nil || !strings.Contains(err.Error(), "invalid_run_id") {
		t.Fatalf("got %v", err)
	}
}

func TestClassifyStatus(t *testing.T) {
	cases := []struct {
		in   string
		want apismoke.StatusKind
	}{
		{runpulse.StatusPending, apismoke.StatusIntermediate},
		{runpulse.StatusRunning, apismoke.StatusIntermediate},
		{runpulse.StatusCompleted, apismoke.StatusTerminalOK},
		{runpulse.StatusFailed, apismoke.StatusTerminalFail},
		{runpulse.StatusTimedOut, apismoke.StatusTerminalFail},
		{"weird", apismoke.StatusUnknown},
	}
	for _, tc := range cases {
		if got := apismoke.ClassifyStatus(tc.in); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.in, got, tc.want)
		}
	}
}

func TestAssertCompletedRun_OK(t *testing.T) {
	run := decodeRunFixture(t, "pulse.run.completed.json")
	nodes := map[string]struct{}{
		"frontend": {}, "product-catalog": {}, "recommendation": {}, "shipping": {},
	}
	if err := apismoke.AssertCompletedRun(run, apismoke.AssertOptions{
		ExpectedWorkflowID:  apismoke.DefaultWorkflowID,
		ExpectedEnvironment: "astronomy-shop/local",
		GraphNodeNames:      nodes,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAssertCompletedRun_FailedTerminal(t *testing.T) {
	run := decodeRunFixture(t, "pulse.run.failed.json")
	err := apismoke.AssertCompletedRun(run, apismoke.AssertOptions{})
	if err == nil || !strings.Contains(err.Error(), "not_completed") {
		t.Fatalf("got %v", err)
	}
}

func TestAssertCompletedRun_InsufficientMapped(t *testing.T) {
	run := decodeRunFixture(t, "pulse.run.completed.json")
	run.Services = run.Services[:1]
	run.GraphNodes = run.GraphNodes[:1]
	err := apismoke.AssertCompletedRun(run, apismoke.AssertOptions{GraphNodeNames: map[string]struct{}{"frontend": {}}})
	if err == nil || !strings.Contains(err.Error(), "insufficient_") {
		t.Fatalf("got %v", err)
	}
}

func TestAssertCompletedRun_MissingCrossService(t *testing.T) {
	run := decodeRunFixture(t, "pulse.run.completed.json")
	run.Trace.HasCrossServiceEdge = false
	run.Trace.CrossServiceEdges = nil
	err := apismoke.AssertCompletedRun(run, apismoke.AssertOptions{
		GraphNodeNames: map[string]struct{}{"frontend": {}, "product-catalog": {}, "recommendation": {}},
	})
	if err == nil || !strings.Contains(err.Error(), "missing_cross_service") {
		t.Fatalf("got %v", err)
	}
}

func TestAssertCompletedRun_IncompleteTrace(t *testing.T) {
	run := decodeRunFixture(t, "pulse.run.completed.json")
	run.Trace.AppServices = []string{"frontend"}
	err := apismoke.AssertCompletedRun(run, apismoke.AssertOptions{
		GraphNodeNames: map[string]struct{}{"frontend": {}, "product-catalog": {}, "recommendation": {}},
	})
	if err == nil || !strings.Contains(err.Error(), "insufficient_app_services") {
		t.Fatalf("got %v", err)
	}
}

func TestAssertCompletedRun_UnrelatedTraceSubgraph(t *testing.T) {
	run := decodeRunFixture(t, "pulse.run.completed.json")
	run.Trace.SubgraphSpanCount = 0
	err := apismoke.AssertCompletedRun(run, apismoke.AssertOptions{
		GraphNodeNames: map[string]struct{}{"frontend": {}, "product-catalog": {}, "recommendation": {}},
	})
	if err == nil || !strings.Contains(err.Error(), "empty_subgraph") {
		t.Fatalf("got %v", err)
	}
}

func TestAssertCompletedRun_GraphNodeMissing(t *testing.T) {
	run := decodeRunFixture(t, "pulse.run.completed.json")
	err := apismoke.AssertCompletedRun(run, apismoke.AssertOptions{
		GraphNodeNames: map[string]struct{}{"shipping": {}}, // missing claimed nodes
	})
	if err == nil || !strings.Contains(err.Error(), "graph_node_missing") {
		t.Fatalf("got %v", err)
	}
}

func TestParseGetRun_Missing(t *testing.T) {
	_, err := apismoke.ParseGetRun(http.StatusNotFound, []byte(`{"error":"not_found"}`), "run_abc")
	if err == nil || !strings.Contains(err.Error(), "missing_run") {
		t.Fatalf("got %v", err)
	}
}

func TestPollRun_PendingRunningCompleted(t *testing.T) {
	states := []string{runpulse.StatusPending, runpulse.StatusRunning, runpulse.StatusCompleted}
	idx := 0
	completed := fixture(t, "pulse.run.completed.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if idx >= len(states) {
			idx = len(states) - 1
		}
		st := states[idx]
		idx++
		if st == runpulse.StatusCompleted {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(completed)
			return
		}
		body := map[string]any{
			"schema_version": "pulse.api.run.v1",
			"generated_at":   time.Now().UTC(),
			"run": map[string]any{
				"id":          "run_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"workflow_id": apismoke.DefaultWorkflowID,
				"status":      st,
				"started_at":  time.Now().UTC(),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()

	res, err := apismoke.PollRun(t.Context(), srv.URL, "run_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", apismoke.PollConfig{
		Interval:    time.Millisecond,
		MaxAttempts: 10,
		HTTPClient:  srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Run.Status != runpulse.StatusCompleted {
		t.Fatalf("status=%s attempts=%d", res.Run.Status, res.Attempts)
	}
	if res.Attempts < 3 {
		t.Fatalf("expected at least 3 polls, got %d", res.Attempts)
	}
}

func TestPollRun_FailedTerminal(t *testing.T) {
	failed := fixture(t, "pulse.run.failed.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(failed)
	}))
	defer srv.Close()
	res, err := apismoke.PollRun(t.Context(), srv.URL, "run_dddddddddddddddddddddddddddddddd", apismoke.PollConfig{
		Interval:   time.Millisecond,
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Run.Status != runpulse.StatusFailed {
		t.Fatalf("%+v", res.Run)
	}
}

func TestPollRun_TimedOutStatus(t *testing.T) {
	body := []byte(`{"schema_version":"pulse.api.run.v1","generated_at":"2026-10-10T12:00:00Z","run":{"id":"run_cccccccccccccccccccccccccccccccc","workflow_id":"astronomy.recommendations-browse.v1","status":"timed_out","started_at":"2026-10-10T12:00:00Z","failure_code":"timed_out","failure_message":"deadline"}}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	res, err := apismoke.PollRun(t.Context(), srv.URL, "run_cccccccccccccccccccccccccccccccc", apismoke.PollConfig{
		Interval:   time.Millisecond,
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if apismoke.ClassifyStatus(res.Run.Status) != apismoke.StatusTerminalFail {
		t.Fatalf("%+v", res.Run)
	}
}

func TestPollRun_PollingTimeout(t *testing.T) {
	running := fixture(t, "pulse.run.running.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(running)
	}))
	defer srv.Close()
	_, err := apismoke.PollRun(t.Context(), srv.URL, "run_cccccccccccccccccccccccccccccccc", apismoke.PollConfig{
		Interval:    time.Millisecond,
		MaxAttempts: 3,
		HTTPClient:  srv.Client(),
	})
	if err == nil || !strings.Contains(err.Error(), "poll_timeout") {
		t.Fatalf("got %v", err)
	}
}

func TestGraphNodeNamesFromAPIGraph(t *testing.T) {
	body := []byte(`{"nodes":[{"name":"frontend"},{"name":"recommendation"}]}`)
	m, err := apismoke.GraphNodeNamesFromAPIGraph(body)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m["frontend"]; !ok {
		t.Fatal(m)
	}
}

func decodeRunFixture(t *testing.T, name string) *runpulse.Run {
	t.Helper()
	var wrap struct {
		Run *runpulse.Run `json:"run"`
	}
	if err := json.Unmarshal(fixture(t, name), &wrap); err != nil {
		t.Fatal(err)
	}
	if wrap.Run == nil {
		t.Fatal("nil run")
	}
	return wrap.Run
}
