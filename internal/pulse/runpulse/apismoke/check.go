// Package apismoke validates Run Pulse HTTP API responses for live Astronomy
// Shop smoke and deterministic fixture tests. It does not start diagnostics
// itself — callers issue real HTTP requests and pass response bodies here.
package apismoke

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	pulseapi "github.com/yashg4509/perch/internal/pulse/api"
	"github.com/yashg4509/perch/internal/pulse/astronomy/tracesmoke"
	"github.com/yashg4509/perch/internal/pulse/runpulse"
)

// DefaultWorkflowID is the only MVP allowlisted Astronomy Shop diagnostic.
const DefaultWorkflowID = runpulse.WorkflowIDAstronomyRecommendationsBrowse

// StatusKind classifies a run status for polling.
type StatusKind int

const (
	StatusUnknown StatusKind = iota
	StatusIntermediate
	StatusTerminalOK
	StatusTerminalFail
)

// CheckError is a structured assertion failure suitable for smoke artifacts.
type CheckError struct {
	Code    string
	Message string
}

func (e *CheckError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Code == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func failf(code, format string, args ...any) *CheckError {
	return &CheckError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// ClassifyStatus maps a run status string to polling behavior.
func ClassifyStatus(status string) StatusKind {
	switch strings.TrimSpace(status) {
	case runpulse.StatusPending, runpulse.StatusRunning:
		return StatusIntermediate
	case runpulse.StatusCompleted:
		return StatusTerminalOK
	case runpulse.StatusFailed, runpulse.StatusTimedOut:
		return StatusTerminalFail
	default:
		return StatusUnknown
	}
}

// RequireWorkflowAvailable asserts GET /api/pulse/workflows lists workflowID
// as available. HTTP 200 with available=false is a failure.
func RequireWorkflowAvailable(body []byte, workflowID string) error {
	if strings.TrimSpace(workflowID) == "" {
		workflowID = DefaultWorkflowID
	}
	var resp pulseapi.WorkflowsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return failf("malformed_workflows", "workflows response is not valid JSON: %v", err)
	}
	if resp.SchemaVersion != pulseapi.SchemaWorkflows {
		return failf("bad_schema", "expected schema_version %q, got %q", pulseapi.SchemaWorkflows, resp.SchemaVersion)
	}
	var found *runpulse.WorkflowAvailability
	for i := range resp.Workflows {
		if resp.Workflows[i].ID == workflowID {
			found = &resp.Workflows[i]
			break
		}
	}
	if found == nil {
		return failf("workflow_missing", "workflow %q not listed in /api/pulse/workflows (count=%d)", workflowID, resp.Count)
	}
	if !found.Available {
		reason := strings.TrimSpace(found.UnavailabilityReason)
		if reason == "" {
			reason = "no reason provided"
		}
		return failf("workflow_unavailable", "workflow %q is listed but unavailable: %s", workflowID, reason)
	}
	return nil
}

// ParseCreateRun validates a POST /api/pulse/runs response body and status.
// Expects HTTP 202 Accepted with a nonempty run id and initial pending/running state.
func ParseCreateRun(httpStatus int, body []byte, workflowID string) (*runpulse.Run, error) {
	if strings.TrimSpace(workflowID) == "" {
		workflowID = DefaultWorkflowID
	}
	if httpStatus == 0 {
		return nil, failf("post_rejected", "POST /api/pulse/runs returned no HTTP status")
	}
	// Canonical success is 202 Accepted (see pulse-run.md / run_handler.go).
	if httpStatus != http.StatusAccepted {
		return nil, failf("post_rejected", "POST /api/pulse/runs unexpected status %d (want 202): %s", httpStatus, truncate(string(body), 400))
	}
	var resp pulseapi.CreateRunResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, failf("malformed_create", "create-run response is not valid JSON: %v", err)
	}
	if resp.SchemaVersion != pulseapi.SchemaRunCreate {
		return nil, failf("bad_schema", "expected schema_version %q, got %q", pulseapi.SchemaRunCreate, resp.SchemaVersion)
	}
	if resp.Run == nil {
		return nil, failf("malformed_create", "create-run response missing run object")
	}
	run := resp.Run
	if !strings.HasPrefix(run.ID, "run_") || len(run.ID) < 8 {
		return nil, failf("invalid_run_id", "create-run returned invalid run id %q", run.ID)
	}
	if run.WorkflowID != workflowID {
		return nil, failf("wrong_workflow", "create-run workflow_id=%q want %q", run.WorkflowID, workflowID)
	}
	switch ClassifyStatus(run.Status) {
	case StatusIntermediate, StatusTerminalOK:
		// pending/running expected; completed is unusual but not rejected at create time.
	case StatusTerminalFail:
		return nil, failf("create_terminal_fail", "create-run returned terminal failure status %q: %s", run.Status, run.FailureMessage)
	default:
		return nil, failf("invalid_status", "create-run returned unknown status %q", run.Status)
	}
	return run.Clone(), nil
}

// ParseGetRun validates a GET /api/pulse/runs/{id} response.
func ParseGetRun(httpStatus int, body []byte, expectedRunID string) (*runpulse.Run, error) {
	if httpStatus == http.StatusNotFound {
		return nil, failf("missing_run", "GET /api/pulse/runs/%s returned 404 (expired, never existed, or lost after viz restart)", expectedRunID)
	}
	if httpStatus < 200 || httpStatus >= 300 {
		return nil, failf("get_run_failed", "GET /api/pulse/runs/%s HTTP %d: %s", expectedRunID, httpStatus, truncate(string(body), 400))
	}
	var resp pulseapi.RunResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, failf("malformed_run", "run response is not valid JSON: %v", err)
	}
	if resp.SchemaVersion != pulseapi.SchemaRun {
		return nil, failf("bad_schema", "expected schema_version %q, got %q", pulseapi.SchemaRun, resp.SchemaVersion)
	}
	if resp.Run == nil {
		return nil, failf("malformed_run", "run response missing run object")
	}
	if expectedRunID != "" && resp.Run.ID != expectedRunID {
		return nil, failf("wrong_run_id", "run id=%q want %q", resp.Run.ID, expectedRunID)
	}
	if ClassifyStatus(resp.Run.Status) == StatusUnknown {
		return nil, failf("invalid_status", "run returned unknown status %q", resp.Run.Status)
	}
	return resp.Run.Clone(), nil
}

// AssertOptions tunes completed-run evidence checks.
type AssertOptions struct {
	ExpectedWorkflowID  string
	ExpectedEnvironment string
	// GraphNodeNames are node name values from GET /api/graph (field "name").
	// When nonempty, every claimed mapped graph_node must exist here.
	GraphNodeNames map[string]struct{}
	// MinMappedAppServices defaults to 2 (qualification contract).
	MinMappedAppServices int
	// RequireCrossServiceEdge defaults to true.
	RequireCrossServiceEdge *bool
}

// AssertCompletedRun verifies a terminal completed diagnostic carries real
// request, qualified-trace, mapping, and cross-service evidence.
func AssertCompletedRun(run *runpulse.Run, opts AssertOptions) error {
	if run == nil {
		return failf("nil_run", "run is nil")
	}
	wf := opts.ExpectedWorkflowID
	if wf == "" {
		wf = DefaultWorkflowID
	}
	minMapped := opts.MinMappedAppServices
	if minMapped <= 0 {
		minMapped = 2
	}
	requireEdge := true
	if opts.RequireCrossServiceEdge != nil {
		requireEdge = *opts.RequireCrossServiceEdge
	}

	if run.Status != runpulse.StatusCompleted {
		return failf("not_completed", "status=%q want completed (failure_code=%s message=%s)",
			run.Status, run.FailureCode, run.FailureMessage)
	}
	if run.WorkflowID != wf {
		return failf("wrong_workflow", "workflow_id=%q want %q", run.WorkflowID, wf)
	}
	if opts.ExpectedEnvironment != "" && run.Environment != opts.ExpectedEnvironment {
		return failf("wrong_environment", "environment=%q want %q", run.Environment, opts.ExpectedEnvironment)
	}
	if !strings.HasPrefix(run.ID, "run_") {
		return failf("invalid_run_id", "run id %q", run.ID)
	}

	if err := assertRequest(run); err != nil {
		return err
	}
	if err := assertTrace(run, requireEdge, minMapped); err != nil {
		return err
	}
	if err := assertServices(run, minMapped, opts.GraphNodeNames); err != nil {
		return err
	}
	if err := assertSafety(run); err != nil {
		return err
	}
	return nil
}

func assertRequest(run *runpulse.Run) error {
	req := run.Request
	if req == nil {
		return failf("missing_request", "completed run missing request evidence")
	}
	if strings.ToUpper(req.Method) != "GET" {
		return failf("bad_request", "request method=%q want GET", req.Method)
	}
	if !req.OK {
		return failf("request_not_ok", "request.ok=false status=%d error=%s", req.StatusCode, req.Error)
	}
	if req.StatusCode < 200 || req.StatusCode >= 300 {
		return failf("request_http_failed", "request status_code=%d (want 2xx)", req.StatusCode)
	}
	if req.ElapsedMs < 0 {
		return failf("bad_timing", "request.elapsed_ms=%v must be nonnegative", req.ElapsedMs)
	}
	if strings.TrimSpace(req.Marker) == "" {
		return failf("missing_marker", "request.marker empty — cannot correlate diagnostic to trace")
	}
	if !strings.HasPrefix(req.Marker, "perch-run-pulse/") || !strings.Contains(req.Marker, run.ID) {
		return failf("marker_mismatch", "request.marker %q must be perch-run-pulse/<run_id> for this diagnostic", req.Marker)
	}
	if strings.Contains(strings.ToLower(req.URLRedacted), "authorization=") ||
		strings.Contains(strings.ToLower(req.URLRedacted), "api_key=") ||
		strings.Contains(strings.ToLower(req.URLRedacted), "token=") {
		return failf("secret_leak", "request.url_redacted appears to contain credentials")
	}
	return nil
}

func assertTrace(run *runpulse.Run, requireEdge bool, minApp int) error {
	tr := run.Trace
	if tr == nil {
		return failf("missing_trace", "completed run missing qualified trace evidence")
	}
	if strings.TrimSpace(tr.TraceID) == "" {
		return failf("missing_trace_id", "trace.trace_id empty")
	}
	if tr.SpanCount <= 0 {
		return failf("empty_trace", "trace.span_count=%d", tr.SpanCount)
	}
	if tr.SubgraphSpanCount <= 0 {
		return failf("empty_subgraph", "trace.subgraph_span_count=%d (marker subgraph empty — unrelated traffic?)", tr.SubgraphSpanCount)
	}
	if len(tr.AppServices) < minApp {
		return failf("insufficient_app_services", "trace.app_services=%v (need >= %d of %v)",
			tr.AppServices, minApp, tracesmoke.ExpectedAppServices)
	}
	if requireEdge {
		if !tr.HasCrossServiceEdge {
			return failf("missing_cross_service", "trace.has_cross_service_edge=false")
		}
		if len(tr.CrossServiceEdges) == 0 {
			return failf("missing_cross_service", "trace.cross_service_edges empty despite has_cross_service_edge")
		}
		for _, e := range tr.CrossServiceEdges {
			if e.ParentService == "" || e.ChildService == "" || e.ParentService == e.ChildService {
				return failf("bad_cross_service", "invalid cross-service edge %+v", e)
			}
			if e.ParentSpanID == "" || e.ChildSpanID == "" {
				return failf("bad_cross_service", "cross-service edge missing span ids %+v", e)
			}
		}
	}
	// Timing evidence: either aggregate duration or per-service span timings.
	hasTiming := tr.TraceDurationMs != nil && *tr.TraceDurationMs >= 0
	if !hasTiming {
		for _, s := range tr.Services {
			if s.SpanCount > 0 && s.MaxSpanDurationMs >= 0 {
				hasTiming = true
				break
			}
		}
	}
	if !hasTiming {
		return failf("missing_timing", "trace lacks nonnegative duration evidence")
	}
	return nil
}

func assertServices(run *runpulse.Run, minMapped int, graphNodes map[string]struct{}) error {
	mapped := 0
	var mappedNames []string
	for _, s := range run.Services {
		if s.Participation != "" && s.Participation != "observed" {
			return failf("fabricated_participation", "service %q participation=%q (must be observed)", s.OTELServiceName, s.Participation)
		}
		if !s.Mapped {
			continue
		}
		mapped++
		mappedNames = append(mappedNames, s.OTELServiceName)
		if s.GraphNode == "" {
			return failf("missing_graph_node", "mapped service %q has empty graph_node", s.OTELServiceName)
		}
		if graphNodes != nil {
			if _, ok := graphNodes[s.GraphNode]; !ok {
				return failf("graph_node_missing", "mapped graph_node %q not present in /api/graph nodes", s.GraphNode)
			}
		}
	}
	if mapped < minMapped {
		return failf("insufficient_mapped", "mapped services=%d (%v) need >= %d", mapped, mappedNames, minMapped)
	}
	if len(run.GraphNodes) < minMapped {
		return failf("insufficient_graph_nodes", "graph_nodes=%v need >= %d", run.GraphNodes, minMapped)
	}
	for _, n := range run.GraphNodes {
		if graphNodes != nil {
			if _, ok := graphNodes[n]; !ok {
				return failf("graph_node_missing", "run.graph_nodes entry %q not in /api/graph", n)
			}
		}
	}
	return nil
}

func assertSafety(run *runpulse.Run) error {
	blob, err := json.Marshal(run)
	if err != nil {
		return failf("marshal", "could not marshal run for safety scan: %v", err)
	}
	lower := strings.ToLower(string(blob))
	for _, needle := range []string{
		`"data":[{"traceid"`, // raw Jaeger list payload shape
		"scenario_ground_truth",
		"ground_truth",
		"authorization: bearer",
		"x-api-key",
	} {
		if strings.Contains(lower, needle) {
			return failf("unsafe_payload", "run JSON appears to contain disallowed content (%s)", needle)
		}
	}
	return nil
}

// GraphNodeNamesFromAPIGraph extracts node "name" values from /api/graph JSON.
func GraphNodeNamesFromAPIGraph(body []byte) (map[string]struct{}, error) {
	var g struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(body, &g); err != nil {
		return nil, failf("malformed_graph", "graph JSON invalid: %v", err)
	}
	out := make(map[string]struct{}, len(g.Nodes))
	for _, n := range g.Nodes {
		name := strings.TrimSpace(n.Name)
		if name != "" {
			out[name] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil, failf("empty_graph", "graph has no named nodes")
	}
	return out, nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
