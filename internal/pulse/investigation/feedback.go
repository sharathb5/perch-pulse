package investigation

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// ValidateFeedback checks feedback invariants without trusting client IDs blindly.
func ValidateFeedback(fb Feedback) error {
	if fb.SchemaVersion != FeedbackSchemaVersion {
		return fmt.Errorf("investigation: feedback schema_version must be %q", FeedbackSchemaVersion)
	}
	if strings.TrimSpace(fb.FeedbackID) == "" {
		return fmt.Errorf("investigation: feedback_id is required")
	}
	if strings.TrimSpace(fb.InvestigationID) == "" {
		return fmt.Errorf("investigation: investigation_id is required")
	}
	if strings.TrimSpace(fb.IncidentID) == "" {
		return fmt.Errorf("investigation: incident_id is required")
	}
	if fb.Timestamp.IsZero() || fb.Timestamp.Location() != time.UTC {
		return fmt.Errorf("investigation: feedback timestamp must be UTC")
	}
	if !ValidAssessment(fb.Assessment) {
		return fmt.Errorf("investigation: invalid assessment %q", fb.Assessment)
	}
	if utf8.RuneCountInString(fb.Rationale) > MaxFeedbackRationale {
		return fmt.Errorf("investigation: rationale exceeds %d characters", MaxFeedbackRationale)
	}
	if utf8.RuneCountInString(fb.ReviewerConfirmedCause) > MaxFeedbackRationale {
		return fmt.Errorf("investigation: reviewer_confirmed_cause exceeds %d characters", MaxFeedbackRationale)
	}
	if looksSecretKey(fb.Rationale) || bearerOrKeyRE.MatchString(fb.Rationale) {
		return fmt.Errorf("investigation: rationale appears to contain secret-shaped content")
	}
	if looksSecretKey(fb.ReviewerConfirmedCause) || bearerOrKeyRE.MatchString(fb.ReviewerConfirmedCause) {
		return fmt.Errorf("investigation: reviewer_confirmed_cause appears to contain secret-shaped content")
	}
	return nil
}

// ValidateFeedbackAgainstInvestigation ensures hypothesis IDs exist and incident matches.
func ValidateFeedbackAgainstInvestigation(fb Feedback, inv Investigation) error {
	if err := ValidateFeedback(fb); err != nil {
		return err
	}
	if fb.InvestigationID != inv.InvestigationID {
		return fmt.Errorf("investigation: feedback investigation_id mismatch")
	}
	if fb.IncidentID != inv.IncidentID {
		return fmt.Errorf("investigation: feedback incident_id must match investigation")
	}
	known := map[string]struct{}{}
	for _, h := range inv.Hypotheses {
		known[h.HypothesisID] = struct{}{}
	}
	for _, id := range fb.UsefulHypothesisIDs {
		if _, ok := known[id]; !ok {
			return fmt.Errorf("investigation: unknown useful hypothesis_id %q", id)
		}
	}
	for _, id := range fb.RejectedHypothesisIDs {
		if _, ok := known[id]; !ok {
			return fmt.Errorf("investigation: unknown rejected hypothesis_id %q", id)
		}
	}
	return nil
}

// FeedbackLimitations documents trust boundaries for stored feedback.
func FeedbackLimitations() []string {
	return []string{
		"Feedback is a human assessment of investigation quality — not independently verified ground truth.",
		"Reviewer-confirmed cause is user-reported unless confirmation_method documents independent evidence.",
		"Feedback does not modify incident evidence, recovery state, hypotheses, or detector thresholds.",
	}
}
