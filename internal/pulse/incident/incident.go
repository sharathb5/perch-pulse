// Package incident builds Pulse incident evidence snapshots from detector
// findings and telemetry samples. It separates observations from inferences
// and does not import scenario ground truth or claim causation.
//
// This package provides the minimal incident model required for change
// correlation (Phase 2F). It is optional relative to core Perch collectors.
package incident

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/detect"
)

const (
	// SchemaVersion is the persisted incident schema.
	SchemaVersion = "pulse.incident.v1"
)

// EvidenceRef is a non-secret pointer to supporting artifacts.
type EvidenceRef struct {
	Ref     string `json:"ref"`
	Summary string `json:"summary"`
}

// Window is an inclusive UTC interval.
type Window struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// SampleDigest is a compact telemetry summary (not a full blob dump).
type SampleDigest struct {
	Signal  detect.SignalType `json:"signal"`
	Count   int               `json:"count"`
	Median  float64           `json:"median"`
	Min     float64           `json:"min"`
	Max     float64           `json:"max"`
	FirstAt time.Time         `json:"first_at"`
	LastAt  time.Time         `json:"last_at"`
}

// PhaseSnapshot holds before/during/after evidence for one incident phase.
type PhaseSnapshot struct {
	Phase      string         `json:"phase"` // before | during | after
	Window     Window         `json:"window"`
	Digests    []SampleDigest `json:"digests,omitempty"`
	Available  bool           `json:"available"`
	Limitation string         `json:"limitation,omitempty"`
}

// Incident is a versioned evidence package around one or more findings.
type Incident struct {
	SchemaVersion      string        `json:"schema_version"`
	IncidentID         string        `json:"incident_id"`
	PrimaryServiceID   string        `json:"primary_service_id"`
	Environment        string        `json:"environment,omitempty"`
	FindingIDs         []string      `json:"finding_ids"`
	SignalTypes        []string      `json:"signal_types,omitempty"`
	FirstDetectedAt    time.Time     `json:"first_detected_at"`
	LastObservedAt     time.Time     `json:"last_observed_at"`
	RecoveredAt        *time.Time    `json:"recovered_at,omitempty"`
	AffectedServiceIDs []string      `json:"affected_service_ids,omitempty"`
	Before             PhaseSnapshot `json:"before"`
	During             PhaseSnapshot `json:"during"`
	After              PhaseSnapshot `json:"after"`
	Observations       []string      `json:"observations"`
	Inferences         []string      `json:"inferences"`
	Limitations        []string      `json:"limitations"`
	Evidence           []EvidenceRef `json:"evidence,omitempty"`
	Summary            string        `json:"summary"`
}

var forbiddenFieldSubstrings = []string{
	"secret", "token", "password", "credential", "apikey", "api_key", "auth",
}

// ValidateReportsSecretFieldNames guards the exported shape.
func ValidateReportsSecretFieldNames() error {
	for _, t := range []reflect.Type{
		reflect.TypeOf(Incident{}),
		reflect.TypeOf(EvidenceRef{}),
		reflect.TypeOf(PhaseSnapshot{}),
	} {
		for i := 0; i < t.NumField(); i++ {
			name := strings.ToLower(t.Field(i).Name)
			for _, bad := range forbiddenFieldSubstrings {
				if strings.Contains(name, bad) {
					return fmt.Errorf("incident: type %s field %s looks like a secret holder", t.Name(), t.Field(i).Name)
				}
			}
		}
	}
	return nil
}

// Validate checks incident invariants.
func (inc Incident) Validate() error {
	if inc.SchemaVersion != SchemaVersion {
		return fmt.Errorf("incident: schema_version must be %q", SchemaVersion)
	}
	if strings.TrimSpace(inc.IncidentID) == "" {
		return fmt.Errorf("incident: incident_id is required")
	}
	if strings.TrimSpace(inc.PrimaryServiceID) == "" {
		return fmt.Errorf("incident: primary_service_id is required")
	}
	if len(inc.FindingIDs) == 0 {
		return fmt.Errorf("incident: finding_ids required")
	}
	if inc.FirstDetectedAt.IsZero() || inc.LastObservedAt.IsZero() {
		return fmt.Errorf("incident: first/last timestamps required")
	}
	if inc.FirstDetectedAt.Location() != time.UTC || inc.LastObservedAt.Location() != time.UTC {
		return fmt.Errorf("incident: timestamps must be UTC")
	}
	if inc.LastObservedAt.Before(inc.FirstDetectedAt) {
		return fmt.Errorf("incident: last_observed_at before first_detected_at")
	}
	if inc.RecoveredAt != nil && inc.RecoveredAt.Location() != time.UTC {
		return fmt.Errorf("incident: recovered_at must be UTC")
	}
	for _, phase := range []PhaseSnapshot{inc.Before, inc.During, inc.After} {
		switch phase.Phase {
		case "before", "during", "after":
		default:
			return fmt.Errorf("incident: invalid phase %q", phase.Phase)
		}
	}
	joined := strings.ToLower(strings.Join(inc.Observations, " ") + " " +
		strings.Join(inc.Inferences, " ") + " " + inc.Summary)
	if claimsCausation(joined) {
		return fmt.Errorf("incident: text must not claim causation")
	}
	return nil
}

func claimsCausation(lower string) bool {
	// Allow explicit negations ("not proven causation", "not proven root cause").
	if strings.Contains(lower, "caused by") ||
		strings.Contains(lower, "deployment caused") ||
		strings.Contains(lower, "change caused") ||
		strings.Contains(lower, "root cause is") {
		return true
	}
	if strings.Contains(lower, "proven root cause") &&
		!strings.Contains(lower, "not proven") &&
		!strings.Contains(lower, "unproven") {
		return true
	}
	if strings.Contains(lower, "root cause") &&
		!strings.Contains(lower, "not") &&
		!strings.Contains(lower, "unproven") {
		return true
	}
	return false
}

// IncidentIDFor builds a deterministic ID from primary service and first detect time.
func IncidentIDFor(serviceID string, first time.Time) string {
	return fmt.Sprintf("inc|%s|%s", serviceID, first.UTC().Format("20060102T150405.000000000Z"))
}

// Open reports whether the incident has not recovered yet.
func (inc Incident) Open() bool {
	return inc.RecoveredAt == nil
}
