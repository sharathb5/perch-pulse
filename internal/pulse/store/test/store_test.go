package store_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/pulse/observation"
	"github.com/yashg4509/perch/internal/pulse/store"
)

func mustObs(t *testing.T, serviceID string, status observation.Status, observedAt, ingestedAt time.Time, source string) observation.Observation {
	t.Helper()
	obs := observation.Observation{
		ServiceID:  serviceID,
		Status:     status,
		ObservedAt: observedAt,
		IngestedAt: ingestedAt,
		Source:     source,
	}
	if err := obs.Validate(); err != nil {
		t.Fatalf("fixture Validate: %v", err)
	}
	return obs
}

func TestMemory_AppendValidAndListByService(t *testing.T) {
	s := store.NewMemory()
	t0 := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)

	a := mustObs(t, "svc-a", observation.StatusHealthy, t0, t0, "stackstatus")
	b := mustObs(t, "svc-a", observation.StatusDegraded, t1, t1, "stackstatus")
	if err := s.Append(a); err != nil {
		t.Fatalf("Append a: %v", err)
	}
	if err := s.Append(b); err != nil {
		t.Fatalf("Append b: %v", err)
	}

	got, err := s.List("svc-a")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List len = %d, want 2", len(got))
	}
	if observation.Compare(got[0], a) != 0 || got[0].Status != a.Status {
		t.Fatalf("List[0] = %+v, want %+v", got[0], a)
	}
	if observation.Compare(got[1], b) != 0 || got[1].Status != b.Status {
		t.Fatalf("List[1] = %+v, want %+v", got[1], b)
	}
}

func TestMemory_RejectInvalid(t *testing.T) {
	s := store.NewMemory()
	invalid := observation.Observation{
		ServiceID:  "svc",
		ObservedAt: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC),
		Status:     observation.StatusStale, // producer-forbidden
		Source:     "stackstatus",
	}
	if err := s.Append(invalid); err == nil {
		t.Fatal("expected Append to reject StatusStale")
	}
	got, err := s.List("svc")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("invalid observation entered store: %+v", got)
	}

	missing := observation.Observation{Status: observation.StatusHealthy, Source: "x"}
	if err := s.Append(missing); err == nil {
		t.Fatal("expected Append to reject incomplete observation")
	}
}

func TestMemory_LatestDeterministic(t *testing.T) {
	s := store.NewMemory()
	t0 := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	older := mustObs(t, "api", observation.StatusHealthy, t0, t0, "stackstatus")
	newer := mustObs(t, "api", observation.StatusUnhealthy, t0.Add(5*time.Minute), t0.Add(5*time.Minute), "stackstatus")

	if err := s.Append(newer); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(older); err != nil {
		t.Fatal(err)
	}

	got, ok, err := s.Latest("api")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if !ok {
		t.Fatal("Latest ok=false")
	}
	if got.Status != observation.StatusUnhealthy {
		t.Fatalf("Latest status = %s, want unhealthy", got.Status)
	}
	if !got.ObservedAt.Equal(newer.ObservedAt) {
		t.Fatalf("Latest ObservedAt = %v, want %v", got.ObservedAt, newer.ObservedAt)
	}
}

func TestMemory_OutOfOrderInsertion(t *testing.T) {
	s := store.NewMemory()
	base := time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)
	late := mustObs(t, "db", observation.StatusHealthy, base.Add(10*time.Minute), base.Add(10*time.Minute), "stackstatus")
	early := mustObs(t, "db", observation.StatusDegraded, base, base, "stackstatus")
	mid := mustObs(t, "db", observation.StatusUnavailable, base.Add(5*time.Minute), base.Add(5*time.Minute), "stackstatus")

	// Insert newest first, then oldest, then middle.
	for _, obs := range []observation.Observation{late, early, mid} {
		if err := s.Append(obs); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	list, err := s.List("db")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("List len = %d, want 3", len(list))
	}
	wantOrder := []observation.Status{
		observation.StatusDegraded,
		observation.StatusUnavailable,
		observation.StatusHealthy,
	}
	for i, st := range wantOrder {
		if list[i].Status != st {
			t.Fatalf("List[%d].Status = %s, want %s (deterministic Compare order)", i, list[i].Status, st)
		}
	}

	latest, ok, err := s.Latest("db")
	if err != nil || !ok {
		t.Fatalf("Latest: ok=%v err=%v", ok, err)
	}
	if latest.Status != observation.StatusHealthy {
		t.Fatalf("out-of-order Latest = %s, want healthy (newest ObservedAt)", latest.Status)
	}
}

func TestMemory_DuplicateCompareEqualIsIdempotentFirstSeen(t *testing.T) {
	s := store.NewMemory()
	t0 := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	obs := mustObs(t, "cache", observation.StatusHealthy, t0, t0, "stackstatus")
	obs.Evidence = &observation.Evidence{Ref: "probe", Summary: "ok"}

	if err := s.Append(obs); err != nil {
		t.Fatal(err)
	}
	dup := obs // same Compare keys
	if err := s.Append(dup); err != nil {
		t.Fatalf("duplicate Append error: %v", err)
	}

	list, err := s.List("cache")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("duplicate grew history to %d; want idempotent size 1", len(list))
	}
}

func TestMemory_DuplicateNilAndEmptyEvidenceKeepsFirstSeen(t *testing.T) {
	// observation.Compare treats nil Evidence and &Evidence{} as equal keys.
	s := store.NewMemory()
	t0 := time.Date(2026, 10, 7, 9, 30, 0, 0, time.UTC)
	first := mustObs(t, "cache2", observation.StatusHealthy, t0, t0, "stackstatus")
	// first.Evidence is nil
	if err := s.Append(first); err != nil {
		t.Fatal(err)
	}
	second := mustObs(t, "cache2", observation.StatusHealthy, t0, t0, "stackstatus")
	second.Evidence = &observation.Evidence{} // Compare-equal to nil
	if observation.Compare(first, second) != 0 {
		t.Fatal("precondition: nil and empty Evidence must Compare equal")
	}
	if err := s.Append(second); err != nil {
		t.Fatal(err)
	}
	list, err := s.List("cache2")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("Compare-equal duplicate grew history to %d", len(list))
	}
	if list[0].Evidence != nil {
		t.Fatalf("first-seen nil Evidence was replaced: %+v", list[0].Evidence)
	}
}

func TestMemory_EqualTimeConflictKeepsWorse(t *testing.T) {
	s := store.NewMemory()
	t0 := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	healthy := mustObs(t, "web", observation.StatusHealthy, t0, t0, "stackstatus")
	degraded := mustObs(t, "web", observation.StatusDegraded, t0, t0, "stackstatus")

	if err := s.Append(healthy); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(degraded); err != nil {
		t.Fatal(err)
	}

	latest, ok, err := s.Latest("web")
	if err != nil || !ok {
		t.Fatalf("Latest: ok=%v err=%v", ok, err)
	}
	if latest.Status != observation.StatusDegraded {
		t.Fatalf("equal-time Latest = %s, want degraded (severity via observation.Compare)", latest.Status)
	}
}

func TestMemory_ServiceIsolation(t *testing.T) {
	s := store.NewMemory()
	t0 := time.Date(2026, 10, 7, 11, 0, 0, 0, time.UTC)
	if err := s.Append(mustObs(t, "alpha", observation.StatusHealthy, t0, t0, "stackstatus")); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(mustObs(t, "beta", observation.StatusUnhealthy, t0, t0, "stackstatus")); err != nil {
		t.Fatal(err)
	}

	alpha, err := s.List("alpha")
	if err != nil {
		t.Fatal(err)
	}
	beta, err := s.List("beta")
	if err != nil {
		t.Fatal(err)
	}
	if len(alpha) != 1 || alpha[0].Status != observation.StatusHealthy {
		t.Fatalf("alpha isolated incorrectly: %+v", alpha)
	}
	if len(beta) != 1 || beta[0].Status != observation.StatusUnhealthy {
		t.Fatalf("beta isolated incorrectly: %+v", beta)
	}

	other, err := s.List("gamma")
	if err != nil {
		t.Fatal(err)
	}
	if other == nil || len(other) != 0 {
		t.Fatalf("unknown service List = %#v, want empty non-nil", other)
	}

	_, ok, err := s.Latest("gamma")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("Latest on empty service should be ok=false")
	}
}

func TestMemory_PreservesTimestampsAndDoesNotRewriteHealthy(t *testing.T) {
	s := store.NewMemory()
	observed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) // old
	ingested := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	obs := mustObs(t, "legacy", observation.StatusHealthy, observed, ingested, "stackstatus")
	if err := s.Append(obs); err != nil {
		t.Fatal(err)
	}

	got, ok, err := s.Latest("legacy")
	if err != nil || !ok {
		t.Fatalf("Latest: ok=%v err=%v", ok, err)
	}
	if !got.ObservedAt.Equal(observed) || !got.IngestedAt.Equal(ingested) {
		t.Fatalf("timestamps mutated: ObservedAt=%v IngestedAt=%v", got.ObservedAt, got.IngestedAt)
	}
	if got.Status != observation.StatusHealthy {
		t.Fatalf("store mutated Status to %s", got.Status)
	}
	// Freshness overlay is caller-side; store must not have rewritten to stale/healthy.
	now := ingested
	if got.EffectiveStatus(now, time.Hour) != observation.StatusStale {
		t.Fatal("expected EffectiveStatus stale for old healthy observation; store must preserve underlying Status")
	}
	if got.Status == observation.StatusStale {
		t.Fatal("store must not persist derived StatusStale")
	}
}

func TestMemory_CloneIsolation(t *testing.T) {
	s := store.NewMemory()
	t0 := time.Date(2026, 10, 7, 13, 0, 0, 0, time.UTC)
	obs := mustObs(t, "clone", observation.StatusHealthy, t0, t0, "stackstatus")
	obs.Evidence = &observation.Evidence{Ref: "r1", Summary: "s1"}
	if err := s.Append(obs); err != nil {
		t.Fatal(err)
	}
	obs.Evidence.Summary = "mutated-caller"
	obs.Status = observation.StatusUnhealthy

	got, err := s.List("clone")
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Evidence.Summary != "s1" {
		t.Fatalf("caller mutation leaked into store: %q", got[0].Evidence.Summary)
	}
	if got[0].Status != observation.StatusHealthy {
		t.Fatalf("caller Status mutation leaked: %s", got[0].Status)
	}

	got[0].Evidence.Summary = "mutated-list"
	again, err := s.List("clone")
	if err != nil {
		t.Fatal(err)
	}
	if again[0].Evidence.Summary != "s1" {
		t.Fatalf("List mutation leaked into store: %q", again[0].Evidence.Summary)
	}
}

func TestMemory_EmptyServiceID(t *testing.T) {
	s := store.NewMemory()
	if _, err := s.List("  "); err == nil {
		t.Fatal("List empty service_id should error")
	}
	if _, _, err := s.Latest(""); err == nil {
		t.Fatal("Latest empty service_id should error")
	}
}

func TestMemory_NoSecretFieldNamesOnContract(t *testing.T) {
	// Store persists Observation only; contract must not grow secret-shaped fields.
	if err := observation.ValidateReportsSecretFieldNames(); err != nil {
		t.Fatal(err)
	}
}

func TestMemory_ListRequiresTrimmedServiceID(t *testing.T) {
	// Ensure error messages stay free of credential-like content (shape check).
	s := store.NewMemory()
	_, err := s.List("")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(strings.ToLower(err.Error()), "token") ||
		strings.Contains(strings.ToLower(err.Error()), "password") {
		t.Fatalf("unexpected secret-like error text: %v", err)
	}
}

func TestMemory_ConcurrentAppendAndRead(t *testing.T) {
	s := store.NewMemory()
	base := time.Date(2026, 10, 7, 14, 0, 0, 0, time.UTC)
	fixtures := make([]observation.Observation, 32)
	for i := range fixtures {
		fixtures[i] = mustObs(t, "race", observation.StatusHealthy, base.Add(time.Duration(i)*time.Second), base, "stackstatus")
	}
	var wg sync.WaitGroup
	for i := range fixtures {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.Append(fixtures[i]); err != nil {
				t.Errorf("Append %d: %v", i, err)
			}
			if _, err := s.List("race"); err != nil {
				t.Errorf("List %d: %v", i, err)
			}
			if _, _, err := s.Latest("race"); err != nil {
				t.Errorf("Latest %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	list, err := s.List("race")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 32 {
		t.Fatalf("after concurrent appends List len = %d, want 32", len(list))
	}
	latest, ok, err := s.Latest("race")
	if err != nil || !ok {
		t.Fatalf("Latest: ok=%v err=%v", ok, err)
	}
	if !latest.ObservedAt.Equal(base.Add(31 * time.Second)) {
		t.Fatalf("Latest ObservedAt = %v, want newest", latest.ObservedAt)
	}
}
