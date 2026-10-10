package api

import (
	"time"

	"github.com/yashg4509/perch/internal/pulse/change"
	"github.com/yashg4509/perch/internal/pulse/correlate"
	"github.com/yashg4509/perch/internal/pulse/incident"
	"github.com/yashg4509/perch/internal/pulse/observation"
)

const (
	// Schema versions for HTTP envelopes (distinct from persisted domain schemas).
	SchemaServices  = "pulse.api.services.v1"
	SchemaIncidents = "pulse.api.incidents.v1"
	SchemaIncident  = "pulse.api.incident.v1"
	SchemaChanges   = "pulse.api.changes.v1"
	SchemaChange    = "pulse.api.change.v1"
	SchemaError     = "pulse.api.error.v1"
)

// Freshness labels for service intelligence.
const (
	FreshnessFresh       = "fresh"
	FreshnessStale       = "stale"
	FreshnessMissing     = "missing"
	FreshnessUnavailable = "unavailable"
)

// DataSources documents what the API read for this response.
type DataSources struct {
	// Observations is "process_memory" when an observation store is wired,
	// otherwise "process_memory_unavailable".
	Observations string `json:"observations"`
	// Incidents is "file_store" when reading from the Pulse data directory.
	Incidents string `json:"incidents"`
	// Changes is "file_store" when reading from the Pulse data directory.
	Changes string `json:"changes"`
	// Correlation is "on_read" when computed at request time, or "none".
	Correlation string `json:"correlation,omitempty"`
}

// ErrorBody is a typed API error (no silent mock fallback).
type ErrorBody struct {
	SchemaVersion string `json:"schema_version"`
	Error         string `json:"error"`
	Code          string `json:"code"`
	// Limitations carry non-secret context (e.g. corrupted artifact).
	Limitations []string `json:"limitations,omitempty"`
}

// ServicesResponse is GET /api/pulse/services.
type ServicesResponse struct {
	SchemaVersion string               `json:"schema_version"`
	GeneratedAt   time.Time            `json:"generated_at"`
	DataSources   DataSources          `json:"data_sources"`
	Limitations   []string             `json:"limitations"`
	Count         int                  `json:"count"`
	Limit         int                  `json:"limit"`
	Services      []ServicePulseStatus `json:"services"`
}

// ServicePulseStatus is Pulse intelligence for one service.
// Active health probes remain on GET /api/status and are never collapsed here.
type ServicePulseStatus struct {
	SchemaVersion string `json:"schema_version"`
	ServiceID     string `json:"service_id"`
	// GraphNode is the perch.yaml / graph node name when an explicit mapping exists.
	GraphNode string `json:"graph_node,omitempty"`
	// MappingSource names how GraphNode was resolved (e.g. "astronomy_compose").
	MappingSource string `json:"mapping_source,omitempty"`

	// Intelligence is telemetry/regression state — not active probe health.
	Intelligence          observation.Status `json:"intelligence"`
	IntelligenceAvailable bool               `json:"intelligence_available"`
	ObservationSource     string             `json:"observation_source,omitempty"`
	LastObservedAt        *time.Time         `json:"last_observed_at,omitempty"`
	Freshness             string             `json:"freshness"`
	AgeSeconds            *float64           `json:"age_seconds,omitempty"`
	OpenIncidentIDs       []string           `json:"open_incident_ids"`
	RelatedIncidentIDs    []string           `json:"related_incident_ids,omitempty"`
	Limitations           []string           `json:"limitations,omitempty"`
	ActiveHealthNote      string             `json:"active_health_note"`
}

// IncidentsResponse is GET /api/pulse/incidents.
type IncidentsResponse struct {
	SchemaVersion string              `json:"schema_version"`
	GeneratedAt   time.Time           `json:"generated_at"`
	DataSources   DataSources         `json:"data_sources"`
	Limitations   []string            `json:"limitations,omitempty"`
	Count         int                 `json:"count"`
	Limit         int                 `json:"limit"`
	Incidents     []incident.Incident `json:"incidents"`
}

// IncidentDetailResponse is GET /api/pulse/incidents/{id}.
// Correlation is embedded to avoid an extra endpoint (Milestone A).
type IncidentDetailResponse struct {
	SchemaVersion string            `json:"schema_version"`
	GeneratedAt   time.Time         `json:"generated_at"`
	DataSources   DataSources       `json:"data_sources"`
	Limitations   []string          `json:"limitations,omitempty"`
	Incident      incident.Incident `json:"incident"`
	// Correlation is computed on read; never presented as causation.
	Correlation *correlate.Report `json:"correlation,omitempty"`
	// GraphNode is the mapped perch graph node for the primary service, if known.
	GraphNode string `json:"graph_node,omitempty"`
}

// ChangesResponse is GET /api/pulse/changes.
type ChangesResponse struct {
	SchemaVersion string         `json:"schema_version"`
	GeneratedAt   time.Time      `json:"generated_at"`
	DataSources   DataSources    `json:"data_sources"`
	Limitations   []string       `json:"limitations,omitempty"`
	Count         int            `json:"count"`
	Limit         int            `json:"limit"`
	Changes       []change.Event `json:"changes"`
}

// ChangeDetailResponse is GET /api/pulse/changes/{id}.
type ChangeDetailResponse struct {
	SchemaVersion string       `json:"schema_version"`
	GeneratedAt   time.Time    `json:"generated_at"`
	DataSources   DataSources  `json:"data_sources"`
	Limitations   []string     `json:"limitations,omitempty"`
	Change        change.Event `json:"change"`
	GraphNodes    []string     `json:"graph_nodes,omitempty"`
}
