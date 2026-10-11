package runpulse

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yashg4509/perch/internal/pulse/astronomy"
	"github.com/yashg4509/perch/internal/pulse/astronomy/tracesmoke"
)

// ServiceMapper resolves OTEL → Pulse → graph node identities.
type ServiceMapper interface {
	GraphNodeForPulseServiceID(serviceID string) (graphNode string, source string, ok bool)
}

// Engine executes allowlisted Run Pulse diagnostics asynchronously.
type Engine struct {
	cfg    Config
	store  *Store
	mapper ServiceMapper
	client *http.Client
	now    func() time.Time

	mu       sync.Mutex
	startMu  sync.Mutex // serializes StartRun concurrency admission
	cancels  map[string]context.CancelFunc
	envPulse string // astronomy environment segment (usually "local")
}

// NewEngine constructs a Run Pulse engine. cfg.DemoBaseURL may be empty
// (workflows listed as unavailable).
func NewEngine(cfg Config, mapper ServiceMapper) (*Engine, error) {
	if cfg.MaxConcurrentRuns <= 0 {
		cfg.MaxConcurrentRuns = 1
	}
	if cfg.MaxRetainedRuns <= 0 {
		cfg.MaxRetainedRuns = 20
	}
	if cfg.RetentionTTL <= 0 {
		cfg.RetentionTTL = 30 * time.Minute
	}
	if cfg.Available() {
		if _, err := ValidateAllowlistedHTTPURL(cfg.DemoBaseURL); err != nil {
			return nil, fmt.Errorf("demo base URL: %w", err)
		}
		if cfg.JaegerBaseURL == "" {
			cfg.JaegerBaseURL = strings.TrimRight(cfg.DemoBaseURL, "/") + "/jaeger/ui"
		}
		if _, err := ValidateAllowlistedHTTPURL(cfg.JaegerBaseURL); err != nil {
			return nil, fmt.Errorf("jaeger base URL: %w", err)
		}
	}
	base := &http.Client{Timeout: 15 * time.Second}
	return &Engine{
		cfg:      cfg,
		store:    NewStore(cfg.MaxRetainedRuns, cfg.RetentionTTL, nil),
		mapper:   mapper,
		client:   SafeHTTPClient(base),
		now:      func() time.Time { return time.Now().UTC() },
		cancels:  make(map[string]context.CancelFunc),
		envPulse: astronomy.LocalEnvironment,
	}, nil
}

// Config returns a copy of the engine config.
func (e *Engine) Config() Config { return e.cfg }

// ListWorkflows returns catalog entries with availability.
func (e *Engine) ListWorkflows() []WorkflowAvailability {
	out := make([]WorkflowAvailability, 0, len(Catalog()))
	for _, w := range Catalog() {
		av := WorkflowAvailability{
			ID:                   w.ID,
			DisplayName:          w.DisplayName,
			Description:          w.Description,
			Environment:          w.Environment,
			SupportedSignals:     append([]string(nil), w.SupportedSignals...),
			Available:            e.cfg.Available(),
			UnavailabilityReason: e.cfg.UnavailabilityReason(),
			HTTPMethod:           w.HTTPMethod,
			RequestSummary:       fmt.Sprintf("%s %s?productIds=%s (read-only)", w.HTTPMethod, w.Path, w.ProductID),
			TelemetryBackend:     w.TelemetryBackend,
			ExpectedAppServices:  append([]string(nil), w.ExpectedAppServices...),
			MaxConcurrent:        w.MaxConcurrent,
			OverallTimeoutSecs:   int(w.OverallTimeout.Seconds()),
		}
		if !av.Available {
			// keep reason
		} else {
			av.UnavailabilityReason = ""
		}
		out = append(out, av)
	}
	return out
}

// WorkflowAvailability is a catalog entry for the API.
type WorkflowAvailability struct {
	ID                   string   `json:"id"`
	DisplayName          string   `json:"display_name"`
	Description          string   `json:"description"`
	Environment          string   `json:"environment"`
	SupportedSignals     []string `json:"supported_signals"`
	Available            bool     `json:"available"`
	UnavailabilityReason string   `json:"unavailability_reason,omitempty"`
	HTTPMethod           string   `json:"http_method"`
	RequestSummary       string   `json:"request_summary"`
	TelemetryBackend     string   `json:"telemetry_backend"`
	ExpectedAppServices  []string `json:"expected_app_services"`
	MaxConcurrent        int      `json:"max_concurrent"`
	OverallTimeoutSecs   int      `json:"overall_timeout_secs"`
}

// StartRun creates a pending run and starts async execution.
// Duplicate browser polls must call Get, not StartRun.
func (e *Engine) StartRun(workflowID string) (*Run, error) {
	wf, ok := LookupWorkflow(workflowID)
	if !ok {
		return nil, &StartError{Code: FailUnsupportedWorkflow, Message: fmt.Sprintf("unsupported workflow %q", workflowID)}
	}
	if !e.cfg.Available() {
		return nil, &StartError{Code: FailWorkflowUnavailable, Message: e.cfg.UnavailabilityReason()}
	}
	// Re-validate targets (env must not silently become non-loopback).
	if _, err := ValidateAllowlistedHTTPURL(e.cfg.DemoBaseURL); err != nil {
		return nil, &StartError{Code: FailMisconfigured, Message: "demo base URL failed allowlist validation: " + err.Error()}
	}
	if _, err := ValidateAllowlistedHTTPURL(e.cfg.JaegerBaseURL); err != nil {
		return nil, &StartError{Code: FailMisconfigured, Message: "jaeger base URL failed allowlist validation: " + err.Error()}
	}

	e.startMu.Lock()
	defer e.startMu.Unlock()

	maxConc := e.cfg.MaxConcurrentRuns
	if wf.MaxConcurrent > 0 && wf.MaxConcurrent < maxConc {
		maxConc = wf.MaxConcurrent
	}
	if e.store.RunningCount() >= maxConc {
		return nil, &StartError{Code: FailConcurrencyLimit, Message: fmt.Sprintf("concurrency limit reached (max %d in-flight diagnostic)", maxConc)}
	}

	id, err := NewRunID()
	if err != nil {
		return nil, &StartError{Code: FailInternal, Message: "could not allocate run id"}
	}
	marker := fmt.Sprintf("perch-run-pulse/%s", id)
	now := e.now()
	run := &Run{
		ID:          id,
		WorkflowID:  wf.ID,
		DisplayName: wf.DisplayName,
		Environment: wf.Environment,
		Status:      StatusPending,
		Phase:       PhaseQueued,
		StartedAt:   now,
		Limitations: []string{
			"On-demand diagnostic only — not continuous monitoring or incident detection.",
			"In-memory run store: results are lost if perch viz restarts.",
			"Observed services come from the qualified trace, not the workflow expected list alone.",
			"HTTP 200 alone does not mean the diagnostic completed; trace qualification is required.",
		},
	}
	e.store.Put(run)

	timeout := wf.OverallTimeout
	if e.cfg.OverallTimeout > 0 {
		timeout = e.cfg.OverallTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	e.mu.Lock()
	e.cancels[id] = cancel
	e.mu.Unlock()

	go e.execute(ctx, cancel, id, wf, marker)
	return run.Clone(), nil
}

// GetRun returns a clone of a run or false if missing/expired.
func (e *Engine) GetRun(id string) (*Run, bool) {
	return e.store.Get(id)
}

// StartError is a classified failure to start a run.
type StartError struct {
	Code    string
	Message string
}

func (e *StartError) Error() string { return e.Message }

func (e *Engine) execute(ctx context.Context, cancel context.CancelFunc, id string, wf Workflow, marker string) {
	defer func() {
		cancel()
		e.mu.Lock()
		delete(e.cancels, id)
		e.mu.Unlock()
	}()

	e.patch(id, func(r *Run) {
		r.Status = StatusRunning
		r.Phase = PhaseSendingRequest
	})

	probeURL := tracesmoke.RecommendationsProbeURL(e.cfg.DemoBaseURL, wf.ProductID)
	if _, err := ValidateAllowlistedHTTPURL(e.cfg.DemoBaseURL); err != nil {
		e.fail(id, FailMisconfigured, "probe target failed allowlist: "+err.Error(), nil)
		return
	}

	reqClient := SafeHTTPClient(&http.Client{Timeout: wf.RequestTimeout})
	windowStart := e.now().Add(-2 * time.Second)
	var lastProbe tracesmoke.ProbeResult
	attempts := 0
	probeOK := false
	for i := 1; i <= wf.MaxRequests; i++ {
		if err := ctx.Err(); err != nil {
			e.finishTimeoutOrCancel(id, err, tracesmoke.Result{})
			return
		}
		attempts = i
		lastProbe = tracesmoke.ProbeGET(ctx, reqClient, probeURL, marker)
		if lastProbe.Error == "" && lastProbe.StatusCode == 200 {
			probeOK = true
			break
		}
		if i < wf.MaxRequests {
			if !sleepCtx(ctx, wf.PollInterval) {
				e.finishTimeoutOrCancel(id, ctx.Err(), tracesmoke.Result{})
				return
			}
		}
	}

	reqResult := &RequestResult{
		Method:        wf.HTTPMethod,
		URLRedacted:   tracesmoke.RedactURL(probeURL),
		StatusCode:    lastProbe.StatusCode,
		ElapsedMs:     lastProbe.Elapsed.Seconds() * 1000,
		Attempts:      attempts,
		OK:            probeOK,
		Error:         lastProbe.Error,
		Marker:        marker,
		BodyBytesRead: lastProbe.BodyBytes,
	}
	e.patch(id, func(r *Run) {
		r.Request = reqResult
		r.Phase = PhaseWaitingForTrace
		if !probeOK {
			r.Warnings = appendUnique(r.Warnings,
				fmt.Sprintf("diagnostic HTTP request did not return 200 (last status=%d); continuing to wait for marked trace evidence", lastProbe.StatusCode))
		}
	})

	// Jaeger reachability
	svcBody, err := tracesmoke.GetJSONBytes(ctx, reqClient, tracesmoke.ServicesURL(e.cfg.JaegerBaseURL))
	if err != nil {
		if ctx.Err() != nil {
			e.finishTimeoutOrCancel(id, ctx.Err(), tracesmoke.Result{})
			return
		}
		e.fail(id, FailTelemetryUnreachable, "Jaeger services API unreachable: "+err.Error(), reqResult)
		return
	}
	if _, err := tracesmoke.ParseServices(svcBody); err != nil {
		e.fail(id, FailTelemetryUnreachable, "Jaeger services API malformed: "+err.Error(), reqResult)
		return
	}

	criteria := tracesmoke.Criteria{
		Workflow:                tracesmoke.RecommendationsBrowse,
		Marker:                  marker,
		PathSubstring:           tracesmoke.RecommendationsPath,
		ProductID:               wf.ProductID,
		WindowStart:             windowStart,
		MinAppServices:          2,
		RequireCrossServiceEdge: true,
	}

	var lastRes tracesmoke.Result
	var foundTrace *tracesmoke.Trace
	for {
		if err := ctx.Err(); err != nil {
			e.finishTimeoutOrCancel(id, err, lastRes)
			return
		}
		criteria.WindowEnd = e.now().Add(2 * time.Second)

		var combined []tracesmoke.Trace
		for _, svc := range wf.QueryServices {
			parsed, qerr := tracesmoke.QueryTraces(ctx, reqClient, e.cfg.JaegerBaseURL, svc, windowStart, criteria.WindowEnd, marker, 50)
			if qerr != nil {
				if ctx.Err() != nil {
					e.finishTimeoutOrCancel(id, ctx.Err(), lastRes)
					return
				}
				// Truncation is handled inside QueryTraces (soft-empty). Any residual
				// truncation error must not abort the run as "malformed JSON".
				if errors.Is(qerr, tracesmoke.ErrResponseTruncated) {
					e.patch(id, func(r *Run) {
						r.Warnings = appendUnique(r.Warnings,
							fmt.Sprintf("Jaeger traces response for %s exceeded size bound; retrying with smaller queries / marker filter", svc))
					})
					continue
				}
				e.fail(id, FailTelemetryUnreachable, fmt.Sprintf("Jaeger traces API unreachable (%s): %v", svc, qerr), reqResult)
				return
			}
			combined = append(combined, parsed.Data...)
		}

		res, found := tracesmoke.FindQualifyingTrace(combined, criteria)
		lastRes = res
		if found {
			for i := range combined {
				if combined[i].TraceID == res.TraceID {
					tr := combined[i]
					foundTrace = &tr
					break
				}
			}
			break
		}
		if !sleepCtx(ctx, wf.PollInterval) {
			e.finishTimeoutOrCancel(id, ctx.Err(), lastRes)
			return
		}
	}

	if foundTrace == nil {
		code := FailTraceNotFound
		msg := lastRes.Reason
		if msg == "" {
			msg = "no qualifying multi-service trace before deadline"
		}
		if lastRes.SpanCount > 0 && len(lastRes.AppServices) < 2 {
			code = FailTraceIncomplete
		}
		e.fail(id, code, msg, reqResult)
		return
	}

	e.patch(id, func(r *Run) {
		r.Phase = PhaseMappingServices
	})

	ev := tracesmoke.BuildEvidence(*foundTrace, lastRes, criteria)
	mapped, unmapped, nodes, errNodes := e.mapServices(ev)

	end := e.now()
	e.patch(id, func(r *Run) {
		r.Status = StatusCompleted
		r.Phase = PhaseDone
		r.EndedAt = &end
		r.Trace = &ev
		r.Services = mapped
		r.UnmappedServices = unmapped
		r.GraphNodes = nodes
		r.GraphNodesWithErrors = errNodes
		r.Warnings = appendUnique(r.Warnings, ev.Warnings...)
		r.FailureCode = ""
		r.FailureMessage = ""
	})
}

func (e *Engine) mapServices(ev tracesmoke.Evidence) (mapped []MappedService, unmapped, nodes, errNodes []string) {
	errBySvc := map[string]bool{}
	for _, s := range ev.Services {
		errBySvc[s.OTELServiceName] = s.HasError
	}
	seenNode := map[string]struct{}{}
	seenErrNode := map[string]struct{}{}

	for _, name := range ev.ObservedServices {
		ms := MappedService{
			OTELServiceName: name,
			Participation:   "observed",
			HasError:        errBySvc[name],
		}
		id, err := astronomy.ServiceID(e.envPulse, name)
		if err == nil {
			ms.PulseServiceID = id
			if e.mapper != nil {
				if node, src, ok := e.mapper.GraphNodeForPulseServiceID(id); ok {
					ms.GraphNode = node
					ms.MappingSource = src
					ms.Mapped = true
					if _, dup := seenNode[node]; !dup {
						seenNode[node] = struct{}{}
						nodes = append(nodes, node)
					}
					if ms.HasError {
						if _, dup := seenErrNode[node]; !dup {
							seenErrNode[node] = struct{}{}
							errNodes = append(errNodes, node)
						}
					}
				}
			}
		}
		if !ms.Mapped {
			unmapped = append(unmapped, name)
		}
		mapped = append(mapped, ms)
	}
	sort.Strings(nodes)
	sort.Strings(errNodes)
	sort.Strings(unmapped)
	return mapped, unmapped, nodes, errNodes
}

func (e *Engine) fail(id, code, msg string, req *RequestResult) {
	end := e.now()
	e.patch(id, func(r *Run) {
		r.Status = StatusFailed
		r.Phase = PhaseDone
		r.EndedAt = &end
		r.FailureCode = code
		r.FailureMessage = msg
		if req != nil && r.Request == nil {
			r.Request = req
		}
	})
}

func (e *Engine) finishTimeoutOrCancel(id string, err error, last tracesmoke.Result) {
	end := e.now()
	code := FailTimedOut
	msg := "diagnostic timed out before a qualifying multi-service trace was found"
	status := StatusTimedOut
	if err == context.Canceled {
		code = FailCancelled
		msg = "diagnostic cancelled"
		status = StatusFailed
	} else if last.Reason != "" {
		if last.SpanCount > 0 && len(last.AppServices) < 2 {
			code = FailTraceIncomplete
			status = StatusFailed
			msg = "incomplete trace evidence before deadline: " + last.Reason
		} else {
			msg = msg + ": " + last.Reason
		}
	}
	e.patch(id, func(r *Run) {
		r.Status = status
		r.Phase = PhaseDone
		r.EndedAt = &end
		r.FailureCode = code
		r.FailureMessage = msg
		if r.Request != nil && !r.Request.OK {
			r.Warnings = appendUnique(r.Warnings, "HTTP request did not succeed before timeout")
		}
	})
}

func (e *Engine) patch(id string, fn func(*Run)) {
	e.store.Update(id, fn)
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

func appendUnique(dst []string, in ...string) []string {
	seen := map[string]struct{}{}
	for _, s := range dst {
		seen[s] = struct{}{}
	}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		dst = append(dst, s)
	}
	return dst
}
