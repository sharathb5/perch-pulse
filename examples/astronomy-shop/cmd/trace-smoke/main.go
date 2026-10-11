// Command trace-smoke sends a bounded Astronomy Shop recommendations browse
// request and fails unless Jaeger returns a same-trace multi-service proof.
//
// Intended for examples/astronomy-shop/scripts/ci-smoke.sh (LIVE SMOKE only).
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/astronomy/tracesmoke"
)

func main() {
	var (
		baseURL      = flag.String("base-url", "http://127.0.0.1:8080", "Astronomy Shop frontend-proxy base URL")
		jaegerBase   = flag.String("jaeger-base", "", "Jaeger UI base (default: <base-url>/jaeger/ui)")
		artifactDir  = flag.String("artifact-dir", "", "directory for bounded diagnostics")
		productID    = flag.String("product-id", tracesmoke.DefaultProductID, "catalog product id for recommendations browse")
		marker       = flag.String("marker", "", "unique user-agent marker (required)")
		deadlineSecs = flag.Int("deadline-secs", 120, "overall deadline seconds covering probes + Jaeger polling")
		pollSecs     = flag.Int("poll-secs", 3, "poll interval seconds")
		reqTimeout   = flag.Int("request-timeout-secs", 15, "per-request HTTP timeout seconds")
		maxRequests  = flag.Int("max-requests", 5, "max bounded probe GETs")
	)
	flag.Parse()

	if strings.TrimSpace(*marker) == "" {
		fail("marker is required (unique per smoke run)")
	}
	if *deadlineSecs <= 0 || *pollSecs <= 0 || *reqTimeout <= 0 || *maxRequests <= 0 {
		fail("deadline-secs, poll-secs, request-timeout-secs, and max-requests must be > 0")
	}
	if *artifactDir == "" {
		fail("artifact-dir is required")
	}
	if err := os.MkdirAll(*artifactDir, 0o750); err != nil {
		fail("mkdir artifact-dir: %v", err)
	}

	jb := strings.TrimSpace(*jaegerBase)
	if jb == "" {
		jb = strings.TrimRight(*baseURL, "/") + "/jaeger/ui"
	}
	jb = strings.TrimRight(jb, "/")

	overallDeadline := time.Now().Add(time.Duration(*deadlineSecs) * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), overallDeadline)
	defer cancel()

	client := &http.Client{Timeout: time.Duration(*reqTimeout) * time.Second}
	windowStart := time.Now().UTC().Add(-2 * time.Second)
	probeURL := tracesmoke.RecommendationsProbeURL(*baseURL, *productID)

	writeFile(*artifactDir, "trace-smoke-config.txt", strings.Join([]string{
		"workflow=" + tracesmoke.RecommendationsBrowse,
		"probe_url=" + tracesmoke.RedactURL(probeURL),
		"jaeger_base=" + jb,
		"marker=" + *marker,
		"product_id=" + *productID,
		"deadline_secs=" + fmt.Sprintf("%d", *deadlineSecs),
		"poll_secs=" + fmt.Sprintf("%d", *pollSecs),
		"max_requests=" + fmt.Sprintf("%d", *maxRequests),
		"window_start=" + windowStart.Format(time.RFC3339Nano),
	}, "\n")+"\n")

	// Bounded probe traffic (read-only recommendations browse).
	// Under load-generator pressure the Envoy proxy may return 504 while the
	// frontend/recommendation spans still export — success is same-trace evidence.
	var lastProbeCode int
	probeOK := false
	for i := 1; i <= *maxRequests; i++ {
		if err := ctx.Err(); err != nil {
			fail("deadline exceeded during probe attempts: %v", err)
		}
		code, n, err := doProbe(ctx, client, probeURL, *marker)
		lastProbeCode = code
		writeFile(*artifactDir, fmt.Sprintf("probe-%d.status.txt", i),
			fmt.Sprintf("http=%d body_bytes=%d err=%v\n", code, n, err))
		fmt.Printf("probe_attempt=%d http=%d body_bytes=%d err=%v\n", i, code, n, err)
		if err == nil && code == 200 {
			probeOK = true
			break
		}
		if i < *maxRequests {
			if !sleepCtx(ctx, time.Duration(*pollSecs)*time.Second) {
				break
			}
		}
	}
	if probeOK {
		okf("probe HTTP 200 for %s", tracesmoke.RecommendationsBrowse)
	} else {
		fmt.Printf("NOTE: probe last HTTP %d after %d attempts; continuing to Jaeger poll for marker spans\n",
			lastProbeCode, *maxRequests)
		writeFile(*artifactDir, "probe-note.txt",
			fmt.Sprintf("last_http=%d attempts=%d; success requires qualifying Jaeger trace\n", lastProbeCode, *maxRequests))
	}

	// Reachability: services API must respond (malformed → immediate fail).
	servicesURL := tracesmoke.ServicesURL(jb)
	servicesBody, err := getBytes(ctx, client, servicesURL)
	if err != nil {
		collectFailureDiagnostics(ctx, client, jb, *artifactDir, "jaeger_unreachable")
		fail("Jaeger services API unreachable at %s: %v", servicesURL, err)
	}
	services, err := tracesmoke.ParseServices(servicesBody)
	if err != nil {
		fail("Jaeger services API malformed (not retrying): %v", err)
	}
	writeFile(*artifactDir, "jaeger-services-names.txt", strings.Join(services.Data, "\n")+"\n")
	okf("Jaeger reachable; service_count=%d", len(services.Data))

	criteria := tracesmoke.Criteria{
		Workflow:                tracesmoke.RecommendationsBrowse,
		Marker:                  *marker,
		PathSubstring:           tracesmoke.RecommendationsPath,
		ProductID:               *productID,
		WindowStart:             windowStart,
		WindowEnd:               time.Time{},
		MinAppServices:          2,
		RequireCrossServiceEdge: true,
	}

	attempts := 0
	var lastRes tracesmoke.Result
	for {
		attempts++
		if err := ctx.Err(); err != nil {
			break
		}
		criteria.WindowEnd = time.Now().UTC().Add(2 * time.Second)

		var combined []tracesmoke.Trace
		for _, svc := range []string{"frontend", "recommendation", "frontend-proxy"} {
			parsed, qerr := queryTraces(ctx, client, jb, svc, windowStart, criteria.WindowEnd, *marker)
			if qerr != nil {
				collectFailureDiagnostics(ctx, client, jb, *artifactDir, "jaeger_traces_unreachable")
				fail("Jaeger traces API unreachable (%s): %v", svc, qerr)
			}
			writeFile(*artifactDir, fmt.Sprintf("jaeger-summary-%s-attempt-%d.txt", svc, attempts),
				summarizeTraces(parsed.Data, 5))
			combined = append(combined, parsed.Data...)
		}

		res, found := tracesmoke.FindQualifyingTrace(combined, criteria)
		lastRes = res
		writeFile(*artifactDir, fmt.Sprintf("qualify-attempt-%d.txt", attempts), strings.Join(res.SummaryLines(), "\n")+"\n")
		fmt.Printf("poll_attempt=%d found=%t reason=%s\n", attempts, found, res.Reason)
		if found {
			writeFile(*artifactDir, "trace-smoke-result.txt", strings.Join(res.SummaryLines(), "\n")+"\n")
			okf("multi-service trace qualified trace_id=%s app_services=%v", res.TraceID, res.AppServices)
			return
		}

		if !sleepCtx(ctx, time.Duration(*pollSecs)*time.Second) {
			break
		}
	}

	writeFile(*artifactDir, "trace-smoke-result.txt", strings.Join(append([]string{
		"ok=false",
		fmt.Sprintf("poll_attempts=%d", attempts),
		fmt.Sprintf("deadline_secs=%d", *deadlineSecs),
	}, lastRes.SummaryLines()...), "\n")+"\n")
	collectFailureDiagnostics(ctx, client, jb, *artifactDir, "no_qualifying_trace")
	if lastRes.SpanCount > 0 && len(lastRes.AppServices) < 2 {
		fail("trace evidence insufficient before deadline: %s", lastRes.Reason)
	}
	fail("no qualifying multi-service trace before deadline (%ds, attempts=%d): %s", *deadlineSecs, attempts, lastRes.Reason)
}

func doProbe(ctx context.Context, client *http.Client, probeURL, marker string) (int, int, error) {
	res := tracesmoke.ProbeGET(ctx, client, probeURL, marker)
	if res.Error != "" {
		return res.StatusCode, res.BodyBytes, fmt.Errorf("%s", res.Error)
	}
	return res.StatusCode, res.BodyBytes, nil
}

func getBytes(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	return tracesmoke.GetJSONBytes(ctx, client, rawURL)
}

func queryTraces(ctx context.Context, client *http.Client, jaegerBase, service string, start, end time.Time, marker string) (tracesmoke.TracesResponse, error) {
	return tracesmoke.QueryTraces(ctx, client, jaegerBase, service, start, end, marker, 50)
}

func summarizeTraces(traces []tracesmoke.Trace, max int) string {
	if len(traces) == 0 {
		return "trace_count=0\n"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("trace_count=%d\n", len(traces)))
	n := 0
	for _, tr := range traces {
		if n >= max {
			b.WriteString("… additional traces omitted …\n")
			break
		}
		b.WriteString(tracesmoke.CompactTraceSummary(tr, 12))
		b.WriteString("---\n")
		n++
	}
	return b.String()
}

var diagnosticContainers = []string{
	"otel-collector", "jaeger", "frontend", "recommendation", "product-catalog",
}

func collectFailureDiagnostics(ctx context.Context, client *http.Client, jaegerBase, dir, reason string) {
	writeFile(dir, "failure-reason.txt", reason+"\n")
	for _, name := range diagnosticContainers {
		out, err := runDockerLogs(name, 80)
		if err != nil {
			writeFile(dir, "docker-logs-"+name+".txt", fmt.Sprintf("unavailable: %v\n", err))
			continue
		}
		writeFile(dir, "docker-logs-"+name+".txt", redactLogText(out))
	}
	if body, err := getBytes(ctx, client, tracesmoke.ServicesURL(jaegerBase)); err == nil {
		if svc, perr := tracesmoke.ParseServices(body); perr == nil {
			writeFile(dir, "jaeger-services-names-on-failure.txt", strings.Join(svc.Data, "\n")+"\n")
		}
	}
}

func runDockerLogs(container string, lines int) (string, error) {
	allowed := false
	for _, name := range diagnosticContainers {
		if container == name {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("container %q not in diagnostic allowlist", container)
	}
	if lines <= 0 || lines > 200 {
		return "", fmt.Errorf("lines out of bounds")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	// #nosec G204 -- binary is constant "docker"; container is allowlisted; lines is bounded int.
	cmd := exec.CommandContext(ctx, "docker", "logs", "--tail", fmt.Sprintf("%d", lines), container)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return snip(string(out), 32*1024), nil
}

func redactLogText(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lower := strings.ToLower(line)
		for _, key := range []string{"authorization", "password", "api_key", "secret", "token=", "bearer "} {
			if strings.Contains(lower, key) {
				lines[i] = "[redacted-line]"
				break
			}
		}
	}
	return strings.Join(lines, "\n")
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func writeFile(dir, name, content string) {
	_ = os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600)
}

func snip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n… truncated …\n"
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAIL: "+format+"\n", args...)
	os.Exit(1)
}

func okf(format string, args ...any) {
	fmt.Printf("OK: "+format+"\n", args...)
}
