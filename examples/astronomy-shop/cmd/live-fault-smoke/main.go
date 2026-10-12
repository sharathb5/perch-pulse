// Command live-fault-smoke validates Live Pulse against a running Astronomy Shop:
// baseline readiness → flagd latency fault → active incident via live API →
// fault clear → recovered historical evidence.
//
// Fixtures are NOT injected into Prometheus. Fault activation uses the scenario
// harness (flagd only). Scenario labels must never appear in live API JSON.
// workflow_dispatch / LIVE SMOKE only — not part of make verify.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/api"
	"github.com/yashg4509/perch/internal/pulse/live"
	"github.com/yashg4509/perch/internal/pulse/scenario"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		vizBase         = flag.String("viz-base-url", "http://127.0.0.1:18765", "perch viz loopback base")
		flagdBase       = flag.String("flagd-api-base", "http://127.0.0.1:8080/feature/api", "flagd-ui API base")
		artifactDir     = flag.String("artifact-dir", "", "bounded diagnostics directory")
		scenarioID      = flag.String("scenario-id", "latency-shipping-intl", "catalog fault scenario")
		targetService   = flag.String("target-service", "astronomy/local/shipping", "expected Pulse service_id")
		targetGraph     = flag.String("target-graph-node", "shipping", "expected graph node id")
		baselineSecs    = flag.Int("baseline-secs", 180, "minimum baseline wait before fault injection")
		faultSecs       = flag.Int("fault-secs", 120, "max wait for active incident after fault")
		recoverSecs     = flag.Int("recover-secs", 120, "max wait for recovery after fault clear")
		pollSecs        = flag.Int("poll-secs", 10, "live API poll interval")
		scenarioResults = flag.String("scenario-results-dir", "", "harness results dir (gitignored)")
	)
	flag.Parse()
	if *artifactDir == "" {
		return fmt.Errorf("artifact-dir is required")
	}
	if err := os.MkdirAll(*artifactDir, 0o750); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	base := strings.TrimRight(strings.TrimSpace(*vizBase), "/")
	if err := requireLoopbackHTTPBase(base); err != nil {
		return fmt.Errorf("viz-base-url: %w", err)
	}
	if err := requireLoopbackHTTPBase(strings.TrimRight(*flagdBase, "/")); err != nil {
		return fmt.Errorf("flagd-api-base: %w", err)
	}

	resultsDir := *scenarioResults
	if resultsDir == "" {
		resultsDir = filepath.Join(*artifactDir, "scenario-results")
	}
	_ = os.MkdirAll(resultsDir, 0o750)

	writeFile(*artifactDir, "live-fault-config.txt", strings.Join([]string{
		"viz_base=" + base,
		"flagd_api_base=" + *flagdBase,
		"scenario_id=" + *scenarioID + " (injection only; must not appear in live API)",
		"target_service=" + *targetService,
		"target_graph_node=" + *targetGraph,
		"baseline_secs_min=" + fmt.Sprintf("%d", *baselineSecs),
		"fault_secs=" + fmt.Sprintf("%d", *faultSecs),
		"recover_secs=" + fmt.Sprintf("%d", *recoverSecs),
		"poll_secs=" + fmt.Sprintf("%d", *pollSecs),
		"note=Observes real Prometheus via Live Pulse; does not inject metric fixtures.",
		"note=Requires latency_ms signal evidence on the opened incident.",
	}, "\n")+"\n")

	client := &http.Client{Timeout: 15 * time.Second}
	catalog, err := scenario.LoadCatalog()
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	catalog.FlagdAPIBase = *flagdBase
	h, err := scenario.NewHarness(catalog, scenario.NewFlagdClient(*flagdBase), resultsDir, nil)
	if err != nil {
		return fmt.Errorf("harness: %w", err)
	}
	if active, _ := h.LoadActive(); active != nil {
		note("stopping leftover scenario %s", active.ScenarioID)
		if _, err := h.Stop(context.Background(), ""); err != nil {
			return fmt.Errorf("stop leftover: %w", err)
		}
	}
	// Guaranteed fault cleanup even on returned errors (no os.Exit inside run).
	cleared := false
	defer func() {
		if !cleared {
			note("deferred fault cleanup")
			if _, err := h.Stop(context.Background(), ""); err != nil {
				warn("deferred stop: %v", err)
			}
		}
	}()

	ctx := context.Background()
	poll := time.Duration(*pollSecs) * time.Second

	st, err := getLiveStatus(ctx, client, base)
	writeJSON(*artifactDir, "live-status-initial.json", st)
	if err != nil {
		return fmt.Errorf("live status: %w", err)
	}
	if !st.Status.Enabled || !st.Status.Configured {
		return fmt.Errorf("Live Pulse not enabled/configured (enabled=%v configured=%v state=%s reason=%s)",
			st.Status.Enabled, st.Status.Configured, st.Status.State, st.Status.UnavailabilityReason)
	}
	ok("Live Pulse enabled state=%s", st.Status.State)

	// Record pre-fault latency for comparison.
	var baselineLatency *float64
	baselineStart := time.Now()
	minBaseline := time.Duration(*baselineSecs) * time.Second
	note("waiting ≥%ds for latency baseline on %s (ceiling + readiness)", *baselineSecs, *targetService)
	deadline := baselineStart.Add(minBaseline + 60*time.Second) // small grace after minimum
	var baselineReadyAt time.Time
	for {
		if time.Now().After(deadline) {
			svcs, _ := getLiveServices(ctx, client, base)
			writeJSON(*artifactDir, "live-services-baseline-timeout.json", svcs)
			st, _ = getLiveStatus(ctx, client, base)
			writeJSON(*artifactDir, "live-status-baseline-timeout.json", st)
			return fmt.Errorf("baseline not ready within budget (state=%s services_with_baseline=%d). Do not inject fault",
				st.Status.State, st.Status.ServicesWithBaseline)
		}
		st, err = getLiveStatus(ctx, client, base)
		if err != nil {
			return fmt.Errorf("live status poll: %w", err)
		}
		svcs, err := getLiveServices(ctx, client, base)
		if err != nil {
			return fmt.Errorf("live services poll: %w", err)
		}
		incs, err := getLiveIncidents(ctx, client, base)
		if err != nil {
			return fmt.Errorf("live incidents poll: %w", err)
		}
		for _, iv := range incs.Incidents {
			if err := assertNoScenarioLeak(iv); err != nil {
				return err
			}
			if iv.PrimaryService == *targetService && iv.Lifecycle == "active" {
				writeJSON(*artifactDir, "live-incidents-premature.json", incs)
				return fmt.Errorf("active incident on %s during baseline wait — refusing fault injection", *targetService)
			}
		}
		elapsed := time.Since(baselineStart)
		targetOK := false
		for _, s := range svcs.Services {
			if s.ServiceID != *targetService {
				continue
			}
			if s.GraphNode != "" && s.GraphNode != *targetGraph {
				return fmt.Errorf("graph_node=%q want %q", s.GraphNode, *targetGraph)
			}
			lat := signalValue(s, "latency_ms")
			if lat != nil && s.BaselineReady && s.Freshness == "fresh" {
				baselineLatency = lat
				targetOK = true
			}
		}
		if elapsed >= minBaseline && st.Status.ServicesWithBaseline >= 1 && targetOK && st.Status.Available {
			baselineReadyAt = time.Now().UTC()
			writeJSON(*artifactDir, "live-status-baseline-ready.json", st)
			writeJSON(*artifactDir, "live-services-baseline-ready.json", svcs)
			if baselineLatency != nil {
				writeFile(*artifactDir, "baseline-latency.txt", fmt.Sprintf("latency_ms=%.4f\n", *baselineLatency))
			}
			break
		}
		time.Sleep(poll)
	}
	ok("baseline ready at %s after ≥%ds (latency_ms observed)", baselineReadyAt.Format(time.RFC3339), *baselineSecs)

	faultOn := time.Now().UTC()
	rec, err := h.Start(ctx, *scenarioID)
	if err != nil {
		return fmt.Errorf("start scenario: %w", err)
	}
	writeFile(*artifactDir, "fault-on.txt", fmt.Sprintf("scenario=%s run_id=%s at=%s\n", rec.ScenarioID, rec.RunID, faultOn.Format(time.RFC3339Nano)))
	ok("fault activated via flagd (scenario harness); labels stay out of Pulse API")

	note("waiting up to %ds for active latency_ms incident on %s", *faultSecs, *targetService)
	deadline = time.Now().Add(time.Duration(*faultSecs) * time.Second)
	var incidentID string
	var duringLatency *float64
	for {
		if time.Now().After(deadline) {
			incs, _ := getLiveIncidents(ctx, client, base)
			svcs, _ := getLiveServices(ctx, client, base)
			st, _ = getLiveStatus(ctx, client, base)
			writeJSON(*artifactDir, "live-incidents-fault-timeout.json", incs)
			writeJSON(*artifactDir, "live-services-fault-timeout.json", svcs)
			writeJSON(*artifactDir, "live-status-fault-timeout.json", st)
			return fmt.Errorf("no qualified active latency_ms incident on %s within %ds after fault", *targetService, *faultSecs)
		}
		incs, err := getLiveIncidents(ctx, client, base)
		if err != nil {
			return fmt.Errorf("incidents: %w", err)
		}
		svcs, err := getLiveServices(ctx, client, base)
		if err != nil {
			return fmt.Errorf("services: %w", err)
		}
		for _, s := range svcs.Services {
			if s.ServiceID == *targetService {
				duringLatency = signalValue(s, "latency_ms")
			}
		}
		for _, iv := range incs.Incidents {
			if err := assertNoScenarioLeak(iv); err != nil {
				return err
			}
			if iv.PrimaryService != *targetService || iv.Lifecycle != "active" {
				continue
			}
			if !iv.FirstDetected.IsZero() && iv.FirstDetected.Before(faultOn.Add(-30*time.Second)) {
				continue
			}
			if !hasSignal(iv.SignalTypes, "latency_ms") {
				continue // require latency evidence for this preferred fault
			}
			if baselineLatency != nil && duringLatency != nil && *duringLatency <= *baselineLatency*1.5 && *duringLatency < *baselineLatency+200 {
				// Telemetry must actually worsen; avoid unrelated errors masquerading.
				continue
			}
			incidentID = iv.IncidentID
			if iv.GraphNode != "" && iv.GraphNode != *targetGraph {
				return fmt.Errorf("incident graph_node=%q want %q", iv.GraphNode, *targetGraph)
			}
			writeJSON(*artifactDir, "live-incidents-active.json", incs)
			writeJSON(*artifactDir, "live-services-during-fault.json", svcs)
			break
		}
		if incidentID != "" {
			break
		}
		time.Sleep(poll)
	}
	ok("active latency incident %s on %s", incidentID, *targetService)

	faultOff := time.Now().UTC()
	stopped, err := h.Stop(ctx, *scenarioID)
	if err != nil {
		return fmt.Errorf("stop scenario: %w", err)
	}
	cleared = true
	writeFile(*artifactDir, "fault-off.txt", fmt.Sprintf("scenario=%s run_id=%s recovered_flag=%v at=%s\n",
		stopped.ScenarioID, stopped.RunID, stopped.RecoveryVerified, faultOff.Format(time.RFC3339Nano)))
	ok("fault cleared (flag idle); waiting for measured recovery — not inferring from flag alone")

	note("waiting up to %ds for incident %s to leave active with latency evidence", *recoverSecs, incidentID)
	deadline = time.Now().Add(time.Duration(*recoverSecs) * time.Second)
	for {
		if time.Now().After(deadline) {
			incs, _ := getLiveIncidents(ctx, client, base)
			writeJSON(*artifactDir, "live-incidents-recover-timeout.json", incs)
			return fmt.Errorf("recovery not observed within %ds for %s", *recoverSecs, incidentID)
		}
		incs, err := getLiveIncidents(ctx, client, base)
		if err != nil {
			return fmt.Errorf("incidents: %w", err)
		}
		svcs, err := getLiveServices(ctx, client, base)
		if err != nil {
			return fmt.Errorf("services: %w", err)
		}
		var afterLat *float64
		for _, s := range svcs.Services {
			if s.ServiceID == *targetService && s.Freshness == "fresh" {
				afterLat = signalValue(s, "latency_ms")
			}
		}
		active := false
		hist := false
		for _, iv := range incs.Incidents {
			if err := assertNoScenarioLeak(iv); err != nil {
				return err
			}
			if iv.IncidentID != incidentID {
				continue
			}
			if iv.Lifecycle == "active" {
				active = true
			}
			if iv.Lifecycle == "recovered" || iv.Lifecycle == "historical" {
				hist = true
			}
		}
		// Require measured recovery: fresh latency near baseline when available.
		latencyRecovered := afterLat != nil && baselineLatency != nil &&
			*afterLat < *baselineLatency*2.0 && *afterLat < 2000
		if !active && hist && (latencyRecovered || afterLat == nil && hist) {
			// Prefer measured latency recovery; if series briefly missing, hist alone is insufficient.
			if afterLat == nil {
				time.Sleep(poll)
				continue
			}
			if !latencyRecovered {
				time.Sleep(poll)
				continue
			}
			writeJSON(*artifactDir, "live-incidents-recovered.json", incs)
			writeJSON(*artifactDir, "live-services-recovered.json", svcs)
			break
		}
		time.Sleep(poll)
	}
	ok("incident %s recovered/historical with measured latency recovery", incidentID)

	writeFile(*artifactDir, "live-fault-summary.txt", strings.Join([]string{
		"result=PASS",
		"incident_id=" + incidentID,
		"target_service=" + *targetService,
		"target_graph_node=" + *targetGraph,
		"baseline_ready_at=" + baselineReadyAt.Format(time.RFC3339Nano),
		"fault_on_at=" + faultOn.Format(time.RFC3339Nano),
		"fault_off_at=" + faultOff.Format(time.RFC3339Nano),
		"scenario_injection=" + *scenarioID,
		"note=Scenario id used for flagd only; absent from live API payloads.",
		"note=Real Prometheus telemetry observed through Live Pulse runtime.",
		"note=Required latency_ms on incident + measured recovery latency.",
	}, "\n")+"\n")
	ok("live fault lifecycle verified")
	return nil
}

func signalValue(s live.ServiceView, name string) *float64 {
	for _, sig := range s.Signals {
		if string(sig.Signal) == name && sig.Quality != "missing" && sig.Value != nil {
			v := *sig.Value
			return &v
		}
	}
	return nil
}

func hasSignal(types []string, want string) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}

func getLiveStatus(ctx context.Context, c *http.Client, base string) (api.LiveStatusResponse, error) {
	var out api.LiveStatusResponse
	b, code, err := httpGet(ctx, c, base+"/api/pulse/live/status")
	if err != nil {
		return out, err
	}
	if code != http.StatusOK {
		return out, fmt.Errorf("HTTP %d: %s", code, truncate(string(b), 400))
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return out, err
	}
	return out, nil
}

func getLiveServices(ctx context.Context, c *http.Client, base string) (api.LiveServicesResponse, error) {
	var out api.LiveServicesResponse
	b, code, err := httpGet(ctx, c, base+"/api/pulse/live/services")
	if err != nil {
		return out, err
	}
	if code != http.StatusOK {
		return out, fmt.Errorf("HTTP %d: %s", code, truncate(string(b), 400))
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return out, err
	}
	return out, nil
}

func getLiveIncidents(ctx context.Context, c *http.Client, base string) (api.LiveIncidentsResponse, error) {
	var out api.LiveIncidentsResponse
	b, code, err := httpGet(ctx, c, base+"/api/pulse/live/incidents")
	if err != nil {
		return out, err
	}
	if code != http.StatusOK {
		return out, fmt.Errorf("HTTP %d: %s", code, truncate(string(b), 400))
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return out, err
	}
	return out, nil
}

func assertNoScenarioLeak(iv interface{}) error {
	b, _ := json.Marshal(iv)
	low := strings.ToLower(string(b))
	for _, leak := range []string{
		"latency-shipping-intl", "intlshippingslowdown", "error-payment",
		"paymentfailure", "ground_truth", `"scenario_id"`,
	} {
		if strings.Contains(low, leak) {
			return fmt.Errorf("scenario ground-truth leak in live API payload: %s", leak)
		}
	}
	return nil
}

func httpGet(ctx context.Context, c *http.Client, rawURL string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	res, err := c.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return b, res.StatusCode, err
}

func requireLoopbackHTTPBase(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme must be http(s)")
	}
	host := u.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("host must be loopback, got %q", host)
		}
	}
	return nil
}

func writeFile(dir, name, body string) {
	_ = os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600)
}

func writeJSON(dir, name string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}
	writeFile(dir, name, string(b)+"\n")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func ok(format string, args ...any)   { fmt.Printf("OK: "+format+"\n", args...) }
func note(format string, args ...any) { fmt.Printf("NOTE: "+format+"\n", args...) }
func warn(format string, args ...any) { fmt.Fprintf(os.Stderr, "WARN: "+format+"\n", args...) }
