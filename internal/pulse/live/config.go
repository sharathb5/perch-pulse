package live

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/astronomy"
	"github.com/yashg4509/perch/internal/pulse/runpulse"
)

// Config is explicit server-side Live Pulse configuration.
// All fields are populated from process environment — never from browser input.
type Config struct {
	// Enabled must be true for the collector to start.
	Enabled bool
	// PrometheusURL is the allowlisted Prometheus HTTP base (loopback only).
	PrometheusURL string
	// PollInterval between collection cycles (default 15s).
	PollInterval time.Duration
	// RequestTimeout for each Prometheus HTTP call (default 10s).
	RequestTimeout time.Duration
	// Environment is the Pulse identity segment (astronomy/<env>/<service>).
	Environment string
	// Freshness is how long a successful sample remains "fresh" (default 2m).
	Freshness time.Duration
	// SampleWindow is the PromQL lookback window (default 2m).
	SampleWindow time.Duration
	// MaxServices bounds service rows retained in the snapshot (default 32).
	MaxServices int
	// PulseDir is the persistence root for incident.FileStore.
	// Empty uses the caller's resolved viz Pulse directory.
	PulseDir string
}

// ConfigFromEnv loads Live Pulse config from environment variables.
//
//	PERCH_LIVE_PULSE_ENABLED=1|true|yes     opt-in switch (default off)
//	PERCH_LIVE_PULSE_PROM_URL               required when enabled (loopback http/https)
//	PERCH_LIVE_PULSE_POLL_INTERVAL          e.g. 15s (default 15s)
//	PERCH_LIVE_PULSE_TIMEOUT                e.g. 10s (default 10s)
//	PERCH_LIVE_PULSE_ENVIRONMENT            default "local"
//	PERCH_LIVE_PULSE_FRESHNESS              e.g. 2m (default 2m)
//	PERCH_LIVE_PULSE_SAMPLE_WINDOW          e.g. 2m (default 2m)
//	PERCH_LIVE_PULSE_MAX_SERVICES           default 32
//
// Persistence uses existing PERCH_PULSE_DIR / .perch/pulse semantics supplied
// by the viz wiring layer (PulseDir on Options), not a separate secret store.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		Enabled:        parseBoolEnv(os.Getenv("PERCH_LIVE_PULSE_ENABLED")),
		PrometheusURL:  strings.TrimSpace(os.Getenv("PERCH_LIVE_PULSE_PROM_URL")),
		PollInterval:   15 * time.Second,
		RequestTimeout: 10 * time.Second,
		Environment:    astronomy.LocalEnvironment,
		Freshness:      2 * time.Minute,
		SampleWindow:   2 * time.Minute,
		MaxServices:    32,
	}
	if v, err := parseDurationEnv("PERCH_LIVE_PULSE_POLL_INTERVAL", cfg.PollInterval); err != nil {
		return Config{}, err
	} else {
		cfg.PollInterval = v
	}
	if v, err := parseDurationEnv("PERCH_LIVE_PULSE_TIMEOUT", cfg.RequestTimeout); err != nil {
		return Config{}, err
	} else {
		cfg.RequestTimeout = v
	}
	if v, err := parseDurationEnv("PERCH_LIVE_PULSE_FRESHNESS", cfg.Freshness); err != nil {
		return Config{}, err
	} else {
		cfg.Freshness = v
	}
	if v, err := parseDurationEnv("PERCH_LIVE_PULSE_SAMPLE_WINDOW", cfg.SampleWindow); err != nil {
		return Config{}, err
	} else {
		cfg.SampleWindow = v
	}
	if env := strings.TrimSpace(os.Getenv("PERCH_LIVE_PULSE_ENVIRONMENT")); env != "" {
		cfg.Environment = env
	}
	if raw := strings.TrimSpace(os.Getenv("PERCH_LIVE_PULSE_MAX_SERVICES")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 128 {
			return Config{}, fmt.Errorf("PERCH_LIVE_PULSE_MAX_SERVICES: must be 1..128")
		}
		cfg.MaxServices = n
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks configuration invariants. Disabled configs need not set a URL.
func (c Config) Validate() error {
	if c.PollInterval < time.Second {
		return fmt.Errorf("live pulse: poll interval must be >= 1s")
	}
	if c.PollInterval > 5*time.Minute {
		return fmt.Errorf("live pulse: poll interval must be <= 5m")
	}
	if c.RequestTimeout < 100*time.Millisecond {
		return fmt.Errorf("live pulse: request timeout must be >= 100ms")
	}
	if c.RequestTimeout > time.Minute {
		return fmt.Errorf("live pulse: request timeout must be <= 1m")
	}
	if c.Freshness < time.Second {
		return fmt.Errorf("live pulse: freshness must be >= 1s")
	}
	if c.SampleWindow < time.Second {
		return fmt.Errorf("live pulse: sample window must be >= 1s")
	}
	if c.MaxServices < 1 || c.MaxServices > 128 {
		return fmt.Errorf("live pulse: max services must be 1..128")
	}
	if strings.TrimSpace(c.Environment) == "" {
		return fmt.Errorf("live pulse: environment is required")
	}
	if _, err := astronomy.ServiceID(c.Environment, "frontend"); err != nil {
		return fmt.Errorf("live pulse: environment: %w", err)
	}
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.PrometheusURL) == "" {
		return fmt.Errorf("live pulse: PERCH_LIVE_PULSE_PROM_URL is required when enabled")
	}
	if _, err := runpulse.ValidateAllowlistedHTTPURL(c.PrometheusURL); err != nil {
		return fmt.Errorf("PERCH_LIVE_PULSE_PROM_URL: %w", err)
	}
	return nil
}

// Available reports whether Live Pulse can collect (enabled + URL configured).
func (c Config) Available() bool {
	return c.Enabled && strings.TrimSpace(c.PrometheusURL) != ""
}

// UnavailabilityReason explains why live status should report unavailable.
func (c Config) UnavailabilityReason() string {
	if !c.Enabled {
		return "Live Pulse is disabled. Set PERCH_LIVE_PULSE_ENABLED=1 and PERCH_LIVE_PULSE_PROM_URL to a loopback Prometheus (e.g. http://127.0.0.1:9090)."
	}
	if strings.TrimSpace(c.PrometheusURL) == "" {
		return "Live Pulse enabled but PERCH_LIVE_PULSE_PROM_URL is not set."
	}
	return ""
}

func parseBoolEnv(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parseDurationEnv(name string, def time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return d, nil
}
