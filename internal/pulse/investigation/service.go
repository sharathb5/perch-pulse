package investigation

import (
	"fmt"
	"sync"
	"time"

	"github.com/yashg4509/perch/internal/pulse/astronomy"
	"github.com/yashg4509/perch/internal/pulse/change"
	"github.com/yashg4509/perch/internal/pulse/incident"
)

// IncidentSource reads persisted incidents (read-only for investigation assembly).
type IncidentSource interface {
	Get(incidentID string) (incident.Incident, bool, error)
}

// ChangeSource lists persisted change events.
type ChangeSource interface {
	List() ([]change.Event, error)
}

// Service assembles and persists investigations with concurrency safety.
type Service struct {
	Incidents IncidentSource
	Changes   ChangeSource
	Store     *FileStore
	Mapper    GraphMapper
	Mapping   astronomy.Mapping
	Now       func() time.Time

	mu sync.Mutex // serializes create for same process
}

// CreateOptions are the only accepted presentation parameters for POST.
type CreateOptions struct {
	IncidentID string
	// ReuseIdentical returns an existing snapshot with the same content fingerprint.
	ReuseIdentical bool
	// LinkedRuns optional explicitly linked Run Pulse summaries.
	LinkedRuns []LinkedRunPulse
}

// Create assembles evidence and persists a versioned snapshot.
func (s *Service) Create(opts CreateOptions) (Investigation, error) {
	if s == nil || s.Incidents == nil || s.Store == nil {
		return Investigation{}, fmt.Errorf("investigation: service not configured")
	}
	id := opts.IncidentID
	if id == "" {
		return Investigation{}, fmt.Errorf("investigation: incident_id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	inc, ok, err := s.Incidents.Get(id)
	if err != nil {
		return Investigation{}, err
	}
	if !ok {
		return Investigation{}, &NotFoundError{Kind: "incident", ID: id}
	}

	var changes []change.Event
	if s.Changes != nil {
		changes, err = s.Changes.List()
		if err != nil {
			return Investigation{}, err
		}
	}

	mapping := s.Mapping
	if len(mapping.Services) == 0 {
		mapping, err = astronomy.Load()
		if err != nil {
			return Investigation{}, err
		}
	}

	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}

	inv, err := Assemble(AssembleInput{
		Incident:   inc,
		Changes:    changes,
		Mapping:    mapping,
		Mapper:     s.Mapper,
		Now:        now,
		LinkedRuns: opts.LinkedRuns,
	})
	if err != nil {
		return Investigation{}, err
	}

	if opts.ReuseIdentical {
		if existing, found, err := s.Store.FindByFingerprint(id, inv.ContentFingerprint); err != nil {
			return Investigation{}, err
		} else if found {
			return existing, nil
		}
	}

	if err := s.Store.Put(inv); err != nil {
		return Investigation{}, err
	}
	return inv, nil
}

// Get returns a persisted investigation.
func (s *Service) Get(investigationID string) (Investigation, error) {
	if s == nil || s.Store == nil {
		return Investigation{}, fmt.Errorf("investigation: service not configured")
	}
	inv, ok, err := s.Store.Get(investigationID)
	if err != nil {
		return Investigation{}, err
	}
	if !ok {
		return Investigation{}, &NotFoundError{Kind: "investigation", ID: investigationID}
	}
	return inv, nil
}

// ContextForIncident returns the latest persisted investigation agent context.
// It is read-only: callers must POST /investigations to create a snapshot first.
func (s *Service) ContextForIncident(incidentID string) (Investigation, AgentContext, error) {
	if s == nil || s.Store == nil {
		return Investigation{}, AgentContext{}, fmt.Errorf("investigation: service not configured")
	}
	if _, ok, err := s.Incidents.Get(incidentID); err != nil {
		return Investigation{}, AgentContext{}, err
	} else if !ok {
		return Investigation{}, AgentContext{}, &NotFoundError{Kind: "incident", ID: incidentID}
	}
	inv, ok, err := s.Store.LatestForIncident(incidentID)
	if err != nil {
		return Investigation{}, AgentContext{}, err
	}
	if !ok {
		return Investigation{}, AgentContext{}, &NotFoundError{
			Kind: "investigation",
			ID:   "for incident " + incidentID + " (POST /api/pulse/investigations first)",
		}
	}
	return inv, BuildAgentContext(inv), nil
}

// AddFeedback validates and persists feedback without mutating investigation/incident.
func (s *Service) AddFeedback(fb Feedback) (Feedback, error) {
	if s == nil || s.Store == nil {
		return Feedback{}, fmt.Errorf("investigation: service not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	inv, ok, err := s.Store.Get(fb.InvestigationID)
	if err != nil {
		return Feedback{}, err
	}
	if !ok {
		return Feedback{}, &NotFoundError{Kind: "investigation", ID: fb.InvestigationID}
	}
	if fb.IncidentID == "" {
		fb.IncidentID = inv.IncidentID
	}
	if fb.SchemaVersion == "" {
		fb.SchemaVersion = FeedbackSchemaVersion
	}
	if fb.Timestamp.IsZero() {
		now := time.Now().UTC()
		if s.Now != nil {
			now = s.Now().UTC()
		}
		fb.Timestamp = now
	} else {
		fb.Timestamp = fb.Timestamp.UTC()
	}
	if fb.FeedbackID == "" {
		fb.FeedbackID = NewFeedbackID(fb.InvestigationID, fb.Timestamp)
	}
	fb.Limitations = FeedbackLimitations()
	if err := ValidateFeedbackAgainstInvestigation(fb, inv); err != nil {
		return Feedback{}, &ValidationError{Msg: err.Error()}
	}
	fb = RedactFeedback(fb)
	if err := s.Store.PutFeedback(fb); err != nil {
		return Feedback{}, err
	}
	return fb, nil
}

// ListFeedback returns feedback rows for an investigation.
func (s *Service) ListFeedback(investigationID string) ([]Feedback, error) {
	if s == nil || s.Store == nil {
		return nil, fmt.Errorf("investigation: service not configured")
	}
	if _, ok, err := s.Store.Get(investigationID); err != nil {
		return nil, err
	} else if !ok {
		return nil, &NotFoundError{Kind: "investigation", ID: investigationID}
	}
	return s.Store.ListFeedback(investigationID)
}

// NotFoundError is returned for missing incidents/investigations.
type NotFoundError struct {
	Kind string
	ID   string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s %q not found", e.Kind, e.ID)
}

// ValidationError wraps client validation failures.
type ValidationError struct {
	Msg string
}

func (e *ValidationError) Error() string { return e.Msg }
