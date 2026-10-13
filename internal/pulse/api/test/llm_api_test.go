package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/pulse/api"
	"github.com/yashg4509/perch/internal/pulse/investigation"
	"github.com/yashg4509/perch/internal/pulse/investigation/llm"
)

func TestLLMAPI_StatusDisabledNoProposeOnGET(t *testing.T) {
	dir := t.TempDir()
	_ = seedPaymentIncident(t, dir)
	h := investigationHandler(t, dir)
	mux := http.NewServeMux()
	h.Register(mux)

	stReq := httptest.NewRequest(http.MethodGet, "/api/pulse/llm/status", nil)
	stRR := httptest.NewRecorder()
	mux.ServeHTTP(stRR, stReq)
	if stRR.Code != http.StatusOK {
		t.Fatalf("status %d %s", stRR.Code, stRR.Body.String())
	}
	var st api.LLMStatusResponse
	if err := json.Unmarshal(stRR.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Status.Available {
		t.Fatal("expected unavailable without LLM service")
	}
	if bytes.Contains(stRR.Body.Bytes(), []byte("sk-")) {
		t.Fatal("status leaked key material")
	}
}

func TestLLMAPI_ProposePersistsAndLeavesF1Intact(t *testing.T) {
	dir := t.TempDir()
	inc := seedPaymentIncident(t, dir)
	h := investigationHandler(t, dir)
	invStore := h.Investigations.Store
	fake := &llm.FakeClient{}
	h.LLM = &llm.Service{
		Config: llm.Config{
			Enabled: true, APIKey: "test-key", BaseURL: "http://127.0.0.1:9/v1",
			Model: "fake", Timeout: 5 * time.Second, MaxTokens: 800, MaxConcurrent: 2,
		},
		Investigations: llm.InvestigationAdapter{Store: invStore},
		Store:          llm.NewFileStore(dir),
		Client:         fake,
		Now:            func() time.Time { return time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC) },
	}
	mux := http.NewServeMux()
	h.Register(mux)

	// Create F1 investigation.
	body := `{"incident_id":` + jsonQuote(inc.IncidentID) + `,"reuse_identical":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/pulse/investigations", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:1"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create %d %s", rr.Code, rr.Body.String())
	}
	var created api.CreateInvestigationResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &created)
	inv := created.Investigation

	// Wire fake content now that we know fingerprint.
	sid := ""
	for _, s := range inv.ObservedSymptoms {
		sid = s.ID
		break
	}
	fake.Response = llm.ChatResponse{
		Content: llm.ExampleValidModelJSON(inv.InvestigationID, inv.ContentFingerprint, sid, "astronomy/local/payment"),
		Model:   "fake",
	}

	// GET investigation must not call LLM.
	getReq := httptest.NewRequest(http.MethodGet, "/api/pulse/investigations/"+inv.InvestigationID, nil)
	getRR := httptest.NewRecorder()
	mux.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK || fake.Calls != 0 {
		t.Fatalf("GET must not call LLM; code=%d calls=%d", getRR.Code, fake.Calls)
	}

	proposeBody := `{"expected_fingerprint":` + jsonQuote(inv.ContentFingerprint) + `}`
	pReq := httptest.NewRequest(http.MethodPost, "/api/pulse/investigations/"+inv.InvestigationID+"/llm-propose", bytes.NewBufferString(proposeBody))
	pReq.Header.Set("Content-Type", "application/json")
	pReq.RemoteAddr = "127.0.0.1:2"
	pRR := httptest.NewRecorder()
	mux.ServeHTTP(pRR, pReq)
	if pRR.Code != http.StatusCreated {
		t.Fatalf("propose %d %s", pRR.Code, pRR.Body.String())
	}
	var prop api.LLMProposeResponse
	if err := json.Unmarshal(pRR.Body.Bytes(), &prop); err != nil {
		t.Fatal(err)
	}
	if prop.Proposal.Diagnosed || prop.Proposal.ProposalID == "" {
		t.Fatal("bad proposal")
	}
	if fake.Calls != 1 {
		t.Fatalf("calls %d", fake.Calls)
	}

	// List proposals
	listReq := httptest.NewRequest(http.MethodGet, "/api/pulse/investigations/"+inv.InvestigationID+"/proposals", nil)
	listRR := httptest.NewRecorder()
	mux.ServeHTTP(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("list %d", listRR.Code)
	}

	// F1 unchanged
	again, err := h.Investigations.Get(inv.InvestigationID)
	if err != nil {
		t.Fatal(err)
	}
	if again.ContentFingerprint != inv.ContentFingerprint {
		t.Fatal("F1 mutated")
	}
	if again.GenerationMethod != investigation.GenerationMethodDeterministic {
		t.Fatal("F1 method changed")
	}

	// Proposal feedback
	fbBody := `{"assessment":"partially_useful","rationale":"ok"}`
	fbReq := httptest.NewRequest(http.MethodPost, "/api/pulse/proposals/"+prop.Proposal.ProposalID+"/feedback", bytes.NewBufferString(fbBody))
	fbReq.Header.Set("Content-Type", "application/json")
	fbReq.RemoteAddr = "127.0.0.1:3"
	fbRR := httptest.NewRecorder()
	mux.ServeHTTP(fbRR, fbReq)
	if fbRR.Code != http.StatusCreated {
		t.Fatalf("proposal feedback %d %s", fbRR.Code, fbRR.Body.String())
	}

	// Status available
	stReq := httptest.NewRequest(http.MethodGet, "/api/pulse/llm/status", nil)
	stRR := httptest.NewRecorder()
	mux.ServeHTTP(stRR, stReq)
	var st api.LLMStatusResponse
	_ = json.Unmarshal(stRR.Body.Bytes(), &st)
	if !st.Status.Available {
		t.Fatal("expected available")
	}

	_ = context.Background()
}
