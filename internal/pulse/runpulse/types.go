package runpulse

import (
	"time"

	"github.com/yashg4509/perch/internal/pulse/astronomy/tracesmoke"
)

// Run status values (terminal: completed, failed, timed_out).
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusTimedOut  = "timed_out"
)

// Phase values exposed when the backend tracks them.
const (
	PhaseQueued          = "queued"
	PhaseSendingRequest  = "sending_request"
	PhaseWaitingForTrace = "waiting_for_trace"
	PhaseMappingServices = "mapping_services"
	PhaseDone            = "done"
)

// FailureCode classifies diagnostic failures for the UI.
const (
	FailUnsupportedWorkflow  = "unsupported_workflow"
	FailWorkflowUnavailable  = "workflow_unavailable"
	FailConcurrencyLimit     = "concurrency_limit"
	FailRequestFailed        = "request_failed"
	FailTelemetryUnreachable = "telemetry_unreachable"
	FailTraceNotFound        = "trace_not_found"
	FailTraceIncomplete      = "trace_incomplete"
	FailTimedOut             = "timed_out"
	FailCancelled            = "cancelled"
	FailMisconfigured        = "server_misconfigured"
	FailInternal             = "internal_error"
)

// MappedService binds an observed OTEL service name to a Pulse/graph identity.
type MappedService struct {
	OTELServiceName string `json:"otel_service_name"`
	PulseServiceID  string `json:"pulse_service_id,omitempty"`
	GraphNode       string `json:"graph_node,omitempty"`
	MappingSource   string `json:"mapping_source,omitempty"`
	Mapped          bool   `json:"mapped"`
	HasError        bool   `json:"has_error"`
	// Participation is always "observed" for listed services (never config-only).
	Participation string `json:"participation"`
}

// RequestResult is the observed HTTP probe outcome.
type RequestResult struct {
	Method        string  `json:"method"`
	URLRedacted   string  `json:"url_redacted"`
	StatusCode    int     `json:"status_code,omitempty"`
	ElapsedMs     float64 `json:"elapsed_ms"`
	Attempts      int     `json:"attempts"`
	OK            bool    `json:"ok"`
	Error         string  `json:"error,omitempty"`
	Marker        string  `json:"marker"`
	BodyBytesRead int     `json:"body_bytes_read,omitempty"`
}

// Run is one user-triggered diagnostic execution (in-memory MVP).
type Run struct {
	ID             string     `json:"id"`
	WorkflowID     string     `json:"workflow_id"`
	DisplayName    string     `json:"display_name"`
	Environment    string     `json:"environment"`
	Status         string     `json:"status"`
	Phase          string     `json:"phase,omitempty"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	FailureCode    string     `json:"failure_code,omitempty"`
	FailureMessage string     `json:"failure_message,omitempty"`
	Warnings       []string   `json:"warnings,omitempty"`

	Request  *RequestResult       `json:"request,omitempty"`
	Trace    *tracesmoke.Evidence `json:"trace,omitempty"`
	Services []MappedService      `json:"services,omitempty"`
	// UnmappedServices lists OTEL names with no graph binding.
	UnmappedServices []string `json:"unmapped_services,omitempty"`
	// GraphNodes are distinct mapped compose_service / perch.yaml node IDs.
	GraphNodes []string `json:"graph_nodes,omitempty"`
	// GraphNodesWithErrors are mapped nodes that had error spans.
	GraphNodesWithErrors []string `json:"graph_nodes_with_errors,omitempty"`

	Limitations []string `json:"limitations,omitempty"`
}

// Clone returns a deep-ish copy safe for concurrent readers.
func (r *Run) Clone() *Run {
	if r == nil {
		return nil
	}
	cp := *r
	if r.EndedAt != nil {
		t := *r.EndedAt
		cp.EndedAt = &t
	}
	if r.Request != nil {
		req := *r.Request
		cp.Request = &req
	}
	if r.Trace != nil {
		tr := *r.Trace
		tr.ObservedServices = append([]string(nil), r.Trace.ObservedServices...)
		tr.AppServices = append([]string(nil), r.Trace.AppServices...)
		tr.Services = append([]tracesmoke.ServiceEvidence(nil), r.Trace.Services...)
		tr.Spans = append([]tracesmoke.SpanEvidence(nil), r.Trace.Spans...)
		tr.CrossServiceEdges = append([]tracesmoke.CrossServiceEdge(nil), r.Trace.CrossServiceEdges...)
		tr.Warnings = append([]string(nil), r.Trace.Warnings...)
		if r.Trace.TraceDurationMs != nil {
			v := *r.Trace.TraceDurationMs
			tr.TraceDurationMs = &v
		}
		cp.Trace = &tr
	}
	cp.Services = append([]MappedService(nil), r.Services...)
	cp.UnmappedServices = append([]string(nil), r.UnmappedServices...)
	cp.GraphNodes = append([]string(nil), r.GraphNodes...)
	cp.GraphNodesWithErrors = append([]string(nil), r.GraphNodesWithErrors...)
	cp.Warnings = append([]string(nil), r.Warnings...)
	cp.Limitations = append([]string(nil), r.Limitations...)
	return &cp
}
