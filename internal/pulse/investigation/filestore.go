package investigation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// FileStore persists investigations and feedback under Dir/investigations/.
type FileStore struct {
	Dir string
	mu  sync.Mutex
}

// NewFileStore returns a file-backed investigation store.
func NewFileStore(dir string) *FileStore {
	return &FileStore{Dir: dir}
}

func (s *FileStore) investigationsDir() string {
	return filepath.Join(s.Dir, "investigations")
}

func (s *FileStore) feedbackDir() string {
	return filepath.Join(s.Dir, "investigation-feedback")
}

func safeFileID(id string) string {
	safe := strings.ReplaceAll(id, "/", "_")
	safe = strings.ReplaceAll(safe, "|", "_")
	safe = strings.ReplaceAll(safe, "..", "_")
	if strings.Contains(safe, string(filepath.Separator)) {
		safe = strings.ReplaceAll(safe, string(filepath.Separator), "_")
	}
	return safe
}

func (s *FileStore) pathFor(id string) string {
	return filepath.Join(s.investigationsDir(), safeFileID(id)+".json")
}

func (s *FileStore) feedbackPathFor(id string) string {
	return filepath.Join(s.feedbackDir(), safeFileID(id)+".json")
}

// Put writes an investigation atomically. Existing IDs are never mutated.
func (s *FileStore) Put(inv Investigation) error {
	if err := inv.Validate(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return err
	}
	if len(raw) > MaxInvestigationBytes {
		return fmt.Errorf("investigation: snapshot exceeds size bound (%d > %d)", len(raw), MaxInvestigationBytes)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.investigationsDir(), 0o750); err != nil {
		return err
	}
	path := s.pathFor(inv.InvestigationID)
	if prev, err := os.ReadFile(path); err == nil && len(prev) > 0 {
		var existing Investigation
		if json.Unmarshal(prev, &existing) == nil {
			if existing.ContentFingerprint == inv.ContentFingerprint {
				return nil // idempotent same content
			}
			return fmt.Errorf("investigation: investigation_id %q already exists (snapshots are immutable)", inv.InvestigationID)
		}
		return fmt.Errorf("investigation: investigation_id %q already exists", inv.InvestigationID)
	}
	if err := writeAtomic(path, raw); err != nil {
		return err
	}
	return s.pruneInvestigationsLocked()
}

// Get loads an investigation by ID.
func (s *FileStore) Get(id string) (Investigation, bool, error) {
	if strings.TrimSpace(id) == "" {
		return Investigation{}, false, fmt.Errorf("investigation: investigation_id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.pathFor(id))
	if os.IsNotExist(err) {
		return Investigation{}, false, nil
	}
	if err != nil {
		return Investigation{}, false, err
	}
	var inv Investigation
	if err := json.Unmarshal(raw, &inv); err != nil {
		return Investigation{}, false, err
	}
	return inv, true, nil
}

// FindByFingerprint returns an existing investigation for incident+fingerprint.
func (s *FileStore) FindByFingerprint(incidentID, fp string) (Investigation, bool, error) {
	list, err := s.ListForIncident(incidentID)
	if err != nil {
		return Investigation{}, false, err
	}
	for _, inv := range list {
		if inv.ContentFingerprint == fp {
			return inv, true, nil
		}
	}
	return Investigation{}, false, nil
}

// ListForIncident returns investigations for an incident, newest first.
func (s *FileStore) ListForIncident(incidentID string) ([]Investigation, error) {
	all, err := s.listAllLocked(false)
	if err != nil {
		return nil, err
	}
	var out []Investigation
	for _, inv := range all {
		if inv.IncidentID == incidentID {
			out = append(out, inv)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].GeneratedAt.Equal(out[j].GeneratedAt) {
			return out[i].GeneratedAt.After(out[j].GeneratedAt)
		}
		return out[i].InvestigationID < out[j].InvestigationID
	})
	return out, nil
}

// LatestForIncident returns the newest investigation for an incident.
func (s *FileStore) LatestForIncident(incidentID string) (Investigation, bool, error) {
	list, err := s.ListForIncident(incidentID)
	if err != nil {
		return Investigation{}, false, err
	}
	if len(list) == 0 {
		return Investigation{}, false, nil
	}
	return list[0], true, nil
}

func (s *FileStore) listAllLocked(alreadyLocked bool) ([]Investigation, error) {
	if !alreadyLocked {
		s.mu.Lock()
		defer s.mu.Unlock()
	}
	entries, err := os.ReadDir(s.investigationsDir())
	if os.IsNotExist(err) {
		return []Investigation{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Investigation
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.investigationsDir(), ent.Name()))
		if err != nil {
			return nil, err
		}
		var inv Investigation
		if err := json.Unmarshal(raw, &inv); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].GeneratedAt.Equal(out[j].GeneratedAt) {
			return out[i].GeneratedAt.After(out[j].GeneratedAt)
		}
		return out[i].InvestigationID < out[j].InvestigationID
	})
	if out == nil {
		out = []Investigation{}
	}
	return out, nil
}

// PutFeedback appends feedback without modifying the investigation or incident.
func (s *FileStore) PutFeedback(fb Feedback) error {
	if err := ValidateFeedback(fb); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(RedactFeedback(fb), "", "  ")
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.feedbackDir(), 0o750); err != nil {
		return err
	}
	path := s.feedbackPathFor(fb.FeedbackID)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("investigation: feedback_id %q already exists", fb.FeedbackID)
	}
	return writeAtomic(path, raw)
}

// ListFeedback returns feedback for an investigation, oldest first.
func (s *FileStore) ListFeedback(investigationID string) ([]Feedback, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.feedbackDir())
	if os.IsNotExist(err) {
		return []Feedback{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Feedback
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.feedbackDir(), ent.Name()))
		if err != nil {
			return nil, err
		}
		var fb Feedback
		if err := json.Unmarshal(raw, &fb); err != nil {
			return nil, err
		}
		if fb.InvestigationID == investigationID {
			out = append(out, fb)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Timestamp.Equal(out[j].Timestamp) {
			return out[i].Timestamp.Before(out[j].Timestamp)
		}
		return out[i].FeedbackID < out[j].FeedbackID
	})
	if out == nil {
		out = []Feedback{}
	}
	return out, nil
}

func writeAtomic(path string, raw []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// pruneInvestigationsLocked drops oldest investigations beyond MaxRetentionInvestigations.
// Caller must hold s.mu. Feedback files are left intact (linked to deleted snapshots remain readable).
func (s *FileStore) pruneInvestigationsLocked() error {
	all, err := s.listAllLocked(true)
	if err != nil {
		return err
	}
	if len(all) <= MaxRetentionInvestigations {
		return nil
	}
	// all is newest-first; delete from the end.
	for i := MaxRetentionInvestigations; i < len(all); i++ {
		_ = os.Remove(s.pathFor(all[i].InvestigationID))
	}
	return nil
}

// NewFeedbackID builds a stable-looking feedback id.
func NewFeedbackID(investigationID string, now time.Time) string {
	ts := now.UTC().Format("20060102T150405.000000000Z")
	return fmt.Sprintf("fb|%s|%s", safeFileID(investigationID), ts)
}
