package runpulse

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

// Store is a bounded in-memory run store (MVP; lost on process restart).
type Store struct {
	mu      sync.Mutex
	runs    map[string]*Run
	order   []string // oldest → newest ids
	maxKeep int
	ttl     time.Duration
	now     func() time.Time
}

// NewStore creates a retention-bounded store.
func NewStore(maxKeep int, ttl time.Duration, now func() time.Time) *Store {
	if maxKeep <= 0 {
		maxKeep = 20
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Store{
		runs:    make(map[string]*Run),
		maxKeep: maxKeep,
		ttl:     ttl,
		now:     now,
	}
}

// NewRunID returns a deterministic-format opaque id: run_<hex>.
func NewRunID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "run_" + hex.EncodeToString(b[:]), nil
}

// Put inserts or replaces a run and enforces retention.
func (s *Store) Put(run *Run) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[run.ID]; !ok {
		s.order = append(s.order, run.ID)
	}
	s.runs[run.ID] = run
	s.expireLocked()
}

// Update applies fn under the lock to the live run pointer.
func (s *Store) Update(id string, fn func(*Run)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return false
	}
	fn(r)
	s.expireLocked()
	return true
}

// Get returns a clone of the run, if present.
func (s *Store) Get(id string) (*Run, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	r, ok := s.runs[id]
	if !ok {
		return nil, false
	}
	return r.Clone(), true
}

// RunningCount returns how many runs are pending or running.
func (s *Store) RunningCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	n := 0
	for _, r := range s.runs {
		if r.Status == StatusPending || r.Status == StatusRunning {
			n++
		}
	}
	return n
}

func (s *Store) expireLocked() {
	now := s.now()
	// Drop TTL-expired finished runs.
	kept := s.order[:0]
	for _, id := range s.order {
		r, ok := s.runs[id]
		if !ok {
			continue
		}
		if isTerminal(r.Status) && r.EndedAt != nil && now.Sub(*r.EndedAt) > s.ttl {
			delete(s.runs, id)
			continue
		}
		kept = append(kept, id)
	}
	s.order = kept
	// Cap by count: drop oldest finished first, then oldest overall if needed.
	for len(s.order) > s.maxKeep {
		dropIdx := -1
		for i, id := range s.order {
			if isTerminal(s.runs[id].Status) {
				dropIdx = i
				break
			}
		}
		if dropIdx < 0 {
			dropIdx = 0
		}
		id := s.order[dropIdx]
		delete(s.runs, id)
		s.order = append(s.order[:dropIdx], s.order[dropIdx+1:]...)
	}
}

func isTerminal(status string) bool {
	return status == StatusCompleted || status == StatusFailed || status == StatusTimedOut
}

// SnapshotIDs returns run ids newest-first (for tests).
func (s *Store) SnapshotIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	out := append([]string(nil), s.order...)
	sort.SliceStable(out, func(i, j int) bool {
		return s.runs[out[i]].StartedAt.After(s.runs[out[j]].StartedAt)
	})
	return out
}
