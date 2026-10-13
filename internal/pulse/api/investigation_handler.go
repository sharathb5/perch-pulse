package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/investigation"
	"github.com/yashg4509/perch/internal/pulse/runpulse"
)

const maxInvestigationBody = 4 * 1024

// registerInvestigationRoutes mounts F1 investigation endpoints.
// Feedback uses a /feedback suffix on the {...} catch-all (Go mux forbids {...} mid-path).
func (h *Handler) registerInvestigationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/pulse/investigations", h.handleCreateInvestigation)
	mux.HandleFunc("POST /api/pulse/investigations/{id...}", h.handleInvestigationMutation)
	mux.HandleFunc("GET /api/pulse/investigations/{id...}", h.handleGetInvestigation)
}

func (h *Handler) investigations() *investigation.Service {
	if h == nil {
		return nil
	}
	return h.Investigations
}

func (h *Handler) handleCreateInvestigation(w http.ResponseWriter, r *http.Request) {
	svc := h.investigations()
	if svc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "investigation_unavailable", "Investigation service is not configured")
		return
	}
	if err := authorizeMutatingPulse(r); err != nil {
		writeAPIError(w, http.StatusForbidden, err.code, err.msg)
		return
	}
	ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if ct == "" || !strings.HasPrefix(ct, "application/json") {
		writeAPIError(w, http.StatusUnsupportedMediaType, "invalid_content_type", "Content-Type must be application/json")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxInvestigationBody+1))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_body", "could not read request body")
		return
	}
	if len(body) > maxInvestigationBody {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body too large")
		return
	}
	var req CreateInvestigationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "malformed JSON body")
		return
	}
	req.IncidentID = strings.TrimSpace(req.IncidentID)
	if req.IncidentID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_input", "incident_id is required")
		return
	}
	if err := ValidateResourceID(req.IncidentID); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if len(req.LinkedRunIDs) > 5 {
		writeAPIError(w, http.StatusBadRequest, "invalid_input", "linked_run_ids is limited to 5 entries")
		return
	}
	var linked []investigation.LinkedRunPulse
	for _, rid := range req.LinkedRunIDs {
		rid = strings.TrimSpace(rid)
		if rid == "" {
			continue
		}
		if !strings.HasPrefix(rid, "run_") {
			writeAPIError(w, http.StatusBadRequest, "invalid_input", "linked_run_ids must start with run_")
			return
		}
		note := "Explicitly linked by client; not auto-executed by investigation assembly."
		status := ""
		workflow := ""
		if h.Runs != nil {
			if run, ok := h.Runs.GetRun(rid); ok && run != nil {
				status = string(run.Status)
				workflow = run.WorkflowID
				note = "Linked Run Pulse snapshot at investigation time — observing request behavior is not root-cause proof."
			} else {
				note = "Linked run id was not found in the in-memory Run Pulse store (may have expired after viz restart)."
			}
		}
		linked = append(linked, investigation.LinkedRunPulse{
			RunID:          rid,
			WorkflowID:     workflow,
			Status:         status,
			Classification: investigation.ClassUnknown,
			Note:           note,
		})
	}

	inv, err := svc.Create(investigation.CreateOptions{
		IncidentID:     req.IncidentID,
		ReuseIdentical: req.ReuseIdentical,
		LinkedRuns:     linked,
	})
	if err != nil {
		writeInvestigationError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusCreated, CreateInvestigationResponse{
		SchemaVersion: SchemaInvestigationCreate,
		GeneratedAt:   time.Now().UTC(),
		Investigation: inv,
		Limitations: []string{
			"Investigation is an evidence snapshot — not a confirmed root-cause diagnosis.",
			"Snapshots are immutable; regenerate for newer evidence.",
			"Loopback auth is not production multi-user security.",
		},
	})
}

func (h *Handler) handleGetInvestigation(w http.ResponseWriter, r *http.Request) {
	svc := h.investigations()
	if svc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "investigation_unavailable", "Investigation service is not configured")
		return
	}
	id := r.PathValue("id")
	if strings.HasSuffix(id, "/feedback") {
		h.handleListFeedback(w, r, strings.TrimSuffix(id, "/feedback"))
		return
	}
	if strings.HasSuffix(id, "/proposals") {
		h.handleListProposals(w, r, strings.TrimSuffix(id, "/proposals"))
		return
	}
	if err := ValidateResourceID(id); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	inv, err := svc.Get(id)
	if err != nil {
		writeInvestigationError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusOK, InvestigationResponse{
		SchemaVersion: SchemaInvestigation,
		GeneratedAt:   time.Now().UTC(),
		Investigation: inv,
		Limitations: []string{
			"Persisted snapshot — not current live telemetry.",
			"diagnosed is always false in F1.",
		},
	})
}

func (h *Handler) handleInvestigationMutation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.HasSuffix(id, "/feedback") {
		h.handleCreateFeedback(w, r, strings.TrimSuffix(id, "/feedback"))
		return
	}
	if strings.HasSuffix(id, "/llm-propose") {
		h.handleLLMPropose(w, r, strings.TrimSuffix(id, "/llm-propose"))
		return
	}
	writeAPIError(w, http.StatusNotFound, "not_found", "POST /api/pulse/investigations/{id}/feedback or .../llm-propose")
}

func (h *Handler) serveInvestigationContext(w http.ResponseWriter, r *http.Request, incidentID string) {
	_ = r
	svc := h.investigations()
	if svc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "investigation_unavailable", "Investigation service is not configured")
		return
	}
	if err := ValidateResourceID(incidentID); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	inv, ctx, err := svc.ContextForIncident(incidentID)
	if err != nil {
		writeInvestigationError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusOK, InvestigationContextResponse{
		SchemaVersion: SchemaInvestigationContext,
		GeneratedAt:   time.Now().UTC(),
		Investigation: inv,
		AgentContext:  ctx,
		Limitations: []string{
			"Agent context separates FACTS / HYPOTHESES / UNKNOWN / NEXT STEPS.",
			"No credentials, ground-truth labels, or unbounded telemetry dumps.",
		},
	})
}

func (h *Handler) handleCreateFeedback(w http.ResponseWriter, r *http.Request, id string) {
	svc := h.investigations()
	if svc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "investigation_unavailable", "Investigation service is not configured")
		return
	}
	if err := authorizeMutatingPulse(r); err != nil {
		writeAPIError(w, http.StatusForbidden, err.code, err.msg)
		return
	}
	if err := ValidateResourceID(id); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if ct == "" || !strings.HasPrefix(ct, "application/json") {
		writeAPIError(w, http.StatusUnsupportedMediaType, "invalid_content_type", "Content-Type must be application/json")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxInvestigationBody+1))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_body", "could not read request body")
		return
	}
	if len(body) > maxInvestigationBody {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body too large")
		return
	}
	var req CreateFeedbackRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "malformed JSON body")
		return
	}
	fb := investigation.Feedback{
		SchemaVersion:          investigation.FeedbackSchemaVersion,
		InvestigationID:        id,
		Assessment:             strings.TrimSpace(req.Assessment),
		UsefulHypothesisIDs:    req.UsefulHypothesisIDs,
		RejectedHypothesisIDs:  req.RejectedHypothesisIDs,
		Rationale:              req.Rationale,
		ReviewerConfirmedCause: req.ReviewerConfirmedCause,
		ConfirmationMethod:     req.ConfirmationMethod,
	}
	stored, err := svc.AddFeedback(fb)
	if err != nil {
		writeInvestigationError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusCreated, CreateFeedbackResponse{
		SchemaVersion: SchemaFeedbackCreate,
		GeneratedAt:   time.Now().UTC(),
		Feedback:      stored,
		Limitations:   investigation.FeedbackLimitations(),
	})
}

func (h *Handler) handleListFeedback(w http.ResponseWriter, r *http.Request, id string) {
	_ = r
	svc := h.investigations()
	if svc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "investigation_unavailable", "Investigation service is not configured")
		return
	}
	if err := ValidateResourceID(id); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	list, err := svc.ListFeedback(id)
	if err != nil {
		writeInvestigationError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusOK, FeedbackListResponse{
		SchemaVersion: SchemaFeedbackList,
		GeneratedAt:   time.Now().UTC(),
		Count:         len(list),
		Feedback:      list,
		Limitations:   investigation.FeedbackLimitations(),
	})
}

type authzErr struct {
	code string
	msg  string
}

func (e *authzErr) Error() string { return e.msg }

func authorizeMutatingPulse(r *http.Request) *authzErr {
	if !runpulse.IsLoopbackRemoteAddr(r.RemoteAddr) {
		return &authzErr{code: "loopback_only", msg: "Pulse investigation writes may only be started from loopback clients"}
	}
	if xf := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xf != "" {
		return &authzErr{code: "proxy_rejected", msg: "X-Forwarded-For is not accepted for Pulse investigation writes"}
	}
	if err := authorizeBrowserOrigin(r); err != nil {
		return &authzErr{code: "origin_rejected", msg: err.Error()}
	}
	return nil
}

func writeInvestigationError(w http.ResponseWriter, err error) {
	var nf *investigation.NotFoundError
	if errors.As(err, &nf) {
		writeAPIError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	var ve *investigation.ValidationError
	if errors.As(err, &ve) {
		writeAPIError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	msg := err.Error()
	if strings.Contains(msg, "already exists") {
		writeAPIError(w, http.StatusConflict, "conflict", msg)
		return
	}
	if strings.Contains(msg, "incident_id is required") || strings.Contains(msg, "invalid") {
		writeAPIError(w, http.StatusBadRequest, "invalid_input", msg)
		return
	}
	writeAPIError(w, http.StatusInternalServerError, "investigation_failed", msg)
}
