package live

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/incident"
	"github.com/yashg4509/perch/internal/pulse/observation"
	"github.com/yashg4509/perch/internal/pulse/runpulse"
	"github.com/yashg4509/perch/internal/pulse/store"
	"github.com/yashg4509/perch/internal/pulse/telem"
)

// defaultDetectConfig matches pipeline.DemoDetectConfig without importing
// pipeline (which would create live → pipeline → api → live).
func defaultDetectConfig() detect.Config {
	cfg := detect.DefaultConfig()
	cfg.BaselineWindow = 2 * time.Minute
	cfg.CurrentWindow = 30 * time.Second
	cfg.MinBaselineSamples = 4
	cfg.MinCurrentSamples = 2
	cfg.LatencyRobustZ = 0
	cfg.MinCallRateForLatency = 0.05
	return cfg
}

// Mapper resolves Pulse service IDs to graph nodes (same contract as pulse/api).
type Mapper interface {
	GraphNodeForPulseServiceID(serviceID string) (graphNode, mappingSource string, ok bool)
}

// Options constructs a Runtime.
type Options struct {
	Config   Config
	PulseDir string
	Mapper   Mapper
	// DetectCfg overrides detector thresholds; zero BaselineWindow uses live defaults.
	DetectCfg detect.Config
	// Clock injects time for tests.
	Clock func() time.Time
	// Source overrides Prometheus collection (tests).
	Source telem.Source
	// IncidentStore overrides FileStore (tests).
	IncidentStore incident.Store
	// ObservationStore receives latest live observations (optional).
	ObservationStore store.Store
}

// Runtime is one collector loop per viz process.
type Runtime struct {
	cfg       Config
	mapper    Mapper
	detector  *detect.Detector
	source    telem.Source
	incidents incident.Store
	obsStore  store.Store
	clock     func() time.Time

	mu        sync.RWMutex
	snap      Snapshot
	openBySvc map[string]string // serviceID -> incidentID for live-managed opens
	samples   []detect.Sample   // bounded recent samples for incident digests
	started   bool
	cancel    context.CancelFunc
	done      chan struct{}
}

// NewRuntime validates options and builds a stopped Runtime.
func NewRuntime(opts Options) (*Runtime, error) {
	cfg := opts.Config
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(opts.PulseDir) == "" && cfg.Available() {
		return nil, fmt.Errorf("live pulse: PulseDir is required when enabled")
	}
	detCfg := opts.DetectCfg
	if detCfg.BaselineWindow == 0 {
		detCfg = defaultDetectConfig()
	}
	d := detect.New(detCfg)
	clock := opts.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	d.SetClock(clock)

	var src telem.Source = opts.Source
	if src == nil && cfg.Available() {
		p := telem.NewPrometheus(cfg.PrometheusURL)
		p.Window = cfg.SampleWindow
		p.Environment = cfg.Environment
		p.HTTPClient = runpulse.SafeHTTPClient(&http.Client{Timeout: cfg.RequestTimeout})
		src = p
	}

	var istore incident.Store = opts.IncidentStore
	if istore == nil && opts.PulseDir != "" {
		istore = incident.NewFileStore(opts.PulseDir)
	}
	obs := opts.ObservationStore
	if obs == nil {
		obs = newLatestStore()
	}

	rt := &Runtime{
		cfg:       cfg,
		mapper:    opts.Mapper,
		detector:  d,
		source:    src,
		incidents: istore,
		obsStore:  obs,
		clock:     clock,
		openBySvc: map[string]string{},
		done:      make(chan struct{}),
	}
	rt.publishDisabled(clock())
	return rt, nil
}

// ObservationStore returns the process-local store wired for historical services API.
func (rt *Runtime) ObservationStore() store.Store {
	if rt == nil {
		return nil
	}
	return rt.obsStore
}

// Snapshot returns a copy of the current read model.
func (rt *Runtime) Snapshot() Snapshot {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return cloneSnapshot(rt.snap)
}

// Start launches the poll loop. Idempotent; no-op when disabled.
func (rt *Runtime) Start(parent context.Context) {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	if rt.started {
		rt.mu.Unlock()
		return
	}
	rt.started = true
	ctx, cancel := context.WithCancel(parent)
	rt.cancel = cancel
	rt.mu.Unlock()

	if !rt.cfg.Available() {
		rt.publishDisabled(rt.clock())
		close(rt.done)
		return
	}

	go func() {
		defer close(rt.done)
		rt.loop(ctx)
	}()
}

// PollOnce runs a single collection cycle. Production uses Start's ticker;
// tests and harnesses call PollOnce with a controlled clock.
func (rt *Runtime) PollOnce(ctx context.Context) {
	if rt == nil {
		return
	}
	rt.poll(ctx)
}

// Stop cancels the collector and waits for exit. No-op if Start was never called.
func (rt *Runtime) Stop() {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	if !rt.started {
		rt.mu.Unlock()
		return
	}
	cancel := rt.cancel
	rt.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	<-rt.done
}

func (rt *Runtime) loop(ctx context.Context) {
	// Immediate first poll, then ticker.
	rt.poll(ctx)
	t := time.NewTicker(rt.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			rt.poll(ctx)
		}
	}
}

func (rt *Runtime) poll(ctx context.Context) {
	now := rt.clock().UTC()
	attempt := now
	samples, err := rt.source.Collect(ctx, now)
	if err != nil {
		rt.publishError(attempt, err)
		return
	}
	if err := rt.detector.Ingest(samples); err != nil {
		rt.publishError(attempt, err)
		return
	}
	findings, err := rt.detector.Evaluate(now)
	if err != nil {
		rt.publishError(attempt, err)
		return
	}
	rt.retainSamples(samples, now)
	if err := rt.syncIncidents(findings, now); err != nil {
		rt.publishError(attempt, err)
		return
	}
	rt.publishSuccess(attempt, now, samples, findings)
}

func (rt *Runtime) retainSamples(samples []detect.Sample, now time.Time) {
	cutoff := now.Add(-(rt.detectorWindowBound() + time.Minute))
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.samples = append(rt.samples, samples...)
	detect.SortSamples(rt.samples)
	trimmed := rt.samples[:0]
	seen := map[string]struct{}{}
	for i := len(rt.samples) - 1; i >= 0; i-- {
		s := rt.samples[i]
		if s.ObservedAt.Before(cutoff) {
			continue
		}
		key := s.ServiceID + "|" + string(s.Signal) + "|" + s.ObservedAt.Format(time.RFC3339Nano) + "|" + fmt.Sprintf("%g", s.Value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		trimmed = append(trimmed, s)
	}
	// Reverse back to ascending.
	for i, j := 0, len(trimmed)-1; i < j; i, j = i+1, j-1 {
		trimmed[i], trimmed[j] = trimmed[j], trimmed[i]
	}
	// Hard cap to avoid unbounded growth under rapid clocks.
	const maxSamples = 4096
	if len(trimmed) > maxSamples {
		trimmed = trimmed[len(trimmed)-maxSamples:]
	}
	rt.samples = trimmed
}

func (rt *Runtime) detectorWindowBound() time.Duration {
	// DemoDetectConfig: baseline 2m + current 30s; keep a little headroom.
	return 3*time.Minute + 30*time.Second
}

func (rt *Runtime) syncIncidents(findings []detect.Finding, now time.Time) error {
	if rt.incidents == nil {
		return nil
	}
	rt.mu.Lock()
	allSamples := append([]detect.Sample(nil), rt.samples...)
	openBySvc := copyStringMap(rt.openBySvc)
	rt.mu.Unlock()

	openBySvcFindings := map[string][]detect.Finding{}
	recoveredBySvc := map[string][]detect.Finding{}
	for _, f := range findings {
		if f.RecoveredAt == nil {
			openBySvcFindings[f.ServiceID] = append(openBySvcFindings[f.ServiceID], f)
		} else {
			recoveredBySvc[f.ServiceID] = append(recoveredBySvc[f.ServiceID], f)
		}
	}

	stillOpen := map[string]string{}
	// Active regressions: build from currently open findings only. Never mix in
	// historical recovered findings (Evaluate returns them every tick) or a new
	// episode would inherit the old incident_id / FirstDetectedAt.
	for svc, group := range openBySvcFindings {
		inc, err := incident.Build(incident.BuildInput{
			Findings:    group,
			Samples:     allSamples,
			Environment: rt.cfg.Environment,
		})
		if err != nil {
			return err
		}
		if prevID, ok := openBySvc[svc]; ok {
			if prev, ok, err := rt.incidents.Get(prevID); err == nil && ok && prev.Open() {
				inc.IncidentID = prev.IncidentID
				inc.FirstDetectedAt = prev.FirstDetectedAt
			}
		}
		if err := rt.incidents.Upsert(inc); err != nil {
			return err
		}
		stillOpen[svc] = inc.IncidentID
	}

	// Recovery: only for services this process was tracking as live-open and
	// that no longer have open findings this tick.
	for svc, prevID := range openBySvc {
		if _, ok := stillOpen[svc]; ok {
			continue
		}
		group := recoveredBySvc[svc]
		if len(group) == 0 {
			// Absence / non-decidable: detector keeps findings open, so we
			// should not reach here. If we do, leave the persisted incident alone.
			continue
		}
		inc, err := incident.Build(incident.BuildInput{
			Findings:    group,
			Samples:     allSamples,
			Environment: rt.cfg.Environment,
		})
		if err != nil {
			return err
		}
		if prev, ok, err := rt.incidents.Get(prevID); err == nil && ok {
			inc.IncidentID = prev.IncidentID
			inc.FirstDetectedAt = prev.FirstDetectedAt
		}
		if err := rt.incidents.Upsert(inc); err != nil {
			return err
		}
	}
	_ = now
	rt.mu.Lock()
	rt.openBySvc = stillOpen
	rt.mu.Unlock()
	return nil
}

func (rt *Runtime) publishDisabled(now time.Time) {
	reason := rt.cfg.UnavailabilityReason()
	st := StatusView{
		SchemaVersion:        SchemaLiveStatus,
		GeneratedAt:          now.UTC(),
		Enabled:              rt.cfg.Enabled,
		Configured:           rt.cfg.Available(),
		Available:            false,
		State:                StateDisabled,
		Environment:          rt.cfg.Environment,
		PrometheusConfigured: strings.TrimSpace(rt.cfg.PrometheusURL) != "",
		ActiveHealthNote:     activeHealthNote,
		RunPulseNote:         "Run Pulse remains independent on /api/pulse/workflows and /api/pulse/runs.",
		Limitations: []string{
			"Live Pulse collector is not running.",
			"Missing or disabled live telemetry is never reported as healthy.",
		},
		UnavailabilityReason: reason,
	}
	if rt.cfg.Enabled && !rt.cfg.Available() {
		st.State = StateUnavailable
	}
	rt.mu.Lock()
	rt.snap = Snapshot{Status: st, Services: []ServiceView{}, Incidents: []IncidentView{}}
	rt.mu.Unlock()
}

func (rt *Runtime) publishError(attempt time.Time, err error) {
	now := rt.clock().UTC()
	msg := sanitizeError(err)
	rt.mu.Lock()
	prev := rt.snap
	st := prev.Status
	st.SchemaVersion = SchemaLiveStatus
	st.GeneratedAt = now
	st.Enabled = true
	st.Configured = true
	st.Available = false
	st.State = StateError
	st.Environment = rt.cfg.Environment
	st.PrometheusConfigured = true
	st.PollInterval = rt.cfg.PollInterval.String()
	st.SampleWindow = rt.cfg.SampleWindow.String()
	st.FreshnessWindow = rt.cfg.Freshness.String()
	st.LastAttemptAt = timePtr(attempt.UTC())
	st.LastError = msg
	st.ActiveHealthNote = activeHealthNote
	st.RunPulseNote = "Run Pulse remains independent on /api/pulse/workflows and /api/pulse/runs."
	st.Limitations = []string{
		"Telemetry provider outage or malformed response; last successful snapshot retained when present.",
		"Provider outage is not recovery and is never reported as healthy.",
	}
	st.UnavailabilityReason = msg
	// Keep prior services/incidents but mark quality/intelligence stale — never healthy.
	services := prev.Services
	for i := range services {
		if services[i].Quality == "fresh" || services[i].Quality == "stale" {
			services[i].Quality = "stale"
			services[i].Freshness = "stale"
			if services[i].Intelligence == observation.StatusHealthy ||
				services[i].Intelligence == observation.StatusUnknown {
				services[i].Intelligence = observation.StatusStale
			}
		}
		for j := range services[i].Signals {
			if services[i].Signals[j].Quality == "fresh" {
				services[i].Signals[j].Quality = "stale"
			}
		}
	}
	rt.snap = Snapshot{Status: st, Services: services, Incidents: prev.Incidents}
	rt.mu.Unlock()
}

func (rt *Runtime) publishSuccess(attempt, now time.Time, samples []detect.Sample, findings []detect.Finding) {
	bySvcSignals := map[string]map[detect.SignalType]detect.Sample{}
	for _, s := range samples {
		if bySvcSignals[s.ServiceID] == nil {
			bySvcSignals[s.ServiceID] = map[detect.SignalType]detect.Sample{}
		}
		bySvcSignals[s.ServiceID][s.Signal] = s
	}

	openFindings := map[string][]detect.Finding{}
	for _, f := range findings {
		if f.RecoveredAt == nil {
			openFindings[f.ServiceID] = append(openFindings[f.ServiceID], f)
		}
	}
	baselineReady := map[string]bool{}
	for svc := range bySvcSignals {
		baselineReady[svc] = rt.detector.HasBaseline(svc, now)
	}

	rt.mu.RLock()
	openBySvc := copyStringMap(rt.openBySvc)
	rt.mu.RUnlock()

	services := make([]ServiceView, 0, len(bySvcSignals))
	activeReg := 0
	unknown := 0
	withBase := 0
	for svc, sigs := range bySvcSignals {
		var lastAt time.Time
		sigVals := map[detect.SignalType]float64{}
		for _, s := range sigs {
			sigVals[s.Signal] = s.Value
			if s.ObservedAt.After(lastAt) {
				lastAt = s.ObservedAt
			}
		}
		q := qualityFor(lastAt, now, rt.cfg.Freshness, true)
		regs := openFindings[svc]
		regOpen := len(regs) > 0
		if regOpen {
			activeReg++
		}
		baseReady := baselineReady[svc]
		if baseReady {
			withBase++
		}
		intel := intelligenceFromState(regOpen, q, baseReady)
		if intel == observation.StatusUnknown || intel == observation.StatusUnavailable {
			unknown++
		}
		if obs, err := observationForService(svc, lastAt, intel, summarizeSignals(sigVals)); err == nil && rt.obsStore != nil {
			_ = rt.obsStore.Append(obs)
		}

		sv := ServiceView{
			ServiceID:        svc,
			Environment:      rt.cfg.Environment,
			Freshness:        q,
			Quality:          q,
			LastObservedAt:   timePtr(lastAt),
			Intelligence:     intel,
			BaselineReady:    baseReady,
			RegressionOpen:   regOpen,
			OpenIncidentIDs:  []string{},
			Signals:          buildSignalViews(sigs, regs, baseReady, q, rt.cfg.SampleWindow.String()),
			ActiveHealthNote: activeHealthNote,
		}
		if id, ok := openBySvc[svc]; ok {
			sv.OpenIncidentIDs = []string{id}
		}
		if rt.mapper != nil {
			if node, src, ok := rt.mapper.GraphNodeForPulseServiceID(svc); ok {
				sv.GraphNode = node
				sv.MappingSource = src
			} else {
				sv.Limitations = append(sv.Limitations, "Service has no explicit graph mapping; not attached to a graph node.")
			}
		}
		if !baseReady {
			sv.Limitations = append(sv.Limitations, "Baseline still warming up; no regression claim from a single sample.")
		}
		services = append(services, sv)
	}
	sort.SliceStable(services, func(i, j int) bool { return services[i].ServiceID < services[j].ServiceID })
	if len(services) > rt.cfg.MaxServices {
		services = services[:rt.cfg.MaxServices]
	}

	incViews := rt.buildIncidentViews(now)
	activeInc := 0
	for _, iv := range incViews {
		if iv.Lifecycle == "active" {
			activeInc++
		}
	}

	state := StateCollecting
	if withBase == 0 && len(services) > 0 {
		state = StateWarmingUp
	}
	if len(services) == 0 {
		state = StateWarmingUp
		unknown++
	}

	st := StatusView{
		SchemaVersion:        SchemaLiveStatus,
		GeneratedAt:          now.UTC(),
		Enabled:              true,
		Configured:           true,
		Available:            true,
		State:                state,
		Environment:          rt.cfg.Environment,
		PrometheusConfigured: true,
		PollInterval:         rt.cfg.PollInterval.String(),
		SampleWindow:         rt.cfg.SampleWindow.String(),
		FreshnessWindow:      rt.cfg.Freshness.String(),
		LastAttemptAt:        timePtr(attempt.UTC()),
		LastSuccessAt:        timePtr(now.UTC()),
		ServicesObserved:     len(services),
		ServicesWithBaseline: withBase,
		ActiveRegressions:    activeReg,
		ActiveIncidents:      activeInc,
		UnavailableOrUnknown: unknown,
		DetectorVersion:      detect.DetectorVersion,
		ActiveHealthNote:     activeHealthNote,
		RunPulseNote:         "Run Pulse remains independent on /api/pulse/workflows and /api/pulse/runs.",
		Limitations: []string{
			"Live snapshot is polled server-side; GET handlers do not query Prometheus.",
			"Missing series are omitted — never treated as zero errors or healthy.",
			"Stale telemetry is not healthy; missing telemetry is not recovery.",
			"Detector findings are not proof of root cause.",
		},
	}

	rt.mu.Lock()
	rt.snap = Snapshot{Status: st, Services: services, Incidents: incViews}
	rt.mu.Unlock()
}

func buildSignalViews(sigs map[detect.SignalType]detect.Sample, regs []detect.Finding, baseReady bool, quality, window string) []SignalView {
	order := []detect.SignalType{detect.SignalLatencyMS, detect.SignalErrorRate, detect.SignalCallRate}
	regBySig := map[detect.SignalType]detect.Finding{}
	for _, f := range regs {
		regBySig[f.SignalType] = f
	}
	out := make([]SignalView, 0, len(order))
	for _, sig := range order {
		s, ok := sigs[sig]
		sv := SignalView{
			Signal:        sig,
			Unit:          signalUnit(sig),
			Window:        window,
			Quality:       "missing",
			BaselineReady: baseReady,
		}
		if ok {
			v := s.Value
			sv.Value = &v
			t := s.ObservedAt.UTC()
			sv.ObservedAt = &t
			sv.Quality = quality
		}
		if f, ok := regBySig[sig]; ok {
			sv.RegressionOpen = true
			score := f.Score
			sv.RegressionScore = &score
			bv := f.BaselineValue
			sv.BaselineValue = &bv
			sv.BaselineReady = true
		}
		out = append(out, sv)
	}
	return out
}

func (rt *Runtime) buildIncidentViews(now time.Time) []IncidentView {
	if rt.incidents == nil {
		return []IncidentView{}
	}
	list, err := rt.incidents.List()
	if err != nil {
		return []IncidentView{}
	}
	rt.mu.RLock()
	liveIDs := map[string]struct{}{}
	for _, id := range rt.openBySvc {
		liveIDs[id] = struct{}{}
	}
	rt.mu.RUnlock()

	out := make([]IncidentView, 0, len(list))
	seen := map[string]struct{}{}
	for _, inc := range list {
		if _, dup := seen[inc.IncidentID]; dup {
			continue
		}
		seen[inc.IncidentID] = struct{}{}
		// Filter to this environment when possible.
		if inc.Environment != "" && inc.Environment != rt.cfg.Environment {
			continue
		}
		lifecycle := "historical"
		source := "historical"
		if _, ok := liveIDs[inc.IncidentID]; ok && inc.Open() {
			lifecycle = "active"
			source = "live"
		} else if !inc.Open() {
			lifecycle = "recovered"
			if _, ok := liveIDs[inc.IncidentID]; ok {
				source = "live"
			}
		} else if inc.Open() {
			// Persisted open but not in this process's live map (restart / historical).
			lifecycle = "active"
			source = "historical"
		}
		iv := IncidentView{
			IncidentID:     inc.IncidentID,
			Lifecycle:      lifecycle,
			Source:         source,
			PrimaryService: inc.PrimaryServiceID,
			FirstDetected:  inc.FirstDetectedAt,
			LastObserved:   inc.LastObservedAt,
			RecoveredAt:    inc.RecoveredAt,
			SignalTypes:    append([]string(nil), inc.SignalTypes...),
			Summary:        inc.Summary,
			Incident:       inc,
			Limitations: []string{
				"Lifecycle labels distinguish live collector state from persisted history.",
				"Missing telemetry after an incident is not recovery.",
			},
		}
		if rt.mapper != nil {
			if node, _, ok := rt.mapper.GraphNodeForPulseServiceID(inc.PrimaryServiceID); ok {
				iv.GraphNode = node
			}
		}
		_ = now
		out = append(out, iv)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].FirstDetected.Equal(out[j].FirstDetected) {
			return out[i].FirstDetected.After(out[j].FirstDetected)
		}
		return out[i].IncidentID < out[j].IncidentID
	})
	const maxInc = 100
	if len(out) > maxInc {
		out = out[:maxInc]
	}
	return out
}

func timePtr(t time.Time) *time.Time {
	t = t.UTC()
	return &t
}

func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func sanitizeError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "cardinality"):
		return "telemetry provider rejected: series cardinality too high"
	case strings.Contains(lower, "exceeds") && strings.Contains(lower, "bytes"):
		return "telemetry provider rejected: response too large"
	case strings.Contains(lower, "status"):
		return "telemetry provider HTTP error"
	case strings.Contains(lower, "json"):
		return "telemetry provider returned malformed JSON"
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline"):
		return "telemetry provider timeout"
	case strings.Contains(lower, "connection refused") || strings.Contains(lower, "connect"):
		return "telemetry provider unreachable"
	default:
		// Do not forward raw provider bodies (may contain sensitive labels).
		return "telemetry provider error"
	}
}

// DisabledSnapshot returns a static unavailable snapshot without a Runtime.
func DisabledSnapshot(now time.Time, reason string) Snapshot {
	if reason == "" {
		reason = "Live Pulse is disabled."
	}
	return Snapshot{
		Status: StatusView{
			SchemaVersion:    SchemaLiveStatus,
			GeneratedAt:      now.UTC(),
			Enabled:          false,
			Configured:       false,
			Available:        false,
			State:            StateDisabled,
			ActiveHealthNote: activeHealthNote,
			RunPulseNote:     "Run Pulse remains independent on /api/pulse/workflows and /api/pulse/runs.",
			Limitations: []string{
				"Live Pulse collector is not running.",
				"Missing or disabled live telemetry is never reported as healthy.",
			},
			UnavailabilityReason: reason,
		},
		Services:  []ServiceView{},
		Incidents: []IncidentView{},
	}
}
