package llm

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/investigation"
)

const (
	// SmokeResultSchemaVersion versions redacted provider smoke summaries.
	SmokeResultSchemaVersion = "pulse.investigation.llm_smoke.v1"
	// DocumentedCompatibleModel is the single F2a-compatible model used for
	// real-provider smoke documentation and optional cost estimates.
	DocumentedCompatibleModel = "gpt-4o-mini"
	// DocumentedCompatibleProvider names the OpenAI-compatible contract.
	DocumentedCompatibleProvider = providerName
)

// SmokeMode selects offline fixture validation vs explicit real-provider mode.
type SmokeMode string

const (
	SmokeModeOffline SmokeMode = "offline"
	SmokeModeReal    SmokeMode = "real"
)

// SmokeOptions configure a single provider compatibility smoke run.
type SmokeOptions struct {
	Mode                SmokeMode
	Investigation       investigation.Investigation
	CaseID              string
	ExpectedFingerprint string
	Persist             bool
	// AuthorizePaidCalls must be true for SmokeModeReal (CLI flag).
	AuthorizePaidCalls bool
	// Client overrides the HTTP client (tests / offline fixture).
	Client ChatClient
	Config Config
	Store  *FileStore
	Now    func() time.Time
}

// SmokeResult is a redacted compatibility summary — never includes secrets,
// full prompts, or unredacted incident payloads.
type SmokeResult struct {
	SchemaVersion        string   `json:"schema_version"`
	Mode                 string   `json:"mode"`
	CaseID               string   `json:"case_id,omitempty"`
	InvestigationID      string   `json:"investigation_id"`
	EvidenceFingerprint  string   `json:"evidence_fingerprint"`
	FingerprintValidated bool     `json:"fingerprint_validated"`
	ProposalID           string   `json:"proposal_id,omitempty"`
	Persisted            bool     `json:"persisted"`
	SchemaValid          bool     `json:"schema_valid"`
	CitationsResolved    bool     `json:"citations_resolved"`
	SupportStatus        string   `json:"support_status,omitempty"`
	Provider             string   `json:"provider"`
	Model                string   `json:"model,omitempty"`
	EndpointHost         string   `json:"endpoint_host,omitempty"`
	FinishReason         string   `json:"finish_reason,omitempty"`
	LatencyMS            int64    `json:"latency_ms"`
	PromptTokens         int      `json:"prompt_tokens,omitempty"`
	CompletionTokens     int      `json:"completion_tokens,omitempty"`
	EstimatedCostUSD     *float64 `json:"estimated_cost_usd,omitempty"`
	CostEstimateNote     string   `json:"cost_estimate_note,omitempty"`
	RealProviderExecuted bool     `json:"real_provider_executed"`
	CompatibilityOK      bool     `json:"compatibility_ok"`
	RedactedSummary      string   `json:"redacted_summary"`
	HypothesisCount      int      `json:"hypothesis_count"`
	ValidationNotes      []string `json:"validation_notes,omitempty"`
	Limitations          []string `json:"limitations"`
	Error                string   `json:"error,omitempty"`
}

// RunProviderSmoke exercises the F2a propose pipeline against one investigation.
// Default/offline mode never contacts a network provider.
func RunProviderSmoke(ctx context.Context, opts SmokeOptions) (SmokeResult, error) {
	inv := opts.Investigation
	if err := inv.Validate(); err != nil {
		return SmokeResult{}, fmt.Errorf("llm smoke: invalid investigation: %w", err)
	}
	mode := opts.Mode
	if mode == "" {
		mode = SmokeModeOffline
	}
	res := SmokeResult{
		SchemaVersion:       SmokeResultSchemaVersion,
		Mode:                string(mode),
		CaseID:              opts.CaseID,
		InvestigationID:     inv.InvestigationID,
		EvidenceFingerprint: inv.ContentFingerprint,
		Provider:            DocumentedCompatibleProvider,
		Limitations:         smokeLimitations(mode),
	}

	expected := strings.TrimSpace(opts.ExpectedFingerprint)
	if expected != "" && expected != inv.ContentFingerprint {
		res.Error = "source fingerprint mismatch"
		return res, &ValidationError{Msg: res.Error}
	}
	res.FingerprintValidated = true

	payload, err := BuildEvidencePayload(inv)
	if err != nil {
		res.Error = "evidence payload build failed"
		return res, err
	}

	cfg := opts.Config
	client := opts.Client
	realExec := false

	switch mode {
	case SmokeModeOffline:
		if client == nil {
			sid := firstSymptomID(inv)
			svcID := inv.Topology.PrimaryServiceID
			client = &FakeClient{
				Response: ChatResponse{
					Content:          ExampleValidModelJSON(inv.InvestigationID, inv.ContentFingerprint, sid, svcID),
					FinishReason:     "stop",
					Model:            DocumentedCompatibleModel,
					PromptTokens:     128,
					CompletionTokens: 256,
					RequestID:        "offline-fixture",
				},
			}
		}
		if !cfg.Enabled {
			cfg.Enabled = true
			cfg.APIKey = "offline-fixture-key"
			cfg.BaseURL = "http://127.0.0.1:9/v1"
			cfg.Model = DocumentedCompatibleModel
			cfg.Timeout = 5 * time.Second
			cfg.MaxTokens = defaultMaxTokens
			cfg.MaxConcurrent = 1
		}
	case SmokeModeReal:
		if !opts.AuthorizePaidCalls {
			res.Error = "real provider mode requires explicit authorization flag"
			return res, &ValidationError{Msg: res.Error}
		}
		if !cfg.Available() {
			res.Error = cfg.UnavailabilityReason()
			return res, &UnavailableError{Reason: res.Error}
		}
		if client == nil {
			client = NewHTTPClientFromConfig(cfg)
		}
		realExec = true
	default:
		return res, &ValidationError{Msg: "smoke mode must be offline or real"}
	}

	// Persist path uses Service.Propose (requires store). Otherwise validate-only.
	if opts.Persist && opts.Store != nil {
		svc := &Service{
			Config:         cfg,
			Investigations: staticInvestigationSource{inv: inv},
			Store:          opts.Store,
			Client:         client,
			Now:            opts.Now,
		}
		start := time.Now()
		prop, err := svc.Propose(ctx, ProposeOptions{
			InvestigationID:     inv.InvestigationID,
			ExpectedFingerprint: inv.ContentFingerprint,
		})
		res.LatencyMS = time.Since(start).Milliseconds()
		res.RealProviderExecuted = realExec
		if err != nil {
			res.Error = redactSmokeErr(err)
			res.CompatibilityOK = false
			return res, err
		}
		res.Persisted = true
		return finishSmoke(fillSmokeFromProposal(res, prop))
	}

	return runSmokeValidateOnly(ctx, inv, payload, client, cfg, res, realExec)
}

func finishSmoke(res SmokeResult) (SmokeResult, error) {
	if res.CompatibilityOK {
		return res, nil
	}
	msg := res.Error
	if msg == "" {
		msg = "llm provider smoke: compatibility failure"
	}
	return res, &ValidationError{Msg: msg}
}

func runSmokeValidateOnly(ctx context.Context, inv investigation.Investigation, payload EvidencePayload, client ChatClient, cfg Config, res SmokeResult, realExec bool) (SmokeResult, error) {
	userMsg, err := BuildUserMessage(payload)
	if err != nil {
		res.Error = "user message build failed"
		return res, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if cfg.MaxTokens < 64 {
		cfg.MaxTokens = defaultMaxTokens
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	resp, err := client.Complete(callCtx, ChatRequest{
		Model:       firstNonEmpty(cfg.Model, DocumentedCompatibleModel),
		System:      SystemPrompt(),
		User:        userMsg,
		MaxTokens:   cfg.MaxTokens,
		Temperature: 0,
	})
	res.LatencyMS = time.Since(start).Milliseconds()
	res.RealProviderExecuted = realExec
	if err != nil {
		if callCtx.Err() == context.DeadlineExceeded {
			res.Error = fmt.Sprintf("provider timeout after %s", timeout)
			return res, fmt.Errorf("llm: %s", res.Error)
		}
		res.Error = redactSmokeErr(err)
		return res, err
	}
	if isTruncatedFinish(resp.FinishReason) {
		res.Error = "provider truncated output (finish_reason=length)"
		return res, &ValidationError{Msg: res.Error}
	}
	prop, err := ParseAndValidateModelOutput(resp.Content, inv, payload)
	if err != nil {
		res.Error = redactSmokeErr(err)
		return res, err
	}
	prop.Provenance = Provenance{
		Provider:           providerName,
		Model:              firstNonEmpty(resp.Model, cfg.Model),
		FinishReason:       resp.FinishReason,
		PromptUsageUnits:   resp.PromptTokens,
		CompleteUsageUnits: resp.CompletionTokens,
		RequestID:          resp.RequestID,
	}
	if u, err := url.Parse(cfg.BaseURL); err == nil {
		prop.Provenance.EndpointHost = u.Host
	}
	prop = RedactProposal(prop)
	res.Persisted = false
	return finishSmoke(fillSmokeFromProposal(res, prop))
}

func fillSmokeFromProposal(res SmokeResult, prop Proposal) SmokeResult {
	res.ProposalID = prop.ProposalID
	res.SchemaValid = true
	res.CitationsResolved = len(prop.RejectedHypotheses) == 0 && len(prop.Hypotheses) > 0
	if len(prop.Hypotheses) > 0 {
		res.SupportStatus = prop.Hypotheses[0].SupportStatus
	} else if len(prop.RejectedHypotheses) > 0 {
		res.SupportStatus = "unverified"
		res.Error = "no accepted hypotheses after citation/policy validation"
	}
	res.Model = prop.Provenance.Model
	res.EndpointHost = prop.Provenance.EndpointHost
	res.FinishReason = prop.Provenance.FinishReason
	res.PromptTokens = prop.Provenance.PromptUsageUnits
	res.CompletionTokens = prop.Provenance.CompleteUsageUnits
	res.HypothesisCount = len(prop.Hypotheses)
	res.ValidationNotes = append([]string{}, prop.ValidationNotes...)
	res.CompatibilityOK = res.SchemaValid && res.FingerprintValidated && res.CitationsResolved && res.Error == ""
	res.RedactedSummary = fmt.Sprintf(
		"mode=%s case=%s inv=%s fp=%s schema_valid=%v hyps=%d support=%s latency_ms=%d tokens=%d/%d real=%v",
		res.Mode, emptyDash(res.CaseID), res.InvestigationID, shortFP(res.EvidenceFingerprint),
		res.SchemaValid, res.HypothesisCount, emptyDash(res.SupportStatus), res.LatencyMS,
		res.PromptTokens, res.CompletionTokens, res.RealProviderExecuted,
	)
	if cost, note, ok := EstimateCostUSD(res.Model, res.PromptTokens, res.CompletionTokens); ok {
		res.EstimatedCostUSD = &cost
		res.CostEstimateNote = note
	} else {
		res.CostEstimateNote = note
	}
	if !res.CompatibilityOK && res.Error != "" {
		return res
	}
	return res
}

// EstimateCostUSD returns a grounded estimate only for the documented model.
// Pricing constants are from public OpenAI gpt-4o-mini list prices as of 2026-10-01
// ($0.15 / 1M input, $0.60 / 1M output). Not a billing guarantee.
func EstimateCostUSD(model string, promptTokens, completionTokens int) (float64, string, bool) {
	m := strings.TrimSpace(model)
	if m != DocumentedCompatibleModel && !strings.HasPrefix(m, DocumentedCompatibleModel+"-") {
		return 0, "cost estimate unavailable for model " + m + " (no grounded pricing table entry)", false
	}
	if promptTokens < 0 || completionTokens < 0 {
		return 0, "cost estimate unavailable: negative token counts", false
	}
	if promptTokens == 0 && completionTokens == 0 {
		return 0, "cost estimate unavailable: no token usage reported", false
	}
	const (
		inputPerMillion  = 0.15
		outputPerMillion = 0.60
	)
	cost := (float64(promptTokens)/1_000_000.0)*inputPerMillion +
		(float64(completionTokens)/1_000_000.0)*outputPerMillion
	note := "estimate from public OpenAI gpt-4o-mini list prices as of 2026-10-01 ($0.15/1M in, $0.60/1M out); not a billing guarantee"
	return cost, note, true
}

func smokeLimitations(mode SmokeMode) []string {
	out := []string{
		"Compatibility smoke verifies schema/citation pipeline — not semantic entailment.",
		"Citation ID resolution ≠ proof that cited evidence supports the claim wording.",
		"Adversarial metadata in evidence remains untrusted input to the model.",
		"No autonomous remediation is performed.",
	}
	if mode == SmokeModeOffline {
		out = append(out,
			"Offline mode uses a fixture response; real provider compatibility was NOT executed.",
			"Token counts and cost estimates in offline mode are fixture-derived, not measured provider usage.",
		)
	} else {
		out = append(out, "Real provider mode requires explicit authorization and incurs provider cost.")
	}
	return out
}

func firstSymptomID(inv investigation.Investigation) string {
	if len(inv.ObservedSymptoms) > 0 {
		return inv.ObservedSymptoms[0].ID
	}
	for _, ref := range inv.SourceReferences {
		if strings.HasPrefix(ref.Ref, "symptom:") {
			return ref.Ref
		}
	}
	return "symptom:missing"
}

func shortFP(fp string) string {
	if len(fp) <= 12 {
		return fp
	}
	return fp[:12] + "…"
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func redactSmokeErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "bearer ") || strings.Contains(lower, "api_key") || strings.Contains(lower, "sk-") {
		return "provider or validation error (redacted)"
	}
	return msg
}

type staticInvestigationSource struct {
	inv investigation.Investigation
}

func (s staticInvestigationSource) Get(id string) (investigation.Investigation, bool, error) {
	if id != s.inv.InvestigationID {
		return investigation.Investigation{}, false, nil
	}
	return s.inv, true, nil
}
