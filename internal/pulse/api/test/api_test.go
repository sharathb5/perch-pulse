package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pulseapi "github.com/yashg4509/perch/internal/pulse/api"
	"github.com/yashg4509/perch/internal/pulse/change"
	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/incident"
	"github.com/yashg4509/perch/internal/pulse/observation"
	"github.com/yashg4509/perch/internal/pulse/store"
)

func TestValidateResourceID_RejectsTraversal(t *testing.T) {
	t.Parallel()
	bad := []string{
		"",
		"../etc/passwd",
		"foo/../../bar",
		"/abs/path",
		"C:/windows",
		"id\x00null",
		"has\tcontrol",
		strings.Repeat("a", 600),
	}
	for _, id := range bad {
		if err := pulseapi.ValidateResourceID(id); err == nil {
			t.Fatalf("expected reject for %q", id)
		}
	}
	good := "inc|astronomy/local/shipping|20261010T044646.884666000Z"
	if err := pulseapi.ValidateResourceID(good); err != nil {
		t.Fatalf("valid id rejected: %v", err)
	}
}

func TestAstronomyMapper_ExplicitComposeMapping(t *testing.T) {
	t.Parallel()
	m, err := pulseapi.NewAstronomyMapper("local")
	if err != nil {
		t.Fatal(err)
	}
	id, src, ok := m.PulseServiceIDForGraphNode("shipping")
	if !ok || id != "astronomy/local/shipping" || src != "astronomy_compose" {
		t.Fatalf("shipping -> %q %q ok=%v", id, src, ok)
	}
	node, _, ok := m.GraphNodeForPulseServiceID("astronomy/local/shipping")
	if !ok || node != "shipping" {
		t.Fatalf("reverse shipping = %q ok=%v", node, ok)
	}
	// No display-name heuristic: unknown labels do not match.
	if _, _, ok := m.PulseServiceIDForGraphNode("Shipping Service"); ok {
		t.Fatal("display-name heuristic must not match")
	}
	infra, _, ok := m.PulseServiceIDForGraphNode("otel-collector")
	if !ok || infra != "astronomy/local/infra/otel-collector" {
		t.Fatalf("otel-collector -> %q ok=%v", infra, ok)
	}
}

func TestServices_EmptyPulseState(t *testing.T) {
	h := newTestHandler(t, t.TempDir(), nil, fixedNow())
	rr := doGET(t, h, "/api/pulse/services")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp pulseapi.ServicesResponse
	mustDecode(t, rr, &resp)
	if resp.SchemaVersion != pulseapi.SchemaServices {
		t.Fatalf("schema=%q", resp.SchemaVersion)
	}
	if resp.Count != 0 || len(resp.Services) != 0 {
		t.Fatalf("want empty services, got %+v", resp.Services)
	}
	if resp.DataSources.Observations != "process_memory_unavailable" {
		t.Fatalf("observations source=%q", resp.DataSources.Observations)
	}
	if pulseapi.ContainsGroundTruthLeak(rr.Body.String()) {
		t.Fatal("ground-truth leak in empty services response")
	}
}

func TestServices_ActiveHealthVsIntelligenceSeparation(t *testing.T) {
	dir := t.TempDir()
	writeOpenIncident(t, dir, "astronomy/local/shipping", "latency_ms", fixedNow().Add(-time.Minute))
	h := newTestHandler(t, dir, nil, fixedNow())
	rr := doGET(t, h, "/api/pulse/services")
	var resp pulseapi.ServicesResponse
	mustDecode(t, rr, &resp)
	if len(resp.Services) == 0 {
		t.Fatal("expected at least one service")
	}
	var ship *pulseapi.ServicePulseStatus
	for i := range resp.Services {
		if resp.Services[i].ServiceID == "astronomy/local/shipping" {
			ship = &resp.Services[i]
			break
		}
	}
	if ship == nil {
		t.Fatal("shipping missing")
	}
	if ship.ActiveHealthNote == "" {
		t.Fatal("active health note required")
	}
	if !strings.Contains(ship.ActiveHealthNote, "/api/status") {
		t.Fatalf("active health note=%q", ship.ActiveHealthNote)
	}
	if ship.Intelligence == observation.StatusHealthy {
		t.Fatal("open latency incident must not report healthy intelligence")
	}
	if ship.Intelligence != observation.StatusDegraded {
		t.Fatalf("intelligence=%q want degraded", ship.Intelligence)
	}
	if ship.GraphNode != "shipping" {
		t.Fatalf("graph_node=%q", ship.GraphNode)
	}
}

func TestServices_StaleObservationNeverHealthy(t *testing.T) {
	dir := t.TempDir()
	mem := store.NewMemory()
	old := fixedNow().Add(-2 * time.Hour)
	if err := mem.Append(observation.Observation{
		ServiceID:  "astronomy/local/cart",
		ObservedAt: old,
		Status:     observation.StatusHealthy,
		Source:     "test",
	}); err != nil {
		t.Fatal(err)
	}
	// Also register the service via a recovered incident so ListServices finds it
	// if observation-only listing is empty — seed via change.
	writeChange(t, dir, "astronomy/local/cart", fixedNow().Add(-3*time.Hour))

	h := newTestHandler(t, dir, mem, fixedNow())
	rr := doGET(t, h, "/api/pulse/services")
	var resp pulseapi.ServicesResponse
	mustDecode(t, rr, &resp)
	var cart *pulseapi.ServicePulseStatus
	for i := range resp.Services {
		if resp.Services[i].ServiceID == "astronomy/local/cart" {
			cart = &resp.Services[i]
			break
		}
	}
	if cart == nil {
		t.Fatal("cart missing")
	}
	if cart.Intelligence != observation.StatusStale {
		t.Fatalf("intelligence=%q want stale", cart.Intelligence)
	}
	if cart.Freshness != pulseapi.FreshnessStale {
		t.Fatalf("freshness=%q", cart.Freshness)
	}
	if cart.Intelligence == observation.StatusHealthy {
		t.Fatal("stale must never be healthy")
	}
}

func TestServices_MissingObservationUnknown(t *testing.T) {
	dir := t.TempDir()
	writeChange(t, dir, "astronomy/local/ad", fixedNow())
	mem := store.NewMemory()
	h := newTestHandler(t, dir, mem, fixedNow())
	rr := doGET(t, h, "/api/pulse/services")
	var resp pulseapi.ServicesResponse
	mustDecode(t, rr, &resp)
	var ad *pulseapi.ServicePulseStatus
	for i := range resp.Services {
		if resp.Services[i].ServiceID == "astronomy/local/ad" {
			ad = &resp.Services[i]
			break
		}
	}
	if ad == nil {
		t.Fatal("ad missing")
	}
	if ad.IntelligenceAvailable {
		t.Fatal("missing observation must not claim available intelligence without open incident")
	}
	if ad.Intelligence == observation.StatusHealthy {
		t.Fatal("missing ≠ healthy")
	}
	if ad.Freshness != pulseapi.FreshnessMissing {
		t.Fatalf("freshness=%q want missing", ad.Freshness)
	}
}

func TestIncidents_ListAndDetailAndNotFound(t *testing.T) {
	dir := t.TempDir()
	id := writeOpenIncident(t, dir, "astronomy/local/payment", "error_rate", fixedNow().Add(-2*time.Minute))
	writeChange(t, dir, "astronomy/local/payment", fixedNow().Add(-5*time.Minute))
	h := newTestHandler(t, dir, nil, fixedNow())

	rr := doGET(t, h, "/api/pulse/incidents")
	var list pulseapi.IncidentsResponse
	mustDecode(t, rr, &list)
	if list.Count != 1 || list.Incidents[0].IncidentID != id {
		t.Fatalf("list=%+v", list)
	}

	detailURL := "/api/pulse/incidents/" + url.PathEscape(id)
	// PathEscape encodes / as %2F; ServeMux may not decode mid-path.
	// Prefer raw path with {id...} matching.
	detailURL = "/api/pulse/incidents/" + id
	rr = doGET(t, h, detailURL)
	if rr.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", rr.Code, rr.Body.String())
	}
	var detail pulseapi.IncidentDetailResponse
	mustDecode(t, rr, &detail)
	if detail.Incident.IncidentID != id {
		t.Fatalf("detail id=%q", detail.Incident.IncidentID)
	}
	if detail.Correlation == nil {
		t.Fatal("correlation required on detail")
	}
	if detail.Correlation.SchemaVersion == "" {
		t.Fatal("correlation schema missing")
	}
	if detail.GraphNode != "payment" {
		t.Fatalf("graph_node=%q", detail.GraphNode)
	}
	body := rr.Body.String()
	if pulseapi.ContainsGroundTruthLeak(body) {
		t.Fatal("ground-truth leak")
	}
	if strings.Contains(strings.ToLower(body), "caused by") {
		t.Fatal("causation claim in response")
	}

	rr = doGET(t, h, "/api/pulse/incidents/inc|astronomy/local/missing|20990101T000000.000000000Z")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("want 404 got %d", rr.Code)
	}
}

func TestChanges_ListDetailLimitsOrdering(t *testing.T) {
	dir := t.TempDir()
	t1 := fixedNow().Add(-10 * time.Minute)
	t2 := fixedNow().Add(-5 * time.Minute)
	t3 := fixedNow().Add(-1 * time.Minute)
	id1 := writeChangeAt(t, dir, "astronomy/local/shipping", t1, "c1")
	id2 := writeChangeAt(t, dir, "astronomy/local/shipping", t2, "c2")
	id3 := writeChangeAt(t, dir, "astronomy/local/shipping", t3, "c3")
	_ = id1
	h := newTestHandler(t, dir, nil, fixedNow())
	h.Reader.DefaultLimit = 2
	h.Reader.MaxLimit = 2

	rr := doGET(t, h, "/api/pulse/changes?limit=2")
	var list pulseapi.ChangesResponse
	mustDecode(t, rr, &list)
	if list.Count != 2 || list.Limit != 2 {
		t.Fatalf("count=%d limit=%d", list.Count, list.Limit)
	}
	// Newest first.
	if list.Changes[0].ChangeID != id3 || list.Changes[1].ChangeID != id2 {
		t.Fatalf("order=%v %v want %s then %s", list.Changes[0].ChangeID, list.Changes[1].ChangeID, id3, id2)
	}

	rr = doGET(t, h, "/api/pulse/changes/"+id2)
	if rr.Code != http.StatusOK {
		t.Fatalf("detail=%d %s", rr.Code, rr.Body.String())
	}
	var detail pulseapi.ChangeDetailResponse
	mustDecode(t, rr, &detail)
	if detail.Change.ChangeID != id2 {
		t.Fatalf("id=%q", detail.Change.ChangeID)
	}
	if len(detail.GraphNodes) != 1 || detail.GraphNodes[0] != "shipping" {
		t.Fatalf("graph_nodes=%v", detail.GraphNodes)
	}
}

func TestInvalidID_AndPathTraversal(t *testing.T) {
	h := newTestHandler(t, t.TempDir(), nil, fixedNow())
	rr := doGET(t, h, "/api/pulse/incidents/../etc/passwd")
	// May be 400 (invalid) or 404 depending on mux cleaning; never 200 with file contents.
	if rr.Code == http.StatusOK {
		t.Fatalf("traversal must not succeed: %s", rr.Body.String())
	}
	rr = doGET(t, h, "/api/pulse/changes/foo/../../secret")
	if rr.Code == http.StatusOK {
		t.Fatal("traversal must not succeed")
	}
}

func TestMalformedPersistedData(t *testing.T) {
	dir := t.TempDir()
	incDir := filepath.Join(dir, "incidents")
	if err := os.MkdirAll(incDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(incDir, "broken.json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newTestHandler(t, dir, nil, fixedNow())
	rr := doGET(t, h, "/api/pulse/incidents")
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var errBody pulseapi.ErrorBody
	mustDecode(t, rr, &errBody)
	if errBody.Code != "corrupted_artifact" && !strings.Contains(errBody.Error, "invalid") {
		t.Fatalf("error=%+v", errBody)
	}
	if pulseapi.ContainsGroundTruthLeak(rr.Body.String()) {
		t.Fatal("gt leak")
	}
}

func TestSecretRedaction(t *testing.T) {
	dir := t.TempDir()
	store := change.NewFileStore(dir)
	deployed := fixedNow().Add(-3 * time.Minute)
	ev, err := change.Record(change.RecordInput{
		Type:        change.ChangeDeployment,
		ServiceIDs:  []string{"astronomy/local/checkout"},
		Environment: "local",
		Title:       "deploy",
		Summary:     "token Bearer sk-abcdefghijklmnopqrstuvwxyz1234 in log",
		DeployedAt:  &deployed,
		Simulated:   true,
		Source:      "test",
		Metadata: map[string]string{
			"api_token": "super-secret-value",
			"note":      "ok",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(ev); err != nil {
		t.Fatal(err)
	}
	h := newTestHandler(t, dir, nil, fixedNow())
	rr := doGET(t, h, "/api/pulse/changes/"+ev.ChangeID)
	body := rr.Body.String()
	if strings.Contains(body, "super-secret-value") {
		t.Fatal("secret leaked in metadata")
	}
	if strings.Contains(body, "api_token") {
		t.Fatal("secret-bearing metadata key must not be emitted")
	}
	if strings.Contains(body, "sk-abcdefghijklmnopqrstuvwxyz1234") {
		t.Fatal("bearer/token leaked in summary")
	}
	if !strings.Contains(body, "[redacted]") {
		t.Fatal("expected redaction marker")
	}
}

func TestStableOrdering_Services(t *testing.T) {
	dir := t.TempDir()
	writeChange(t, dir, "astronomy/local/zulu", fixedNow())
	writeChange(t, dir, "astronomy/local/alpha", fixedNow())
	writeChange(t, dir, "astronomy/local/mike", fixedNow())
	h := newTestHandler(t, dir, nil, fixedNow())
	rr := doGET(t, h, "/api/pulse/services")
	var resp pulseapi.ServicesResponse
	mustDecode(t, rr, &resp)
	if len(resp.Services) != 3 {
		t.Fatalf("count=%d", len(resp.Services))
	}
	prev := ""
	for _, s := range resp.Services {
		if prev != "" && s.ServiceID < prev {
			t.Fatalf("unsorted: %q after %q", s.ServiceID, prev)
		}
		prev = s.ServiceID
	}
}

func TestNoGroundTruthImport(t *testing.T) {
	// Compile-time / package boundary: this test file must not import scenario.
	// Runtime: responses from synthetic fixtures must not contain GT markers.
	h := newTestHandler(t, t.TempDir(), nil, fixedNow())
	for _, path := range []string{
		"/api/pulse/services",
		"/api/pulse/incidents",
		"/api/pulse/changes",
	} {
		rr := doGET(t, h, path)
		if pulseapi.ContainsGroundTruthLeak(rr.Body.String()) {
			t.Fatalf("gt leak on %s", path)
		}
	}
}

func TestMethodNotAllowed_AndExistingPatterns(t *testing.T) {
	h := newTestHandler(t, t.TempDir(), nil, fixedNow())
	mux := http.NewServeMux()
	h.Register(mux)
	// POST must not match GET-only routes (404 or 405 depending on mux).
	req := httptest.NewRequest(http.MethodPost, "/api/pulse/services", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code == http.StatusOK {
		t.Fatal("POST must not succeed on read-only pulse API")
	}
}

// --- helpers ---

func fixedNow() time.Time {
	return time.Date(2026, 10, 10, 6, 0, 0, 0, time.UTC)
}

func newTestHandler(t *testing.T, dir string, obs *store.Memory, now time.Time) *pulseapi.Handler {
	t.Helper()
	mapper, err := pulseapi.NewAstronomyMapper("local")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := pulseapi.NewFileReader(dir, mapper)
	if err != nil {
		t.Fatal(err)
	}
	reader.Now = func() time.Time { return now }
	reader.Freshness = 5 * time.Minute
	if obs != nil {
		reader.WithObservationStore(obs)
	}
	return &pulseapi.Handler{Reader: reader}
}

func doGET(t *testing.T, h *pulseapi.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func mustDecode(t *testing.T, rr *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), v); err != nil {
		t.Fatalf("json: %v body=%s", err, rr.Body.String())
	}
}

func writeOpenIncident(t *testing.T, dir, serviceID, signal string, first time.Time) string {
	t.Helper()
	first = first.UTC()
	last := first.Add(30 * time.Second)
	id := incident.IncidentIDFor(serviceID, first)
	inc := incident.Incident{
		SchemaVersion:      incident.SchemaVersion,
		IncidentID:         id,
		PrimaryServiceID:   serviceID,
		Environment:        "local",
		FindingIDs:         []string{serviceID + "|" + signal + "|" + first.Format("20060102T150405.000000000Z")},
		SignalTypes:        []string{signal},
		FirstDetectedAt:    first,
		LastObservedAt:     last,
		AffectedServiceIDs: []string{serviceID},
		Before: incident.PhaseSnapshot{
			Phase:     "before",
			Window:    incident.Window{Start: first.Add(-2 * time.Minute), End: first},
			Available: false,
		},
		During: incident.PhaseSnapshot{
			Phase:     "during",
			Window:    incident.Window{Start: first, End: last},
			Available: true,
			Digests: []incident.SampleDigest{{
				Signal:  detect.SignalType(signal),
				Count:   3,
				Median:  100,
				Min:     50,
				Max:     200,
				FirstAt: first,
				LastAt:  last,
			}},
		},
		After: incident.PhaseSnapshot{
			Phase:      "after",
			Window:     incident.Window{Start: last, End: last},
			Available:  false,
			Limitation: "recovery window not yet observed",
		},
		Observations: []string{"detector reported regression on " + serviceID},
		Inferences:   []string{"possible regression; not proven causation"},
		Limitations:  []string{"synthetic fixture"},
		Summary:      "open incident fixture for " + serviceID,
	}
	if err := incident.NewFileStore(dir).Append(inc); err != nil {
		t.Fatal(err)
	}
	return id
}

func writeChange(t *testing.T, dir, serviceID string, at time.Time) string {
	t.Helper()
	return writeChangeAt(t, dir, serviceID, at, fmt.Sprintf("sim-%d", at.UnixNano()))
}

func writeChangeAt(t *testing.T, dir, serviceID string, at time.Time, marker string) string {
	t.Helper()
	at = at.UTC()
	ev, err := change.Record(change.RecordInput{
		Type:        change.ChangeDeployment,
		ServiceIDs:  []string{serviceID},
		Environment: "local",
		Title:       "fixture " + marker,
		Summary:     "synthetic change",
		DeployedAt:  &at,
		ObservedAt:  at,
		Simulated:   true,
		Source:      "test",
		Metadata:    map[string]string{"marker": marker},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := change.NewFileStore(dir).Append(ev); err != nil {
		t.Fatal(err)
	}
	return ev.ChangeID
}
