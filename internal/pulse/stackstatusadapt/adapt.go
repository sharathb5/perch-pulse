// Package stackstatusadapt converts live stackstatus collector rows into Pulse
// observations. It is pure (no I/O) and optional relative to perch status.
package stackstatusadapt

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/observation"
	"github.com/yashg4509/perch/internal/stackstatus"
)

// Source is the observation.Source value for rows adapted from stackstatus.
const Source = "stackstatus"

var (
	urlLikeDetail = regexp.MustCompile(`(?i)https?://\S+`)
	secretQuery   = regexp.MustCompile(`(?i)(auth_key|auth_signature|token=|api_key=)[^&\s]*`)
)

// Options supplies caller-controlled timestamps for adapted observations.
type Options struct {
	// ObservedAt is when the underlying stackstatus collection completed.
	// Required; zero time yields an observation that fails Validate.
	ObservedAt time.Time

	// IngestedAt is when Pulse accepted the observation. Zero means unknown.
	IngestedAt time.Time

	// Environment scopes ServiceID when adapting a full EnvReport (e.g. production/api).
	// FromEnvReport sets this from rep.Env automatically.
	Environment string
}

// FromNodeReport maps one stackstatus row to a Pulse observation.
func FromNodeReport(row stackstatus.NodeReport, opts Options) observation.Observation {
	obs := observation.Observation{
		ServiceID:  serviceID(opts.Environment, row.Name),
		ObservedAt: opts.ObservedAt,
		IngestedAt: opts.IngestedAt,
		Status:     mapStatus(row),
		Source:     Source,
	}
	if ev := buildEvidence(row); ev != nil {
		obs.Evidence = ev
	}
	return obs
}

// FromEnvReport maps every node in an environment report using the same timestamps.
func FromEnvReport(rep *stackstatus.EnvReport, opts Options) []observation.Observation {
	if rep == nil || len(rep.Nodes) == 0 {
		return nil
	}
	opts.Environment = strings.TrimSpace(rep.Env)
	out := make([]observation.Observation, 0, len(rep.Nodes))
	for _, row := range rep.Nodes {
		out = append(out, FromNodeReport(row, opts))
	}
	return out
}

func serviceID(env, nodeName string) string {
	name := strings.TrimSpace(nodeName)
	env = strings.TrimSpace(env)
	if env == "" {
		return name
	}
	return env + "/" + name
}

func mapStatus(row stackstatus.NodeReport) observation.Status {
	switch row.StatusSource {
	case stackstatus.SourceAppEnv:
		return observation.StatusUnavailable
	case stackstatus.SourceUnchecked:
		return observation.StatusUnavailable
	case stackstatus.SourceUnconfigured, stackstatus.SourcePlaceholder:
		return observation.StatusUnavailable
	}

	if row.Healthy {
		if row.ErrorRate != nil && *row.ErrorRate > 0 {
			return observation.StatusDegraded
		}
		return observation.StatusHealthy
	}

	if noUsableSignal(row) {
		return observation.StatusUnavailable
	}

	return observation.StatusUnhealthy
}

func noUsableSignal(row stackstatus.NodeReport) bool {
	if probeSetupFailure(row.Detail) || noUsableSignalFailure(row.Detail) {
		return true
	}
	if row.StatusSource == stackstatus.SourceAPI {
		if strings.Contains(strings.ToLower(row.Detail), "missing credential") {
			return true
		}
		if apiProbeNoUsableSignal(row.Detail) {
			return true
		}
	}
	return false
}

func probeSetupFailure(detail string) bool {
	d := strings.TrimSpace(strings.ToLower(detail))
	if d == "" {
		return false
	}
	for _, prefix := range []string{
		"missing credential",
		"node needs a ",
		"need pusher_key",
		"signed probe not configured",
	} {
		if strings.HasPrefix(d, prefix) {
			return true
		}
	}
	return false
}

func noUsableSignalFailure(detail string) bool {
	d := strings.TrimSpace(strings.ToLower(detail))
	if d == "" {
		return false
	}
	for _, phrase := range []string{
		"probe timed out",
		"credential invalid or expired",
		"connection refused",
		"no such host",
		"i/o timeout",
		"network is unreachable",
		"client.timeout",
		"context deadline exceeded",
		" eof",
	} {
		if strings.Contains(d, phrase) {
			return true
		}
	}
	return false
}

func apiProbeNoUsableSignal(detail string) bool {
	d := strings.ToLower(strings.TrimSpace(detail))
	if d == "" {
		return false
	}
	if strings.Contains(d, "provider: http:") && !strings.Contains(d, "provider: http 2") {
		return true
	}
	for _, code := range []string{"401", "403", "408", "429"} {
		if strings.Contains(d, "http "+code) || strings.Contains(d, "http "+code+" ") {
			return true
		}
	}
	return false
}

func buildEvidence(row stackstatus.NodeReport) *observation.Evidence {
	ref := strings.TrimSpace(row.StatusSource)
	if ref != "" {
		ref = "stackstatus/" + ref
	}
	prov := strings.TrimSpace(row.Provider)
	if ref == "" && prov == "" && strings.TrimSpace(row.Detail) == "" && row.ErrorRate == nil {
		return nil
	}
	if ref == "" && prov != "" {
		ref = "provider/" + prov
	}

	var summaryParts []string
	if prov != "" {
		summaryParts = append(summaryParts, "provider="+prov)
	}
	if d := sanitizeDetail(row.Detail); d != "" {
		summaryParts = append(summaryParts, d)
	}
	if row.ErrorRate != nil {
		summaryParts = append(summaryParts, fmt.Sprintf("error_rate=%g", *row.ErrorRate))
	}
	summary := strings.Join(summaryParts, "; ")
	if ref == "" && summary == "" {
		return nil
	}
	return &observation.Evidence{Ref: ref, Summary: summary}
}

func sanitizeDetail(detail string) string {
	d := strings.TrimSpace(detail)
	if d == "" {
		return ""
	}
	if urlLikeDetail.MatchString(d) || secretQuery.MatchString(d) {
		return redactedProbeDetail(d)
	}
	if isAllowlistedDetail(d) {
		return d
	}
	return "status detail (redacted)"
}

func redactedProbeDetail(d string) string {
	lower := strings.ToLower(d)
	if strings.Contains(lower, "401") || strings.Contains(lower, "403") ||
		strings.Contains(lower, "unauthorized") || strings.Contains(lower, "forbidden") {
		return "credential invalid or expired"
	}
	return "probe error (detail redacted)"
}

func isAllowlistedDetail(detail string) bool {
	d := strings.TrimSpace(strings.ToLower(detail))
	if d == "" {
		return false
	}
	for _, prefix := range []string{
		"probe timed out",
		"credential invalid or expired",
		"missing credential",
		"node needs a ",
		"custom health command failed",
		"no http status probe",
		"credential present; probe not scheduled",
		"deployable host status api not implemented yet",
		"probe error:",
		"signed probe not configured",
		"need pusher_key",
		"inngest_dev=1",
		"database_url points at neon",
		"configured from project .env",
	} {
		if strings.HasPrefix(d, prefix) || strings.Contains(d, prefix) {
			return true
		}
	}
	if strings.HasPrefix(d, "project ") || strings.HasPrefix(d, "pusher http ") {
		return true
	}
	if strings.HasPrefix(d, "provider: http ") {
		// Vendor HTTP status line without echoing response body beyond status code.
		return !strings.Contains(d, ": {") && len(d) < 120
	}
	return false
}
