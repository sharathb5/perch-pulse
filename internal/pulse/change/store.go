package change

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Store is the local change-event API.
type Store interface {
	// Append validates and stores e. Duplicate ChangeID is an idempotent no-op
	// when the payload Compare-equals the stored event; conflicting same-ID
	// payloads return an error (event integrity).
	Append(e Event) error
	// Get returns a change by ID.
	Get(changeID string) (Event, bool, error)
	// List returns all events in ascending Compare order.
	List() ([]Event, error)
	// ListForService returns events that list serviceID among ServiceIDs.
	ListForService(serviceID string) ([]Event, error)
}

// Memory is a concurrency-safe, process-local Store.
type Memory struct {
	mu   sync.RWMutex
	byID map[string]Event
}

// NewMemory returns an empty in-memory change store.
func NewMemory() *Memory {
	return &Memory{byID: make(map[string]Event)}
}

// Append implements Store.
func (m *Memory) Append(e Event) error {
	if err := e.Validate(); err != nil {
		return err
	}
	stored := cloneEvent(e)

	m.mu.Lock()
	defer m.mu.Unlock()

	if prev, ok := m.byID[stored.ChangeID]; ok {
		if eventsEqual(prev, stored) {
			return nil // idempotent duplicate
		}
		return fmt.Errorf("change: duplicate change_id %q with conflicting payload", stored.ChangeID)
	}
	m.byID[stored.ChangeID] = stored
	return nil
}

// Get implements Store.
func (m *Memory) Get(changeID string) (Event, bool, error) {
	if strings.TrimSpace(changeID) == "" {
		return Event{}, false, fmt.Errorf("change: change_id is required")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.byID[changeID]
	if !ok {
		return Event{}, false, nil
	}
	return cloneEvent(e), true, nil
}

// List implements Store.
func (m *Memory) List() ([]Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Event, 0, len(m.byID))
	for _, e := range m.byID {
		out = append(out, cloneEvent(e))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return Compare(out[i], out[j]) < 0
	})
	return out, nil
}

// ListForService implements Store.
func (m *Memory) ListForService(serviceID string) ([]Event, error) {
	if strings.TrimSpace(serviceID) == "" {
		return nil, fmt.Errorf("change: service_id is required")
	}
	all, err := m.List()
	if err != nil {
		return nil, err
	}
	var out []Event
	for _, e := range all {
		for _, id := range e.ServiceIDs {
			if id == serviceID {
				out = append(out, e)
				break
			}
		}
	}
	if out == nil {
		out = []Event{}
	}
	return out, nil
}

func cloneEvent(e Event) Event {
	out := e
	if e.DeployedAt != nil {
		t := e.DeployedAt.UTC()
		out.DeployedAt = &t
	}
	if e.PRNumber != nil {
		n := *e.PRNumber
		out.PRNumber = &n
	}
	if e.ServiceIDs != nil {
		out.ServiceIDs = append([]string(nil), e.ServiceIDs...)
	}
	if e.FilesChanged != nil {
		out.FilesChanged = append([]string(nil), e.FilesChanged...)
	}
	if e.Evidence != nil {
		out.Evidence = append([]EvidenceRef(nil), e.Evidence...)
	}
	if e.Metadata != nil {
		out.Metadata = make(map[string]string, len(e.Metadata))
		for k, v := range e.Metadata {
			out.Metadata[k] = v
		}
	}
	return out
}

func eventsEqual(a, b Event) bool {
	if a.SchemaVersion != b.SchemaVersion || a.ChangeID != b.ChangeID ||
		a.ChangeType != b.ChangeType || a.Source != b.Source ||
		a.Repository != b.Repository || a.CommitSHA != b.CommitSHA ||
		a.ParentSHA != b.ParentSHA || a.Branch != b.Branch ||
		a.Environment != b.Environment || a.ServiceMapping != b.ServiceMapping ||
		a.Actor != b.Actor || a.Title != b.Title || a.Summary != b.Summary {
		return false
	}
	if !a.CreatedAt.Equal(b.CreatedAt) || !a.ObservedAt.Equal(b.ObservedAt) {
		return false
	}
	if (a.DeployedAt == nil) != (b.DeployedAt == nil) {
		return false
	}
	if a.DeployedAt != nil && !a.DeployedAt.Equal(*b.DeployedAt) {
		return false
	}
	if (a.PRNumber == nil) != (b.PRNumber == nil) {
		return false
	}
	if a.PRNumber != nil && *a.PRNumber != *b.PRNumber {
		return false
	}
	if len(a.ServiceIDs) != len(b.ServiceIDs) {
		return false
	}
	for i := range a.ServiceIDs {
		if a.ServiceIDs[i] != b.ServiceIDs[i] {
			return false
		}
	}
	if len(a.FilesChanged) != len(b.FilesChanged) {
		return false
	}
	for i := range a.FilesChanged {
		if a.FilesChanged[i] != b.FilesChanged[i] {
			return false
		}
	}
	if len(a.Evidence) != len(b.Evidence) {
		return false
	}
	for i := range a.Evidence {
		if a.Evidence[i] != b.Evidence[i] {
			return false
		}
	}
	if len(a.Metadata) != len(b.Metadata) {
		return false
	}
	for k, v := range a.Metadata {
		if b.Metadata[k] != v {
			return false
		}
	}
	return true
}
