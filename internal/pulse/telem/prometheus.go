// Package telem collects replaceable telemetry inputs for Pulse detectors.
// Prometheus is the first backend; Databricks may implement the same Source
// surface later without changing detect package contracts.
package telem

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/astronomy"
	"github.com/yashg4509/perch/internal/pulse/detect"
)

const (
	defaultHTTPTimeout = 10 * time.Second
	// maxPromResponseBytes bounds a single Prometheus HTTP body.
	maxPromResponseBytes = 4 << 20 // 4 MiB
	// defaultMaxSeries rejects label/cardinality explosions per query.
	defaultMaxSeries = 64
)

// Source yields detector samples at a point in time.
type Source interface {
	Collect(ctx context.Context, at time.Time) ([]detect.Sample, error)
}

// Prometheus scrapes windowed spanmetrics from a Prometheus HTTP API.
// Queries are fixed/allowlisted in Collect — never accept PromQL from callers.
type Prometheus struct {
	BaseURL    string
	HTTPClient *http.Client
	Window     time.Duration // lookback for increase(); default 2m
	Services   []string      // OTEL service names; empty = default shop core
	// Environment is the Pulse identity segment (astronomy/<env>/<service>).
	// Empty defaults to astronomy.LocalEnvironment.
	Environment string
	// MaxSeries caps series returned per query (default 64).
	MaxSeries int
}

// NewPrometheus returns a client with timeouts.
func NewPrometheus(baseURL string) *Prometheus {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:9090"
	}
	return &Prometheus{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
		// Spanmetrics via OTLP often need >=2m of increase() for stable points.
		Window:      2 * time.Minute,
		Environment: astronomy.LocalEnvironment,
		MaxSeries:   defaultMaxSeries,
		Services: []string{
			"payment", "shipping", "checkout", "frontend", "cart",
			"product-catalog", "recommendation", "ad", "currency", "email", "quote",
		},
	}
}

type promResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []any             `json:"value"`
		} `json:"result"`
	} `json:"data"`
	Error string `json:"error"`
}

// Collect queries latency_ms, error_rate, and call_rate per configured service.
// Missing series are omitted — never invented as zero errors or healthy status.
func (p *Prometheus) Collect(ctx context.Context, at time.Time) ([]detect.Sample, error) {
	at = at.UTC()
	win := p.Window
	if win <= 0 {
		win = 2 * time.Minute
	}
	winStr := formatPromDuration(win)
	env := strings.TrimSpace(p.Environment)
	if env == "" {
		env = astronomy.LocalEnvironment
	}

	// Fixed allowlisted PromQL against Astronomy Shop spanmetrics
	// (traces_span_metrics_* from the OTEL demo collector).
	latencyQ := fmt.Sprintf(
		`histogram_quantile(0.99, sum by (service_name, le) (rate(traces_span_metrics_duration_milliseconds_bucket[%s])))`,
		winStr,
	)
	errorQ := fmt.Sprintf(
		`sum by (service_name) (increase(traces_span_metrics_calls_total{status_code="STATUS_CODE_ERROR"}[%s])) / clamp_min(sum by (service_name) (increase(traces_span_metrics_calls_total[%s])), 1)`,
		winStr, winStr,
	)
	callQ := fmt.Sprintf(
		`sum by (service_name) (increase(traces_span_metrics_calls_total[%s])) / %g`,
		winStr, win.Seconds(),
	)

	want := map[string]struct{}{}
	for _, s := range p.Services {
		want[s] = struct{}{}
	}

	lat, err := p.query(ctx, latencyQ)
	if err != nil {
		return nil, err
	}
	errRates, err := p.query(ctx, errorQ)
	if err != nil {
		return nil, err
	}
	calls, err := p.query(ctx, callQ)
	if err != nil {
		return nil, err
	}

	out := make([]detect.Sample, 0, len(want)*3)
	appendPresent := func(signal detect.SignalType, m map[string]float64) error {
		names := p.Services
		if len(names) == 0 {
			for name := range m {
				names = append(names, name)
			}
		}
		for _, name := range names {
			val, ok := m[name]
			if !ok {
				continue
			}
			id, err := astronomy.ServiceID(env, name)
			if err != nil {
				return err
			}
			s := detect.Sample{
				ServiceID:  id,
				Signal:     signal,
				Value:      val,
				ObservedAt: at,
				Source:     "prometheus",
			}
			if err := s.Validate(); err != nil {
				return err
			}
			out = append(out, s)
		}
		return nil
	}
	if err := appendPresent(detect.SignalLatencyMS, lat); err != nil {
		return nil, err
	}
	if err := appendPresent(detect.SignalCallRate, calls); err != nil {
		return nil, err
	}
	// Error rate: Prometheus omits the ERROR series when there are zero errors.
	// Emit 0 only when the service has a fresh call_rate observation — never invent
	// healthy zeros for services that were not observed this scrape.
	names := p.Services
	if len(names) == 0 {
		for name := range calls {
			names = append(names, name)
		}
	}
	for _, name := range names {
		if _, observed := calls[name]; !observed {
			continue
		}
		val, ok := errRates[name]
		if !ok {
			val = 0
		}
		id, err := astronomy.ServiceID(env, name)
		if err != nil {
			return nil, err
		}
		s := detect.Sample{
			ServiceID:  id,
			Signal:     detect.SignalErrorRate,
			Value:      val,
			ObservedAt: at,
			Source:     "prometheus",
		}
		if err := s.Validate(); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	detect.SortSamples(out)
	return out, nil
}

func (p *Prometheus) query(ctx context.Context, promQL string) (map[string]float64, error) {
	u, err := url.Parse(p.BaseURL + "/api/v1/query")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("query", promQL)
	u.RawQuery = q.Encode()

	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telem: prometheus query: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPromResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxPromResponseBytes {
		return nil, fmt.Errorf("telem: prometheus response exceeds %d bytes", maxPromResponseBytes)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("telem: prometheus status %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var pr promResponse
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, fmt.Errorf("telem: prometheus json: %w", err)
	}
	if pr.Status != "success" {
		return nil, fmt.Errorf("telem: prometheus error: %s", pr.Error)
	}
	maxSeries := p.MaxSeries
	if maxSeries <= 0 {
		maxSeries = defaultMaxSeries
	}
	if len(pr.Data.Result) > maxSeries {
		return nil, fmt.Errorf("telem: prometheus cardinality %d exceeds max %d", len(pr.Data.Result), maxSeries)
	}
	out := map[string]float64{}
	for _, r := range pr.Data.Result {
		name := r.Metric["service_name"]
		if name == "" {
			continue
		}
		if len(r.Value) < 2 {
			continue
		}
		// Prometheus instant vector: [timestamp_seconds, "value_string"]
		tsRaw := r.Value[0]
		switch v := tsRaw.(type) {
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
				continue
			}
		case json.Number:
			f, err := v.Float64()
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
				continue
			}
		case string:
			f, err := strconv.ParseFloat(v, 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
				continue
			}
		default:
			// Accept missing/odd timestamp encodings only when value parses;
			// sample ObservedAt comes from the Collect clock, not Prom ts.
		}
		raw, ok := r.Value[1].(string)
		if !ok {
			continue
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		out[name] = v
	}
	return out, nil
}

func formatPromDuration(d time.Duration) string {
	sec := int(d.Seconds())
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	if sec%60 == 0 {
		return fmt.Sprintf("%dm", sec/60)
	}
	return fmt.Sprintf("%ds", sec)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
