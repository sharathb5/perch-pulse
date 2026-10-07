// Package observation defines the typed Pulse observation contract.
//
// An Observation is a service signal at an explicit point in time. Freshness is
// derived from ObservedAt and a caller-supplied threshold; stale telemetry must
// never be treated as current healthy status. This package is optional relative
// to live Perch collectors and does not perform I/O.
package observation

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Status is a constrained observation state.
//
// The zero value is unknown and must never be treated as healthy.
// StatusStale is reserved for derived freshness overlays (see EffectiveStatus);
// producers must not store StatusStale (Validate rejects it).
type Status string

const (
	// StatusUnknown means the signal state was not established.
	StatusUnknown Status = ""

	// StatusHealthy means the service appeared healthy at ObservedAt.
	StatusHealthy Status = "healthy"

	// StatusDegraded means the service was partially impaired at ObservedAt.
	StatusDegraded Status = "degraded"

	// StatusUnhealthy means the service appeared unhealthy at ObservedAt.
	StatusUnhealthy Status = "unhealthy"

	// StatusUnavailable means no usable signal could be obtained.
	StatusUnavailable Status = "unavailable"

	// StatusStale means the observation is past the freshness threshold.
	// Derived via EffectiveStatus; not a valid producer-reported Status.
	StatusStale Status = "stale"
)

// Valid reports whether s is a recognized Status constant (including StatusStale).
func (s Status) Valid() bool {
	switch s {
	case StatusUnknown, StatusHealthy, StatusDegraded, StatusUnhealthy, StatusUnavailable, StatusStale:
		return true
	default:
		return false
	}
}

// IsProducerStatus reports whether s may be stored on Observation by a producer.
// StatusStale is derived-only and returns false.
func (s Status) IsProducerStatus() bool {
	switch s {
	case StatusUnknown, StatusHealthy, StatusDegraded, StatusUnhealthy, StatusUnavailable:
		return true
	default:
		return false
	}
}

// IsHealthy reports whether s is exactly StatusHealthy.
// Unknown, unavailable, stale, and all other values are not healthy.
func (s Status) IsHealthy() bool {
	return s == StatusHealthy
}

// String returns a stable label for logging and tests. Unknown renders as "unknown".
func (s Status) String() string {
	if s == StatusUnknown {
		return "unknown"
	}
	return string(s)
}

// severityRank orders statuses for fail-closed conflict resolution.
// Higher means worse / more uncertain; used only when timestamps tie in Compare.
func severityRank(s Status) int {
	switch s {
	case StatusUnhealthy:
		return 6
	case StatusUnavailable:
		return 5
	case StatusUnknown:
		return 4
	case StatusDegraded:
		return 3
	case StatusStale:
		return 2
	case StatusHealthy:
		return 1
	default:
		return 4 // unrecognized treated like unknown
	}
}

// Evidence holds optional non-secret references supporting an observation.
// Callers must never place credentials, tokens, or secret values here.
// This package does not scan string contents; producers redact before ingest.
type Evidence struct {
	// Ref is a non-secret reference (probe name, run id, doc path).
	Ref string `json:"ref,omitempty"`

	// Summary is a short non-secret note. Must not contain credentials.
	Summary string `json:"summary,omitempty"`
}

// Observation is a typed service signal at a specific point in time.
type Observation struct {
	// ServiceID identifies the service or graph node this signal applies to.
	ServiceID string `json:"service_id"`

	// ObservedAt is when the underlying signal was produced. Freshness is
	// evaluated against this timestamp, never against slice order.
	ObservedAt time.Time `json:"observed_at"`

	// IngestedAt is when Pulse accepted the observation. Zero means unknown.
	// Used only as a deterministic tie-breaker for Latest/Compare; not for freshness.
	IngestedAt time.Time `json:"ingested_at,omitempty"`

	// Status is the reported state at ObservedAt. Zero value is unknown and
	// must not be treated as healthy. Must be a producer status (not StatusStale).
	Status Status `json:"status"`

	// Source identifies the collector or system that produced the signal
	// (for example "stackstatus"). Must not contain credentials.
	Source string `json:"source"`

	// Evidence is optional non-secret supporting metadata.
	Evidence *Evidence `json:"evidence,omitempty"`
}

// forbiddenFieldSubstrings are disallowed in exported field names on the
// observation contract (struct shape guard; not a content scanner).
var forbiddenFieldSubstrings = []string{
	"secret", "token", "password", "credential", "apikey", "api_key", "auth",
}

// ValidateReportsSecretFieldNames reports whether Observation or Evidence expose
// field names that look like secret holders. Used by tests as a contract guard.
func ValidateReportsSecretFieldNames() error {
	for _, t := range []reflect.Type{reflect.TypeOf(Observation{}), reflect.TypeOf(Evidence{})} {
		for i := 0; i < t.NumField(); i++ {
			name := strings.ToLower(t.Field(i).Name)
			for _, bad := range forbiddenFieldSubstrings {
				if strings.Contains(name, bad) {
					return fmt.Errorf("observation: type %s field %s looks like a secret holder", t.Name(), t.Field(i).Name)
				}
			}
		}
	}
	return nil
}

// Validate reports contract violations. It does not evaluate freshness or scan
// string values for embedded secrets (producer responsibility).
func (o Observation) Validate() error {
	if strings.TrimSpace(o.ServiceID) == "" {
		return fmt.Errorf("observation: service_id is required")
	}
	if o.ObservedAt.IsZero() {
		return fmt.Errorf("observation: observed_at is required")
	}
	if !o.Status.IsProducerStatus() {
		if o.Status == StatusStale {
			return fmt.Errorf("observation: status stale is derived-only; producers must report the underlying state")
		}
		return fmt.Errorf("observation: invalid status %q", o.Status)
	}
	if strings.TrimSpace(o.Source) == "" {
		return fmt.Errorf("observation: source is required")
	}
	return nil
}

// Age returns how old the observation is relative to now.
// If ObservedAt is zero, Age returns a negative duration to signal undefined age.
// Future ObservedAt values yield a negative age.
func (o Observation) Age(now time.Time) time.Duration {
	if o.ObservedAt.IsZero() {
		return -1
	}
	return now.Sub(o.ObservedAt)
}

// IsStale reports whether the observation is outside the freshness window.
//
// Rules:
//   - A non-positive freshness threshold is treated as always stale (fail closed).
//   - A zero ObservedAt cannot prove freshness and is stale.
//   - ObservedAt after now (clock skew / future-dated) is stale (fail closed).
//   - An observation is stale when age >= freshness (threshold boundary is stale).
//
// IsStale does not inspect Status; a healthy-but-old observation is still stale.
func (o Observation) IsStale(now time.Time, freshness time.Duration) bool {
	if freshness <= 0 {
		return true
	}
	if o.ObservedAt.IsZero() {
		return true
	}
	if o.ObservedAt.After(now) {
		return true
	}
	return !o.ObservedAt.After(now.Add(-freshness))
}

// EffectiveStatus returns the status callers should act on after freshness.
//
// If the observation is stale, the result is StatusStale even when the stored
// Status is StatusHealthy. Unknown and unavailable statuses are preserved when
// the observation is still fresh so missing data never becomes healthy.
func (o Observation) EffectiveStatus(now time.Time, freshness time.Duration) Status {
	if o.IsStale(now, freshness) {
		return StatusStale
	}
	if !o.Status.IsProducerStatus() {
		return StatusUnknown
	}
	return o.Status
}

// Compare orders observations from older/less-preferred to newer/more-preferred
// for "latest" selection.
//
// Ordering keys (all explicit; never insertion order):
//  1. ObservedAt ascending (later observation time is preferred)
//  2. IngestedAt ascending (later ingest wins ties)
//  3. ServiceID, Source lexicographically
//  4. Status by severityRank ascending (worse/more-uncertain wins time ties so
//     conflicting equal-time signals never resolve to healthy over degraded/etc.)
//  5. Evidence.Ref, then Evidence.Summary lexicographically (nil evidence sorts first)
//
// Equal keys return 0. Duplicate or out-of-order arrivals therefore have a
// single content-determined winner when passed to Latest (slice order ignored
// except for byte-identical compare keys).
func Compare(a, b Observation) int {
	if c := compareTime(a.ObservedAt, b.ObservedAt); c != 0 {
		return c
	}
	if c := compareTime(a.IngestedAt, b.IngestedAt); c != 0 {
		return c
	}
	if c := strings.Compare(a.ServiceID, b.ServiceID); c != 0 {
		return c
	}
	if c := strings.Compare(a.Source, b.Source); c != 0 {
		return c
	}
	if sa, sb := severityRank(a.Status), severityRank(b.Status); sa != sb {
		if sa < sb {
			return -1
		}
		return 1
	}
	if c := strings.Compare(evidenceRef(a.Evidence), evidenceRef(b.Evidence)); c != 0 {
		return c
	}
	return strings.Compare(evidenceSummary(a.Evidence), evidenceSummary(b.Evidence))
}

func evidenceRef(e *Evidence) string {
	if e == nil {
		return ""
	}
	return e.Ref
}

func evidenceSummary(e *Evidence) string {
	if e == nil {
		return ""
	}
	return e.Summary
}

func compareTime(a, b time.Time) int {
	// Normalize locations so equal wall times compare equal regardless of zone pointer.
	au, bu := a.UTC(), b.UTC()
	switch {
	case au.Before(bu):
		return -1
	case au.After(bu):
		return 1
	default:
		return 0
	}
}

// Latest returns the newest/most-preferred observation according to Compare.
//
// Arrival / slice order is ignored: the winner is determined by Compare keys
// only. When Compare returns 0 for two elements, the earlier slice index is
// kept (byte-identical keys). ok is false when obs is empty.
func Latest(obs []Observation) (Observation, bool) {
	if len(obs) == 0 {
		return Observation{}, false
	}
	bestIdx := 0
	for i := 1; i < len(obs); i++ {
		if Compare(obs[i], obs[bestIdx]) > 0 {
			bestIdx = i
		}
	}
	return obs[bestIdx], true
}
