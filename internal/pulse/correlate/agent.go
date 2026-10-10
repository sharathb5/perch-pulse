package correlate

import (
	"fmt"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/change"
	"github.com/yashg4509/perch/internal/pulse/incident"
)

// AgentBrief is a compact, non-secret summary for coding agents.
type AgentBrief struct {
	IncidentID      string   `json:"incident_id"`
	PrimaryService  string   `json:"primary_service"`
	SummaryLines    []string `json:"summary_lines"`
	TopChangeID     string   `json:"top_change_id,omitempty"`
	TopChangeScore  float64  `json:"top_change_score,omitempty"`
	StrongCandidate bool     `json:"strong_candidate"`
	Caveat          string   `json:"caveat"`
}

// FormatAgentContext renders a compact plain-text brief.
// It must not dump git history or raw telemetry blobs.
func FormatAgentContext(inc incident.Incident, rep Report, topChange *change.Event) string {
	var b strings.Builder
	b.WriteString("Pulse incident correlation (not proven causation)\n")
	b.WriteString(fmt.Sprintf("Incident: %s\n", inc.IncidentID))
	b.WriteString(fmt.Sprintf("Service: %s\n", inc.PrimaryServiceID))
	if len(inc.SignalTypes) > 0 {
		b.WriteString(fmt.Sprintf("Signals: %s\n", strings.Join(inc.SignalTypes, ", ")))
	}
	b.WriteString(fmt.Sprintf("Detected: %s\n", inc.FirstDetectedAt.UTC().Format(time.RFC3339)))

	// Compact finding digest from during-window if present.
	for _, d := range inc.During.Digests {
		if d.Signal == "error_rate" {
			b.WriteString(fmt.Sprintf("During error_rate median=%.3f (n=%d)\n", d.Median, d.Count))
		}
		if d.Signal == "latency_ms" {
			b.WriteString(fmt.Sprintf("During latency_ms median=%.0f (n=%d)\n", d.Median, d.Count))
		}
	}

	if rep.NoStrongCandidate || rep.Top1 == nil {
		b.WriteString("Change correlation: no strong candidate found.\n")
	} else {
		top := rep.Top1
		b.WriteString(fmt.Sprintf(
			"Related change candidate: %s ranked #%d score=%.1f (%.0fs before regression, overlap=%s).\n",
			top.ChangeID, top.Rank, top.Score, top.TemporalDistanceSecs, top.ServiceOverlap,
		))
		if topChange != nil {
			if topChange.CommitSHA != "" {
				sha := topChange.CommitSHA
				if len(sha) > 12 {
					sha = sha[:12]
				}
				b.WriteString(fmt.Sprintf("Change commit: %s\n", sha))
			}
			if topChange.Metadata["simulated"] == "true" {
				b.WriteString("Note: change is a simulated deployment marker (demo), not a real cloud deploy.\n")
			}
			if len(topChange.ServiceIDs) > 0 {
				b.WriteString(fmt.Sprintf("Change affected services: %s\n", strings.Join(topChange.ServiceIDs, ", ")))
			}
		}
		b.WriteString("Inference: strong temporal/service correlation — not proven causation.\n")
	}
	b.WriteString("Limitation: ground-truth scenario labels were not used in correlation scoring.\n")
	return b.String()
}

// Brief builds a structured agent brief from incident + report.
func Brief(inc incident.Incident, rep Report) AgentBrief {
	b := AgentBrief{
		IncidentID:      inc.IncidentID,
		PrimaryService:  inc.PrimaryServiceID,
		StrongCandidate: rep.StrongCandidate,
		Caveat:          "correlation is not proven causation",
	}
	b.SummaryLines = append(b.SummaryLines,
		fmt.Sprintf("%s degradation detected.", inc.PrimaryServiceID),
	)
	if rep.Top1 != nil && rep.StrongCandidate {
		b.TopChangeID = rep.Top1.ChangeID
		b.TopChangeScore = rep.Top1.Score
		b.SummaryLines = append(b.SummaryLines, fmt.Sprintf(
			"A change affecting overlapping services occurred %.0f seconds before the regression (score=%.1f).",
			rep.Top1.TemporalDistanceSecs, rep.Top1.Score,
		))
		b.SummaryLines = append(b.SummaryLines,
			"This is a strong temporal/service correlation, not proven causation.",
		)
	} else {
		b.SummaryLines = append(b.SummaryLines, "No strong related change candidate found.")
	}
	return b
}
