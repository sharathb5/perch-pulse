package api

import (
	"regexp"
	"strings"

	"github.com/yashg4509/perch/internal/pulse/change"
	"github.com/yashg4509/perch/internal/pulse/incident"
)

var (
	secretKeySubstrings = []string{
		"secret", "token", "password", "credential", "apikey", "api_key", "auth", "bearer",
	}
	// bearerOrKeyRE matches common leaked credential shapes in free text.
	bearerOrKeyRE = regexp.MustCompile(`(?i)(bearer\s+[a-z0-9._\-]{8,}|sk-[a-z0-9]{16,}|api[_-]?key\s*[:=]\s*\S+)`)
)

func looksSecretKey(k string) bool {
	lk := strings.ToLower(strings.TrimSpace(k))
	for _, bad := range secretKeySubstrings {
		if strings.Contains(lk, bad) {
			return true
		}
	}
	return false
}

func redactText(s string) string {
	if s == "" {
		return s
	}
	return bearerOrKeyRE.ReplaceAllString(s, "[redacted]")
}

// RedactIncident strips secret-shaped metadata from an incident copy.
func RedactIncident(inc incident.Incident) incident.Incident {
	out := inc
	out.Summary = redactText(inc.Summary)
	if len(inc.Observations) > 0 {
		out.Observations = make([]string, len(inc.Observations))
		for i, s := range inc.Observations {
			out.Observations[i] = redactText(s)
		}
	}
	if len(inc.Inferences) > 0 {
		out.Inferences = make([]string, len(inc.Inferences))
		for i, s := range inc.Inferences {
			out.Inferences[i] = redactText(s)
		}
	}
	if len(inc.Limitations) > 0 {
		out.Limitations = make([]string, len(inc.Limitations))
		for i, s := range inc.Limitations {
			out.Limitations[i] = redactText(s)
		}
	}
	if len(inc.Evidence) > 0 {
		out.Evidence = make([]incident.EvidenceRef, len(inc.Evidence))
		for i, e := range inc.Evidence {
			out.Evidence[i] = incident.EvidenceRef{
				Ref:     redactText(e.Ref),
				Summary: redactText(e.Summary),
			}
		}
	}
	return out
}

// RedactChange strips secret-shaped keys/values from a change event copy.
func RedactChange(e change.Event) change.Event {
	out := e
	out.Title = redactText(e.Title)
	out.Summary = redactText(e.Summary)
	out.Actor = redactText(e.Actor)
	if e.Metadata != nil {
		out.Metadata = make(map[string]string, len(e.Metadata))
		for k, v := range e.Metadata {
			// Never emit secret-bearing keys or values; replace the key name too.
			if looksSecretKey(k) || looksSecretKey(v) || bearerOrKeyRE.MatchString(k) || bearerOrKeyRE.MatchString(v) {
				out.Metadata["redacted_metadata"] = "[redacted]"
				continue
			}
			out.Metadata[k] = redactText(v)
		}
	}
	if len(e.Evidence) > 0 {
		out.Evidence = make([]change.EvidenceRef, len(e.Evidence))
		for i, ref := range e.Evidence {
			out.Evidence[i] = change.EvidenceRef{
				Ref:     redactText(ref.Ref),
				Summary: redactText(ref.Summary),
			}
		}
	}
	return out
}

// ContainsGroundTruthLeak reports whether payload text looks like scenario
// evaluation labels leaked into an API response (field names / schema ids).
// Used by tests and as a defense-in-depth check on serialized bodies.
func ContainsGroundTruthLeak(s string) bool {
	lower := strings.ToLower(s)
	needles := []string{
		`"ground_truth"`,
		`"ground-truth"`,
		"ground_truth:",
		`"scenario_id"`,
		"scenario_id:",
		`"expected_change_id"`,
		"expected_change_id:",
		`"expected_change"`,
		"pulse.scenario.v1",
		"label_leak",
	}
	for _, n := range needles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}
