package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/pulse/api"
	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/incident"
	"github.com/yashg4509/perch/internal/pulse/investigation"
)

func seedPaymentIncident(t *testing.T, dir string) incident.Incident {
	t.Helper()
	at := time.Date(2026, 10, 10, 13, 0, 0, 0, time.UTC)
	pay := "astronomy/local/payment"
	f := detect.Finding{
		SchemaVersion: detect.SchemaVersion, FindingID: detect.FindingIDFor(pay, detect.SignalErrorRate, at),
		DetectorVersion: detect.DetectorVersion, ServiceID: pay, SignalType: detect.SignalErrorRate,
		ObservedWindow: detect.Window{Start: at.Add(-40 * time.Second), End: at},
		BaselineWindow: detect.Window{Start: at.Add(-3 * time.Minute), End: at.Add(-40 * time.Second)},
		ObservedValue:  0.41, BaselineValue: 0, Threshold: 0.05, Score: 2,
		Severity: detect.SeverityCritical, FirstDetectedAt: at, LastObservedAt: at.Add(30 * time.Second),
		Summary: "payment error regression",
	}
	var samples []detect.Sample
	for i := 0; i < 5; i++ {
		samples = append(samples, detect.Sample{ServiceID: pay, Signal: detect.SignalErrorRate, Value: 0,
			ObservedAt: at.Add(-90 * time.Second).Add(time.Duration(i) * 10 * time.Second), Source: "test"})
	}
	for i := 0; i < 5; i++ {
		samples = append(samples, detect.Sample{ServiceID: pay, Signal: detect.SignalErrorRate, Value: 0.41,
			ObservedAt: at.Add(time.Duration(i) * 5 * time.Second), Source: "test"})
	}
	inc, err := incident.Build(incident.BuildInput{Findings: []detect.Finding{f}, Samples: samples, Environment: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if err := incident.NewFileStore(dir).Append(inc); err != nil {
		t.Fatal(err)
	}
	return inc
}

func investigationHandler(t *testing.T, dir string) *api.Handler {
	t.Helper()
	reader, err := api.NewFileReader(dir, api.EmptyMapper{})
	if err != nil {
		t.Fatal(err)
	}
	return &api.Handler{
		Reader: reader,
		Investigations: &investigation.Service{
			Incidents: reader.Incidents,
			Changes:   reader.Changes,
			Store:     investigation.NewFileStore(dir),
			Now:       func() time.Time { return time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC) },
		},
	}
}

func TestInvestigationAPI_CreateGetFeedbackContext(t *testing.T) {
	dir := t.TempDir()
	inc := seedPaymentIncident(t, dir)
	h := investigationHandler(t, dir)
	mux := http.NewServeMux()
	h.Register(mux)

	body := `{"incident_id":` + jsonQuote(inc.IncidentID) + `,"reuse_identical":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/pulse/investigations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create %d %s", rr.Code, rr.Body.String())
	}
	var created api.CreateInvestigationResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Investigation.Diagnosed {
		t.Fatal("diagnosed")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/pulse/investigations/"+created.Investigation.InvestigationID, nil)
	getRR := httptest.NewRecorder()
	mux.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get %d %s", getRR.Code, getRR.Body.String())
	}

	ctxReq := httptest.NewRequest(http.MethodGet, "/api/pulse/incidents/"+inc.IncidentID+"/investigation-context", nil)
	ctxRR := httptest.NewRecorder()
	mux.ServeHTTP(ctxRR, ctxReq)
	if ctxRR.Code != http.StatusOK {
		t.Fatalf("context %d %s", ctxRR.Code, ctxRR.Body.String())
	}

	hyp := created.Investigation.Hypotheses[0].HypothesisID
	fbBody, _ := json.Marshal(map[string]any{
		"assessment":            "useful",
		"useful_hypothesis_ids": []string{hyp},
		"rationale":             "good leads",
	})
	fbReq := httptest.NewRequest(http.MethodPost, "/api/pulse/investigations/"+created.Investigation.InvestigationID+"/feedback", bytes.NewReader(fbBody))
	fbReq.Header.Set("Content-Type", "application/json")
	fbReq.RemoteAddr = "127.0.0.1:9999"
	fbRR := httptest.NewRecorder()
	mux.ServeHTTP(fbRR, fbReq)
	if fbRR.Code != http.StatusCreated {
		t.Fatalf("feedback %d %s", fbRR.Code, fbRR.Body.String())
	}

	// Non-loopback POST rejected.
	bad := httptest.NewRequest(http.MethodPost, "/api/pulse/investigations", strings.NewReader(body))
	bad.Header.Set("Content-Type", "application/json")
	bad.RemoteAddr = "8.8.8.8:12345"
	badRR := httptest.NewRecorder()
	mux.ServeHTTP(badRR, bad)
	if badRR.Code != http.StatusForbidden {
		t.Fatalf("expected loopback_only, got %d", badRR.Code)
	}

	// Unknown incident.
	miss := httptest.NewRequest(http.MethodPost, "/api/pulse/investigations", strings.NewReader(`{"incident_id":"inc|missing"}`))
	miss.Header.Set("Content-Type", "application/json")
	miss.RemoteAddr = "127.0.0.1:1"
	missRR := httptest.NewRecorder()
	mux.ServeHTTP(missRR, miss)
	if missRR.Code != http.StatusNotFound {
		t.Fatalf("missing incident %d", missRR.Code)
	}

	_ = filepath.Join(dir, "investigations")
}

func TestInvestigationAPI_RejectsInvalidFeedback(t *testing.T) {
	dir := t.TempDir()
	inc := seedPaymentIncident(t, dir)
	h := investigationHandler(t, dir)
	mux := http.NewServeMux()
	h.Register(mux)
	body := `{"incident_id":` + jsonQuote(inc.IncidentID) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/pulse/investigations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:1"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	var created api.CreateInvestigationResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &created)

	fbBody := `{"assessment":"bogus","useful_hypothesis_ids":["x"]}`
	fbReq := httptest.NewRequest(http.MethodPost, "/api/pulse/investigations/"+created.Investigation.InvestigationID+"/feedback", strings.NewReader(fbBody))
	fbReq.Header.Set("Content-Type", "application/json")
	fbReq.RemoteAddr = "127.0.0.1:1"
	fbRR := httptest.NewRecorder()
	mux.ServeHTTP(fbRR, fbReq)
	if fbRR.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d %s", fbRR.Code, fbRR.Body.String())
	}
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
