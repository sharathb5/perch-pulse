package tracesmoke

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// MaxResponseBytes bounds Jaeger/API response bodies.
	MaxResponseBytes = 512 * 1024
	// MaxProbeBodyBytes bounds probe response discard size.
	MaxProbeBodyBytes = 64 * 1024
)

// HTTPDoer is satisfied by *http.Client.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// ProbeResult is the observed outcome of one diagnostic HTTP request.
type ProbeResult struct {
	StatusCode  int
	BodyBytes   int
	Elapsed     time.Duration
	URLRedacted string
	Error       string
}

// ProbeGET issues a bounded GET with the marker as User-Agent.
// Callers must supply an allowlisted URL (SSRF checks live outside this package).
func ProbeGET(ctx context.Context, client HTTPDoer, rawURL, marker string) ProbeResult {
	out := ProbeResult{URLRedacted: RedactURL(rawURL)}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		out.Error = err.Error()
		out.Elapsed = time.Since(start)
		return out
	}
	req.Header.Set("User-Agent", marker)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	out.Elapsed = time.Since(start)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	defer resp.Body.Close()
	n, _ := io.Copy(io.Discard, io.LimitReader(resp.Body, MaxProbeBodyBytes))
	out.StatusCode = resp.StatusCode
	out.BodyBytes = int(n)
	return out
}

// GetJSONBytes GETs a URL and returns a bounded body for 2xx responses.
func GetJSONBytes(ctx context.Context, client HTTPDoer, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4*1024))
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes))
}

// ServicesURL builds Jaeger GET /api/services.
func ServicesURL(jaegerBase string) string {
	return strings.TrimRight(jaegerBase, "/") + "/api/services"
}

// TracesURL builds Jaeger GET /api/traces for one service and time window.
// When marker is non-empty, an exact user_agent tag filter is applied.
func TracesURL(jaegerBase, service string, start, end time.Time, limit int, marker string) string {
	if limit <= 0 {
		limit = 50
	}
	q := url.Values{}
	q.Set("service", service)
	q.Set("limit", fmt.Sprintf("%d", limit))
	q.Set("start", fmt.Sprintf("%d", start.UnixMicro()))
	q.Set("end", fmt.Sprintf("%d", end.UnixMicro()))
	if marker != "" {
		tags, _ := json.Marshal(map[string]string{"user_agent": marker})
		q.Set("tags", string(tags))
	}
	return strings.TrimRight(jaegerBase, "/") + "/api/traces?" + q.Encode()
}

// QueryTraces fetches traces for a service, preferring a user_agent tag filter.
// Falls back to an unfiltered window query when the filter returns empty.
func QueryTraces(ctx context.Context, client HTTPDoer, jaegerBase, service string, start, end time.Time, marker string, limit int) (TracesResponse, error) {
	raw, err := GetJSONBytes(ctx, client, TracesURL(jaegerBase, service, start, end, limit, marker))
	if err == nil {
		parsed, perr := ParseTraces(raw)
		if perr != nil {
			return TracesResponse{}, perr
		}
		if len(parsed.Data) > 0 {
			return parsed, nil
		}
	}
	raw, err = GetJSONBytes(ctx, client, TracesURL(jaegerBase, service, start, end, limit, ""))
	if err != nil {
		return TracesResponse{}, err
	}
	parsed, perr := ParseTraces(raw)
	if perr != nil {
		return TracesResponse{}, perr
	}
	return parsed, nil
}

// RedactURL strips userinfo and non-allowlisted query params for API surfaces.
// Only the recommendations productIds query key is preserved when present.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[redacted-url]"
	}
	u.User = nil
	u.Fragment = ""
	productIDs := u.Query().Get("productIds")
	u.RawQuery = ""
	if productIDs != "" {
		q := url.Values{}
		q.Set("productIds", productIDs)
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// RecommendationsProbeURL builds the fixed Astronomy recommendations browse URL.
func RecommendationsProbeURL(baseURL, productID string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	pid := strings.TrimSpace(productID)
	if pid == "" {
		pid = DefaultProductID
	}
	return fmt.Sprintf("%s%s?productIds=%s", base, RecommendationsPath, url.QueryEscape(pid))
}
