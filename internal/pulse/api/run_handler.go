package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/runpulse"
)

const maxCreateRunBody = 4 * 1024

// registerRunRoutes mounts Run Pulse endpoints. POST create requires loopback.
func (h *Handler) registerRunRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/pulse/workflows", h.handleWorkflows)
	mux.HandleFunc("POST /api/pulse/runs", h.handleCreateRun)
	mux.HandleFunc("GET /api/pulse/runs/{id}", h.handleGetRun)
}

func (h *Handler) handleWorkflows(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Runs == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "run_pulse_unavailable", "Run Pulse engine is not configured")
		return
	}
	list := h.Runs.ListWorkflows()
	resp := WorkflowsResponse{
		SchemaVersion: SchemaWorkflows,
		GeneratedAt:   time.Now().UTC(),
		Limitations: []string{
			"Allowlisted workflows only — clients cannot supply arbitrary URLs.",
			"MVP supports the local Astronomy Shop recommendations-browse diagnostic.",
			"Not continuous monitoring, incident detection, or automated remediation.",
		},
		Count:     len(list),
		Workflows: list,
	}
	writeAPIJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleCreateRun(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Runs == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "run_pulse_unavailable", "Run Pulse engine is not configured")
		return
	}
	if !runpulse.IsLoopbackRemoteAddr(r.RemoteAddr) {
		writeAPIError(w, http.StatusForbidden, "loopback_only",
			"Run Pulse diagnostics may only be started from loopback clients (perch viz is localhost-oriented)")
		return
	}
	// Do not trust forwarded headers for authorization.
	if xf := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xf != "" {
		writeAPIError(w, http.StatusForbidden, "proxy_rejected",
			"X-Forwarded-For is not accepted for Run Pulse start")
		return
	}
	if err := authorizeBrowserOrigin(r); err != nil {
		writeAPIError(w, http.StatusForbidden, "origin_rejected", err.Error())
		return
	}
	ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if ct == "" || !strings.HasPrefix(ct, "application/json") {
		writeAPIError(w, http.StatusUnsupportedMediaType, "invalid_content_type",
			"Content-Type must be application/json")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxCreateRunBody+1))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_body", "could not read request body")
		return
	}
	if len(body) > maxCreateRunBody {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body too large")
		return
	}
	var req CreateRunRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "malformed JSON body")
		return
	}
	req.WorkflowID = strings.TrimSpace(req.WorkflowID)
	if req.WorkflowID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_input", "workflow_id is required")
		return
	}

	run, err := h.Runs.StartRun(req.WorkflowID)
	if err != nil {
		var se *runpulse.StartError
		if errors.As(err, &se) {
			status := http.StatusBadRequest
			switch se.Code {
			case runpulse.FailConcurrencyLimit:
				status = http.StatusTooManyRequests
			case runpulse.FailWorkflowUnavailable, runpulse.FailMisconfigured:
				status = http.StatusServiceUnavailable
			case runpulse.FailUnsupportedWorkflow:
				status = http.StatusNotFound
			}
			writeAPIError(w, status, se.Code, se.Message)
			return
		}
		writeAPIError(w, http.StatusInternalServerError, runpulse.FailInternal, err.Error())
		return
	}

	resp := CreateRunResponse{
		SchemaVersion: SchemaRunCreate,
		GeneratedAt:   time.Now().UTC(),
		Run:           run,
		Limitations: []string{
			"Poll GET /api/pulse/runs/{id} for status; do not POST again to refresh.",
			"completed requires successful trace qualification, not merely HTTP 200.",
		},
	}
	writeAPIJSON(w, http.StatusAccepted, resp)
}

func (h *Handler) handleGetRun(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Runs == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "run_pulse_unavailable", "Run Pulse engine is not configured")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if err := ValidateResourceID(id); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if !strings.HasPrefix(id, "run_") {
		writeAPIError(w, http.StatusBadRequest, "invalid_id", "run id must start with run_")
		return
	}
	run, ok := h.Runs.GetRun(id)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found",
			"run not found (expired, never existed, or lost after viz restart)")
		return
	}
	resp := RunResponse{
		SchemaVersion: SchemaRun,
		GeneratedAt:   time.Now().UTC(),
		Run:           run,
		Limitations: []string{
			"In-memory MVP store — runs do not survive perch viz restart.",
			"Span timings are individual observed spans; do not treat summed overlapping spans as end-to-end latency.",
			"Request elapsed_ms is server-measured HTTP time; trace_duration_ms is subgraph wall time when present.",
		},
	}
	writeAPIJSON(w, http.StatusOK, resp)
}

// authorizeBrowserOrigin rejects cross-site browser POSTs to the local viz.
// Same-origin (or missing Origin for non-browser clients) is allowed when Host
// is loopback. Cross-origin pages cannot start diagnostics.
func authorizeBrowserOrigin(r *http.Request) error {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		// Non-browser clients (curl) typically omit Origin.
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid Origin")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("Origin scheme not allowed")
	}
	host := u.Hostname()
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("cross-origin Run Pulse start is not allowed")
		}
	}
	reqHost := r.Host
	if h, _, err := net.SplitHostPort(reqHost); err == nil {
		reqHost = h
	}
	if reqHost != "localhost" && reqHost != "127.0.0.1" && reqHost != "::1" {
		if ip := net.ParseIP(reqHost); ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("Host is not a loopback viz listener")
		}
	}
	return nil
}
