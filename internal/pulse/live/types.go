package live

import (
	"time"

	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/incident"
	"github.com/yashg4509/perch/internal/pulse/observation"
)

const (
	// Schema versions for live HTTP envelopes.
	SchemaLiveStatus    = "pulse.api.live_status.v1"
	SchemaLiveServices  = "pulse.api.live_services.v1"
	SchemaLiveIncidents = "pulse.api.live_incidents.v1"

	// RuntimeState values for GET /api/pulse/live/status.
	StateDisabled    = "disabled"
	StateUnavailable = "unavailable"
	StateWarmingUp   = "warming_up"
	StateCollecting  = "collecting"
	StateError       = "error"
)

// SignalView is one observed metric for a service in the live snapshot.
type SignalView struct {
	Signal          detect.SignalType `json:"signal"`
	Value           *float64          `json:"value,omitempty"`
	Unit            string            `json:"unit"`
	Window          string            `json:"window"`
	ObservedAt      *time.Time        `json:"observed_at,omitempty"`
	Quality         string            `json:"quality"` // fresh | stale | missing | unavailable
	BaselineReady   bool              `json:"baseline_ready"`
	BaselineValue   *float64          `json:"baseline_value,omitempty"`
	RegressionOpen  bool              `json:"regression_open"`
	RegressionScore *float64          `json:"regression_score,omitempty"`
}

// ServiceView is live telemetry for one canonical Pulse service ID.
type ServiceView struct {
	ServiceID        string             `json:"service_id"`
	GraphNode        string             `json:"graph_node,omitempty"`
	MappingSource    string             `json:"mapping_source,omitempty"`
	Environment      string             `json:"environment"`
	Freshness        string             `json:"freshness"`
	Quality          string             `json:"quality"`
	LastObservedAt   *time.Time         `json:"last_observed_at,omitempty"`
	Intelligence     observation.Status `json:"intelligence"`
	BaselineReady    bool               `json:"baseline_ready"`
	RegressionOpen   bool               `json:"regression_open"`
	OpenIncidentIDs  []string           `json:"open_incident_ids"`
	Signals          []SignalView       `json:"signals"`
	ActiveHealthNote string             `json:"active_health_note"`
	Limitations      []string           `json:"limitations,omitempty"`
}

// IncidentView wraps a persisted/live incident with lifecycle labels.
type IncidentView struct {
	IncidentID     string            `json:"incident_id"`
	Lifecycle      string            `json:"lifecycle"` // active | recovered | historical
	Source         string            `json:"source"`    // live | historical
	PrimaryService string            `json:"primary_service_id"`
	GraphNode      string            `json:"graph_node,omitempty"`
	FirstDetected  time.Time         `json:"first_detected_at"`
	LastObserved   time.Time         `json:"last_observed_at"`
	RecoveredAt    *time.Time        `json:"recovered_at,omitempty"`
	SignalTypes    []string          `json:"signal_types,omitempty"`
	Summary        string            `json:"summary"`
	Incident       incident.Incident `json:"incident"`
	Limitations    []string          `json:"limitations,omitempty"`
}

// StatusView is GET /api/pulse/live/status.
type StatusView struct {
	SchemaVersion        string     `json:"schema_version"`
	GeneratedAt          time.Time  `json:"generated_at"`
	Enabled              bool       `json:"enabled"`
	Configured           bool       `json:"configured"`
	Available            bool       `json:"available"`
	State                string     `json:"state"`
	Environment          string     `json:"environment,omitempty"`
	PrometheusConfigured bool       `json:"prometheus_configured"`
	PollInterval         string     `json:"poll_interval,omitempty"`
	SampleWindow         string     `json:"sample_window,omitempty"`
	FreshnessWindow      string     `json:"freshness_window,omitempty"`
	LastAttemptAt        *time.Time `json:"last_attempt_at,omitempty"`
	LastSuccessAt        *time.Time `json:"last_success_at,omitempty"`
	LastError            string     `json:"last_error,omitempty"`
	ServicesObserved     int        `json:"services_observed"`
	ServicesWithBaseline int        `json:"services_with_baseline"`
	ActiveRegressions    int        `json:"active_regressions"`
	ActiveIncidents      int        `json:"active_incidents"`
	UnavailableOrUnknown int        `json:"unavailable_or_unknown"`
	DetectorVersion      string     `json:"detector_version,omitempty"`
	ActiveHealthNote     string     `json:"active_health_note"`
	RunPulseNote         string     `json:"run_pulse_note"`
	Limitations          []string   `json:"limitations"`
	UnavailabilityReason string     `json:"unavailability_reason,omitempty"`
}

// Snapshot is the concurrency-safe read model published each poll cycle.
type Snapshot struct {
	Status    StatusView
	Services  []ServiceView
	Incidents []IncidentView
}

// clone helpers keep API readers from mutating runtime state.
func cloneSnapshot(s Snapshot) Snapshot {
	out := s
	out.Status.Limitations = append([]string(nil), s.Status.Limitations...)
	out.Services = make([]ServiceView, len(s.Services))
	for i, svc := range s.Services {
		out.Services[i] = svc
		out.Services[i].OpenIncidentIDs = append([]string(nil), svc.OpenIncidentIDs...)
		out.Services[i].Signals = append([]SignalView(nil), svc.Signals...)
		out.Services[i].Limitations = append([]string(nil), svc.Limitations...)
	}
	out.Incidents = make([]IncidentView, len(s.Incidents))
	for i, inc := range s.Incidents {
		out.Incidents[i] = inc
		out.Incidents[i].SignalTypes = append([]string(nil), inc.SignalTypes...)
		out.Incidents[i].Limitations = append([]string(nil), inc.Limitations...)
	}
	return out
}
