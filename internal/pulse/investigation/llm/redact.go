package llm

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

func redactHypothesis(h Hypothesis) Hypothesis {
	h.Statement = redactText(h.Statement)
	h.SuggestedVerification = redactText(h.SuggestedVerification)
	h.SupportingEvidenceRefs = redactStrings(h.SupportingEvidenceRefs)
	h.CounterEvidenceRefs = redactStrings(h.CounterEvidenceRefs)
	h.MissingEvidence = redactStrings(h.MissingEvidence)
	h.RejectedReason = redactText(h.RejectedReason)
	return h
}

// RedactProposal strips secret-shaped substrings from free text fields.
func RedactProposal(p Proposal) Proposal {
	out := p
	out.MissingEvidence = redactStrings(p.MissingEvidence)
	out.Unknowns = redactStrings(p.Unknowns)
	out.SuggestedVerification = redactStrings(p.SuggestedVerification)
	out.Limitations = redactStrings(p.Limitations)
	out.ValidationNotes = redactStrings(p.ValidationNotes)
	if len(p.Hypotheses) > 0 {
		out.Hypotheses = make([]Hypothesis, len(p.Hypotheses))
		for i, h := range p.Hypotheses {
			out.Hypotheses[i] = redactHypothesis(h)
		}
	}
	if len(p.RejectedHypotheses) > 0 {
		out.RejectedHypotheses = make([]Hypothesis, len(p.RejectedHypotheses))
		for i, h := range p.RejectedHypotheses {
			out.RejectedHypotheses[i] = redactHypothesis(h)
		}
	}
	// Provenance never carries secrets; still scrub accidental key-shaped model ids.
	out.Provenance.Model = redactText(p.Provenance.Model)
	out.Provenance.RequestID = redactText(p.Provenance.RequestID)
	return out
}

// RedactFeedback strips secret-shaped substrings from proposal feedback.
func RedactFeedback(fb Feedback) Feedback {
	out := fb
	out.Rationale = redactText(fb.Rationale)
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
