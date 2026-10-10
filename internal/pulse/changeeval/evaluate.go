// Package changeeval scores change↔incident correlations against expected
// fixture labels AFTER correlation output exists.
//
// It must not be imported by change, incident, correlate, or detect.
// Expected change IDs come from test/demo fixtures — never from scenario
// ground-truth fields inside the correlation scorer.
package changeeval

import (
	"fmt"
	"time"

	"github.com/yashg4509/perch/internal/pulse/correlate"
)

const ReportSchemaVersion = "pulse.changeeval.v1"

// Case is one evaluation fixture: expected attribution for an incident.
type Case struct {
	Name                    string
	IncidentID              string
	ExpectedTop1ChangeID    string // empty + ExpectNoStrongCandidate => expect none
	ExpectNoStrongCandidate bool
	Notes                   string
}

// CaseResult is per-case scoring.
type CaseResult struct {
	Name               string `json:"name"`
	IncidentID         string `json:"incident_id"`
	Top1ChangeID       string `json:"top1_change_id,omitempty"`
	Top1Correct        bool   `json:"top1_correct"`
	Top3Correct        bool   `json:"top3_correct"`
	NoCandidateCorrect bool   `json:"no_candidate_correct"`
	ExpectNone         bool   `json:"expect_none"`
	StrongCandidate    bool   `json:"strong_candidate"`
	Notes              string `json:"notes,omitempty"`
}

// Summary aggregates change-attribution evaluation.
type Summary struct {
	SchemaVersion       string       `json:"schema_version"`
	GeneratedAt         time.Time    `json:"generated_at"`
	Cases               []CaseResult `json:"cases"`
	Top1Correct         int          `json:"top1_correct"`
	Top3Correct         int          `json:"top3_correct"`
	NoCandidateCorrect  int          `json:"no_candidate_correct"`
	AttributionEligible int          `json:"attribution_eligible"`
	NoCandidateEligible int          `json:"no_candidate_eligible"`
}

// Evaluate compares a correlation report to expected fixture labels.
func Evaluate(c Case, rep correlate.Report) (CaseResult, error) {
	if rep.IncidentID != "" && c.IncidentID != "" && rep.IncidentID != c.IncidentID {
		return CaseResult{}, fmt.Errorf("changeeval: incident_id mismatch report=%s case=%s", rep.IncidentID, c.IncidentID)
	}
	out := CaseResult{
		Name:            c.Name,
		IncidentID:      c.IncidentID,
		StrongCandidate: rep.StrongCandidate,
		ExpectNone:      c.ExpectNoStrongCandidate || c.ExpectedTop1ChangeID == "",
		Notes:           c.Notes,
	}
	if rep.Top1 != nil {
		out.Top1ChangeID = rep.Top1.ChangeID
	}

	if out.ExpectNone {
		out.NoCandidateCorrect = rep.NoStrongCandidate
		out.Top1Correct = rep.NoStrongCandidate
		out.Top3Correct = rep.NoStrongCandidate
		return out, nil
	}

	out.Top1Correct = rep.Top1 != nil && rep.Top1.ChangeID == c.ExpectedTop1ChangeID
	for _, r := range rep.Top3 {
		if r.ChangeID == c.ExpectedTop1ChangeID {
			out.Top3Correct = true
			break
		}
	}
	return out, nil
}

// Summarize aggregates case results.
func Summarize(results []CaseResult) Summary {
	s := Summary{
		SchemaVersion: ReportSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Cases:         results,
	}
	for _, r := range results {
		if r.ExpectNone {
			s.NoCandidateEligible++
			if r.NoCandidateCorrect {
				s.NoCandidateCorrect++
			}
			continue
		}
		s.AttributionEligible++
		if r.Top1Correct {
			s.Top1Correct++
		}
		if r.Top3Correct {
			s.Top3Correct++
		}
	}
	return s
}
