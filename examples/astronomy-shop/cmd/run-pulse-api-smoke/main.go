// Command run-pulse-api-smoke exercises the REAL Run Pulse HTTP API against a
// running perch viz server (POST /api/pulse/runs + GET /api/pulse/runs/{id}).
//
// Intended for examples/astronomy-shop/scripts/ci-smoke.sh (LIVE SMOKE only).
// Does not call the runpulse package engine directly and is not a substitute
// for cmd/trace-smoke (library-level Jaeger qualification).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/runpulse"
	"github.com/yashg4509/perch/internal/pulse/runpulse/apismoke"
)

func main() {
	var (
		vizBase      = flag.String("viz-base-url", "http://127.0.0.1:18765", "perch viz loopback base URL")
		artifactDir  = flag.String("artifact-dir", "", "directory for bounded diagnostics")
		graphJSON    = flag.String("graph-json", "", "path to /api/graph JSON for node mapping checks")
		workflowID   = flag.String("workflow-id", apismoke.DefaultWorkflowID, "allowlisted workflow id")
		deadlineSecs = flag.Int("deadline-secs", 120, "overall deadline for create + poll")
		pollSecs     = flag.Int("poll-secs", 2, "poll interval seconds")
		maxAttempts  = flag.Int("max-attempts", 60, "max GET poll attempts")
	)
	flag.Parse()

	if *artifactDir == "" {
		fail("artifact-dir is required")
	}
	if err := os.MkdirAll(*artifactDir, 0o750); err != nil {
		fail("mkdir artifact-dir: %v", err)
	}
	if *deadlineSecs <= 0 || *pollSecs <= 0 || *maxAttempts <= 0 {
		fail("deadline-secs, poll-secs, and max-attempts must be > 0")
	}

	base := strings.TrimRight(strings.TrimSpace(*vizBase), "/")
	if err := requireLoopbackHTTPBase(base); err != nil {
		fail("viz-base-url: %v", err)
	}
	writeFile(*artifactDir, "run-pulse-api-smoke-config.txt", strings.Join([]string{
		"viz_base=" + base,
		"workflow_id=" + *workflowID,
		"deadline_secs=" + fmt.Sprintf("%d", *deadlineSecs),
		"poll_secs=" + fmt.Sprintf("%d", *pollSecs),
		"max_attempts=" + fmt.Sprintf("%d", *maxAttempts),
		"graph_json=" + *graphJSON,
		"note=Invokes real HTTP API only; does not call runpulse.Engine directly.",
		"note=Companion to cmd/trace-smoke which qualifies Jaeger outside the viz API.",
	}, "\n")+"\n")

	client := &http.Client{Timeout: 20 * time.Second}
	deadline := time.Now().Add(time.Duration(*deadlineSecs) * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	// --- workflows availability ---
	wfBody, wfCode, err := httpGet(ctx, client, base+"/api/pulse/workflows")
	writeFile(*artifactDir, "run-pulse-workflows.http", fmt.Sprintf("HTTP %d\n", wfCode))
	writeFile(*artifactDir, "run-pulse-workflows.json", string(wfBody)+"\n")
	if err != nil {
		fail("GET /api/pulse/workflows: %v", err)
	}
	if wfCode != http.StatusOK {
		fail("GET /api/pulse/workflows HTTP %d (see run-pulse-workflows.json)", wfCode)
	}
	if err := apismoke.RequireWorkflowAvailable(wfBody, *workflowID); err != nil {
		fail("workflow availability: %v", err)
	}
	ok("workflow %s available via GET /api/pulse/workflows", *workflowID)

	var graphNodes map[string]struct{}
	if strings.TrimSpace(*graphJSON) != "" {
		gb, err := os.ReadFile(*graphJSON)
		if err != nil {
			fail("read graph-json: %v", err)
		}
		graphNodes, err = apismoke.GraphNodeNamesFromAPIGraph(gb)
		if err != nil {
			fail("parse graph-json: %v", err)
		}
	}

	// --- POST create ---
	postBody := []byte(fmt.Sprintf(`{"workflow_id":%q}`, *workflowID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/pulse/runs", bytes.NewReader(postBody))
	if err != nil {
		fail("build POST: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Intentionally omit Origin (non-browser client) and X-Forwarded-For.
	resp, err := client.Do(req)
	if err != nil {
		fail("POST /api/pulse/runs: %v", err)
	}
	createRaw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	writeFile(*artifactDir, "run-pulse-create.http", fmt.Sprintf("HTTP %d\n", resp.StatusCode))
	writeFile(*artifactDir, "run-pulse-create.json", string(createRaw)+"\n")

	created, err := apismoke.ParseCreateRun(resp.StatusCode, createRaw, *workflowID)
	if err != nil {
		fail("create run: %v", err)
	}
	ok("POST /api/pulse/runs accepted run_id=%s status=%s", created.ID, created.Status)
	writeFile(*artifactDir, "run-pulse-run-id.txt", created.ID+"\n")

	// --- poll ---
	pollRes, err := apismoke.PollRun(ctx, base, created.ID, apismoke.PollConfig{
		Interval:        time.Duration(*pollSecs) * time.Second,
		OverallDeadline: deadline,
		MaxAttempts:     *maxAttempts,
		HTTPClient:      client,
	})
	if pollRes != nil && pollRes.Run != nil {
		writeRunArtifact(*artifactDir, pollRes.Run)
		writeFile(*artifactDir, "run-pulse-poll-meta.txt", fmt.Sprintf(
			"attempts=%d last_http=%d last_status=%s\n",
			pollRes.Attempts, pollRes.LastHTTPCode, pollRes.LastStatus,
		))
	}
	if err != nil {
		fail("poll run: %v", err)
	}
	run := pollRes.Run
	switch apismoke.ClassifyStatus(run.Status) {
	case apismoke.StatusTerminalFail:
		fail("run reached terminal failure status=%s code=%s message=%s",
			run.Status, run.FailureCode, run.FailureMessage)
	case apismoke.StatusTerminalOK:
		// continue to evidence asserts
	default:
		fail("run ended in unexpected status %q", run.Status)
	}

	if err := apismoke.AssertCompletedRun(run, apismoke.AssertOptions{
		ExpectedWorkflowID:  *workflowID,
		ExpectedEnvironment: "astronomy-shop/local",
		GraphNodeNames:      graphNodes,
	}); err != nil {
		fail("completed run evidence: %v", err)
	}

	summary := []string{
		"status=completed",
		"run_id=" + run.ID,
		"workflow_id=" + run.WorkflowID,
		"environment=" + run.Environment,
		fmt.Sprintf("request_ok=%v status_code=%d elapsed_ms=%g marker=%s",
			run.Request.OK, run.Request.StatusCode, run.Request.ElapsedMs, run.Request.Marker),
		"trace_id=" + run.Trace.TraceID,
		fmt.Sprintf("span_count=%d subgraph_span_count=%d", run.Trace.SpanCount, run.Trace.SubgraphSpanCount),
		fmt.Sprintf("app_services=%s", strings.Join(run.Trace.AppServices, ",")),
		fmt.Sprintf("graph_nodes=%s", strings.Join(run.GraphNodes, ",")),
		fmt.Sprintf("cross_service_edges=%d", len(run.Trace.CrossServiceEdges)),
		fmt.Sprintf("poll_attempts=%d", pollRes.Attempts),
	}
	writeFile(*artifactDir, "run-pulse-api-smoke-result.txt", strings.Join(summary, "\n")+"\n")
	for _, line := range summary {
		fmt.Println(line)
	}
	ok("Run Pulse HTTP API diagnostic completed with qualified multi-service evidence")
}

func httpGet(ctx context.Context, client *http.Client, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return b, resp.StatusCode, err
}

func writeRunArtifact(dir string, run *runpulse.Run) {
	b, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return
	}
	writeFile(dir, "run-pulse-last-run.json", string(b)+"\n")
}

func writeFile(dir, name, contents string) {
	_ = os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAIL: "+format+"\n", args...)
	os.Exit(1)
}

func ok(format string, args ...any) {
	fmt.Printf("OK: "+format+"\n", args...)
}

// requireLoopbackHTTPBase keeps the smoke client aligned with viz's localhost
// posture — no remote viz targets from this CLI.
func requireLoopbackHTTPBase(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("must be an absolute http(s) URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q not allowed", u.Scheme)
	}
	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("host %q is not loopback (refuse non-local viz)", host)
}
