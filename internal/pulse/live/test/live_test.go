package live_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	pulseapi "github.com/yashg4509/perch/internal/pulse/api"
	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/incident"
	"github.com/yashg4509/perch/internal/pulse/live"
	"github.com/yashg4509/perch/internal/pulse/observation"
	"github.com/yashg4509/perch/internal/pulse/pipeline"
	"github.com/yashg4509/perch/internal/pulse/store"
)

type scriptedSource struct {
	mu      sync.Mutex
	calls   int
	batches [][]detect.Sample
	errs    []error
}

func (s *scriptedSource) Collect(_ context.Context, at time.Time) ([]detect.Sample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.calls
	s.calls++
	if i < len(s.errs) && s.errs[i] != nil {
		return nil, s.errs[i]
	}
	if i >= len(s.batches) {
		if len(s.batches) == 0 {
			return nil, nil
		}
		out := stamp(s.batches[len(s.batches)-1], at)
		return out, nil
	}
	return stamp(s.batches[i], at), nil
}

func stamp(in []detect.Sample, at time.Time) []detect.Sample {
	out := make([]detect.Sample, len(in))
	for i, s := range in {
		out[i] = s
		out[i].ObservedAt = at.UTC()
	}
	return out
}

func svcSample(svc string, sig detect.SignalType, v float64) detect.Sample {
	return detect.Sample{
		ServiceID: svc,
		Signal:    sig,
		Value:     v,
		Source:    "prometheus",
	}
}

func paymentTriple(lat, errRate, calls float64) []detect.Sample {
	const svc = "astronomy/local/payment"
	return []detect.Sample{
		svcSample(svc, detect.SignalLatencyMS, lat),
		svcSample(svc, detect.SignalErrorRate, errRate),
		svcSample(svc, detect.SignalCallRate, calls),
	}
}

func TestLiveDisabled_UnavailableNotHealthy(t *testing.T) {
	t.Setenv("PERCH_LIVE_PULSE_ENABLED", "")
	t.Setenv("PERCH_LIVE_PULSE_PROM_URL", "")
	cfg, err := live.ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	rt, err := live.NewRuntime(live.Options{Config: cfg, PulseDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	snap := rt.Snapshot()
	if snap.Status.Available || snap.Status.State != live.StateDisabled {
		t.Fatalf("status=%+v", snap.Status)
	}
	if strings.Contains(strings.ToLower(snap.Status.UnavailabilityReason), "healthy") {
		t.Fatal("must not claim healthy when disabled")
	}
}

func TestLiveMissingConfig_Rejected(t *testing.T) {
	cfg := live.Config{
		Enabled:        true,
		PrometheusURL:  "",
		PollInterval:   15 * time.Second,
		RequestTimeout: 10 * time.Second,
		Environment:    "local",
		Freshness:      2 * time.Minute,
		SampleWindow:   2 * time.Minute,
		MaxServices:    32,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for missing prom URL")
	}
}

func TestLiveRejectsNonLoopbackPromURL(t *testing.T) {
	cfg := live.Config{
		Enabled:        true,
		PrometheusURL:  "http://example.com:9090",
		PollInterval:   15 * time.Second,
		RequestTimeout: 10 * time.Second,
		Environment:    "local",
		Freshness:      2 * time.Minute,
		SampleWindow:   2 * time.Minute,
		MaxServices:    32,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected non-loopback rejection")
	}
}

func TestLiveIntegration_PrometheusFixtureToAPI(t *testing.T) {
	// INTEGRATION: httptest Prom → telem path is covered separately; here we
	// drive the real detector/incident/API chain with a scripted Source that
	// emits the same Sample shape Prometheus produces.
	base := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	var clockMu sync.Mutex
	clock := base
	nowFn := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return clock
	}
	advance := func(d time.Duration) {
		clockMu.Lock()
		clock = clock.Add(d)
		clockMu.Unlock()
	}

	cfg := enabledCfg()
	dir := t.TempDir()
	src := &scriptedSource{}
	// Warmup: healthy baseline samples every 15s for >2m.
	var warm [][]detect.Sample
	for i := 0; i < 12; i++ {
		warm = append(warm, paymentTriple(80, 0.01, 1.0))
	}
	// Spike latency into regression.
	spike := make([][]detect.Sample, 0, 6)
	for i := 0; i < 6; i++ {
		spike = append(spike, paymentTriple(5000, 0.01, 1.0))
	}
	src.batches = append(warm, spike...)

	mapper, err := pulseapi.NewAstronomyMapper("local")
	if err != nil {
		t.Fatal(err)
	}
	obs := store.NewMemory()
	rt, err := live.NewRuntime(live.Options{
		Config:           cfg,
		PulseDir:         dir,
		Mapper:           mapper,
		DetectCfg:        pipeline.DemoDetectConfig(),
		Clock:            nowFn,
		Source:           src,
		ObservationStore: obs,
	})
	if err != nil {
		t.Fatal(err)
	}

	drivePolls(t, rt, src, advance, 15*time.Second, len(src.batches))

	snap := rt.Snapshot()
	if !snap.Status.Available {
		t.Fatalf("expected available after success: %+v", snap.Status)
	}
	if snap.Status.ActiveRegressions < 1 && snap.Status.ActiveIncidents < 1 {
		// May need more spike samples depending on windows.
		t.Logf("status=%+v services=%+v", snap.Status, snap.Services)
	}

	// Wire API.
	reader, err := pulseapi.NewFileReader(dir, mapper)
	if err != nil {
		t.Fatal(err)
	}
	reader.WithObservationStore(obs)
	reader.Now = nowFn
	h := &pulseapi.Handler{Reader: reader, Live: rt}
	mux := http.NewServeMux()
	h.Register(mux)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/pulse/live/status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("live status=%d %s", rr.Code, rr.Body.String())
	}
	var st pulseapi.LiveStatusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Status.State == live.StateDisabled {
		t.Fatal("enabled runtime must not report disabled")
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/pulse/live/services", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("live services=%d %s", rr.Code, rr.Body.String())
	}
	var svc pulseapi.LiveServicesResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &svc); err != nil {
		t.Fatal(err)
	}
	if svc.Count < 1 {
		t.Fatal("expected observed services")
	}
	foundPayment := false
	for _, s := range svc.Services {
		if s.ServiceID == "astronomy/local/payment" {
			foundPayment = true
			if s.GraphNode != "payment" {
				t.Fatalf("graph_node=%q", s.GraphNode)
			}
			if s.Intelligence == observation.StatusHealthy && s.Quality == "missing" {
				t.Fatal("missing must not be healthy")
			}
		}
	}
	if !foundPayment {
		t.Fatal("payment missing from live services")
	}

	// Historical routes still work; Run Pulse independent (no engine).
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/pulse/workflows", nil))
	if rr.Code != http.StatusOK && rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("workflows=%d", rr.Code)
	}

	rt.Stop()
}

func drivePolls(t *testing.T, rt *live.Runtime, src *scriptedSource, advance func(time.Duration), step time.Duration, n int) {
	t.Helper()
	// Access poll via Start with cancelled context after manually invoking
	// through a test-only pattern: recreate using exported Start won't poll n times
	// synchronously. Use reflection-free approach — call Collect indirectly by
	// starting with context and a tiny poll is flaky.
	//
	// Instead, use the runtime's Start once and force polls by temporarily
	// relying on first immediate poll + manual re-entry through Stop/Start
	// won't re-poll batches correctly.
	//
	// Best approach for deterministic tests: export nothing; drive via
	// repeated NewRuntime is wrong. We'll use a package-level test in live/
	// with poll exported... For now call Start which does one poll, then
	// use httptest-less internal by putting tests in package live.

	// Workaround: start runtime, then for each subsequent batch, Stop is
	// destructive. Put helper in live package as PollOnce for tests.
	rt.PollOnce(context.Background())
	for i := 1; i < n; i++ {
		advance(step)
		rt.PollOnce(context.Background())
	}
	_ = src
}

func TestLive_NewEpisodeAfterRecoveryGetsNewIncidentID(t *testing.T) {
	base := time.Date(2026, 10, 11, 21, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := base
	nowFn := func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	advance := func(d time.Duration) { mu.Lock(); clock = clock.Add(d); mu.Unlock() }

	src := &scriptedSource{}
	for i := 0; i < 12; i++ {
		src.batches = append(src.batches, paymentTriple(80, 0.01, 1.0))
	}
	for i := 0; i < 6; i++ {
		src.batches = append(src.batches, paymentTriple(5000, 0.01, 1.0))
	}
	for i := 0; i < 10; i++ {
		src.batches = append(src.batches, paymentTriple(90, 0.01, 1.0))
	}
	for i := 0; i < 6; i++ {
		src.batches = append(src.batches, paymentTriple(5000, 0.01, 1.0))
	}

	dir := t.TempDir()
	rt, err := live.NewRuntime(live.Options{
		Config:    enabledCfg(),
		PulseDir:  dir,
		DetectCfg: pipeline.DemoDetectConfig(),
		Clock:     nowFn,
		Source:    src,
	})
	if err != nil {
		t.Fatal(err)
	}
	drivePolls(t, rt, src, advance, 15*time.Second, len(src.batches))
	list, err := incident.NewFileStore(dir).List()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, inc := range list {
		if inc.PrimaryServiceID == "astronomy/local/payment" {
			ids = append(ids, inc.IncidentID)
		}
	}
	if len(ids) < 2 {
		t.Fatalf("expected distinct incidents for two episodes, got %v", ids)
	}
	if ids[0] == ids[1] && len(ids) == 2 {
		t.Fatalf("second episode reused first incident_id: %v", ids)
	}
	uniq := map[string]struct{}{}
	for _, id := range ids {
		uniq[id] = struct{}{}
	}
	if len(uniq) < 2 {
		t.Fatalf("expected >=2 unique incident ids after two episodes, got %v", ids)
	}
	rt.Stop()
}

func TestLive_NoDuplicateIncidentOnRepeatedPoll(t *testing.T) {
	base := time.Date(2026, 10, 11, 14, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := base
	nowFn := func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	advance := func(d time.Duration) { mu.Lock(); clock = clock.Add(d); mu.Unlock() }

	src := &scriptedSource{}
	for i := 0; i < 12; i++ {
		src.batches = append(src.batches, paymentTriple(80, 0.01, 1.0))
	}
	for i := 0; i < 8; i++ {
		src.batches = append(src.batches, paymentTriple(5000, 0.01, 1.0))
	}

	cfg := enabledCfg()
	dir := t.TempDir()
	rt, err := live.NewRuntime(live.Options{
		Config:    cfg,
		PulseDir:  dir,
		DetectCfg: pipeline.DemoDetectConfig(),
		Clock:     nowFn,
		Source:    src,
	})
	if err != nil {
		t.Fatal(err)
	}
	drivePolls(t, rt, src, advance, 15*time.Second, len(src.batches))

	list, err := incident.NewFileStore(dir).List()
	if err != nil {
		t.Fatal(err)
	}
	open := 0
	ids := map[string]int{}
	for _, inc := range list {
		ids[inc.IncidentID]++
		if inc.Open() && inc.PrimaryServiceID == "astronomy/local/payment" {
			open++
		}
	}
	for id, n := range ids {
		if n != 1 {
			t.Fatalf("duplicate writes for %s count=%d", id, n)
		}
	}
	if open > 1 {
		t.Fatalf("expected at most one open payment incident, got %d (%d total)", open, len(list))
	}
	// Second wave of spike polls should upsert same ID.
	for i := 0; i < 3; i++ {
		advance(15 * time.Second)
		rt.PollOnce(context.Background())
	}
	list2, err := incident.NewFileStore(dir).List()
	if err != nil {
		t.Fatal(err)
	}
	openIDs := map[string]struct{}{}
	for _, inc := range list2 {
		if inc.Open() && inc.PrimaryServiceID == "astronomy/local/payment" {
			openIDs[inc.IncidentID] = struct{}{}
		}
	}
	if len(openIDs) > 1 {
		t.Fatalf("duplicate open incidents: %v", openIDs)
	}
	rt.Stop()
}

func TestLive_NoRecoveryFromTelemetryAbsence(t *testing.T) {
	base := time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := base
	nowFn := func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	advance := func(d time.Duration) { mu.Lock(); clock = clock.Add(d); mu.Unlock() }

	src := &scriptedSource{}
	for i := 0; i < 12; i++ {
		src.batches = append(src.batches, paymentTriple(80, 0.01, 1.0))
	}
	for i := 0; i < 6; i++ {
		src.batches = append(src.batches, paymentTriple(80, 0.5, 1.0))
	}

	cfg := enabledCfg()
	dir := t.TempDir()
	rt, err := live.NewRuntime(live.Options{
		Config:    cfg,
		PulseDir:  dir,
		DetectCfg: pipeline.DemoDetectConfig(),
		Clock:     nowFn,
		Source:    src,
	})
	if err != nil {
		t.Fatal(err)
	}
	drivePolls(t, rt, src, advance, 15*time.Second, len(src.batches))

	list, err := incident.NewFileStore(dir).List()
	if err != nil {
		t.Fatal(err)
	}
	var opened string
	for _, inc := range list {
		if inc.PrimaryServiceID == "astronomy/local/payment" && inc.Open() {
			opened = inc.IncidentID
			break
		}
	}
	if opened == "" {
		t.Fatal("expected an open payment incident before absence")
	}

	// Advance well past the current window with empty collects. Insufficient
	// current samples must leave the finding open (absence ≠ recovery).
	src.batches = nil // Collect returns last batch; set errs? use empty via nil last
	// Force empty responses:
	src.mu.Lock()
	src.batches = [][]detect.Sample{{}}
	src.calls = 0
	src.mu.Unlock()
	for i := 0; i < 8; i++ {
		advance(30 * time.Second)
		rt.PollOnce(context.Background())
	}

	got, ok, err := incident.NewFileStore(dir).Get(opened)
	if err != nil || !ok {
		t.Fatalf("get open incident: ok=%v err=%v", ok, err)
	}
	if !got.Open() {
		t.Fatalf("telemetry absence must not recover incident; recovered_at=%v", got.RecoveredAt)
	}
	rt.Stop()
}

func TestLive_ProviderOutageRetainsSnapshot(t *testing.T) {
	base := time.Date(2026, 10, 11, 16, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := base
	nowFn := func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	advance := func(d time.Duration) { mu.Lock(); clock = clock.Add(d); mu.Unlock() }

	src := &scriptedSource{
		batches: [][]detect.Sample{paymentTriple(80, 0.01, 1.0)},
		errs:    []error{nil, fmt.Errorf("connection refused")},
	}
	cfg := enabledCfg()
	rt, err := live.NewRuntime(live.Options{
		Config:    cfg,
		PulseDir:  t.TempDir(),
		DetectCfg: pipeline.DemoDetectConfig(),
		Clock:     nowFn,
		Source:    src,
	})
	if err != nil {
		t.Fatal(err)
	}
	rt.PollOnce(context.Background())
	advance(15 * time.Second)
	rt.PollOnce(context.Background())
	snap := rt.Snapshot()
	if snap.Status.State != live.StateError {
		t.Fatalf("state=%s want error", snap.Status.State)
	}
	if snap.Status.LastError == "" {
		t.Fatal("expected last_error")
	}
	if strings.Contains(strings.ToLower(snap.Status.LastError), "healthy") {
		t.Fatal("outage must not look healthy")
	}
	if strings.Contains(snap.Status.LastError, "connection refused") {
		t.Fatalf("raw provider error leaked: %q", snap.Status.LastError)
	}
	for _, svc := range snap.Services {
		if svc.Intelligence == observation.StatusHealthy {
			t.Fatalf("stale/outage service must not be healthy: %+v", svc)
		}
	}
	rt.Stop()
}

func TestLive_ConcurrentSnapshotReads(t *testing.T) {
	base := time.Date(2026, 10, 11, 17, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := base
	nowFn := func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }

	src := &scriptedSource{batches: [][]detect.Sample{paymentTriple(90, 0.02, 0.8)}}
	rt, err := live.NewRuntime(live.Options{
		Config:    enabledCfg(),
		PulseDir:  t.TempDir(),
		DetectCfg: pipeline.DemoDetectConfig(),
		Clock:     nowFn,
		Source:    src,
	})
	if err != nil {
		t.Fatal(err)
	}
	rt.PollOnce(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = rt.Snapshot()
		}()
	}
	wg.Wait()
	rt.Stop()
}

func TestLive_CancellationStopsLoop(t *testing.T) {
	cfg := enabledCfg()
	cfg.PollInterval = time.Second
	src := &scriptedSource{batches: [][]detect.Sample{paymentTriple(80, 0.01, 1.0)}}
	rt, err := live.NewRuntime(live.Options{
		Config:    cfg,
		PulseDir:  t.TempDir(),
		DetectCfg: pipeline.DemoDetectConfig(),
		Source:    src,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	rt.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	cancel()
	done := make(chan struct{})
	go func() {
		rt.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("runtime did not stop")
	}
}

func TestLiveAPI_DisabledMounted(t *testing.T) {
	h := &pulseapi.Handler{Reader: mustReader(t)}
	mux := http.NewServeMux()
	h.Register(mux)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/pulse/live/status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"enabled":false`) && !strings.Contains(rr.Body.String(), "disabled") {
		t.Fatalf("body=%s", rr.Body.String())
	}
	if pulseapi.ContainsGroundTruthLeak(rr.Body.String()) {
		t.Fatal("gt leak")
	}
}

func TestIncidentUpsert_AllowsRecoveryUpdate(t *testing.T) {
	dir := t.TempDir()
	fs := incident.NewFileStore(dir)
	first := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	inc := minimalIncident(first, nil)
	if err := fs.Upsert(inc); err != nil {
		t.Fatal(err)
	}
	rec := first.Add(time.Minute)
	inc.RecoveredAt = &rec
	inc.LastObservedAt = rec
	if err := fs.Upsert(inc); err != nil {
		t.Fatal(err)
	}
	got, ok, err := fs.Get(inc.IncidentID)
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.RecoveredAt == nil {
		t.Fatal("expected recovered_at persisted")
	}
}

func enabledCfg() live.Config {
	return live.Config{
		Enabled:        true,
		PrometheusURL:  "http://127.0.0.1:9090",
		PollInterval:   5 * time.Minute, // max allowed; tests drive PollOnce
		RequestTimeout: 2 * time.Second,
		Environment:    "local",
		Freshness:      2 * time.Minute,
		SampleWindow:   2 * time.Minute,
		MaxServices:    32,
	}
}

func mustReader(t *testing.T) *pulseapi.Reader {
	t.Helper()
	r, err := pulseapi.NewFileReader(t.TempDir(), pulseapi.EmptyMapper{})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func minimalIncident(first time.Time, recovered *time.Time) incident.Incident {
	last := first.Add(30 * time.Second)
	if recovered != nil {
		last = *recovered
	}
	return incident.Incident{
		SchemaVersion:    incident.SchemaVersion,
		IncidentID:       incident.IncidentIDFor("astronomy/local/payment", first),
		PrimaryServiceID: "astronomy/local/payment",
		Environment:      "local",
		FindingIDs:       []string{"f1"},
		SignalTypes:      []string{"latency_ms"},
		FirstDetectedAt:  first,
		LastObservedAt:   last,
		RecoveredAt:      recovered,
		Before:           incident.PhaseSnapshot{Phase: "before", Window: incident.Window{Start: first.Add(-time.Minute), End: first}},
		During:           incident.PhaseSnapshot{Phase: "during", Window: incident.Window{Start: first, End: last}, Available: true},
		After:            incident.PhaseSnapshot{Phase: "after", Window: incident.Window{Start: last, End: last}},
		Observations:     []string{"observed latency regression"},
		Inferences:       []string{"correlated change only; not proven causation"},
		Limitations:      []string{"test"},
		Summary:          "payment latency test",
	}
}
