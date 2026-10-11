package live

import (
	"fmt"
	"strings"
	"sync"

	"github.com/yashg4509/perch/internal/pulse/observation"
	"github.com/yashg4509/perch/internal/pulse/store"
)

// latestStore keeps one observation per service (bounded process-local memory).
type latestStore struct {
	mu   sync.RWMutex
	byID map[string]observation.Observation
}

func newLatestStore() *latestStore {
	return &latestStore{byID: make(map[string]observation.Observation)}
}

var _ store.Store = (*latestStore)(nil)

func (s *latestStore) Append(obs observation.Observation) error {
	if err := obs.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[obs.ServiceID] = obs
	return nil
}

func (s *latestStore) List(serviceID string) ([]observation.Observation, error) {
	if strings.TrimSpace(serviceID) == "" {
		return nil, fmt.Errorf("store: service_id is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	obs, ok := s.byID[serviceID]
	if !ok {
		return []observation.Observation{}, nil
	}
	return []observation.Observation{obs}, nil
}

func (s *latestStore) Latest(serviceID string) (observation.Observation, bool, error) {
	if strings.TrimSpace(serviceID) == "" {
		return observation.Observation{}, false, fmt.Errorf("store: service_id is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	obs, ok := s.byID[serviceID]
	return obs, ok, nil
}
