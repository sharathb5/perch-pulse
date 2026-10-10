package change

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// FileStore persists change events as one JSON file per change ID under Dir/changes/.
type FileStore struct {
	Dir string
	mu  sync.Mutex
}

// NewFileStore returns a file-backed store rooted at dir.
func NewFileStore(dir string) *FileStore {
	return &FileStore{Dir: dir}
}

func (s *FileStore) changesDir() string {
	return filepath.Join(s.Dir, "changes")
}

// Append implements Store.
func (s *FileStore) Append(e Event) error {
	if err := e.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.changesDir(), 0o750); err != nil {
		return err
	}
	path := s.pathFor(e.ChangeID)
	if prev, err := os.ReadFile(path); err == nil {
		var existing Event
		if json.Unmarshal(prev, &existing) == nil && eventsEqual(existing, e) {
			return nil
		}
		if len(prev) > 0 {
			return fmt.Errorf("change: duplicate change_id %q with conflicting payload", e.ChangeID)
		}
	}
	raw, err := json.MarshalIndent(cloneEvent(e), "", "  ")
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
func (s *FileStore) Get(changeID string) (Event, bool, error) {
	if strings.TrimSpace(changeID) == "" {
		return Event{}, false, fmt.Errorf("change: change_id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.pathFor(changeID))
	if os.IsNotExist(err) {
		return Event{}, false, nil
	}
	if err != nil {
		return Event{}, false, err
	}
	var e Event
	if err := json.Unmarshal(raw, &e); err != nil {
		return Event{}, false, err
	}
	return e, true, nil
}

// List implements Store.
func (s *FileStore) List() ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.changesDir())
	if os.IsNotExist(err) {
		return []Event{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Event
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.changesDir(), ent.Name()))
		if err != nil {
			return nil, err
		}
		var e Event
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return Compare(out[i], out[j]) < 0
	})
	if out == nil {
		out = []Event{}
	}
	return out, nil
}

// ListForService implements Store.
func (s *FileStore) ListForService(serviceID string) ([]Event, error) {
	all, err := s.List()
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

func (s *FileStore) pathFor(id string) string {
	safe := strings.ReplaceAll(id, "/", "_")
	safe = strings.ReplaceAll(safe, "|", "_")
	return filepath.Join(s.changesDir(), safe+".json")
}
