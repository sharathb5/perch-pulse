package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/investigation"
	"github.com/yashg4509/perch/internal/pulse/investigation/llm"
)

func (h *Handler) registerLLMRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/pulse/llm/status", h.handleLLMStatus)
	mux.HandleFunc("GET /api/pulse/proposals/{id...}", h.handleGetProposal)
	mux.HandleFunc("POST /api/pulse/proposals/{id...}", h.handleProposalMutation)
}

func (h *Handler) llm() *llm.Service {
	if h == nil {
		return nil
	}
	return h.LLM
}

func (h *Handler) handleLLMStatus(w http.ResponseWriter, r *http.Request) {
	_ = r
	svc := h.llm()
	var st llm.Status
	if svc == nil {
		st = llm.Status{
			SchemaVersion: llm.StatusSchemaVersion,
			Available:     false,
			Reason:        "LLM investigation is not configured on this viz process",
			Provider:      "openai_compatible",
		}
	} else {
		st = svc.Status()
	}
	writeAPIJSON(w, http.StatusOK, LLMStatusResponse{
		SchemaVersion: SchemaLLMStatus,
		GeneratedAt:   time.Now().UTC(),
		Status:        st,
		Limitations: []string{
			"Status never includes API keys or full endpoint URLs.",
			"LLM mode is optional and disabled by default.",
			"Deterministic F1 investigation remains available when LLM is unavailable.",
		},
	})
}

func (h *Handler) handleLLMPropose(w http.ResponseWriter, r *http.Request, investigationID string) {
	svc := h.llm()
	if svc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "llm_unavailable", "LLM investigation is not configured")
		return
	}
	if err := authorizeMutatingPulse(r); err != nil {
		writeAPIError(w, http.StatusForbidden, err.code, err.msg)
		return
	}
	if err := ValidateResourceID(investigationID); err != nil {
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
	var req LLMProposeRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_json", "malformed JSON body")
			return
		}
	}
	prop, err := svc.Propose(r.Context(), llm.ProposeOptions{
		InvestigationID:     investigationID,
		ExpectedFingerprint: strings.TrimSpace(req.ExpectedFingerprint),
	})
	if err != nil {
		writeLLMError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusCreated, LLMProposeResponse{
		SchemaVersion: SchemaLLMPropose,
		GeneratedAt:   time.Now().UTC(),
		Proposal:      prop,
		Limitations: append([]string{
			"LLM proposal is not a confirmed root-cause diagnosis.",
			"F1 deterministic investigation is unchanged.",
		}, llm.DefaultLimitations()...),
	})
}

func (h *Handler) handleListProposals(w http.ResponseWriter, r *http.Request, investigationID string) {
	_ = r
	svc := h.llm()
	if svc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "llm_unavailable", "LLM investigation is not configured")
		return
	}
	if err := ValidateResourceID(investigationID); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	list, err := svc.ListForInvestigation(investigationID)
	if err != nil {
		writeLLMError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusOK, LLMProposalListResponse{
		SchemaVersion: SchemaLLMProposalList,
		GeneratedAt:   time.Now().UTC(),
		Count:         len(list),
		Proposals:     list,
		Limitations: []string{
			"Proposals are immutable model outputs validated against F1 evidence.",
			"Listing does not invoke the provider.",
		},
	})
}

func (h *Handler) handleGetProposal(w http.ResponseWriter, r *http.Request) {
	svc := h.llm()
	if svc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "llm_unavailable", "LLM investigation is not configured")
		return
	}
	id := r.PathValue("id")
	if strings.HasSuffix(id, "/feedback") {
		h.handleListProposalFeedback(w, r, strings.TrimSuffix(id, "/feedback"))
		return
	}
	if err := ValidateResourceID(id); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	prop, err := svc.Get(id)
	if err != nil {
		writeLLMError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusOK, LLMProposalResponse{
		SchemaVersion: SchemaLLMProposal,
		GeneratedAt:   time.Now().UTC(),
		Proposal:      prop,
		Limitations:   llm.DefaultLimitations(),
	})
}

func (h *Handler) handleProposalMutation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.HasSuffix(id, "/feedback") {
		h.handleCreateProposalFeedback(w, r, strings.TrimSuffix(id, "/feedback"))
		return
	}
	writeAPIError(w, http.StatusNotFound, "not_found", "POST /api/pulse/proposals/{id}/feedback to submit proposal feedback")
}

func (h *Handler) handleCreateProposalFeedback(w http.ResponseWriter, r *http.Request, id string) {
	svc := h.llm()
	if svc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "llm_unavailable", "LLM investigation is not configured")
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
	var req LLMProposalFeedbackRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "malformed JSON body")
		return
	}
	stored, err := svc.AddFeedback(llm.Feedback{
		SchemaVersion:         llm.ProposalFeedbackSchemaVersion,
		ProposalID:            id,
		Assessment:            strings.TrimSpace(req.Assessment),
		UsefulHypothesisIDs:   req.UsefulHypothesisIDs,
		RejectedHypothesisIDs: req.RejectedHypothesisIDs,
		Rationale:             req.Rationale,
	})
	if err != nil {
		writeLLMError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusCreated, LLMProposalFeedbackResponse{
		SchemaVersion: SchemaLLMProposalFeedbackCreate,
		GeneratedAt:   time.Now().UTC(),
		Feedback:      stored,
		Limitations:   llm.FeedbackLimitations(),
	})
}

func (h *Handler) handleListProposalFeedback(w http.ResponseWriter, r *http.Request, id string) {
	_ = r
	svc := h.llm()
	if svc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "llm_unavailable", "LLM investigation is not configured")
		return
	}
	if err := ValidateResourceID(id); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	list, err := svc.ListFeedback(id)
	if err != nil {
		writeLLMError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusOK, LLMProposalFeedbackListResponse{
		SchemaVersion: SchemaLLMProposalFeedbackList,
		GeneratedAt:   time.Now().UTC(),
		Count:         len(list),
		Feedback:      list,
		Limitations:   llm.FeedbackLimitations(),
	})
}

func writeLLMError(w http.ResponseWriter, err error) {
	var unavail *llm.UnavailableError
	if errors.As(err, &unavail) {
		writeAPIError(w, http.StatusServiceUnavailable, "llm_unavailable", err.Error())
		return
	}
	var busy *llm.BusyError
	if errors.As(err, &busy) {
		writeAPIError(w, http.StatusTooManyRequests, "llm_busy", err.Error())
		return
	}
	var rate *llm.RateLimitError
	if errors.As(err, &rate) {
		writeAPIError(w, http.StatusTooManyRequests, "llm_rate_limited", err.Error())
		return
	}
	var ve *llm.ValidationError
	if errors.As(err, &ve) {
		writeAPIError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	var nf *investigation.NotFoundError
	if errors.As(err, &nf) {
		writeAPIError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	msg := err.Error()
	if strings.Contains(msg, "timeout") {
		writeAPIError(w, http.StatusGatewayTimeout, "llm_timeout", "llm provider timeout")
		return
	}
	// Never forward raw provider/transport text — may echo credentials.
	_ = msg
	writeAPIError(w, http.StatusInternalServerError, "llm_failed", "llm investigation failed")
}
