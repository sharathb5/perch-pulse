package llm

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTimeout       = 20 * time.Second
	defaultMaxTokens     = 1200
	defaultMaxConcurrent = 1
	defaultModel         = "gpt-4o-mini"
	providerName         = "openai_compatible"
)

// Config is explicit server-side LLM investigator configuration.
// Disabled by default. Credentials never appear in Status or logs.
type Config struct {
	Enabled       bool
	APIKey        string // never serialized; never logged
	BaseURL       string // e.g. https://api.openai.com/v1
	Model         string
	Timeout       time.Duration
	MaxTokens     int
	MaxConcurrent int
}

// ConfigFromEnv loads LLM config from environment variables.
//
//	PERCH_PULSE_LLM_ENABLED=1          required opt-in
//	PERCH_PULSE_LLM_API_KEY            required when enabled
//	PERCH_PULSE_LLM_BASE_URL           required when enabled (HTTPS unless loopback)
//	PERCH_PULSE_LLM_MODEL              optional (default gpt-4o-mini)
//	PERCH_PULSE_LLM_TIMEOUT_SEC        optional (default 20)
//	PERCH_PULSE_LLM_MAX_TOKENS         optional (default 1200, max 4096)
//	PERCH_PULSE_LLM_MAX_CONCURRENT     optional (default 1, max 4)
//
// Client-supplied provider URLs are never accepted.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		Enabled:       envTruthy(os.Getenv("PERCH_PULSE_LLM_ENABLED")),
		APIKey:        strings.TrimSpace(os.Getenv("PERCH_PULSE_LLM_API_KEY")),
		BaseURL:       strings.TrimSpace(os.Getenv("PERCH_PULSE_LLM_BASE_URL")),
		Model:         strings.TrimSpace(os.Getenv("PERCH_PULSE_LLM_MODEL")),
		Timeout:       defaultTimeout,
		MaxTokens:     defaultMaxTokens,
		MaxConcurrent: defaultMaxConcurrent,
	}
	if cfg.Model == "" {
		cfg.Model = defaultModel
	}
	if v := strings.TrimSpace(os.Getenv("PERCH_PULSE_LLM_TIMEOUT_SEC")); v != "" {
		sec, err := strconv.Atoi(v)
		if err != nil || sec < 1 || sec > 120 {
			return Config{}, fmt.Errorf("PERCH_PULSE_LLM_TIMEOUT_SEC must be 1..120")
		}
		cfg.Timeout = time.Duration(sec) * time.Second
	}
	if v := strings.TrimSpace(os.Getenv("PERCH_PULSE_LLM_MAX_TOKENS")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 64 || n > 4096 {
			return Config{}, fmt.Errorf("PERCH_PULSE_LLM_MAX_TOKENS must be 64..4096")
		}
		cfg.MaxTokens = n
	}
	if v := strings.TrimSpace(os.Getenv("PERCH_PULSE_LLM_MAX_CONCURRENT")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 4 {
			return Config{}, fmt.Errorf("PERCH_PULSE_LLM_MAX_CONCURRENT must be 1..4")
		}
		cfg.MaxConcurrent = n
	}
	if !cfg.Enabled {
		return cfg, nil
	}
	if cfg.APIKey == "" {
		return Config{}, fmt.Errorf("PERCH_PULSE_LLM_API_KEY is required when LLM investigation is enabled")
	}
	if cfg.BaseURL == "" {
		return Config{}, fmt.Errorf("PERCH_PULSE_LLM_BASE_URL is required when LLM investigation is enabled")
	}
	if _, err := ValidateProviderBaseURL(cfg.BaseURL); err != nil {
		return Config{}, fmt.Errorf("PERCH_PULSE_LLM_BASE_URL: %w", err)
	}
	return cfg, nil
}

// Available reports whether LLM mode can run (enabled + credentials + endpoint).
func (c Config) Available() bool {
	return c.Enabled && c.APIKey != "" && c.BaseURL != ""
}

// UnavailabilityReason explains why LLM mode cannot run.
func (c Config) UnavailabilityReason() string {
	if c.Available() {
		return ""
	}
	if !c.Enabled {
		return "LLM investigation is disabled. Set PERCH_PULSE_LLM_ENABLED=1 with a server-side API key and HTTPS base URL to opt in."
	}
	if c.APIKey == "" {
		return "LLM investigation enabled but PERCH_PULSE_LLM_API_KEY is missing."
	}
	if c.BaseURL == "" {
		return "LLM investigation enabled but PERCH_PULSE_LLM_BASE_URL is missing."
	}
	return "LLM investigation is unavailable."
}

// Status returns a secret-free availability snapshot.
func (c Config) Status() Status {
	st := Status{
		SchemaVersion: StatusSchemaVersion,
		Enabled:       c.Enabled,
		Configured:    c.APIKey != "" && c.BaseURL != "",
		Available:     c.Available(),
		Reason:        c.UnavailabilityReason(),
		Provider:      providerName,
	}
	if c.Available() {
		st.Model = c.Model
		if u, err := url.Parse(c.BaseURL); err == nil {
			st.EndpointHost = u.Host
		}
		st.Reason = ""
	}
	return st
}

// ValidateProviderBaseURL enforces safe endpoint configuration.
// Requires https:// except for loopback http:// used by local test fakes.
// Rejects userinfo, query credentials, and non-http(s) schemes.
func ValidateProviderBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.User != nil {
		return nil, fmt.Errorf("URL must not embed credentials")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("URL must not include query or fragment")
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("URL host required")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		// ok
	case "http":
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			// also allow localhost name for loopback fakes
			if !isLoopbackHost(host) {
				return nil, fmt.Errorf("http is only allowed for loopback endpoints; use https")
			}
		}
	default:
		return nil, fmt.Errorf("scheme must be https (or http on loopback)")
	}
	// Normalize: no trailing slash preference enforced at call site.
	return u, nil
}

func isLoopbackHost(host string) bool {
	h := strings.ToLower(host)
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func envTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
