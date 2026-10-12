package llm

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

// FileStore persists proposals and proposal feedback under Dir.
type FileStore struct {
	Dir string
	mu  sync.Mutex
}

// NewFileStore returns a file-backed proposal store.
func NewFileStore(dir string) *FileStore {
	return &FileStore{Dir: dir}
}

func (s *FileStore) proposalsDir() string {
	return filepath.Join(s.Dir, "investigation-proposals")
}

func (s *FileStore) feedbackDir() string {
	return filepath.Join(s.Dir, "investigation-proposal-feedback")
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
	return filepath.Join(s.proposalsDir(), safeFileID(id)+".json")
}

func (s *FileStore) feedbackPathFor(id string) string {
	return filepath.Join(s.feedbackDir(), safeFileID(id)+".json")
}

// Put writes a proposal atomically. Existing IDs are never mutated.
func (s *FileStore) Put(p Proposal) error {
	if err := p.Validate(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(RedactProposal(p), "", "  ")
	if err != nil {
		return err
	}
	if len(raw) > MaxProposalBytes {
		return fmt.Errorf("llm: proposal exceeds size bound (%d > %d)", len(raw), MaxProposalBytes)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.proposalsDir(), 0o750); err != nil {
		return err
	}
	path := s.pathFor(p.ProposalID)
	if prev, err := os.ReadFile(path); err == nil && len(prev) > 0 {
		return fmt.Errorf("llm: proposal_id %q already exists (proposals are immutable)", p.ProposalID)
	}
	if err := writeAtomic(path, raw); err != nil {
		return err
	}
	return s.pruneLocked()
}

// Get loads a proposal by ID.
func (s *FileStore) Get(id string) (Proposal, bool, error) {
	if strings.TrimSpace(id) == "" {
		return Proposal{}, false, fmt.Errorf("llm: proposal_id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.pathFor(id))
	if os.IsNotExist(err) {
		return Proposal{}, false, nil
	}
	if err != nil {
		return Proposal{}, false, err
	}
	var p Proposal
	if err := json.Unmarshal(raw, &p); err != nil {
		return Proposal{}, false, err
	}
	return p, true, nil
}

// ListForInvestigation returns proposals for a source investigation, newest first.
func (s *FileStore) ListForInvestigation(investigationID string) ([]Proposal, error) {
	all, err := s.listAllLocked(false)
	if err != nil {
		return nil, err
	}
	var out []Proposal
	for _, p := range all {
		if p.SourceInvestigationID == investigationID {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].GeneratedAt.Equal(out[j].GeneratedAt) {
			return out[i].GeneratedAt.After(out[j].GeneratedAt)
		}
		return out[i].ProposalID < out[j].ProposalID
	})
	return out, nil
}

func (s *FileStore) listAllLocked(alreadyLocked bool) ([]Proposal, error) {
	if !alreadyLocked {
		s.mu.Lock()
		defer s.mu.Unlock()
	}
	entries, err := os.ReadDir(s.proposalsDir())
	if os.IsNotExist(err) {
		return []Proposal{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Proposal
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.proposalsDir(), ent.Name()))
		if err != nil {
			return nil, err
		}
		var p Proposal
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].GeneratedAt.Equal(out[j].GeneratedAt) {
			return out[i].GeneratedAt.After(out[j].GeneratedAt)
		}
		return out[i].ProposalID < out[j].ProposalID
	})
	if out == nil {
		out = []Proposal{}
	}
	return out, nil
}

// PutFeedback appends proposal feedback without modifying the proposal.
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
		return fmt.Errorf("llm: feedback_id %q already exists", fb.FeedbackID)
	}
	return writeAtomic(path, raw)
}

// ListFeedback returns feedback for a proposal, oldest first.
func (s *FileStore) ListFeedback(proposalID string) ([]Feedback, error) {
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
		if fb.ProposalID == proposalID {
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

func (s *FileStore) pruneLocked() error {
	all, err := s.listAllLocked(true)
	if err != nil {
		return err
	}
	if len(all) <= MaxProposalRetention {
		return nil
	}
	for i := MaxProposalRetention; i < len(all); i++ {
		_ = os.Remove(s.pathFor(all[i].ProposalID))
	}
	return nil
}

func writeAtomic(path string, raw []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// NewProposalID builds a proposal id.
func NewProposalID(investigationID string, now time.Time) string {
	ts := now.UTC().Format("20060102T150405.000000000Z")
	return fmt.Sprintf("prop|%s|%s", safeFileID(investigationID), ts)
}

// NewFeedbackID builds a proposal feedback id.
func NewFeedbackID(proposalID string, now time.Time) string {
	ts := now.UTC().Format("20060102T150405.000000000Z")
	return fmt.Sprintf("pfb|%s|%s", safeFileID(proposalID), ts)
}

// ValidateFeedback checks proposal feedback invariants.
func ValidateFeedback(fb Feedback) error {
	if fb.SchemaVersion != ProposalFeedbackSchemaVersion {
		return fmt.Errorf("llm: feedback schema_version must be %q", ProposalFeedbackSchemaVersion)
	}
	if strings.TrimSpace(fb.FeedbackID) == "" || strings.TrimSpace(fb.ProposalID) == "" {
		return fmt.Errorf("llm: feedback requires ids")
	}
	if !investigationValidAssessment(fb.Assessment) {
		return fmt.Errorf("llm: invalid assessment %q", fb.Assessment)
	}
	if fb.Timestamp.IsZero() || fb.Timestamp.Location() != time.UTC {
		return fmt.Errorf("llm: feedback timestamp must be UTC")
	}
	return nil
}

func investigationValidAssessment(a string) bool {
	switch a {
	case "useful", "partially_useful", "not_useful", "insufficient_evidence":
		return true
	default:
		return false
	}
}
