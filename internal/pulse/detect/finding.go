package detect

import (
	"fmt"
	"strings"
	"time"
)

const (
	// SchemaVersion is the persisted finding schema.
	SchemaVersion = "pulse.finding.v1"

	// DetectorVersion identifies this explainable baseline detector.
	DetectorVersion = "baseline-rel-mad-v1"
)

// Severity ranks finding urgency (not causation).
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Window is an inclusive observation interval in UTC.
type Window struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// EvidenceRef is a non-secret pointer to supporting telemetry.
type EvidenceRef struct {
	Ref     string `json:"ref"`
	Summary string `json:"summary"`
}

// ImpactCandidate is a topology-informed likely-affected service.
// Language is correlational; not proven causation.
type ImpactCandidate struct {
	ServiceID string  `json:"service_id"`
	Rank      int     `json:"rank"`
	Score     float64 `json:"score"`
	Relation  string  `json:"relation"` // "primary" | "also_degraded" | "downstream_of_primary"
	Note      string  `json:"note"`
}

// Finding is a versioned detector output for one service/signal regression.
type Finding struct {
	SchemaVersion   string            `json:"schema_version"`
	FindingID       string            `json:"finding_id"`
	DetectorVersion string            `json:"detector_version"`
	ServiceID       string            `json:"service_id"`
	SignalType      SignalType        `json:"signal_type"`
	ObservedWindow  Window            `json:"observed_window"`
	BaselineWindow  Window            `json:"baseline_window"`
	ObservedValue   float64           `json:"observed_value"`
	BaselineValue   float64           `json:"baseline_value"`
	Threshold       float64           `json:"threshold"`
	Score           float64           `json:"score"`
	Severity        Severity          `json:"severity"`
	FirstDetectedAt time.Time         `json:"first_detected_at"`
	LastObservedAt  time.Time         `json:"last_observed_at"`
	RecoveredAt     *time.Time        `json:"recovered_at,omitempty"`
	Evidence        []EvidenceRef     `json:"evidence,omitempty"`
	CandidateImpact []ImpactCandidate `json:"candidate_impact,omitempty"`
	Summary         string            `json:"summary"`
}

// Validate checks finding invariants.
func (f Finding) Validate() error {
	if f.SchemaVersion != SchemaVersion {
		return fmt.Errorf("detect: schema_version must be %q", SchemaVersion)
	}
	if f.DetectorVersion == "" {
		return fmt.Errorf("detect: detector_version is required")
	}
	if f.FindingID == "" {
		return fmt.Errorf("detect: finding_id is required")
	}
	if f.ServiceID == "" {
		return fmt.Errorf("detect: service_id is required")
	}
	if !f.SignalType.Valid() {
		return fmt.Errorf("detect: invalid signal_type %q", f.SignalType)
	}
	if f.FirstDetectedAt.IsZero() || f.LastObservedAt.IsZero() {
		return fmt.Errorf("detect: first/last detected timestamps required")
	}
	if f.FirstDetectedAt.Location() != time.UTC || f.LastObservedAt.Location() != time.UTC {
		return fmt.Errorf("detect: timestamps must be UTC")
	}
	if f.LastObservedAt.Before(f.FirstDetectedAt) {
		return fmt.Errorf("detect: last_observed_at before first_detected_at")
	}
	if f.RecoveredAt != nil {
		if f.RecoveredAt.Location() != time.UTC {
			return fmt.Errorf("detect: recovered_at must be UTC")
		}
		if f.RecoveredAt.Before(f.FirstDetectedAt) {
			return fmt.Errorf("detect: recovered_at before first_detected_at")
		}
	}
	switch f.Severity {
	case SeverityInfo, SeverityWarning, SeverityCritical:
	default:
		return fmt.Errorf("detect: invalid severity %q", f.Severity)
	}
	lower := strings.ToLower(f.Summary)
	// Reject affirmative causation claims only (allow "not proven causation").
	if strings.Contains(lower, "caused by") ||
		strings.Contains(lower, "root cause is") ||
		strings.Contains(lower, "proven root cause") ||
		(strings.Contains(lower, "root cause") && !strings.Contains(lower, "not") && !strings.Contains(lower, "unproven")) {
		return fmt.Errorf("detect: summary must not claim causation")
	}
	return nil
}

// FindingIDFor builds a deterministic ID from service, signal, and first-detect time.
func FindingIDFor(serviceID string, signal SignalType, first time.Time) string {
	return fmt.Sprintf("%s|%s|%s", serviceID, signal, first.UTC().Format("20060102T150405.000000000Z"))
}

// Open reports whether the finding has not recovered yet.
func (f Finding) Open() bool {
	return f.RecoveredAt == nil
}
