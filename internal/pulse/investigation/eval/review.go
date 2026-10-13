package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// ReviewSchemaVersion versions human proposal reviews.
	ReviewSchemaVersion = "pulse.investigation.human_review.v1"
	// RubricVersion identifies the F2c review question set.
	RubricVersion = "pulse.investigation.rubric.f2c.v1"
)

// ReviewAnswer is a ternary judgment — never invents a score when unevaluated.
type ReviewAnswer string

const (
	ReviewYes          ReviewAnswer = "yes"
	ReviewNo           ReviewAnswer = "no"
	ReviewPartial      ReviewAnswer = "partial"
	ReviewNotEvaluated ReviewAnswer = "not_evaluated"
)

// HumanReview attaches a lightweight rubric evaluation to one proposal.
// Reviews are subjective; they are never treated as verified root cause.
type HumanReview struct {
	SchemaVersion           string       `json:"schema_version"`
	ReviewID                string       `json:"review_id"`
	ProposalID              string       `json:"proposal_id"`
	SourceFingerprint       string       `json:"source_fingerprint"`
	EvaluatorProvenance     string       `json:"evaluator_provenance"`
	Timestamp               time.Time    `json:"timestamp"`
	RubricVersion           string       `json:"rubric_version"`
	ObservedFactsAccurate   ReviewAnswer `json:"observed_facts_accurate"`
	ReferencesSupportClaims ReviewAnswer `json:"references_support_claims"`
	DistinguishesHypotheses ReviewAnswer `json:"distinguishes_hypotheses_from_facts"`
	AcknowledgesUnknowns    ReviewAnswer `json:"acknowledges_unknowns"`
	VerificationActionable  ReviewAnswer `json:"verification_steps_actionable"`
	MoreUsefulThanF1        ReviewAnswer `json:"more_useful_than_deterministic_f1"`
	Notes                   string       `json:"notes,omitempty"`
	Limitations             []string     `json:"limitations"`
}

// RubricQuestions returns the F2c human review questions.
func RubricQuestions() []string {
	return []string{
		"Are observed facts accurate?",
		"Do references support the claims?",
		"Does the report distinguish hypotheses from confirmed facts?",
		"Are important unknowns acknowledged?",
		"Are verification steps actionable?",
		"Is the report more useful than deterministic F1?",
	}
}

// ReviewLimitations documents honesty constraints for human reviews.
func ReviewLimitations() []string {
	return []string{
		"Human reviews are subjective usefulness judgments — not independently verified root cause.",
		"Absent reviews must be labeled not_evaluated; do not invent answers.",
		"Citation existence checks remain separate from semantic support judgments here.",
		"Feedback/reviews cannot mutate F1 incident evidence or investigation snapshots.",
	}
}

// Validate checks review structural invariants.
func (r HumanReview) Validate() error {
	if r.SchemaVersion != ReviewSchemaVersion {
		return fmt.Errorf("review: schema_version must be %q", ReviewSchemaVersion)
	}
	if strings.TrimSpace(r.ReviewID) == "" {
		return fmt.Errorf("review: review_id required")
	}
	if strings.TrimSpace(r.ProposalID) == "" {
		return fmt.Errorf("review: proposal_id required")
	}
	if strings.TrimSpace(r.SourceFingerprint) == "" {
		return fmt.Errorf("review: source_fingerprint required")
	}
	if strings.TrimSpace(r.EvaluatorProvenance) == "" {
		return fmt.Errorf("review: evaluator_provenance required")
	}
	if r.RubricVersion != RubricVersion {
		return fmt.Errorf("review: rubric_version must be %q", RubricVersion)
	}
	for _, a := range []ReviewAnswer{
		r.ObservedFactsAccurate, r.ReferencesSupportClaims, r.DistinguishesHypotheses,
		r.AcknowledgesUnknowns, r.VerificationActionable, r.MoreUsefulThanF1,
	} {
		if !validReviewAnswer(a) {
			return fmt.Errorf("review: invalid answer %q", a)
		}
	}
	return nil
}

func validReviewAnswer(a ReviewAnswer) bool {
	switch a {
	case ReviewYes, ReviewNo, ReviewPartial, ReviewNotEvaluated:
		return true
	default:
		return false
	}
}

// NewUnevaluatedReview returns a review shell with all dimensions NOT EVALUATED.
func NewUnevaluatedReview(proposalID, fingerprint, evaluator string, now time.Time) HumanReview {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	return HumanReview{
		SchemaVersion:           ReviewSchemaVersion,
		ReviewID:                fmt.Sprintf("review|%s|%d", sanitizeID(proposalID), now.Unix()),
		ProposalID:              proposalID,
		SourceFingerprint:       fingerprint,
		EvaluatorProvenance:     evaluator,
		Timestamp:               now,
		RubricVersion:           RubricVersion,
		ObservedFactsAccurate:   ReviewNotEvaluated,
		ReferencesSupportClaims: ReviewNotEvaluated,
		DistinguishesHypotheses: ReviewNotEvaluated,
		AcknowledgesUnknowns:    ReviewNotEvaluated,
		VerificationActionable:  ReviewNotEvaluated,
		MoreUsefulThanF1:        ReviewNotEvaluated,
		Limitations:             ReviewLimitations(),
	}
}

// LoadReviewsDir loads *.json human reviews from a directory (optional).
func LoadReviewsDir(dir string) ([]HumanReview, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []HumanReview
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name())) // #nosec G304 -- operator-supplied review dir
		if err != nil {
			return nil, err
		}
		var r HumanReview
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("review: %s: %w", e.Name(), err)
		}
		if err := r.Validate(); err != nil {
			return nil, fmt.Errorf("review: %s: %w", e.Name(), err)
		}
		out = append(out, r)
	}
	return out, nil
}

// SummarizeReviews aggregates reviews for report enrichment without inventing scores.
type ReviewSummary struct {
	Count              int                       `json:"count"`
	RubricVersion      string                    `json:"rubric_version"`
	DimensionCounts    map[string]map[string]int `json:"dimension_counts"`
	NotEvaluatedNote   string                    `json:"not_evaluated_note,omitempty"`
	SubjectiveOnlyNote string                    `json:"subjective_only_note"`
}

// SummarizeHumanReviews rolls up answers with explicit not_evaluated tracking.
func SummarizeHumanReviews(reviews []HumanReview) *ReviewSummary {
	if len(reviews) == 0 {
		return &ReviewSummary{
			Count:              0,
			RubricVersion:      RubricVersion,
			NotEvaluatedNote:   "No human reviews attached — semantic dimensions marked NOT EVALUATED.",
			SubjectiveOnlyNote: "Human reviews are subjective; absence is not a mechanical failure.",
			DimensionCounts:    map[string]map[string]int{},
		}
	}
	dims := []string{
		"observed_facts_accurate",
		"references_support_claims",
		"distinguishes_hypotheses_from_facts",
		"acknowledges_unknowns",
		"verification_steps_actionable",
		"more_useful_than_deterministic_f1",
	}
	counts := map[string]map[string]int{}
	for _, d := range dims {
		counts[d] = map[string]int{}
	}
	for _, r := range reviews {
		inc := func(dim string, a ReviewAnswer) {
			counts[dim][string(a)]++
		}
		inc("observed_facts_accurate", r.ObservedFactsAccurate)
		inc("references_support_claims", r.ReferencesSupportClaims)
		inc("distinguishes_hypotheses_from_facts", r.DistinguishesHypotheses)
		inc("acknowledges_unknowns", r.AcknowledgesUnknowns)
		inc("verification_steps_actionable", r.VerificationActionable)
		inc("more_useful_than_deterministic_f1", r.MoreUsefulThanF1)
	}
	return &ReviewSummary{
		Count:              len(reviews),
		RubricVersion:      RubricVersion,
		DimensionCounts:    counts,
		SubjectiveOnlyNote: "Human reviews are subjective usefulness judgments — not verified root cause.",
	}
}

func sanitizeID(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, s)
	if len(s) > 64 {
		return s[:64]
	}
	return s
}
