package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/yashg4509/perch/internal/pulse/investigation"
)

// InvestigationSource loads immutable F1 investigations.
type InvestigationSource interface {
	Get(investigationID string) (investigation.Investigation, bool, error)
}

// Service generates and persists constrained LLM proposals.
type Service struct {
	Config         Config
	Investigations InvestigationSource
	Store          *FileStore
	Client         ChatClient
	Now            func() time.Time

	sem onceSemaphore
	mu  sync.Mutex
}

type onceSemaphore struct {
	once sync.Once
	ch   chan struct{}
	n    int
}

func (s *onceSemaphore) init(n int) {
	s.once.Do(func() {
		if n < 1 {
			n = 1
		}
		s.n = n
		s.ch = make(chan struct{}, n)
	})
}

func (s *onceSemaphore) acquire() bool {
	if s.ch == nil {
		return false
	}
	select {
	case s.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *onceSemaphore) release() {
	if s.ch == nil {
		return
	}
	select {
	case <-s.ch:
	default:
	}
}

// Status returns secret-free availability.
func (s *Service) Status() Status {
	if s == nil {
		return Status{
			SchemaVersion: StatusSchemaVersion,
			Available:     false,
			Reason:        "LLM investigation service is not configured",
			Provider:      providerName,
		}
	}
	return s.Config.Status()
}

// ProposeOptions control a single explicit generation request.
type ProposeOptions struct {
	InvestigationID string
	// ExpectedFingerprint when set must match the F1 snapshot fingerprint.
	ExpectedFingerprint string
}

// Propose generates a validated proposal from an F1 investigation.
// No background calls; caller must invoke explicitly (POST / CLI).
func (s *Service) Propose(ctx context.Context, opts ProposeOptions) (Proposal, error) {
	if s == nil || s.Investigations == nil || s.Store == nil {
		return Proposal{}, &UnavailableError{Reason: "LLM investigation service is not configured"}
	}
	if !s.Config.Available() {
		return Proposal{}, &UnavailableError{Reason: s.Config.UnavailabilityReason()}
	}
	if s.Client == nil {
		return Proposal{}, &UnavailableError{Reason: "LLM provider client is not configured"}
	}
	id := strings.TrimSpace(opts.InvestigationID)
	if id == "" {
		return Proposal{}, &ValidationError{Msg: "investigation_id is required"}
	}

	s.sem.init(s.Config.MaxConcurrent)
	if !s.sem.acquire() {
		return Proposal{}, &BusyError{}
	}
	defer s.sem.release()

	inv, ok, err := s.Investigations.Get(id)
	if err != nil {
		return Proposal{}, err
	}
	if !ok {
		return Proposal{}, &investigation.NotFoundError{Kind: "investigation", ID: id}
	}
	if opts.ExpectedFingerprint != "" && opts.ExpectedFingerprint != inv.ContentFingerprint {
		return Proposal{}, &ValidationError{Msg: "source fingerprint mismatch"}
	}

	payload, err := BuildEvidencePayload(inv)
	if err != nil {
		return Proposal{}, err
	}
	userMsg, err := BuildUserMessage(payload)
	if err != nil {
		return Proposal{}, err
	}

	timeout := s.Config.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	chatReq := ChatRequest{
		Model:       s.Config.Model,
		System:      SystemPrompt(),
		User:        userMsg,
		MaxTokens:   s.Config.MaxTokens,
		Temperature: 0,
	}
	resp, err := s.Client.Complete(callCtx, chatReq)
	if err != nil {
		if callCtx.Err() == context.DeadlineExceeded {
			return Proposal{}, fmt.Errorf("llm: provider timeout after %s", timeout)
		}
		return Proposal{}, err
	}
	// Truncated completions are untrusted incomplete JSON even if a parser recovers.
	if isTruncatedFinish(resp.FinishReason) {
		return Proposal{}, &ValidationError{Msg: "provider truncated output (finish_reason=length); incomplete response rejected"}
	}

	prop, err := ParseAndValidateModelOutput(resp.Content, inv, payload)
	if err != nil {
		return Proposal{}, err
	}

	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	prop.GeneratedAt = now
	prop.ProposalID = NewProposalID(inv.InvestigationID, now)
	prop.Provenance = Provenance{
		Provider:           providerName,
		Model:              firstNonEmpty(resp.Model, s.Config.Model),
		FinishReason:       resp.FinishReason,
		PromptUsageUnits:   resp.PromptTokens,
		CompleteUsageUnits: resp.CompletionTokens,
		RequestID:          resp.RequestID,
	}
	if u, err := url.Parse(s.Config.BaseURL); err == nil {
		prop.Provenance.EndpointHost = u.Host
	}
	prop = RedactProposal(prop)
	if err := prop.Validate(); err != nil {
		return Proposal{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Store.Put(prop); err != nil {
		return Proposal{}, err
	}
	return prop, nil
}

// Get returns a persisted proposal.
func (s *Service) Get(proposalID string) (Proposal, error) {
	if s == nil || s.Store == nil {
		return Proposal{}, fmt.Errorf("llm: service not configured")
	}
	p, ok, err := s.Store.Get(proposalID)
	if err != nil {
		return Proposal{}, err
	}
	if !ok {
		return Proposal{}, &investigation.NotFoundError{Kind: "proposal", ID: proposalID}
	}
	return p, nil
}

// ListForInvestigation lists proposals for an F1 investigation.
func (s *Service) ListForInvestigation(investigationID string) ([]Proposal, error) {
	if s == nil || s.Store == nil {
		return nil, fmt.Errorf("llm: service not configured")
	}
	return s.Store.ListForInvestigation(investigationID)
}

// AddFeedback validates and persists proposal feedback.
func (s *Service) AddFeedback(fb Feedback) (Feedback, error) {
	if s == nil || s.Store == nil {
		return Feedback{}, fmt.Errorf("llm: service not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	prop, ok, err := s.Store.Get(fb.ProposalID)
	if err != nil {
		return Feedback{}, err
	}
	if !ok {
		return Feedback{}, &investigation.NotFoundError{Kind: "proposal", ID: fb.ProposalID}
	}
	if fb.SourceInvestigationID == "" {
		fb.SourceInvestigationID = prop.SourceInvestigationID
	}
	if fb.SchemaVersion == "" {
		fb.SchemaVersion = ProposalFeedbackSchemaVersion
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	if fb.Timestamp.IsZero() {
		fb.Timestamp = now
	} else {
		fb.Timestamp = fb.Timestamp.UTC()
	}
	if fb.FeedbackID == "" {
		fb.FeedbackID = NewFeedbackID(fb.ProposalID, fb.Timestamp)
	}
	fb.Limitations = FeedbackLimitations()

	known := map[string]struct{}{}
	for _, h := range prop.Hypotheses {
		known[h.HypothesisID] = struct{}{}
	}
	for _, id := range fb.UsefulHypothesisIDs {
		if _, ok := known[id]; !ok {
			return Feedback{}, &ValidationError{Msg: "unknown useful hypothesis " + id}
		}
	}
	for _, id := range fb.RejectedHypothesisIDs {
		if _, ok := known[id]; !ok {
			return Feedback{}, &ValidationError{Msg: "unknown rejected hypothesis " + id}
		}
	}
	if !investigation.ValidAssessment(fb.Assessment) {
		return Feedback{}, &ValidationError{Msg: "invalid assessment"}
	}
	fb = RedactFeedback(fb)
	if err := ValidateFeedback(fb); err != nil {
		return Feedback{}, &ValidationError{Msg: err.Error()}
	}
	if err := s.Store.PutFeedback(fb); err != nil {
		return Feedback{}, err
	}
	return fb, nil
}

// ListFeedback returns feedback for a proposal.
func (s *Service) ListFeedback(proposalID string) ([]Feedback, error) {
	if s == nil || s.Store == nil {
		return nil, fmt.Errorf("llm: service not configured")
	}
	if _, ok, err := s.Store.Get(proposalID); err != nil {
		return nil, err
	} else if !ok {
		return nil, &investigation.NotFoundError{Kind: "proposal", ID: proposalID}
	}
	return s.Store.ListFeedback(proposalID)
}

// NewHTTPClientFromConfig builds the default OpenAI-compatible HTTP client
// wrapped with bounded retries for transient 429/5xx failures only.
func NewHTTPClientFromConfig(cfg Config) ChatClient {
	inner := &HTTPChatClient{
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
		HTTPClient: &http.Client{
			Timeout: cfg.Timeout,
			// Refuse redirects so Authorization is never forwarded to an unvalidated URL.
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	return &RetryingChatClient{Inner: inner, MaxAttempts: defaultRetryAttempts, Backoff: defaultRetryBackoff}
}

func isTruncatedFinish(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "length", "max_tokens":
		return true
	default:
		return false
	}
}

// InvestigationAdapter adapts investigation.Service / FileStore to InvestigationSource.
type InvestigationAdapter struct {
	Store *investigation.FileStore
	Svc   *investigation.Service
}

// Get loads an investigation.
func (a InvestigationAdapter) Get(id string) (investigation.Investigation, bool, error) {
	if a.Svc != nil {
		inv, err := a.Svc.Get(id)
		if err != nil {
			if _, ok := err.(*investigation.NotFoundError); ok {
				return investigation.Investigation{}, false, nil
			}
			return investigation.Investigation{}, false, err
		}
		return inv, true, nil
	}
	if a.Store == nil {
		return investigation.Investigation{}, false, fmt.Errorf("llm: investigation source missing")
	}
	return a.Store.Get(id)
}
