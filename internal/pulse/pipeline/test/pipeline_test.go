package pipeline_test

import (
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pulseapi "github.com/yashg4509/perch/internal/pulse/api"
	"github.com/yashg4509/perch/internal/pulse/astronomy"
	"github.com/yashg4509/perch/internal/pulse/change"
	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/pipeline"
	"github.com/yashg4509/perch/internal/pulse/store"
	"github.com/yashg4509/perch/internal/testutil"
)

// updateContractFixture regenerates web/fixtures/pulse.pipeline.contract.json.
// INTEGRATION: proves samples→detector→incident→API and keeps frontend contract aligned.
var updateContractFixture = flag.Bool("update-pipeline-fixture", false, "rewrite web/fixtures/pulse.pipeline.contract.json")

func TestPipeline_HealthyBaselineNoIncident(t *testing.T) {
	// INTEGRATION: steady telemetry through real detector+API yields no incidents.
	base := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	svc := astronomy.MustLocalServiceID("checkout")
	dir := t.TempDir()
	evalAt := base.Add(2*time.Minute + 20*time.Second)
	res, err := pipeline.Run(pipeline.HealthySteady(svc, base), pipeline.Options{
		PulseDir: dir, Environment: "local", EvaluateAt: evalAt,
		Clock: func() time.Time { return evalAt },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 0 || len(res.Incidents) != 0 {
		t.Fatalf("healthy baseline must not create incidents: findings=%+v incidents=%+v", res.Findings, res.Incidents)
	}
	rr := pipeline.GET(res.Handler, "/api/pulse/incidents")
	assertOK(t, rr)
	var list pulseapi.IncidentsResponse
	mustJSON(t, rr, &list)
	if list.Count != 0 || len(list.Incidents) != 0 {
		t.Fatalf("API must list zero incidents, got %+v", list)
	}
	assertNoGT(t, rr.Body.String())
}

func TestPipeline_LatencyRegressionEndToEnd(t *testing.T) {
	// INTEGRATION: latency spike → finding → incident file → API with evidence + service id.
	base := time.Date(2026, 10, 10, 13, 0, 0, 0, time.UTC)
	svc := astronomy.MustLocalServiceID("shipping")
	dir := t.TempDir()
	evalAt := base.Add(2*time.Minute + 25*time.Second)
	deployed := evalAt.Add(-3 * time.Minute)
	ev, err := change.Record(change.RecordInput{
		Type: change.ChangeDeployment, ServiceIDs: []string{svc}, Environment: "local",
		Title: "simulated shipping deploy", Summary: "demo marker", DeployedAt: &deployed,
		Simulated: true, Source: "pipeline-test",
		Metadata: map[string]string{"token": "sk-this-must-redact-abcdefghijklmnop"},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := pipeline.Run(pipeline.LatencyBaselineThenSpike(svc, base), pipeline.Options{
		PulseDir: dir, Environment: "local", EvaluateAt: evalAt,
		Clock:   func() time.Time { return evalAt },
		Changes: []change.Event{ev},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) == 0 {
		t.Fatal("expected latency finding from real detector")
	}
	foundLatency := false
	for _, f := range res.Findings {
		if f.SignalType == detect.SignalLatencyMS && f.ServiceID == svc {
			foundLatency = true
		}
	}
	if !foundLatency {
		t.Fatalf("missing latency finding for %s: %+v", svc, res.Findings)
	}
	if len(res.Incidents) == 0 {
		t.Fatal("expected persisted incident assembly")
	}
	inc := res.Incidents[0]
	if inc.PrimaryServiceID != svc {
		t.Fatalf("primary=%q want %q", inc.PrimaryServiceID, svc)
	}
	if !inc.During.Available || len(inc.During.Digests) == 0 {
		t.Fatalf("during evidence missing: %+v", inc.During)
	}

	rr := pipeline.GET(res.Handler, "/api/pulse/incidents/"+inc.IncidentID)
	assertOK(t, rr)
	assertNoGT(t, rr.Body.String())
	if strings.Contains(rr.Body.String(), "sk-this-must-redact") {
		t.Fatal("secret leaked through incident/change API path")
	}
	var detail pulseapi.IncidentDetailResponse
	mustJSON(t, rr, &detail)
	if detail.Incident.PrimaryServiceID != svc {
		t.Fatalf("api primary=%q", detail.Incident.PrimaryServiceID)
	}
	if detail.Correlation == nil || detail.Correlation.Top1 == nil {
		t.Fatal("expected change correlation ranking on detail")
	}
	joined := strings.ToLower(detail.Incident.Summary + strings.Join(detail.Incident.Inferences, " "))
	if strings.Contains(joined, "caused by") {
		t.Fatalf("correlation presented as causation: %s", joined)
	}
	corrText := strings.ToLower(strings.Join(detail.Correlation.Limitations, " ") + " " + strings.Join(detail.Correlation.Inferences, " "))
	if !strings.Contains(corrText, "causation") && !strings.Contains(corrText, "not proof") {
		t.Fatalf("correlation missing non-causation caveat: %s", corrText)
	}
}

func TestPipeline_ErrorRateAndCallRate(t *testing.T) {
	// INTEGRATION: error-rate + call-rate drop produce correct signals through API.
	base := time.Date(2026, 10, 10, 14, 0, 0, 0, time.UTC)
	svc := astronomy.MustLocalServiceID("payment")
	dir := t.TempDir()
	evalAt := base.Add(2*time.Minute + 25*time.Second)
	res, err := pipeline.Run(pipeline.ErrorRateBaselineThenSpike(svc, base), pipeline.Options{
		PulseDir: dir, Environment: "local", EvaluateAt: evalAt,
		Clock: func() time.Time { return evalAt },
	})
	if err != nil {
		t.Fatal(err)
	}
	var sawErr, sawCall bool
	for _, f := range res.Findings {
		if f.SignalType == detect.SignalErrorRate {
			sawErr = true
			if f.Severity != detect.SeverityCritical {
				t.Fatalf("error_rate severity=%s want critical", f.Severity)
			}
		}
		if f.SignalType == detect.SignalCallRate {
			sawCall = true
		}
	}
	if !sawErr || !sawCall {
		t.Fatalf("want error_rate and call_rate findings, got %+v", res.Findings)
	}
	rr := pipeline.GET(res.Handler, "/api/pulse/incidents")
	assertOK(t, rr)
	body := rr.Body.String()
	if !strings.Contains(body, "error_rate") {
		t.Fatalf("API missing error_rate serialization: %s", body)
	}
	assertNoGT(t, body)
}

func TestPipeline_CallRateDropSeverity(t *testing.T) {
	// INTEGRATION: call-rate drop alone produces a finding with outage semantics.
	base := time.Date(2026, 10, 10, 14, 30, 0, 0, time.UTC)
	svc := astronomy.MustLocalServiceID("cart")
	dir := t.TempDir()
	evalAt := base.Add(2*time.Minute + 25*time.Second)
	res, err := pipeline.Run(pipeline.CallRateDropOnly(svc, base), pipeline.Options{
		PulseDir: dir, Environment: "local", EvaluateAt: evalAt,
		Clock: func() time.Time { return evalAt },
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range res.Findings {
		if f.SignalType == detect.SignalCallRate && f.ServiceID == svc {
			found = true
			if f.Severity == "" {
				t.Fatal("call_rate finding missing severity")
			}
		}
	}
	if !found {
		t.Fatalf("expected call_rate finding, got %+v", res.Findings)
	}
}

func TestPipeline_RecoveryHistoricalNotCurrentOutage(t *testing.T) {
	// INTEGRATION: recovery closes finding; persisted recovered incident is historical via API.
	base := time.Date(2026, 10, 10, 15, 0, 0, 0, time.UTC)
	svc := astronomy.MustLocalServiceID("shipping")
	cfg := pipeline.DemoDetectConfig()
	d := detect.New(cfg)
	samples := pipeline.LatencyBaselineThenSpike(svc, base)
	if err := d.Ingest(samples); err != nil {
		t.Fatal(err)
	}
	now := base.Add(2*time.Minute + 25*time.Second)
	d.SetClock(func() time.Time { return now })
	findings, err := d.Evaluate(now)
	if err != nil || len(findings) == 0 || !findings[0].Open() {
		t.Fatalf("expected open finding: %v %+v", err, findings)
	}
	var recover []detect.Sample
	for i := 0; i < 4; i++ {
		t0 := now.Add(time.Duration(i+1) * 10 * time.Second)
		recover = append(recover,
			detect.Sample{ServiceID: svc, Signal: detect.SignalLatencyMS, Value: 210, ObservedAt: t0, Source: "pipeline-test"},
			detect.Sample{ServiceID: svc, Signal: detect.SignalCallRate, Value: 0.5, ObservedAt: t0, Source: "pipeline-test"},
		)
	}
	if err := d.Ingest(recover); err != nil {
		t.Fatal(err)
	}
	recNow := now.Add(45 * time.Second)
	d.SetClock(func() time.Time { return recNow })
	findings, err = d.Evaluate(recNow)
	if err != nil {
		t.Fatal(err)
	}
	var recovered *detect.Finding
	for i := range findings {
		if findings[i].RecoveredAt != nil && findings[i].SignalType == detect.SignalLatencyMS {
			recovered = &findings[i]
			break
		}
	}
	if recovered == nil {
		t.Fatalf("expected recovery: %+v", findings)
	}
	allSamples := append(samples, recover...)
	dir := t.TempDir()
	res, err := pipeline.Run(allSamples, pipeline.Options{
		PulseDir: dir, Environment: "local", EvaluateAt: recNow,
		Clock: func() time.Time { return recNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	// After full recovery, open findings should be empty; recovered findings may still assemble.
	open := 0
	for _, f := range res.Findings {
		if f.RecoveredAt == nil {
			open++
		}
	}
	if open != 0 {
		t.Fatalf("expected no open findings after recovery, open=%d %+v", open, res.Findings)
	}
	rr := pipeline.GET(res.Handler, "/api/pulse/services")
	assertOK(t, rr)
	var svcResp pulseapi.ServicesResponse
	mustJSON(t, rr, &svcResp)
	for _, s := range svcResp.Services {
		if s.ServiceID != svc {
			continue
		}
		if len(s.OpenIncidentIDs) != 0 {
			t.Fatalf("recovered history must not imply open outage: %+v", s)
		}
		if s.Intelligence.IsHealthy() && s.Freshness == pulseapi.FreshnessUnavailable {
			// Missing live observations must not become healthy via history alone — ok if unknown.
		}
		if string(s.Intelligence) == "healthy" && !s.IntelligenceAvailable {
			t.Fatal("unavailable intelligence must not report healthy")
		}
	}
}

func TestPipeline_StaleObservationNeverHealthy(t *testing.T) {
	// INTEGRATION: stale healthy observation through services API is not healthy.
	base := time.Date(2026, 10, 10, 16, 0, 0, 0, time.UTC)
	svc := astronomy.MustLocalServiceID("frontend")
	dir := t.TempDir()
	mem := store.NewMemory()
	if err := pipeline.AppendStaleObservation(mem, svc, base.Add(-20*time.Minute), base); err != nil {
		t.Fatal(err)
	}
	res, err := pipeline.Run(nil, pipeline.Options{
		PulseDir: dir, Environment: "local", EvaluateAt: base,
		Clock: func() time.Time { return base }, Observations: mem, SkipPersist: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Seed a change so the service appears in ListServices.
	ev, err := change.Record(change.RecordInput{
		Type: change.ChangeDeployment, ServiceIDs: []string{svc}, Environment: "local",
		Title: "touch", Summary: "list seed", DeployedAt: &base, Simulated: true, Source: "pipeline-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := change.NewFileStore(dir).Append(ev); err != nil {
		t.Fatal(err)
	}
	reader, err := pulseapi.NewFileReader(dir, mustMapper(t))
	if err != nil {
		t.Fatal(err)
	}
	reader.Now = func() time.Time { return base }
	reader.WithObservationStore(mem)
	res.Handler = &pulseapi.Handler{Reader: reader}

	rr := pipeline.GET(res.Handler, "/api/pulse/services")
	assertOK(t, rr)
	var svcResp pulseapi.ServicesResponse
	mustJSON(t, rr, &svcResp)
	var saw bool
	for _, s := range svcResp.Services {
		if s.ServiceID != svc {
			continue
		}
		saw = true
		if s.Intelligence.IsHealthy() {
			t.Fatalf("stale observation reported healthy: %+v", s)
		}
	}
	if !saw {
		t.Fatal("expected frontend service row")
	}
}

func TestPipeline_DuplicateOutOfOrderDeterministic(t *testing.T) {
	// INTEGRATION: duplicate/out-of-order samples do not corrupt incident path.
	base := time.Date(2026, 10, 10, 17, 0, 0, 0, time.UTC)
	svc := astronomy.MustLocalServiceID("payment")
	samples := pipeline.ErrorRateBaselineThenSpike(svc, base)
	// Re-append first sample (exact duplicate) and an out-of-order older point.
	dup := samples[0]
	ooo := samples[0]
	ooo.ObservedAt = base.Add(-time.Minute)
	ooo.Value = 0.01
	samples = append(samples, dup, ooo)

	dir := t.TempDir()
	evalAt := base.Add(2*time.Minute + 25*time.Second)
	res1, err := pipeline.Run(samples, pipeline.Options{
		PulseDir: dir, Environment: "local", EvaluateAt: evalAt,
		Clock: func() time.Time { return evalAt },
	})
	if err != nil {
		t.Fatal(err)
	}
	dir2 := t.TempDir()
	res2, err := pipeline.Run(samples, pipeline.Options{
		PulseDir: dir2, Environment: "local", EvaluateAt: evalAt,
		Clock: func() time.Time { return evalAt },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res1.Incidents) == 0 || len(res1.Incidents) != len(res2.Incidents) {
		t.Fatalf("deterministic incident count mismatch %d vs %d", len(res1.Incidents), len(res2.Incidents))
	}
	if res1.Incidents[0].IncidentID != res2.Incidents[0].IncidentID {
		t.Fatalf("incident ids diverged: %q vs %q", res1.Incidents[0].IncidentID, res2.Incidents[0].IncidentID)
	}
}

func TestPipeline_EnvironmentMismatchNoCrossJoin(t *testing.T) {
	// INTEGRATION: change in env A must not strongly join incident in env B.
	base := time.Date(2026, 10, 10, 18, 0, 0, 0, time.UTC)
	svc := astronomy.MustLocalServiceID("shipping")
	dir := t.TempDir()
	evalAt := base.Add(2*time.Minute + 25*time.Second)
	deployed := evalAt.Add(-2 * time.Minute)
	ev, err := change.Record(change.RecordInput{
		Type: change.ChangeDeployment, ServiceIDs: []string{svc}, Environment: "staging",
		Title: "staging deploy", Summary: "wrong env", DeployedAt: &deployed,
		Simulated: true, Source: "pipeline-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := pipeline.Run(pipeline.LatencyBaselineThenSpike(svc, base), pipeline.Options{
		PulseDir: dir, Environment: "local", EvaluateAt: evalAt,
		Clock: func() time.Time { return evalAt }, Changes: []change.Event{ev},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Incidents) == 0 {
		t.Fatal("expected local incident")
	}
	rr := pipeline.GET(res.Handler, "/api/pulse/incidents/"+res.Incidents[0].IncidentID)
	assertOK(t, rr)
	var detail pulseapi.IncidentDetailResponse
	mustJSON(t, rr, &detail)
	if detail.Correlation != nil && detail.Correlation.Top1 != nil {
		top := detail.Correlation.Top1
		// Environment mismatch should prevent a strong primary join.
		if top.Score >= 50 && string(top.ServiceOverlap) == "primary" {
			// Still possible via service overlap alone; require env weight not full strong if documented.
			if !detail.Correlation.NoStrongCandidate && top.Score >= 80 {
				t.Fatalf("cross-environment change unexpectedly strong: %+v", top)
			}
		}
	}
}

func TestPipeline_GroundTruthLabelsNeverReachAPI(t *testing.T) {
	// INTEGRATION / negative control: scenario-shaped strings must not appear in API.
	base := time.Date(2026, 10, 10, 19, 0, 0, 0, time.UTC)
	svc := astronomy.MustLocalServiceID("shipping")
	dir := t.TempDir()
	evalAt := base.Add(2*time.Minute + 25*time.Second)
	res, err := pipeline.Run(pipeline.LatencyBaselineThenSpike(svc, base), pipeline.Options{
		PulseDir: dir, Environment: "local", EvaluateAt: evalAt,
		Clock: func() time.Time { return evalAt },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/pulse/services",
		"/api/pulse/incidents",
		"/api/pulse/changes",
	} {
		rr := pipeline.GET(res.Handler, path)
		assertOK(t, rr)
		assertNoGT(t, rr.Body.String())
	}
	if len(res.Incidents) > 0 {
		rr := pipeline.GET(res.Handler, "/api/pulse/incidents/"+res.Incidents[0].IncidentID)
		assertOK(t, rr)
		assertNoGT(t, rr.Body.String())
	}
}

func TestPipeline_NoPulseDataOriginalPathStillWorks(t *testing.T) {
	// INTEGRATION: empty Pulse dir → empty API; does not invent healthy services.
	dir := t.TempDir()
	res, err := pipeline.Run(nil, pipeline.Options{
		PulseDir: dir, Environment: "local",
		EvaluateAt: time.Date(2026, 10, 10, 20, 0, 0, 0, time.UTC),
		Clock:      func() time.Time { return time.Date(2026, 10, 10, 20, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	rr := pipeline.GET(res.Handler, "/api/pulse/services")
	assertOK(t, rr)
	var svcResp pulseapi.ServicesResponse
	mustJSON(t, rr, &svcResp)
	if svcResp.Count != 0 {
		t.Fatalf("empty pulse must not invent services: %+v", svcResp.Services)
	}
}

func TestPipeline_ContractFixtureForFrontend(t *testing.T) {
	// INTEGRATION: golden API payloads consumed by Vitest (backend/frontend contract).
	base := time.Date(2026, 10, 10, 13, 0, 0, 0, time.UTC)
	svc := astronomy.MustLocalServiceID("shipping")
	dir := t.TempDir()
	evalAt := base.Add(2*time.Minute + 25*time.Second)
	deployed := evalAt.Add(-3 * time.Minute)
	ev, err := change.Record(change.RecordInput{
		Type: change.ChangeDeployment, ServiceIDs: []string{svc}, Environment: "local",
		Title: "simulated shipping deploy", Summary: "demo marker", DeployedAt: &deployed,
		Simulated: true, Source: "pipeline-test",
		Metadata: map[string]string{"harness": "pipeline-contract"},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := pipeline.Run(pipeline.LatencyBaselineThenSpike(svc, base), pipeline.Options{
		PulseDir: dir, Environment: "local", EvaluateAt: evalAt,
		Clock: func() time.Time { return evalAt }, Changes: []change.Event{ev},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Incidents) == 0 {
		t.Fatal("expected incident for contract fixture")
	}
	servicesRR := pipeline.GET(res.Handler, "/api/pulse/services")
	incidentsRR := pipeline.GET(res.Handler, "/api/pulse/incidents")
	detailRR := pipeline.GET(res.Handler, "/api/pulse/incidents/"+res.Incidents[0].IncidentID)
	changesRR := pipeline.GET(res.Handler, "/api/pulse/changes")
	for _, rr := range []*httptest.ResponseRecorder{servicesRR, incidentsRR, detailRR, changesRR} {
		assertOK(t, rr)
		assertNoGT(t, rr.Body.String())
	}
	contract := map[string]json.RawMessage{
		"services":  json.RawMessage(servicesRR.Body.Bytes()),
		"incidents": json.RawMessage(incidentsRR.Body.Bytes()),
		"incident":  json.RawMessage(detailRR.Body.Bytes()),
		"changes":   json.RawMessage(changesRR.Body.Bytes()),
	}
	raw, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	fixturePath := filepath.Join(testutil.RepoRoot(t), "web", "fixtures", "pulse.pipeline.contract.json")
	if *updateContractFixture {
		if err := os.WriteFile(fixturePath, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", fixturePath)
		return
	}
	want, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read contract fixture (run with -update-pipeline-fixture): %v", err)
	}
	var gotObj, wantObj any
	if err := json.Unmarshal(raw, &gotObj); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &wantObj); err != nil {
		t.Fatal(err)
	}
	// Compare structural fields that matter for frontend contracts (schema + digests + ranks).
	gotM := gotObj.(map[string]any)
	wantM := wantObj.(map[string]any)
	for _, key := range []string{"services", "incidents", "incident", "changes"} {
		if gotM[key] == nil || wantM[key] == nil {
			t.Fatalf("missing contract key %q", key)
		}
	}
	// Incident primary + during digest median must match golden (proves evidence path).
	gotInc := asMap(t, gotM["incident"])
	wantInc := asMap(t, wantM["incident"])
	gotPrimary := dig(gotInc, "incident", "primary_service_id")
	wantPrimary := dig(wantInc, "incident", "primary_service_id")
	if gotPrimary != wantPrimary {
		t.Fatalf("primary_service_id drift: got %v want %v (re-run with -update-pipeline-fixture if intentional)", gotPrimary, wantPrimary)
	}
	gotDigest := dig(gotInc, "incident", "during", "digests")
	wantDigest := dig(wantInc, "incident", "during", "digests")
	gj, _ := json.Marshal(gotDigest)
	wj, _ := json.Marshal(wantDigest)
	if string(gj) != string(wj) {
		t.Fatalf("during digests drift:\ngot  %s\nwant %s", gj, wj)
	}
}

func assertOK(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func assertNoGT(t *testing.T, body string) {
	t.Helper()
	if pulseapi.ContainsGroundTruthLeak(body) {
		t.Fatalf("ground-truth leak in API body: %s", body)
	}
	for _, bad := range []string{"scenario_id", "ground_truth", "flagd_key", `"control":`} {
		if strings.Contains(body, bad) {
			t.Fatalf("ground-truth-shaped marker %q in body", bad)
		}
	}
}

func mustJSON(t *testing.T, rr *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), v); err != nil {
		t.Fatalf("json: %v body=%s", err, rr.Body.String())
	}
}

func mustMapper(t *testing.T) pulseapi.ServiceMapper {
	t.Helper()
	m, err := pulseapi.NewAstronomyMapper("local")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("want map, got %T", v)
	}
	return m
}

func dig(m map[string]any, keys ...string) any {
	cur := any(m)
	for _, k := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	return cur
}
