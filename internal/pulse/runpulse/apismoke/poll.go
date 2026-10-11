package apismoke

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/runpulse"
)

// PollConfig bounds GET /api/pulse/runs/{id} polling.
type PollConfig struct {
	// Interval between polls (default 2s).
	Interval time.Duration
	// OverallDeadline is the absolute deadline; if zero, uses ctx only.
	OverallDeadline time.Time
	// MaxAttempts caps polls when > 0 (in addition to deadline).
	MaxAttempts int
	// HTTPClient defaults to http.DefaultClient.
	HTTPClient *http.Client
}

// PollResult captures the last observed run and poll metadata.
type PollResult struct {
	Run          *runpulse.Run
	Attempts     int
	LastHTTPCode int
	LastStatus   string
}

// PollRun polls until terminal status, context cancel, deadline, or max attempts.
func PollRun(ctx context.Context, vizBaseURL, runID string, cfg PollConfig) (*PollResult, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, failf("invalid_run_id", "run id required for polling")
	}
	base := strings.TrimRight(strings.TrimSpace(vizBaseURL), "/")
	if base == "" {
		return nil, failf("misconfigured", "viz base URL required")
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	url := fmt.Sprintf("%s/api/pulse/runs/%s", base, runID)
	out := &PollResult{}
	attempt := 0
	for {
		if err := ctx.Err(); err != nil {
			return out, failf("poll_timeout", "polling canceled: %v (last_status=%s attempts=%d)", err, out.LastStatus, out.Attempts)
		}
		if !cfg.OverallDeadline.IsZero() && time.Now().After(cfg.OverallDeadline) {
			return out, failf("poll_timeout", "polling deadline exceeded (last_status=%s attempts=%d)", out.LastStatus, out.Attempts)
		}
		if cfg.MaxAttempts > 0 && attempt >= cfg.MaxAttempts {
			return out, failf("poll_timeout", "max poll attempts %d exceeded (last_status=%s)", cfg.MaxAttempts, out.LastStatus)
		}
		attempt++
		out.Attempts = attempt

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return out, failf("get_run_failed", "build GET: %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return out, failf("get_run_failed", "GET %s: %v", url, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		out.LastHTTPCode = resp.StatusCode
		if readErr != nil {
			return out, failf("get_run_failed", "read body: %v", readErr)
		}
		run, err := ParseGetRun(resp.StatusCode, body, runID)
		if err != nil {
			return out, err
		}
		out.Run = run
		out.LastStatus = run.Status

		switch ClassifyStatus(run.Status) {
		case StatusIntermediate:
			// continue
		case StatusTerminalOK, StatusTerminalFail:
			return out, nil
		default:
			return out, failf("invalid_status", "unknown status %q", run.Status)
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return out, failf("poll_timeout", "polling canceled: %v (last_status=%s attempts=%d)", ctx.Err(), out.LastStatus, out.Attempts)
		case <-timer.C:
		}
	}
}
