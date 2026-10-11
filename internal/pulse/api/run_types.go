package api

import (
	"time"

	"github.com/yashg4509/perch/internal/pulse/runpulse"
)

const (
	SchemaWorkflows = "pulse.api.workflows.v1"
	SchemaRun       = "pulse.api.run.v1"
	SchemaRunCreate = "pulse.api.runs.create.v1"
)

// WorkflowsResponse is GET /api/pulse/workflows.
type WorkflowsResponse struct {
	SchemaVersion string                          `json:"schema_version"`
	GeneratedAt   time.Time                       `json:"generated_at"`
	Limitations   []string                        `json:"limitations"`
	Count         int                             `json:"count"`
	Workflows     []runpulse.WorkflowAvailability `json:"workflows"`
}

// CreateRunRequest is POST /api/pulse/runs body.
type CreateRunRequest struct {
	WorkflowID string `json:"workflow_id"`
}

// CreateRunResponse is the immediate ack for POST /api/pulse/runs.
type CreateRunResponse struct {
	SchemaVersion string        `json:"schema_version"`
	GeneratedAt   time.Time     `json:"generated_at"`
	Run           *runpulse.Run `json:"run"`
	Limitations   []string      `json:"limitations,omitempty"`
}

// RunResponse is GET /api/pulse/runs/{id}.
type RunResponse struct {
	SchemaVersion string        `json:"schema_version"`
	GeneratedAt   time.Time     `json:"generated_at"`
	Run           *runpulse.Run `json:"run"`
	Limitations   []string      `json:"limitations,omitempty"`
}
