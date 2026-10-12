package investigation

import (
	"regexp"
	"strings"
)

var bearerOrKeyRE = regexp.MustCompile(`(?i)(bearer\s+[a-z0-9._\-]{8,}|sk-[a-z0-9]{16,}|api[_-]?key\s*[:=]\s*\S+)`)

func redactText(s string) string {
	if s == "" {
		return s
	}
	return bearerOrKeyRE.ReplaceAllString(s, "[redacted]")
}

func redactStrings(in []string) []string {
	if len(in) == 0 {
		return in
	}
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = redactText(s)
	}
	return out
}

// RedactInvestigation strips secret-shaped substrings from free text fields.
func RedactInvestigation(inv Investigation) Investigation {
	out := inv
	out.Limitations = redactStrings(inv.Limitations)
	out.MissingEvidence = redactStrings(inv.MissingEvidence)
	out.Unknowns = redactStrings(inv.Unknowns)
	out.SuggestedVerification = redactStrings(inv.SuggestedVerification)
	if len(inv.ObservedSymptoms) > 0 {
		out.ObservedSymptoms = make([]Symptom, len(inv.ObservedSymptoms))
		for i, s := range inv.ObservedSymptoms {
			s.Statement = redactText(s.Statement)
			s.EvidenceRefs = redactStrings(s.EvidenceRefs)
			out.ObservedSymptoms[i] = s
		}
	}
	if len(inv.Hypotheses) > 0 {
		out.Hypotheses = make([]Hypothesis, len(inv.Hypotheses))
		for i, h := range inv.Hypotheses {
			h.Statement = redactText(h.Statement)
			h.SuggestedVerification = redactText(h.SuggestedVerification)
			h.SupportingEvidenceRefs = redactStrings(h.SupportingEvidenceRefs)
			h.CounterEvidenceRefs = redactStrings(h.CounterEvidenceRefs)
			h.MissingEvidence = redactStrings(h.MissingEvidence)
			out.Hypotheses[i] = h
		}
	}
	if len(inv.SourceReferences) > 0 {
		out.SourceReferences = make([]EvidenceRef, len(inv.SourceReferences))
		for i, e := range inv.SourceReferences {
			e.Ref = redactText(e.Ref)
			e.Summary = redactText(e.Summary)
			out.SourceReferences[i] = e
		}
	}
	if len(inv.ChangeCandidates) > 0 {
		out.ChangeCandidates = make([]ChangeCandidateRef, len(inv.ChangeCandidates))
		for i, c := range inv.ChangeCandidates {
			c.RankingBasis = redactText(c.RankingBasis)
			c.Observations = redactStrings(c.Observations)
			c.Inferences = redactStrings(c.Inferences)
			c.Limitations = redactStrings(c.Limitations)
			out.ChangeCandidates[i] = c
		}
	}
	return out
}

// RedactFeedback strips secret-shaped substrings from feedback free text.
func RedactFeedback(fb Feedback) Feedback {
	out := fb
	out.Rationale = redactText(fb.Rationale)
	out.ReviewerConfirmedCause = redactText(fb.ReviewerConfirmedCause)
	out.ConfirmationMethod = redactText(fb.ConfirmationMethod)
	out.Limitations = redactStrings(fb.Limitations)
	return out
}

func looksSecretKey(k string) bool {
	lk := strings.ToLower(strings.TrimSpace(k))
	for _, bad := range []string{"secret", "token", "password", "credential", "apikey", "api_key", "auth", "bearer"} {
		if strings.Contains(lk, bad) {
			return true
		}
	}
	return false
}
