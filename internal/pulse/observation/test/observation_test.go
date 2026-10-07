package observation_test

import (
	"strings"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/pulse/observation"
)

func TestStatus_zeroValueIsNotHealthy(t *testing.T) {
	var zero observation.Status
	if zero.IsHealthy() {
		t.Fatal("zero Status must not be healthy")
	}
	if zero != observation.StatusUnknown {
		t.Fatalf("zero Status = %q, want StatusUnknown", zero)
	}
	if zero.String() != "unknown" {
		t.Fatalf("String() = %q, want unknown", zero.String())
	}
}

func TestStatus_unavailableIsNotHealthy(t *testing.T) {
	if observation.StatusUnavailable.IsHealthy() {
		t.Fatal("unavailable must not be healthy")
	}
	if observation.StatusUnknown.IsHealthy() {
		t.Fatal("unknown must not be healthy")
	}
	if !observation.StatusHealthy.IsHealthy() {
		t.Fatal("healthy must be healthy")
	}
}

func TestEffectiveStatus_healthyButOldIsStale(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	obs := observation.Observation{
		ServiceID:  "api",
		ObservedAt: now.Add(-10 * time.Minute),
		Status:     observation.StatusHealthy,
		Source:     "stackstatus",
	}
	if !obs.IsStale(now, 5*time.Minute) {
		t.Fatal("healthy-but-old observation must be stale")
	}
	got := obs.EffectiveStatus(now, 5*time.Minute)
	if got != observation.StatusStale {
		t.Fatalf("EffectiveStatus = %s, want stale", got)
	}
	if got.IsHealthy() {
		t.Fatal("stale effective status must not be healthy")
	}
}

func TestEffectiveStatus_freshHealthyRemainsHealthy(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	obs := observation.Observation{
		ServiceID:  "api",
		ObservedAt: now.Add(-1 * time.Minute),
		Status:     observation.StatusHealthy,
		Source:     "stackstatus",
	}
	if obs.IsStale(now, 5*time.Minute) {
		t.Fatal("fresh observation must not be stale")
	}
	if got := obs.EffectiveStatus(now, 5*time.Minute); got != observation.StatusHealthy {
		t.Fatalf("EffectiveStatus = %s, want healthy", got)
	}
}

func TestEffectiveStatus_unavailableStaysUnavailableWhenFresh(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	obs := observation.Observation{
		ServiceID:  "db",
		ObservedAt: now.Add(-30 * time.Second),
		Status:     observation.StatusUnavailable,
		Source:     "stackstatus",
	}
	got := obs.EffectiveStatus(now, 5*time.Minute)
	if got != observation.StatusUnavailable {
		t.Fatalf("EffectiveStatus = %s, want unavailable", got)
	}
	if got.IsHealthy() {
		t.Fatal("unavailable must not become healthy")
	}
}

func TestEffectiveStatus_unknownStaysUnknownWhenFresh(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	obs := observation.Observation{
		ServiceID:  "worker",
		ObservedAt: now,
		Status:     observation.StatusUnknown,
		Source:     "fixture",
	}
	got := obs.EffectiveStatus(now, time.Minute)
	if got != observation.StatusUnknown {
		t.Fatalf("EffectiveStatus = %s, want unknown", got)
	}
	if got.IsHealthy() {
		t.Fatal("unknown must not become healthy")
	}
}

func TestIsStale_boundaryAtThresholdIsStale(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	freshness := 5 * time.Minute
	atBoundary := observation.Observation{
		ServiceID:  "api",
		ObservedAt: now.Add(-freshness),
		Status:     observation.StatusHealthy,
		Source:     "stackstatus",
	}
	if !atBoundary.IsStale(now, freshness) {
		t.Fatal("age == freshness must be stale (fail closed at boundary)")
	}
	justInside := atBoundary
	justInside.ObservedAt = now.Add(-freshness + time.Nanosecond)
	if justInside.IsStale(now, freshness) {
		t.Fatal("age < freshness must be fresh")
	}
}

func TestIsStale_nonPositiveThresholdIsAlwaysStale(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	obs := observation.Observation{
		ServiceID:  "api",
		ObservedAt: now,
		Status:     observation.StatusHealthy,
		Source:     "stackstatus",
	}
	if !obs.IsStale(now, 0) {
		t.Fatal("zero freshness threshold must fail closed as stale")
	}
	if !obs.IsStale(now, -time.Second) {
		t.Fatal("negative freshness threshold must fail closed as stale")
	}
}

func TestIsStale_zeroObservedAtIsStale(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	obs := observation.Observation{
		ServiceID: "api",
		Status:    observation.StatusHealthy,
		Source:    "stackstatus",
	}
	if !obs.IsStale(now, time.Hour) {
		t.Fatal("missing ObservedAt cannot prove freshness")
	}
}

func TestIsStale_futureObservedAtIsStale(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	obs := observation.Observation{
		ServiceID:  "api",
		ObservedAt: now.Add(time.Minute),
		Status:     observation.StatusHealthy,
		Source:     "stackstatus",
	}
	if !obs.IsStale(now, 5*time.Minute) {
		t.Fatal("future ObservedAt must fail closed as stale")
	}
	if got := obs.EffectiveStatus(now, 5*time.Minute); got != observation.StatusStale {
		t.Fatalf("EffectiveStatus = %s, want stale", got)
	}
}

func TestLatest_prefersNewerObservedAtRegardlessOfArrivalOrder(t *testing.T) {
	t0 := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	older := observation.Observation{
		ServiceID:  "api",
		ObservedAt: t0,
		IngestedAt: t0.Add(2 * time.Second),
		Status:     observation.StatusHealthy,
		Source:     "stackstatus",
	}
	newer := observation.Observation{
		ServiceID:  "api",
		ObservedAt: t0.Add(time.Minute),
		IngestedAt: t0, // ingested earlier than older, but observed later
		Status:     observation.StatusDegraded,
		Source:     "stackstatus",
	}

	// Out-of-order arrival: older appended after newer.
	got, ok := observation.Latest([]observation.Observation{newer, older})
	if !ok {
		t.Fatal("expected ok")
	}
	if got.Status != observation.StatusDegraded || !got.ObservedAt.Equal(newer.ObservedAt) {
		t.Fatalf("Latest = %+v, want newer degraded observation by ObservedAt", got)
	}

	// Reverse arrival order must yield the same winner.
	got2, ok := observation.Latest([]observation.Observation{older, newer})
	if !ok {
		t.Fatal("expected ok")
	}
	if observation.Compare(got, got2) != 0 || got2.Status != got.Status {
		t.Fatalf("Latest must be order-independent; got %+v vs %+v", got, got2)
	}
}

func TestLatest_ingestTieBreakWhenObservedAtEqual(t *testing.T) {
	t0 := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	firstIngest := observation.Observation{
		ServiceID:  "api",
		ObservedAt: t0,
		IngestedAt: t0,
		Status:     observation.StatusHealthy,
		Source:     "stackstatus",
	}
	secondIngest := observation.Observation{
		ServiceID:  "api",
		ObservedAt: t0,
		IngestedAt: t0.Add(time.Second),
		Status:     observation.StatusUnhealthy,
		Source:     "stackstatus",
	}

	// Duplicate ObservedAt; later IngestedAt wins even if it arrives first in the slice.
	got, ok := observation.Latest([]observation.Observation{secondIngest, firstIngest})
	if !ok {
		t.Fatal("expected ok")
	}
	if got.Status != observation.StatusUnhealthy {
		t.Fatalf("got status %s, want unhealthy from later ingest", got.Status)
	}
}

func TestLatest_equalTimeConflictPrefersWorseStatus(t *testing.T) {
	t0 := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	healthy := observation.Observation{
		ServiceID:  "api",
		ObservedAt: t0,
		IngestedAt: t0,
		Status:     observation.StatusHealthy,
		Source:     "stackstatus",
	}
	degraded := healthy
	degraded.Status = observation.StatusDegraded

	// Lexical "healthy" > "degraded"; severity must prefer degraded (fail closed).
	got, ok := observation.Latest([]observation.Observation{healthy, degraded})
	if !ok {
		t.Fatal("expected ok")
	}
	if got.Status != observation.StatusDegraded {
		t.Fatalf("got %s, want degraded over healthy on equal-time conflict", got.Status)
	}
	got2, ok := observation.Latest([]observation.Observation{degraded, healthy})
	if !ok || got2.Status != observation.StatusDegraded {
		t.Fatalf("order-independent conflict resolution failed: %+v", got2)
	}
}

func TestLatest_empty(t *testing.T) {
	if _, ok := observation.Latest(nil); ok {
		t.Fatal("empty slice must return ok=false")
	}
}

func TestValidate_rejectsMissingFieldsAndInvalidStatus(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	valid := observation.Observation{
		ServiceID:  "api",
		ObservedAt: now,
		Status:     observation.StatusHealthy,
		Source:     "stackstatus",
		Evidence:   &observation.Evidence{Ref: "probe/api", Summary: "http 200"},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid observation: %v", err)
	}

	badStatus := valid
	badStatus.Status = observation.Status("not-a-status")
	if err := badStatus.Validate(); err == nil {
		t.Fatal("expected invalid status error")
	}

	staleStatus := valid
	staleStatus.Status = observation.StatusStale
	if err := staleStatus.Validate(); err == nil {
		t.Fatal("expected derived-only stale rejection")
	}
	if err := staleStatus.Validate(); err != nil && !strings.Contains(err.Error(), "derived-only") {
		t.Fatalf("unexpected error: %v", err)
	}

	noService := valid
	noService.ServiceID = " "
	if err := noService.Validate(); err == nil {
		t.Fatal("expected missing service_id error")
	}
}

func TestObservation_hasNoSecretFields(t *testing.T) {
	// Contract guard: Observation/Evidence field names must not look like secret
	// holders. Credentials belong in internal/credentials. String-content
	// redaction remains a producer-boundary responsibility.
	if err := observation.ValidateReportsSecretFieldNames(); err != nil {
		t.Fatal(err)
	}
}
