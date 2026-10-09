package scenario_test

import (
	"context"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/pulse/astronomy"
	"github.com/yashg4509/perch/internal/pulse/observation"
	"github.com/yashg4509/perch/internal/pulse/scenario"
)

var (
	failWrites atomic.Bool
	writeDelay atomic.Int64 // nanoseconds
)

func TestCatalogLoadAndRequiredTypes(t *testing.T) {
	c, err := scenario.LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if c.DemoPin != "3.1.0" {
		t.Fatalf("demo pin: %s", c.DemoPin)
	}
	ids := map[string]bool{}
	for _, s := range c.Scenarios {
		ids[s.ID] = true
		if err := s.Validate(c.Environment); err != nil {
			t.Fatalf("%s: %v", s.ID, err)
		}
	}
	for _, need := range []string{
		"latency-shipping-intl",
		"error-payment",
		"outage-payment",
		"control-emit-raw-pii",
	} {
		if !ids[need] {
			t.Fatalf("missing scenario %s", need)
		}
	}
}

func TestInvalidDefinitionRejected(t *testing.T) {
	env := astronomy.LocalEnvironment
	valid := scenario.Definition{
		ID:             "latency-shipping-intl",
		Type:           scenario.TypeLatency,
		Classification: scenario.ClassificationFault,
		TargetOTEL:     "shipping",
		Mechanism: scenario.Mechanism{
			Kind:          scenario.MechanismFlagd,
			Flag:          "intlShippingSlowdown",
			ActiveVariant: "5sec",
			IdleVariant:   "off",
		},
	}
	if err := valid.Validate(env); err != nil {
		t.Fatalf("valid: %v", err)
	}

	cases := []struct {
		name string
		mut  func(*scenario.Definition)
	}{
		{"bad id", func(d *scenario.Definition) { d.ID = "Bad_ID" }},
		{"bad type", func(d *scenario.Definition) { d.Type = "chaos" }},
		{"neutral not control", func(d *scenario.Definition) {
			d.Type = scenario.TypeNeutral
			d.Classification = scenario.ClassificationFault
		}},
		{"control not neutral", func(d *scenario.Definition) {
			d.Type = scenario.TypeLatency
			d.Classification = scenario.ClassificationControl
		}},
		{"same variants", func(d *scenario.Definition) {
			d.Mechanism.ActiveVariant = "off"
			d.Mechanism.IdleVariant = "off"
		}},
		{"bad target", func(d *scenario.Definition) { d.TargetOTEL = "Not A Service" }},
		{"control with affected", func(d *scenario.Definition) {
			d.Type = scenario.TypeNeutral
			d.Classification = scenario.ClassificationControl
			d.ExpectedAffectedOTEL = []string{"checkout"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := valid
			tc.mut(&d)
			if err := d.Validate(env); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestDeterministicRunIDAndTimestamps(t *testing.T) {
	c, err := scenario.LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	def, err := c.Get("error-payment")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 9, 18, 0, 0, 123456789, time.UTC)
	r1, err := scenario.NewRecord(def, c.Environment, start)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := scenario.NewRecord(def, c.Environment, start)
	if err != nil {
		t.Fatal(err)
	}
	want := "error-payment-20261009T180000.123456789Z"
	if r1.RunID != want || r2.RunID != want {
		t.Fatalf("run id: got %q %q want %q", r1.RunID, r2.RunID, want)
	}
	if !r1.StartTime.Equal(start) || r1.StartTime.Location() != time.UTC {
		t.Fatalf("start time: %v", r1.StartTime)
	}
	if r1.TargetServiceID != astronomy.MustLocalServiceID("payment") {
		t.Fatalf("target: %s", r1.TargetServiceID)
	}
	if r1.Classification != scenario.ClassificationFault {
		t.Fatalf("classification: %s", r1.Classification)
	}
}

func TestControlVsFaultClassification(t *testing.T) {
	c, err := scenario.LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	ctrl, err := c.Get("control-emit-raw-pii")
	if err != nil {
		t.Fatal(err)
	}
	if ctrl.Classification != scenario.ClassificationControl || ctrl.Type != scenario.TypeNeutral {
		t.Fatalf("control: %+v", ctrl)
	}
	if len(ctrl.ExpectedAffectedOTEL) != 0 {
		t.Fatalf("control must have empty affected: %v", ctrl.ExpectedAffectedOTEL)
	}
	fault, err := c.Get("outage-payment")
	if err != nil {
		t.Fatal(err)
	}
	if fault.Classification != scenario.ClassificationFault {
		t.Fatalf("fault classification: %s", fault.Classification)
	}
}

func TestServiceIdentityCompatibility(t *testing.T) {
	c, err := scenario.LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := astronomy.Load()
	if err != nil {
		t.Fatal(err)
	}
	otel := map[string]struct{}{}
	for _, s := range mapping.Services {
		if s.OTELServiceName != "" {
			otel[s.OTELServiceName] = struct{}{}
		}
	}
	for _, def := range c.Scenarios {
		if _, ok := otel[def.TargetOTEL]; !ok {
			t.Fatalf("%s target %q not in astronomy mapping", def.ID, def.TargetOTEL)
		}
		rec, err := scenario.NewRecord(def, c.Environment, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		// Opaque non-empty ServiceID compatible with observation.
		obs := observation.Observation{
			ServiceID:  rec.TargetServiceID,
			Status:     observation.StatusHealthy,
			ObservedAt: rec.StartTime,
			IngestedAt: rec.StartTime,
			Source:     "test",
		}
		if err := obs.Validate(); err != nil {
			t.Fatalf("observation reject scenario target id: %v", err)
		}
	}
}

func TestNoSecretsInRecord(t *testing.T) {
	params := map[string]string{"api_key": "x"}
	rec := scenario.Record{
		SchemaVersion:      scenario.SchemaVersion,
		ScenarioID:         "error-payment",
		RunID:              "error-payment-1",
		ScenarioType:       scenario.TypeErrorRate,
		Classification:     scenario.ClassificationFault,
		Environment:        "local",
		TargetServiceID:    astronomy.MustLocalServiceID("payment"),
		StartTime:          time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		InjectedParameters: params,
		State:              scenario.StateIdle,
		Mechanism: scenario.Mechanism{
			Kind:          scenario.MechanismFlagd,
			Flag:          "paymentFailure",
			ActiveVariant: "50%",
			IdleVariant:   "off",
		},
	}
	if err := rec.Validate(); err == nil {
		t.Fatal("expected secret key rejection")
	}
}

func TestLabelsSeparatedFromObservation(t *testing.T) {
	// Scenario package must not appear in observation package imports.
	assertPackageDoesNotImport(t, "internal/pulse/observation", "internal/pulse/scenario")
	assertPackageDoesNotImport(t, "internal/pulse/store", "internal/pulse/scenario")
	// Scenario may use astronomy ids but must not write into observation types
	// as ground truth — Record JSON must not look like an Observation.
	c, err := scenario.LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	def, _ := c.Get("latency-shipping-intl")
	rec, err := scenario.NewRecord(def, c.Environment, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	var asMap map[string]any
	if err := json.Unmarshal(b, &asMap); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"status", "observed_at", "ingested_at", "evidence", "freshness"} {
		if _, ok := asMap[forbidden]; ok {
			t.Fatalf("ground-truth record must not carry observation field %q", forbidden)
		}
	}
	if _, ok := asMap["scenario_id"]; !ok {
		t.Fatal("missing scenario_id")
	}
	if _, ok := asMap["classification"]; !ok {
		t.Fatal("missing classification")
	}
}

func TestStartStopLifecycleAndDuplicates(t *testing.T) {
	srv, flagState := newFlagdTestServer(t, map[string]string{
		"paymentFailure": "off",
	}, map[string][]string{
		"paymentFailure": {"off", "50%", "100%"},
	})
	defer srv.Close()

	c, err := scenario.LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	c.FlagdAPIBase = srv.URL

	dir := t.TempDir()
	start := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	clock := &stepClock{t: start}
	h, err := scenario.NewHarness(c, scenario.NewFlagdClient(srv.URL), dir, clock.Now)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	rec, err := h.Start(ctx, "error-payment")
	if err != nil {
		t.Fatal(err)
	}
	if rec.State != scenario.StateActive || !rec.ActivationVerified {
		t.Fatalf("active: %+v", rec)
	}
	if flagState["paymentFailure"] != "50%" {
		t.Fatalf("flag state: %v", flagState)
	}
	if _, err := h.Start(ctx, "error-payment"); !errors.Is(err, scenario.ErrAlreadyActive) {
		t.Fatalf("duplicate start: %v", err)
	}
	if _, err := h.Start(ctx, "outage-payment"); !errors.Is(err, scenario.ErrAlreadyActive) {
		t.Fatalf("duplicate start other: %v", err)
	}

	clock.Advance(2 * time.Minute)
	stopped, err := h.Stop(ctx, "error-payment")
	if err != nil {
		t.Fatal(err)
	}
	if stopped.State != scenario.StateStopped || stopped.EndTime == nil {
		t.Fatalf("stopped: %+v", stopped)
	}
	if !stopped.RecoveryVerified || stopped.Recovered == nil || !*stopped.Recovered {
		t.Fatalf("recovery: %+v", stopped)
	}
	if flagState["paymentFailure"] != "off" {
		t.Fatalf("flag not reverted: %v", flagState)
	}
	if _, err := h.Stop(ctx, "error-payment"); !errors.Is(err, scenario.ErrNotActive) {
		t.Fatalf("duplicate stop: %v", err)
	}

	// Persisted run file exists and validates.
	path := filepath.Join(dir, rec.RunID+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved scenario.Record
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if err := saved.Validate(); err != nil {
		t.Fatal(err)
	}
	if saved.State != scenario.StateStopped {
		t.Fatalf("saved state %s", saved.State)
	}
}

func TestStopWithoutStart(t *testing.T) {
	srv, _ := newFlagdTestServer(t, map[string]string{"paymentFailure": "off"}, map[string][]string{
		"paymentFailure": {"off", "50%"},
	})
	defer srv.Close()
	c, _ := scenario.LoadCatalog()
	h, err := scenario.NewHarness(c, scenario.NewFlagdClient(srv.URL), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Stop(context.Background(), ""); !errors.Is(err, scenario.ErrNotActive) {
		t.Fatalf("got %v", err)
	}
}

func TestExclusiveStartLock(t *testing.T) {
	srv, _ := newFlagdTestServer(t, map[string]string{
		"paymentFailure":     "off",
		"paymentUnreachable": "off",
	}, map[string][]string{
		"paymentFailure":     {"off", "50%"},
		"paymentUnreachable": {"off", "on"},
	})
	defer srv.Close()
	c, _ := scenario.LoadCatalog()
	dir := t.TempDir()
	client := scenario.NewFlagdClient(srv.URL)
	h1, err := scenario.NewHarness(c, client, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := scenario.NewHarness(c, client, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h1.Start(context.Background(), "error-payment"); err != nil {
		t.Fatal(err)
	}
	if _, err := h2.Start(context.Background(), "outage-payment"); !errors.Is(err, scenario.ErrAlreadyActive) {
		t.Fatalf("expected ErrAlreadyActive, got %v", err)
	}
}

func TestFailedStopIsRetryable(t *testing.T) {
	srv, state := newFlagdTestServer(t, map[string]string{"paymentFailure": "off"}, map[string][]string{
		"paymentFailure": {"off", "50%"},
	})
	defer srv.Close()
	c, _ := scenario.LoadCatalog()
	client := scenario.NewFlagdClient(srv.URL)
	client.PollWait = 200 * time.Millisecond
	h, err := scenario.NewHarness(c, client, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := h.Start(ctx, "error-payment"); err != nil {
		t.Fatal(err)
	}

	// Force stop write path to leave a failed active run by breaking the flag
	// name temporarily via state deletion... instead: mark active as failed
	// after a simulated recovery miss by setting flag to a stuck value the
	// idle wait won't accept, then restore.
	state["paymentFailure"] = "50%" // still active
	// Make WaitForVariant time out by removing idle from allowed reads? Server
	// always returns state value. Point write at a handler that fails once.
	failWrites.Store(true)
	defer failWrites.Store(false)
	if _, err := h.Stop(ctx, "error-payment"); err == nil {
		t.Fatal("expected stop failure")
	}
	active, err := h.LoadActive()
	if err != nil || active == nil || active.State != scenario.StateFailed {
		t.Fatalf("expected failed active run, got %+v err=%v", active, err)
	}
	// Retry after writes work again.
	failWrites.Store(false)
	state["paymentFailure"] = "50%"
	stopped, err := h.Stop(ctx, "error-payment")
	if err != nil {
		t.Fatal(err)
	}
	if stopped.State != scenario.StateStopped || !stopped.RecoveryVerified {
		t.Fatalf("retry stop: %+v", stopped)
	}
	if state["paymentFailure"] != "off" {
		t.Fatalf("flag=%s", state["paymentFailure"])
	}
}

func TestAsyncFlagdWriteStillActivates(t *testing.T) {
	srv, state := newFlagdTestServer(t, map[string]string{"paymentFailure": "off"}, map[string][]string{
		"paymentFailure": {"off", "50%"},
	})
	defer srv.Close()
	writeDelay.Store(int64(150 * time.Millisecond))
	defer writeDelay.Store(0)

	c, _ := scenario.LoadCatalog()
	client := scenario.NewFlagdClient(srv.URL)
	client.PollWait = 2 * time.Second
	h, err := scenario.NewHarness(c, client, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := h.Start(context.Background(), "error-payment")
	if err != nil {
		t.Fatal(err)
	}
	if !rec.ActivationVerified || state["paymentFailure"] != "50%" {
		t.Fatalf("rec=%+v state=%v", rec, state)
	}
	if _, err := h.Stop(context.Background(), "error-payment"); err != nil {
		t.Fatal(err)
	}
}

func TestAstronomyValidateServiceIDShape(t *testing.T) {
	if err := astronomy.ValidateServiceIDShape(astronomy.MustLocalServiceID("checkout")); err != nil {
		t.Fatal(err)
	}
	if err := astronomy.ValidateServiceIDShape("production/api"); err == nil {
		t.Fatal("expected reject")
	}
}

type stepClock struct {
	t time.Time
}

func (c *stepClock) Now() time.Time { return c.t.UTC() }
func (c *stepClock) Advance(d time.Duration) {
	c.t = c.t.Add(d)
}

func newFlagdTestServer(t *testing.T, variants map[string]string, allowed map[string][]string) (*httptest.Server, map[string]string) {
	t.Helper()
	state := map[string]string{}
	for k, v := range variants {
		state[k] = v
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/read", func(w http.ResponseWriter, r *http.Request) {
		flags := map[string]any{}
		for name, def := range state {
			vars := map[string]any{}
			for _, v := range allowed[name] {
				vars[v] = true
			}
			flags[name] = map[string]any{
				"defaultVariant": def,
				"state":          "ENABLED",
				"variants":       vars,
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"flags": flags})
	})
	mux.HandleFunc("/write", func(w http.ResponseWriter, r *http.Request) {
		if failWrites.Load() {
			http.Error(w, "injected write failure", http.StatusBadGateway)
			return
		}
		var body struct {
			Data struct {
				Flags map[string]struct {
					DefaultVariant string `json:"defaultVariant"`
				} `json:"flags"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		apply := func() {
			for name, entry := range body.Data.Flags {
				state[name] = entry.DefaultVariant
			}
		}
		if d := time.Duration(writeDelay.Load()); d > 0 {
			// Mimic flagd-ui GenServer.cast: acknowledge before state changes.
			go func() {
				time.Sleep(d)
				apply()
			}()
		} else {
			apply()
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	})
	return httptest.NewServer(mux), state
}

func assertPackageDoesNotImport(t *testing.T, pkgPath, forbidden string) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	dir := filepath.Join(root, pkgPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(path, forbidden) {
				t.Fatalf("%s imports forbidden %s via %s", pkgPath, forbidden, path)
			}
		}
	}
}
