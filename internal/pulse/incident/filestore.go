package incident

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// FileStore persists incidents as one JSON file per ID under Dir/incidents/.
type FileStore struct {
	Dir string
	mu  sync.Mutex
}

// NewFileStore returns a file-backed incident store.
func NewFileStore(dir string) *FileStore {
	return &FileStore{Dir: dir}
}

func (s *FileStore) incidentsDir() string {
	return filepath.Join(s.Dir, "incidents")
}

// Append implements Store.
func (s *FileStore) Append(inc Incident) error {
	if err := inc.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.incidentsDir(), 0o750); err != nil {
		return err
	}
	path := s.pathFor(inc.IncidentID)
	if prev, err := os.ReadFile(path); err == nil && len(prev) > 0 {
		var existing Incident
		if json.Unmarshal(prev, &existing) == nil && incidentsEqual(existing, inc) {
			return nil
		}
		return fmt.Errorf("incident: duplicate incident_id %q with conflicting payload", inc.IncidentID)
	}
	raw, err := json.MarshalIndent(cloneIncident(inc), "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Get implements Store.
func (s *FileStore) Get(incidentID string) (Incident, bool, error) {
	if strings.TrimSpace(incidentID) == "" {
		return Incident{}, false, fmt.Errorf("incident: incident_id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.pathFor(incidentID))
	if os.IsNotExist(err) {
		return Incident{}, false, nil
	}
	if err != nil {
		return Incident{}, false, err
	}
	var inc Incident
	if err := json.Unmarshal(raw, &inc); err != nil {
		return Incident{}, false, err
	}
	return inc, true, nil
}

// List implements Store.
func (s *FileStore) List() ([]Incident, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.incidentsDir())
	if os.IsNotExist(err) {
		return []Incident{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Incident
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.incidentsDir(), ent.Name()))
		if err != nil {
			return nil, err
		}
		var inc Incident
		if err := json.Unmarshal(raw, &inc); err != nil {
			return nil, err
		}
		out = append(out, inc)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].FirstDetectedAt.Equal(out[j].FirstDetectedAt) {
			return out[i].FirstDetectedAt.Before(out[j].FirstDetectedAt)
		}
		return out[i].IncidentID < out[j].IncidentID
	})
	if out == nil {
		out = []Incident{}
	}
	return out, nil
}

func (s *FileStore) pathFor(id string) string {
	safe := strings.ReplaceAll(id, "/", "_")
	safe = strings.ReplaceAll(safe, "|", "_")
	return filepath.Join(s.incidentsDir(), safe+".json")
}
