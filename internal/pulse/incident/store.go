package incident

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Store is the local incident API.
type Store interface {
	Append(inc Incident) error
	Get(incidentID string) (Incident, bool, error)
	List() ([]Incident, error)
}

// Memory is a concurrency-safe, process-local incident store.
type Memory struct {
	mu   sync.RWMutex
	byID map[string]Incident
}

// NewMemory returns an empty in-memory incident store.
func NewMemory() *Memory {
	return &Memory{byID: make(map[string]Incident)}
}

// Append validates and stores inc. Duplicate IDs with equal content are
// idempotent; conflicting payloads return an error.
func (m *Memory) Append(inc Incident) error {
	if err := inc.Validate(); err != nil {
		return err
	}
	stored := cloneIncident(inc)
	m.mu.Lock()
	defer m.mu.Unlock()
	if prev, ok := m.byID[stored.IncidentID]; ok {
		if incidentsEqual(prev, stored) {
			return nil
		}
		return fmt.Errorf("incident: duplicate incident_id %q with conflicting payload", stored.IncidentID)
	}
	m.byID[stored.IncidentID] = stored
	return nil
}

// Get returns an incident by ID.
func (m *Memory) Get(incidentID string) (Incident, bool, error) {
	if strings.TrimSpace(incidentID) == "" {
		return Incident{}, false, fmt.Errorf("incident: incident_id is required")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	inc, ok := m.byID[incidentID]
	if !ok {
		return Incident{}, false, nil
	}
	return cloneIncident(inc), true, nil
}

// List returns incidents ordered by FirstDetectedAt, then IncidentID.
func (m *Memory) List() ([]Incident, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Incident, 0, len(m.byID))
	for _, inc := range m.byID {
		out = append(out, cloneIncident(inc))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].FirstDetectedAt.Equal(out[j].FirstDetectedAt) {
			return out[i].FirstDetectedAt.Before(out[j].FirstDetectedAt)
		}
		return out[i].IncidentID < out[j].IncidentID
	})
	return out, nil
}

func cloneIncident(inc Incident) Incident {
	out := inc
	if inc.RecoveredAt != nil {
		t := inc.RecoveredAt.UTC()
		out.RecoveredAt = &t
	}
	out.FindingIDs = append([]string(nil), inc.FindingIDs...)
	out.SignalTypes = append([]string(nil), inc.SignalTypes...)
	out.AffectedServiceIDs = append([]string(nil), inc.AffectedServiceIDs...)
	out.Observations = append([]string(nil), inc.Observations...)
	out.Inferences = append([]string(nil), inc.Inferences...)
	out.Limitations = append([]string(nil), inc.Limitations...)
	out.Evidence = append([]EvidenceRef(nil), inc.Evidence...)
	out.Before.Digests = append([]SampleDigest(nil), inc.Before.Digests...)
	out.During.Digests = append([]SampleDigest(nil), inc.During.Digests...)
	out.After.Digests = append([]SampleDigest(nil), inc.After.Digests...)
	return out
}

func incidentsEqual(a, b Incident) bool {
	ra, err := json.Marshal(a)
	if err != nil {
		return false
	}
	rb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(ra) == string(rb)
}
