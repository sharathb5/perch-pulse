package stackstatusadapt_test

import (
	"strings"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/pulse/observation"
	"github.com/yashg4509/perch/internal/pulse/stackstatusadapt"
	"github.com/yashg4509/perch/internal/stackstatus"
)

func testOpts(t *testing.T) stackstatusadapt.Options {
	t.Helper()
	return stackstatusadapt.Options{
		ObservedAt: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC),
		IngestedAt: time.Date(2026, 10, 7, 12, 0, 1, 0, time.UTC),
	}
}

func TestFromNodeReport_appEnvIsUnavailableNotHealthy(t *testing.T) {
	row := stackstatus.NodeReport{
		Name:         "inngest",
		Provider:     "inngest",
		Healthy:      true,
		StatusSource: stackstatus.SourceAppEnv,
		Configured:   true,
		Detail:       "INNGEST_DEV=1 (local dev server)",
	}
	obs := stackstatusadapt.FromNodeReport(row, testOpts(t))
	if obs.Status != observation.StatusUnavailable {
		t.Fatalf("status = %s, want unavailable (config-only signal)", obs.Status)
	}
	if obs.Status.IsHealthy() {
		t.Fatal("app_env must not map to healthy")
	}
}

func TestFromNodeReport_uncheckedNoProbeHealthyIsUnavailable(t *testing.T) {
	row := stackstatus.NodeReport{
		Name:         "legacy",
		Provider:     "legacy",
		Healthy:      true,
		StatusSource: stackstatus.SourceUnchecked,
		Configured:   true,
		Detail:       "no HTTP status probe (use CLI or app .env)",
	}
	obs := stackstatusadapt.FromNodeReport(row, testOpts(t))
	if obs.Status != observation.StatusUnavailable {
		t.Fatalf("status = %s, want unavailable", obs.Status)
	}
}

func TestFromNodeReport_probeSetupFailureIsUnavailable(t *testing.T) {
	row := stackstatus.NodeReport{
		Name:         "web",
		Provider:     "vercel",
		Healthy:      false,
		StatusSource: stackstatus.SourceProbe,
		Configured:   true,
		Detail:       "node needs a project field for Vercel probe",
	}
	obs := stackstatusadapt.FromNodeReport(row, testOpts(t))
	if obs.Status != observation.StatusUnavailable {
		t.Fatalf("status = %s, want unavailable", obs.Status)
	}
}

func TestFromNodeReport_healthyLiveProbe(t *testing.T) {
	row := stackstatus.NodeReport{
		Name:         "api",
		Provider:     "openai",
		Healthy:      true,
		StatusSource: stackstatus.SourceAPI,
		Configured:   true,
		Detail:       "vendor ok",
	}
	obs := stackstatusadapt.FromNodeReport(row, testOpts(t))
	if obs.Status != observation.StatusHealthy {
		t.Fatalf("status = %s, want healthy", obs.Status)
	}
	if err := obs.Validate(); err != nil {
		t.Fatal(err)
	}
	if obs.Source != stackstatusadapt.Source {
		t.Fatalf("source = %q", obs.Source)
	}
	if obs.ServiceID != "api" {
		t.Fatalf("service_id = %q", obs.ServiceID)
	}
}

func TestFromNodeReport_unhealthyLiveProbe(t *testing.T) {
	row := stackstatus.NodeReport{
		Name:         "web",
		Provider:     "vercel",
		Healthy:      false,
		StatusSource: stackstatus.SourceAPI,
		Configured:   true,
		Detail:       "HTTP 503",
	}
	obs := stackstatusadapt.FromNodeReport(row, testOpts(t))
	if obs.Status != observation.StatusUnhealthy {
		t.Fatalf("status = %s, want unhealthy", obs.Status)
	}
	if obs.Status.IsHealthy() {
		t.Fatal("failed probe must not map to healthy")
	}
}

func TestFromNodeReport_missingCredentialsNotHealthy(t *testing.T) {
	row := stackstatus.NodeReport{
		Name:         "billing",
		Provider:     "stripe",
		Healthy:      false,
		StatusSource: stackstatus.SourceUnconfigured,
		Configured:   false,
		Detail:       "missing credential (run perch auth sync-env)",
	}
	obs := stackstatusadapt.FromNodeReport(row, testOpts(t))
	if obs.Status != observation.StatusUnavailable {
		t.Fatalf("status = %s, want unavailable", obs.Status)
	}
	if obs.Status.IsHealthy() {
		t.Fatal("missing credentials must not map to healthy")
	}
}

func TestFromNodeReport_uncheckedWithoutSignalIsUnavailable(t *testing.T) {
	row := stackstatus.NodeReport{
		Name:         "openai",
		Provider:     "openai",
		Healthy:      false,
		StatusSource: stackstatus.SourceUnchecked,
		Configured:   true,
		Detail:       "credential present; probe not scheduled",
	}
	obs := stackstatusadapt.FromNodeReport(row, testOpts(t))
	if obs.Status != observation.StatusUnavailable {
		t.Fatalf("status = %s, want unavailable", obs.Status)
	}
}

func TestFromNodeReport_degradedWhenHealthyWithErrorRate(t *testing.T) {
	rate := 0.05
	row := stackstatus.NodeReport{
		Name:         "backend",
		Provider:     "render",
		Healthy:      true,
		StatusSource: stackstatus.SourceAPI,
		Configured:   true,
		ErrorRate:    &rate,
	}
	obs := stackstatusadapt.FromNodeReport(row, testOpts(t))
	if obs.Status != observation.StatusDegraded {
		t.Fatalf("status = %s, want degraded", obs.Status)
	}
}

func TestFromNodeReport_neverEmitsStale(t *testing.T) {
	cases := []stackstatus.NodeReport{
		{Name: "a", Provider: "custom", Healthy: true, StatusSource: stackstatus.SourceShell, Configured: true},
		{Name: "b", Provider: "x", Healthy: false, StatusSource: stackstatus.SourceAPI, Configured: true},
		{Name: "c", Provider: "y", Healthy: false, StatusSource: stackstatus.SourceUnconfigured, Configured: false},
	}
	opts := testOpts(t)
	for _, row := range cases {
		obs := stackstatusadapt.FromNodeReport(row, opts)
		if obs.Status == observation.StatusStale {
			t.Fatalf("adapter must not emit stale for row %+v", row)
		}
		if err := obs.Validate(); err != nil {
			t.Fatalf("row %+v: %v", row, err)
		}
	}
}

func TestFromNodeReport_observedAtIsCallerControlled(t *testing.T) {
	want := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	row := stackstatus.NodeReport{Name: "api", Provider: "openai", Healthy: true, StatusSource: stackstatus.SourceAPI, Configured: true}
	obs := stackstatusadapt.FromNodeReport(row, stackstatusadapt.Options{ObservedAt: want})
	if !obs.ObservedAt.Equal(want) {
		t.Fatalf("ObservedAt = %v, want %v", obs.ObservedAt, want)
	}
}

func TestFromEnvReport_mapsAllNodes(t *testing.T) {
	rep := &stackstatus.EnvReport{
		Env: "production",
		Nodes: []stackstatus.NodeReport{
			{Name: "web", Provider: "vercel", Healthy: true, StatusSource: stackstatus.SourceProbe, Configured: true},
			{Name: "db", Provider: "supabase", Healthy: false, StatusSource: stackstatus.SourcePlaceholder, Configured: true},
		},
	}
	got := stackstatusadapt.FromEnvReport(rep, testOpts(t))
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ServiceID != "production/web" || got[1].ServiceID != "production/db" {
		t.Fatalf("service ids = %q, %q", got[0].ServiceID, got[1].ServiceID)
	}
	if got[0].Status != observation.StatusHealthy || got[1].Status != observation.StatusUnavailable {
		t.Fatalf("statuses = %s, %s", got[0].Status, got[1].Status)
	}
}

func TestFromNodeReport_probeTimeoutIsUnavailable(t *testing.T) {
	row := stackstatus.NodeReport{
		Name:         "web",
		Provider:     "vercel",
		Healthy:      false,
		StatusSource: stackstatus.SourceProbe,
		Configured:   true,
		Detail:       "probe timed out",
	}
	obs := stackstatusadapt.FromNodeReport(row, testOpts(t))
	if obs.Status != observation.StatusUnavailable {
		t.Fatalf("status = %s, want unavailable", obs.Status)
	}
}

func TestFromNodeReport_apiTransportFailureIsUnavailable(t *testing.T) {
	row := stackstatus.NodeReport{
		Name:         "api",
		Provider:     "openai",
		Healthy:      false,
		StatusSource: stackstatus.SourceAPI,
		Configured:   true,
		Detail:       `provider: http: Get "https://api.openai.com/v1/models": EOF`,
	}
	obs := stackstatusadapt.FromNodeReport(row, testOpts(t))
	if obs.Status != observation.StatusUnavailable {
		t.Fatalf("status = %s, want unavailable", obs.Status)
	}
}

func TestFromNodeReport_api503ResponseIsUnhealthy(t *testing.T) {
	row := stackstatus.NodeReport{
		Name:         "api",
		Provider:     "openai",
		Healthy:      false,
		StatusSource: stackstatus.SourceAPI,
		Configured:   true,
		Detail:       "provider: http 503 Service Unavailable: upstream error",
	}
	obs := stackstatusadapt.FromNodeReport(row, testOpts(t))
	if obs.Status != observation.StatusUnhealthy {
		t.Fatalf("status = %s, want unhealthy", obs.Status)
	}
}

func TestFromNodeReport_redactsURLDetailsInEvidence(t *testing.T) {
	row := stackstatus.NodeReport{
		Name:         "rt",
		Provider:     "pusher",
		Healthy:      false,
		StatusSource: stackstatus.SourceAPI,
		Configured:   true,
		Detail:       `Get "https://api.pusher.com/apps/1/channels?auth_key=secret&auth_signature=sig": dial tcp: i/o timeout`,
	}
	obs := stackstatusadapt.FromNodeReport(row, testOpts(t))
	if obs.Evidence == nil || !strings.Contains(obs.Evidence.Summary, "redacted") {
		t.Fatalf("evidence = %+v, want redacted summary", obs.Evidence)
	}
}

func TestFromNodeReport_redactsUnknownResponseBodyInEvidence(t *testing.T) {
	row := stackstatus.NodeReport{
		Name:         "api",
		Provider:     "stripe",
		Healthy:      false,
		StatusSource: stackstatus.SourceAPI,
		Configured:   true,
		Detail:       `provider: http 401 Unauthorized: {"error":"invalid api key sk_live_secret"}`,
	}
	obs := stackstatusadapt.FromNodeReport(row, testOpts(t))
	if obs.Evidence == nil || strings.Contains(obs.Evidence.Summary, "sk_live") {
		t.Fatalf("evidence must not echo response body: %+v", obs.Evidence)
	}
}

func TestFromEnvReport_nilReport(t *testing.T) {
	if got := stackstatusadapt.FromEnvReport(nil, testOpts(t)); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}
