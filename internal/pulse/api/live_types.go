package api

import (
	"time"

	"github.com/yashg4509/perch/internal/pulse/live"
)

// LiveStatusResponse is GET /api/pulse/live/status.
type LiveStatusResponse struct {
	SchemaVersion string          `json:"schema_version"`
	GeneratedAt   time.Time       `json:"generated_at"`
	Status        live.StatusView `json:"status"`
	Limitations   []string        `json:"limitations,omitempty"`
}

// LiveServicesResponse is GET /api/pulse/live/services.
type LiveServicesResponse struct {
	SchemaVersion string             `json:"schema_version"`
	GeneratedAt   time.Time          `json:"generated_at"`
	Count         int                `json:"count"`
	Services      []live.ServiceView `json:"services"`
	Status        live.StatusView    `json:"status"`
	Limitations   []string           `json:"limitations,omitempty"`
}

// LiveIncidentsResponse is GET /api/pulse/live/incidents.
type LiveIncidentsResponse struct {
	SchemaVersion string              `json:"schema_version"`
	GeneratedAt   time.Time           `json:"generated_at"`
	Count         int                 `json:"count"`
	Incidents     []live.IncidentView `json:"incidents"`
	Status        live.StatusView     `json:"status"`
	Limitations   []string            `json:"limitations,omitempty"`
}
