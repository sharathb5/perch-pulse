package runpulse

import (
	"time"

	"github.com/yashg4509/perch/internal/pulse/astronomy/tracesmoke"
)

// WorkflowIDAstronomyRecommendationsBrowse is the only MVP allowlisted workflow.
const WorkflowIDAstronomyRecommendationsBrowse = "astronomy.recommendations-browse.v1"

// Workflow is a server-side allowlisted diagnostic definition.
// Clients may only select by ID — never supply URLs or methods.
type Workflow struct {
	ID                  string
	DisplayName         string
	Description         string
	Environment         string
	SupportedSignals    []string
	HTTPMethod          string
	Path                string
	ProductID           string
	ExpectedAppServices []string
	TelemetryBackend    string
	MaxRequests         int
	RequestTimeout      time.Duration
	PollInterval        time.Duration
	OverallTimeout      time.Duration
	MaxConcurrent       int
	QueryServices       []string
}

// Catalog returns the static allowlist (MVP: one Astronomy Shop workflow).
func Catalog() []Workflow {
	return []Workflow{astronomyRecommendationsBrowse()}
}

func astronomyRecommendationsBrowse() Workflow {
	return Workflow{
		ID:          WorkflowIDAstronomyRecommendationsBrowse,
		DisplayName: "Recommendations Browse",
		Description: "Safe read-only GET /api/recommendations against the local Astronomy Shop demo; correlates a marked request with a Jaeger multi-service trace.",
		Environment: "astronomy-shop/local",
		SupportedSignals: []string{
			"http_request",
			"distributed_trace",
			"service_identity_mapping",
		},
		HTTPMethod:          "GET",
		Path:                tracesmoke.RecommendationsPath,
		ProductID:           tracesmoke.DefaultProductID,
		ExpectedAppServices: append([]string(nil), tracesmoke.ExpectedAppServices...),
		TelemetryBackend:    "jaeger",
		MaxRequests:         3,
		RequestTimeout:      10 * time.Second,
		PollInterval:        2 * time.Second,
		OverallTimeout:      90 * time.Second,
		MaxConcurrent:       1,
		QueryServices:       []string{"frontend", "recommendation", "frontend-proxy"},
	}
}

// LookupWorkflow returns a workflow by ID.
func LookupWorkflow(id string) (Workflow, bool) {
	for _, w := range Catalog() {
		if w.ID == id {
			return w, true
		}
	}
	return Workflow{}, false
}
