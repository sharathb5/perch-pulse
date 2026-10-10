package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/change"
	"github.com/yashg4509/perch/internal/pulse/incident"
	"github.com/yashg4509/perch/internal/pulse/observation"
	"github.com/yashg4509/perch/internal/pulse/store"
)

// IncidentReader reads persisted incidents.
type IncidentReader interface {
	List() ([]incident.Incident, error)
	Get(id string) (incident.Incident, bool, error)
}

// ChangeReader reads persisted change events.
type ChangeReader interface {
	List() ([]change.Event, error)
	Get(id string) (change.Event, bool, error)
}

// ObservationReader reads process-local observation history (optional).
type ObservationReader interface {
	Latest(serviceID string) (observation.Observation, bool, error)
}

// Reader is the smallest replaceable read surface for the Pulse HTTP API.
type Reader struct {
	Incidents    IncidentReader
	Changes      ChangeReader
	Observations ObservationReader // nil → observations unavailable
	Mapper       ServiceMapper
	Now          func() time.Time
	Freshness    time.Duration
	DefaultLimit int
	MaxLimit     int
}

// DefaultFreshness is the observation freshness window for EffectiveStatus.
const DefaultFreshness = 5 * time.Minute

// DefaultListLimit is the default page size for list endpoints.
const DefaultListLimit = 50

// MaxListLimit caps ?limit= for list endpoints.
const MaxListLimit = 200

// NewFileReader builds a Reader rooted at pulseDir (incidents/ + changes/).
// Observations default to unavailable (nil) — viz does not run a collector.
func NewFileReader(pulseDir string, mapper ServiceMapper) (*Reader, error) {
	dir := strings.TrimSpace(pulseDir)
	if dir == "" {
		return nil, fmt.Errorf("pulse api: data directory is required")
	}
	// Resolve to absolute cleaned path; refuse if it does not exist yet —
	// missing dir is treated as empty stores (FileStore List returns []).
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("pulse api: resolve data dir: %w", err)
	}
	abs = filepath.Clean(abs)
	if mapper == nil {
		mapper = EmptyMapper{}
	}
	return &Reader{
		Incidents:    incident.NewFileStore(abs),
		Changes:      change.NewFileStore(abs),
		Observations: nil,
		Mapper:       mapper,
		Now:          func() time.Time { return time.Now().UTC() },
		Freshness:    DefaultFreshness,
		DefaultLimit: DefaultListLimit,
		MaxLimit:     MaxListLimit,
	}, nil
}

// WithObservationStore wires a process-local observation Store (tests / future).
func (r *Reader) WithObservationStore(s store.Store) *Reader {
	if r == nil {
		return nil
	}
	r.Observations = s
	return r
}

// ResolveLimit parses ?limit= with defaults and caps.
func (r *Reader) ResolveLimit(raw string) int {
	def := DefaultListLimit
	max := MaxListLimit
	if r != nil {
		if r.DefaultLimit > 0 {
			def = r.DefaultLimit
		}
		if r.MaxLimit > 0 {
			max = r.MaxLimit
		}
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def
	}
	var n int
	for _, c := range raw {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
		if n > max {
			return max
		}
	}
	if n <= 0 {
		return def
	}
	return n
}

// nowUTC returns the configured clock.
func (r *Reader) nowUTC() time.Time {
	if r != nil && r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

// freshnessWindow returns the configured freshness duration.
func (r *Reader) freshnessWindow() time.Duration {
	if r != nil && r.Freshness > 0 {
		return r.Freshness
	}
	return DefaultFreshness
}

// observationSourceLabel documents whether observations are wired.
func (r *Reader) observationSourceLabel() string {
	if r != nil && r.Observations != nil {
		return "process_memory"
	}
	return "process_memory_unavailable"
}

// EnsurePulseDirExists creates the pulse root if missing (0700). Optional for
// empty-state demos; readers tolerate missing incidents/changes subdirs.
func EnsurePulseDirExists(dir string) error {
	return os.MkdirAll(dir, 0o700)
}
