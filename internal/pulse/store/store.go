// Package store provides a local historical observation store for Pulse.
//
// The Memory implementation is process-local, requires no external services
// (including Databricks), and does not mutate observation health semantics.
// Callers derive freshness via observation.IsStale / EffectiveStatus; the store
// preserves producer Status and timestamps as written.
//
// Duplicate semantics: an Append whose observation.Compare keys equal an
// already-stored row for the same ServiceID is an idempotent no-op.
//
// This package does not scan string contents for secrets; producers must redact
// before ingest. The store persists only observation.Observation fields.
package store

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/yashg4509/perch/internal/pulse/observation"
)

// Store is the local historical observation API. Implementations must keep
// services isolated, reject invalid observations, and select Latest via
// observation.Compare / observation.Latest (never arrival order alone).
type Store interface {
	// Append validates and stores obs under obs.ServiceID.
	// Compare-equal duplicates for that service are accepted without growth.
	Append(obs observation.Observation) error

	// List returns observations for serviceID in ascending observation.Compare
	// order (oldest / least-preferred first). Unknown services yield an empty
	// non-nil slice.
	List(serviceID string) ([]observation.Observation, error)

	// Latest returns the most preferred observation for serviceID using
	// observation.Latest. ok is false when the service has no history.
	Latest(serviceID string) (observation.Observation, bool, error)
}

// Memory is a concurrency-safe, process-local Store. History is lost when the
// process exits. Suitable as the smallest Phase 1 backend behind Store.
type Memory struct {
	mu        sync.RWMutex
	byService map[string][]observation.Observation
}

// NewMemory returns an empty in-memory observation store.
func NewMemory() *Memory {
	return &Memory{byService: make(map[string][]observation.Observation)}
}

// Append implements Store.
func (m *Memory) Append(obs observation.Observation) error {
	if err := obs.Validate(); err != nil {
		return err
	}
	stored := clone(obs)

	m.mu.Lock()
	defer m.mu.Unlock()

	existing := m.byService[stored.ServiceID]
	for i := range existing {
		if observation.Compare(existing[i], stored) == 0 {
			// Idempotent duplicate: preserve first-seen copy; do not grow.
			return nil
		}
	}
	m.byService[stored.ServiceID] = append(existing, stored)
	return nil
}

// List implements Store.
func (m *Memory) List(serviceID string) ([]observation.Observation, error) {
	if strings.TrimSpace(serviceID) == "" {
		return nil, fmt.Errorf("store: service_id is required")
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	src := m.byService[serviceID]
	out := make([]observation.Observation, len(src))
	for i := range src {
		out[i] = clone(src[i])
	}
	sort.SliceStable(out, func(i, j int) bool {
		return observation.Compare(out[i], out[j]) < 0
	})
	return out, nil
}

// Latest implements Store.
func (m *Memory) Latest(serviceID string) (observation.Observation, bool, error) {
	if strings.TrimSpace(serviceID) == "" {
		return observation.Observation{}, false, fmt.Errorf("store: service_id is required")
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	src := m.byService[serviceID]
	if len(src) == 0 {
		return observation.Observation{}, false, nil
	}
	// Work on clones so Latest cannot expose shared Evidence pointers.
	cloned := make([]observation.Observation, len(src))
	for i := range src {
		cloned[i] = clone(src[i])
	}
	best, ok := observation.Latest(cloned)
	return best, ok, nil
}

func clone(o observation.Observation) observation.Observation {
	out := o
	if o.Evidence != nil {
		ev := *o.Evidence
		out.Evidence = &ev
	}
	return out
}
