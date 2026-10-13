package llm

import (
	"sort"
	"strings"
)

// Machine-readable rejection categories derived from validator RejectedReason strings.
// These are diagnostic labels only — they do not weaken validation policy.
const (
	RejectionInvalidEvidenceID         = "invalid_evidence_id"
	RejectionMissingCitation           = "missing_citation"
	RejectionInvalidServiceReference   = "invalid_service_reference"
	RejectionUnsupportedCausalClaim    = "unsupported_causal_claim"
	RejectionCitationSupportUnverified = "citation_support_unverified"
	RejectionSchemaOrPolicy            = "schema_or_policy_rejection"
	RejectionUnknown                   = "unknown_validation_failure"
)

// CategorizeRejectionReason maps a validator RejectedReason to a stable category.
func CategorizeRejectionReason(reason string) string {
	r := strings.ToLower(strings.TrimSpace(reason))
	if r == "" {
		return RejectionUnknown
	}
	switch {
	case strings.Contains(r, "fabricated evidence id"),
		strings.Contains(r, "fabricated counter-evidence id"):
		return RejectionInvalidEvidenceID
	case strings.Contains(r, "missing supporting_evidence_refs"):
		return RejectionMissingCitation
	case strings.Contains(r, "fabricated service id"):
		return RejectionInvalidServiceReference
	case strings.Contains(r, "causal attribution"),
		strings.Contains(r, "confirmed-root-cause"),
		strings.Contains(r, "confirmed root cause"):
		return RejectionUnsupportedCausalClaim
	case strings.Contains(r, "citation support unverified"):
		return RejectionCitationSupportUnverified
	case strings.Contains(r, "classification must be inferred"),
		strings.Contains(r, "confidence percentages"),
		strings.Contains(r, "invented metric"),
		strings.Contains(r, "empty statement"),
		strings.Contains(r, "statement too long"),
		strings.Contains(r, "duplicate hypothesis_id"):
		return RejectionSchemaOrPolicy
	default:
		return RejectionUnknown
	}
}

// CollectRejectionCategories returns sorted unique categories from rejected hypotheses.
func CollectRejectionCategories(rejected []Hypothesis) []string {
	if len(rejected) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	for _, h := range rejected {
		cat := CategorizeRejectionReason(h.RejectedReason)
		seen[cat] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// HasRejectionCategory reports whether categories includes cat.
func HasRejectionCategory(categories []string, cat string) bool {
	for _, c := range categories {
		if c == cat {
			return true
		}
	}
	return false
}
