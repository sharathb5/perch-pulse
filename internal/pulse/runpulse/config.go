package runpulse

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config is explicit server-side Run Pulse configuration.
// When DemoBaseURL is empty, workflows are listed as unavailable.
type Config struct {
	// DemoBaseURL is the allowlisted Astronomy Shop frontend-proxy base
	// (e.g. http://127.0.0.1:8080). Must be loopback.
	DemoBaseURL string
	// JaegerBaseURL defaults to DemoBaseURL + "/jaeger/ui" when empty.
	JaegerBaseURL string
	// MaxConcurrentRuns caps in-flight diagnostics (default 1).
	MaxConcurrentRuns int
	// MaxRetainedRuns bounds in-memory run history (default 20).
	MaxRetainedRuns int
	// RetentionTTL drops finished runs after this duration (default 30m).
	RetentionTTL time.Duration
	// OverallTimeout overrides workflow overall timeout when > 0.
	OverallTimeout time.Duration
}

// ConfigFromEnv loads Run Pulse config from environment variables.
//
//	PERCH_RUN_PULSE_DEMO_BASE_URL   required for availability (loopback only)
//	PERCH_RUN_PULSE_JAEGER_BASE     optional Jaeger UI base (loopback only)
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		DemoBaseURL:       strings.TrimSpace(os.Getenv("PERCH_RUN_PULSE_DEMO_BASE_URL")),
		JaegerBaseURL:     strings.TrimSpace(os.Getenv("PERCH_RUN_PULSE_JAEGER_BASE")),
		MaxConcurrentRuns: 1,
		MaxRetainedRuns:   20,
		RetentionTTL:      30 * time.Minute,
	}
	if cfg.DemoBaseURL == "" {
		return cfg, nil
	}
	if _, err := ValidateAllowlistedHTTPURL(cfg.DemoBaseURL); err != nil {
		return Config{}, fmt.Errorf("PERCH_RUN_PULSE_DEMO_BASE_URL: %w", err)
	}
	if cfg.JaegerBaseURL == "" {
		cfg.JaegerBaseURL = strings.TrimRight(cfg.DemoBaseURL, "/") + "/jaeger/ui"
	}
	if _, err := ValidateAllowlistedHTTPURL(cfg.JaegerBaseURL); err != nil {
		return Config{}, fmt.Errorf("PERCH_RUN_PULSE_JAEGER_BASE: %w", err)
	}
	return cfg, nil
}

// Available reports whether a demo endpoint is configured.
func (c Config) Available() bool {
	return strings.TrimSpace(c.DemoBaseURL) != ""
}

// UnavailabilityReason explains why Run Pulse cannot start a workflow.
func (c Config) UnavailabilityReason() string {
	if c.Available() {
		return ""
	}
	return "Astronomy Shop demo endpoint not configured. Set PERCH_RUN_PULSE_DEMO_BASE_URL to a loopback URL (e.g. http://127.0.0.1:8080)."
}
