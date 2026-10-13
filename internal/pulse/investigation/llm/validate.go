package llm

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yashg4509/perch/internal/pulse/investigation"
)

// modelOutput is the JSON object the model must emit.
type modelOutput struct {
	SchemaVersion         string            `json:"schema_version"`
	SourceInvestigationID string            `json:"source_investigation_id"`
	SourceFingerprint     string            `json:"source_fingerprint"`
	Diagnosed             bool              `json:"diagnosed"`
	Hypotheses            []modelHypothesis `json:"hypotheses"`
	MissingEvidence       []string          `json:"missing_evidence"`
	Unknowns              []string          `json:"unknowns"`
	SuggestedVerification []string          `json:"suggested_verification_steps"`
	Limitations           []string          `json:"limitations"`
}

type modelHypothesis struct {
	HypothesisID           string   `json:"hypothesis_id"`
	Statement              string   `json:"statement"`
	Classification         string   `json:"classification"`
	SupportingEvidenceRefs []string `json:"supporting_evidence_refs"`
	CounterEvidenceRefs    []string `json:"counter_evidence_refs"`
	MissingEvidence        []string `json:"missing_evidence"`
	SuggestedVerification  string   `json:"suggested_verification"`
	Kind                   string   `json:"kind"`
}

var (
	confirmedCauseRE = regexp.MustCompile(`(?i)\b(root\s*cause\s+is|confirmed\s+root\s*cause|definitely\s+caused|proven\s+causation|we\s+diagnosed|is\s+the\s+root\s*cause)\b`)
	// causalClaimRE rejects assertive causation phrasing even when diagnosed=false.
	// Lexical only — not entailment. Validated ≠ proven support.
	causalClaimRE      = regexp.MustCompile(`(?i)\b(caused\s+by|was\s+caused|this\s+caused|deployment\s+caused|change\s+caused|due\s+to\s+the\s+(deploy|deployment|change|release)|proves?\s+that|is\s+proof\s+that|therefore\s+the\s+root)\b`)
	confidencePctRE    = regexp.MustCompile(`(?i)\b(\d{1,3})\s*%\s*(confident|confidence|certainty)\b|\bconfidence\s*[:=]\s*\d`)
	fabricatedMetricRE = regexp.MustCompile(`(?i)\b(p99|error[_ ]?budget|slo\s*burn|mttr|cpu\s*=\s*\d|memory\s*=\s*\d)`)
)

// ParseAndValidateModelOutput parses model JSON and validates against the source investigation + payload.
func ParseAndValidateModelOutput(raw string, inv investigation.Investigation, payload EvidencePayload) (Proposal, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Proposal{}, fmtValidation("empty model output")
	}
	// Strip accidental markdown fences.
	if strings.HasPrefix(raw, "```") {
		raw = stripFence(raw)
	}
	var out modelOutput
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		// Retry without DisallowUnknownFields for slightly divergent providers.
		if err2 := json.Unmarshal([]byte(raw), &out); err2 != nil {
			return Proposal{}, fmtValidation("malformed model JSON: %v", err)
		}
	}
	return validateModelOutput(out, inv, payload)
}

func stripFence(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```JSON")
	s = strings.TrimPrefix(s, "```")
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func validateModelOutput(out modelOutput, inv investigation.Investigation, payload EvidencePayload) (Proposal, error) {
	notes := []string{}
	if out.SchemaVersion != "" && out.SchemaVersion != ModelOutputSchemaVersion && out.SchemaVersion != ProposalSchemaVersion {
		return Proposal{}, fmtValidation("unsupported model schema_version %q", out.SchemaVersion)
	}
	if out.Diagnosed {
		return Proposal{}, fmtValidation("model set diagnosed=true (rejected)")
	}
	if out.SourceInvestigationID != "" && out.SourceInvestigationID != inv.InvestigationID {
		return Proposal{}, fmtValidation("source_investigation_id mismatch")
	}
	if out.SourceFingerprint != "" && out.SourceFingerprint != inv.ContentFingerprint {
		return Proposal{}, fmtValidation("source_fingerprint mismatch")
	}
	if out.SourceFingerprint == "" {
		notes = append(notes, "model omitted source_fingerprint; filled from F1 snapshot")
	}
	if out.SourceInvestigationID == "" {
		notes = append(notes, "model omitted source_investigation_id; filled from F1 snapshot")
	}

	citeOK := map[string]struct{}{}
	for _, id := range payload.AllowedCitationIDs {
		citeOK[id] = struct{}{}
	}
	svcOK := map[string]struct{}{}
	for _, id := range payload.AllowedServiceIDs {
		svcOK[id] = struct{}{}
	}

	var accepted []Hypothesis
	var rejected []Hypothesis
	seenIDs := map[string]struct{}{}

	hyps := out.Hypotheses
	if len(hyps) > MaxHypotheses {
		notes = append(notes, fmt.Sprintf("truncated hypotheses from %d to %d", len(hyps), MaxHypotheses))
		hyps = hyps[:MaxHypotheses]
	}

	for i, mh := range hyps {
		h, rejReason, note := validateOneHypothesis(mh, i, citeOK, svcOK, inv, seenIDs)
		if note != "" {
			notes = append(notes, note)
		}
		if rejReason != "" {
			h.SupportStatus = "unverified"
			h.RejectedReason = rejReason
			rejected = append(rejected, h)
			continue
		}
		h.SupportStatus = "validated"
		accepted = append(accepted, h)
	}

	if len(accepted) == 0 && len(rejected) > 0 {
		notes = append(notes, "all model hypotheses were rejected by validation policy")
	}

	prop := Proposal{
		SchemaVersion:         ProposalSchemaVersion,
		SourceInvestigationID: inv.InvestigationID,
		SourceIncidentID:      inv.IncidentID,
		SourceFingerprint:     inv.ContentFingerprint,
		GenerationMethod:      GenerationMethodLLM,
		Diagnosed:             false,
		Hypotheses:            accepted,
		RejectedHypotheses:    rejected,
		MissingEvidence:       clipLoose(out.MissingEvidence, 12, MaxMissingItemRunes),
		Unknowns:              ensureUnknowns(clipLoose(out.Unknowns, 12, MaxMissingItemRunes)),
		SuggestedVerification: clipLoose(out.SuggestedVerification, 8, MaxVerificationRunes),
		Limitations:           uniqueStrings(append(DefaultLimitations(), clipLoose(out.Limitations, 8, MaxMissingItemRunes)...)),
		ValidationNotes:       notes,
	}
	if err := prop.ValidateContent(); err != nil {
		return Proposal{}, err
	}
	return prop, nil
}

func validateOneHypothesis(
	mh modelHypothesis,
	idx int,
	citeOK, svcOK map[string]struct{},
	inv investigation.Investigation,
	seenIDs map[string]struct{},
) (Hypothesis, string, string) {
	id := strings.TrimSpace(mh.HypothesisID)
	if id == "" {
		id = fmt.Sprintf("hyp|llm|%d", idx+1)
	}
	if _, dup := seenIDs[id]; dup {
		return Hypothesis{}, "duplicate hypothesis_id", ""
	}
	seenIDs[id] = struct{}{}

	stmt := strings.TrimSpace(mh.Statement)
	h := Hypothesis{
		HypothesisID:           id,
		Statement:              stmt,
		Classification:         investigation.ClassInferred,
		SupportingEvidenceRefs: cleanRefs(mh.SupportingEvidenceRefs),
		CounterEvidenceRefs:    cleanRefs(mh.CounterEvidenceRefs),
		MissingEvidence:        clipLoose(mh.MissingEvidence, 6, MaxMissingItemRunes),
		SuggestedVerification:  strings.TrimSpace(mh.SuggestedVerification),
		Kind:                   strings.TrimSpace(mh.Kind),
	}
	if h.Kind == "" {
		h.Kind = "llm_hypothesis"
	}
	if stmt == "" {
		return h, "empty statement", ""
	}
	if utf8.RuneCountInString(stmt) > MaxStatementRunes {
		return h, "statement too long", ""
	}
	class := strings.ToUpper(strings.TrimSpace(mh.Classification))
	if class != "" && class != string(investigation.ClassInferred) {
		return h, "classification must be INFERRED", ""
	}
	if confirmedCauseRE.MatchString(stmt) || confirmedCauseRE.MatchString(h.SuggestedVerification) {
		return h, "unsupported confirmed-root-cause language", ""
	}
	if causalClaimRE.MatchString(stmt) {
		return h, "causal attribution language is not allowed (correlation ≠ causation)", ""
	}
	if confidencePctRE.MatchString(stmt) {
		return h, "confidence percentages are not allowed", ""
	}
	if fabricatedMetricRE.MatchString(stmt) && !statementSupportedByObservedMetrics(stmt, inv) {
		return h, "invented metric language without supporting observed evidence", ""
	}
	if len(h.SupportingEvidenceRefs) == 0 {
		return h, "missing supporting_evidence_refs", ""
	}
	for _, ref := range h.SupportingEvidenceRefs {
		if _, ok := citeOK[ref]; !ok {
			return h, fmt.Sprintf("fabricated evidence id %q", ref), ""
		}
	}
	for _, ref := range h.CounterEvidenceRefs {
		if _, ok := citeOK[ref]; !ok {
			return h, fmt.Sprintf("fabricated counter-evidence id %q", ref), ""
		}
	}
	// Soft check: service IDs mentioned as astronomy/... must be allowlisted.
	for _, tok := range extractServiceTokens(stmt) {
		if _, ok := svcOK[tok]; !ok {
			return h, fmt.Sprintf("fabricated service id %q", tok), ""
		}
	}
	note := ""
	if !citationsPlausiblySupport(stmt, h.SupportingEvidenceRefs, inv) {
		// Spec: do not treat syntactically valid citations as proof of support.
		// Policy: keep as unverified rather than hard-reject when citations resolve.
		h.SupportStatus = "unverified"
		note = fmt.Sprintf("hypothesis %s citations resolve but claim support is unverified", id)
		return h, "citation support unverified for claim wording", note
	}
	return h, "", note
}

func cleanRefs(in []string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, r := range in {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
		if len(out) >= 12 {
			break
		}
	}
	return out
}

func clipLoose(in []string, maxItems, maxRunes int) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if utf8.RuneCountInString(s) > maxRunes {
			s = string([]rune(s)[:maxRunes]) + "…"
		}
		out = append(out, s)
		if len(out) >= maxItems {
			break
		}
	}
	return out
}

func ensureUnknowns(in []string) []string {
	has := false
	for _, u := range in {
		if strings.Contains(strings.ToUpper(u), "UNKNOWN") || strings.Contains(strings.ToLower(u), "root cause") {
			has = true
			break
		}
	}
	if !has {
		in = append([]string{"Root cause is UNKNOWN — LLM proposal does not diagnose the incident."}, in...)
	}
	return in
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

var serviceTokenRE = regexp.MustCompile(`astronomy/[a-z0-9_./-]+`)

func extractServiceTokens(stmt string) []string {
	return serviceTokenRE.FindAllString(stmt, -1)
}

func statementSupportedByObservedMetrics(stmt string, inv investigation.Investigation) bool {
	// Only allow metric-ish language when baselines/symptoms exist for the same signal words.
	lower := strings.ToLower(stmt)
	for _, s := range inv.ObservedSymptoms {
		if strings.Contains(lower, strings.ToLower(string(s.SignalType))) {
			return true
		}
	}
	for _, bc := range inv.BaselineComparisons {
		if strings.Contains(lower, strings.ToLower(string(bc.SignalType))) {
			return true
		}
	}
	return false
}

// citationsPlausiblySupport is a conservative lexical check — not NLP entailment.
// Returns false when the statement asserts specifics absent from cited evidence summaries.
func citationsPlausiblySupport(stmt string, refs []string, inv investigation.Investigation) bool {
	if len(refs) == 0 {
		return false
	}
	// Build bag of evidence text from cited refs.
	bag := strings.ToLower(strings.Join(evidenceTextForRefs(refs, inv), " "))
	if strings.TrimSpace(bag) == "" {
		return false
	}
	lower := strings.ToLower(stmt)
	// If statement invents a change id not present in bag/refs, fail.
	for _, m := range regexp.MustCompile(`change:[a-zA-Z0-9_./|-]+`).FindAllString(lower, -1) {
		found := false
		for _, r := range refs {
			if strings.EqualFold(r, m) || strings.EqualFold("change:"+strings.TrimPrefix(r, "change:"), m) {
				found = true
				break
			}
		}
		if !found && !strings.Contains(bag, m) {
			return false
		}
	}
	// Require at least one overlapping content word of length >= 4 with evidence bag
	// (beyond stopwords), OR a direct ref id mention.
	for _, r := range refs {
		if strings.Contains(lower, strings.ToLower(r)) {
			return true
		}
	}
	words := strings.FieldsFunc(lower, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-')
	})
	overlap := 0
	for _, w := range words {
		if len(w) < 4 {
			continue
		}
		switch w {
		case "that", "this", "with", "from", "during", "service", "incident", "hypothesis", "inferred", "because":
			continue
		}
		if strings.Contains(bag, w) {
			overlap++
		}
	}
	return overlap >= 1
}

func evidenceTextForRefs(refs []string, inv investigation.Investigation) []string {
	want := map[string]struct{}{}
	for _, r := range refs {
		want[r] = struct{}{}
	}
	var parts []string
	for _, e := range inv.SourceReferences {
		if _, ok := want[e.Ref]; ok {
			parts = append(parts, e.Ref, e.Summary)
		}
	}
	for _, s := range inv.ObservedSymptoms {
		if _, ok := want[s.ID]; ok {
			parts = append(parts, s.ID, s.Statement, string(s.SignalType), s.ServiceID)
		}
	}
	for _, bc := range inv.BaselineComparisons {
		cid := fmt.Sprintf("baseline:%s:%s", bc.ServiceID, bc.SignalType)
		if _, ok := want[cid]; ok {
			parts = append(parts, cid, string(bc.SignalType), bc.ServiceID, bc.Direction)
		}
	}
	for _, c := range inv.ChangeCandidates {
		cid := "change:" + c.ChangeID
		if _, ok := want[cid]; ok {
			parts = append(parts, cid, c.ChangeID, c.RankingBasis)
			parts = append(parts, c.Observations...)
		}
	}
	for _, ph := range []string{"phase:before", "phase:during", "phase:after"} {
		if _, ok := want[ph]; ok {
			parts = append(parts, ph)
		}
	}
	return parts
}
