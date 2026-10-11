package pipeline

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	pulseapi "github.com/yashg4509/perch/internal/pulse/api"
	"github.com/yashg4509/perch/internal/pulse/change"
	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/incident"
	"github.com/yashg4509/perch/internal/pulse/observation"
	"github.com/yashg4509/perch/internal/pulse/store"
)

// Config tunes the harness detector windows for deterministic fixtures.
func DemoDetectConfig() detect.Config {
	cfg := detect.DefaultConfig()
	cfg.BaselineWindow = 2 * time.Minute
	cfg.CurrentWindow = 30 * time.Second
	cfg.MinBaselineSamples = 4
	cfg.MinCurrentSamples = 2
	cfg.LatencyRobustZ = 0
	cfg.MinCallRateForLatency = 0.05
	return cfg
}

// Result is the outcome of one harness run through real packages.
type Result struct {
	Findings  []detect.Finding
	Incidents []incident.Incident
	PulseDir  string
	Handler   *pulseapi.Handler
	Samples   []detect.Sample
}

// Options control a harness run.
type Options struct {
	PulseDir    string
	Environment string
	DetectCfg   detect.Config
	Clock       func() time.Time
	// EvaluateAt is passed to Detector.Evaluate (defaults to Clock()).
	EvaluateAt time.Time
	// Changes are persisted before API reads (optional).
	Changes []change.Event
	// Observations optional process-local store for services API freshness.
	Observations store.Store
	// SkipPersist skips writing incidents (still returns built incidents).
	SkipPersist bool
	// PersistOpenOnly writes only findings that are still open (RecoveredAt == nil).
	// When false (default), all built incidents are persisted.
	BuildFromOpenFindings bool
}

// Run ingests samples into a real detector, builds incidents from findings,
// optionally persists them, and returns an API handler over the same directory.
func Run(samples []detect.Sample, opts Options) (Result, error) {
	if opts.PulseDir == "" {
		return Result{}, fmt.Errorf("pipeline: PulseDir is required")
	}
	cfg := opts.DetectCfg
	if cfg.BaselineWindow == 0 {
		cfg = DemoDetectConfig()
	}
	d := detect.New(cfg)
	if opts.Clock != nil {
		d.SetClock(opts.Clock)
	}
	if err := d.Ingest(samples); err != nil {
		return Result{}, err
	}
	evalAt := opts.EvaluateAt
	if evalAt.IsZero() {
		if opts.Clock != nil {
			evalAt = opts.Clock()
		} else {
			evalAt = time.Now().UTC()
		}
	}
	findings, err := d.Evaluate(evalAt)
	if err != nil {
		return Result{}, err
	}

	env := opts.Environment
	if env == "" {
		env = "local"
	}

	var built []incident.Incident
	toBuild := findings
	if opts.BuildFromOpenFindings {
		toBuild = nil
		for _, f := range findings {
			if f.RecoveredAt == nil {
				toBuild = append(toBuild, f)
			}
		}
	}
	// Group findings by primary service for incident assembly.
	bySvc := map[string][]detect.Finding{}
	for _, f := range toBuild {
		bySvc[f.ServiceID] = append(bySvc[f.ServiceID], f)
	}
	for _, group := range bySvc {
		inc, err := incident.Build(incident.BuildInput{
			Findings:    group,
			Samples:     samples,
			Environment: env,
		})
		if err != nil {
			return Result{}, err
		}
		built = append(built, inc)
	}

	if !opts.SkipPersist {
		istore := incident.NewFileStore(opts.PulseDir)
		for _, inc := range built {
			if err := istore.Append(inc); err != nil {
				return Result{}, err
			}
		}
		cstore := change.NewFileStore(opts.PulseDir)
		for _, ev := range opts.Changes {
			if err := cstore.Append(ev); err != nil {
				return Result{}, err
			}
		}
	}

	var mapper pulseapi.ServiceMapper = pulseapi.EmptyMapper{}
	if am, err := pulseapi.NewAstronomyMapper(env); err == nil {
		mapper = am
	}
	reader, err := pulseapi.NewFileReader(opts.PulseDir, mapper)
	if err != nil {
		return Result{}, err
	}
	if opts.Clock != nil {
		reader.Now = opts.Clock
	}
	if opts.Observations != nil {
		reader.WithObservationStore(opts.Observations)
	}
	h := &pulseapi.Handler{Reader: reader}
	return Result{
		Findings:  findings,
		Incidents: built,
		PulseDir:  opts.PulseDir,
		Handler:   h,
		Samples:   samples,
	}, nil
}

// GET is a convenience for asserting API JSON from a harness result.
func GET(h *pulseapi.Handler, path string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	h.Register(mux)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
	return rr
}

// AppendStaleObservation writes an observation that must never read as healthy
// when stale relative to now.
func AppendStaleObservation(s store.Store, serviceID string, observedAt, now time.Time) error {
	if s == nil {
		return fmt.Errorf("pipeline: nil observation store")
	}
	obs := observation.Observation{
		ServiceID:  serviceID,
		Source:     "stackstatus",
		ObservedAt: observedAt.UTC(),
		IngestedAt: observedAt.UTC(),
		Status:     observation.StatusHealthy,
		Evidence:   &observation.Evidence{Ref: "pipeline-test", Summary: "stale fixture — must not read healthy when freshness expires"},
	}
	if err := obs.Validate(); err != nil {
		return err
	}
	return s.Append(obs)
}
