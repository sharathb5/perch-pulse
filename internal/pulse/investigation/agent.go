package investigation

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AgentContextSchema is the stable agent-facing JSON contract.
const AgentContextSchema = "pulse.investigation.agent_context.v1"

// AgentContext is a bounded, redacted projection for coding agents.
type AgentContext struct {
	SchemaVersion   string    `json:"schema_version"`
	InvestigationID string    `json:"investigation_id"`
	IncidentID      string    `json:"incident_id"`
	GeneratedAt     time.Time `json:"generated_at"`
	IncidentState   string    `json:"incident_state"`
	Facts           []string  `json:"facts"`
	Hypotheses      []string  `json:"hypotheses"`
	Unknown         []string  `json:"unknown"`
	NextSteps       []string  `json:"next_steps"`
	Limitations     []string  `json:"limitations"`
	Diagnosed       bool      `json:"diagnosed"`
}

// FormatAgentContextJSON returns versioned agent JSON (no credentials, no GT).
func FormatAgentContextJSON(inv Investigation) ([]byte, error) {
	ctx := BuildAgentContext(inv)
	return json.MarshalIndent(ctx, "", "  ")
}

// BuildAgentContext projects an investigation into agent-readable sections.
func BuildAgentContext(inv Investigation) AgentContext {
	inv = RedactInvestigation(inv)
	ctx := AgentContext{
		SchemaVersion:   AgentContextSchema,
		InvestigationID: inv.InvestigationID,
		IncidentID:      inv.IncidentID,
		GeneratedAt:     inv.GeneratedAt,
		IncidentState:   inv.IncidentState,
		Diagnosed:       false,
		Limitations: append([]string{
			"FACTS are OBSERVED evidence only.",
			"HYPOTHESES are INFERRED and not proven root causes.",
			"Correlation is not causation.",
			"This snapshot is not current live state.",
		}, inv.Limitations...),
	}
	for _, s := range inv.ObservedSymptoms {
		if s.Classification == ClassObserved {
			ctx.Facts = append(ctx.Facts, s.Statement)
		}
	}
	for _, bc := range inv.BaselineComparisons {
		if bc.Classification == ClassObserved && bc.BeforeMedian != nil && bc.DuringMedian != nil {
			ctx.Facts = append(ctx.Facts, fmt.Sprintf(
				"Baseline %s %s: before_median=%g (n=%d) during_median=%g (n=%d) direction=%s",
				bc.ServiceID, bc.SignalType, *bc.BeforeMedian, bc.BeforeCount, *bc.DuringMedian, bc.DuringCount, bc.Direction,
			))
		}
	}
	for _, c := range inv.ChangeCandidates {
		ctx.Facts = append(ctx.Facts, fmt.Sprintf(
			"Change candidate %s rank=%d score=%.1f overlap=%s (ranking basis recorded; not causation)",
			c.ChangeID, c.Rank, c.Score, c.ServiceOverlap,
		))
	}
	ctx.Facts = append(ctx.Facts, fmt.Sprintf("Incident state at investigation time: %s", inv.IncidentState))
	ctx.Facts = append(ctx.Facts, fmt.Sprintf("Evidence freshness: %s", inv.EvidenceFreshness))

	for _, h := range inv.Hypotheses {
		ctx.Hypotheses = append(ctx.Hypotheses, fmt.Sprintf("[%s] %s", h.HypothesisID, h.Statement))
	}
	ctx.Unknown = append(ctx.Unknown, inv.Unknowns...)
	ctx.Unknown = append(ctx.Unknown, inv.MissingEvidence...)
	ctx.NextSteps = append(ctx.NextSteps, inv.SuggestedVerification...)
	ctx.Limitations = uniquePreserveOrder(ctx.Limitations)
	return ctx
}

// FormatAgentContextText renders a compact plain-text brief.
func FormatAgentContextText(inv Investigation) string {
	ctx := BuildAgentContext(inv)
	var b strings.Builder
	b.WriteString("Pulse investigation context (not a confirmed root cause)\n")
	b.WriteString(fmt.Sprintf("Schema: %s\n", ctx.SchemaVersion))
	b.WriteString(fmt.Sprintf("Investigation: %s\n", ctx.InvestigationID))
	b.WriteString(fmt.Sprintf("Incident: %s\n", ctx.IncidentID))
	b.WriteString(fmt.Sprintf("State: %s\n", ctx.IncidentState))
	b.WriteString("\nFACTS:\n")
	for _, f := range ctx.Facts {
		b.WriteString("- " + f + "\n")
	}
	b.WriteString("\nHYPOTHESES:\n")
	for _, h := range ctx.Hypotheses {
		b.WriteString("- " + h + "\n")
	}
	b.WriteString("\nUNKNOWN:\n")
	for _, u := range ctx.Unknown {
		b.WriteString("- " + u + "\n")
	}
	b.WriteString("\nNEXT STEPS:\n")
	for _, s := range ctx.NextSteps {
		b.WriteString("- " + s + "\n")
	}
	b.WriteString("\nLIMITATIONS:\n")
	for _, l := range ctx.Limitations {
		b.WriteString("- " + l + "\n")
	}
	return b.String()
}
