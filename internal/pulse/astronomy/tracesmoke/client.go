package tracesmoke

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// MaxResponseBytes bounds Jaeger/API response bodies.
	// Bodies larger than this are treated as truncated (not silently sliced).
	MaxResponseBytes = 512 * 1024
	// MaxProbeBodyBytes bounds probe response discard size.
	MaxProbeBodyBytes = 64 * 1024
)

// ErrResponseTruncated reports that a Jaeger/API body exceeded MaxResponseBytes.
// Callers should retry with a smaller query limit or treat the fetch as empty
// for polling — not as a hard "malformed JSON" failure.
var ErrResponseTruncated = errors.New("response truncated: exceeded MaxResponseBytes")

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
// If the body exceeds MaxResponseBytes, it returns ErrResponseTruncated rather
// than a silently truncated buffer that would fail JSON parsing mid-document.
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(MaxResponseBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > MaxResponseBytes {
		return nil, fmt.Errorf("%w (limit %d bytes)", ErrResponseTruncated, MaxResponseBytes)
	}
	if err := rejectNonJSONBody(resp.Header.Get("Content-Type"), body); err != nil {
		return nil, err
	}
	return body, nil
}

func rejectNonJSONBody(contentType string, body []byte) error {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if ct != "" && strings.Contains(ct, "text/html") {
		return fmt.Errorf("non-JSON Content-Type %q", contentType)
	}
	trim := bytes.TrimSpace(body)
	if len(trim) > 0 && trim[0] == '<' {
		return fmt.Errorf("non-JSON response (looks like HTML)")
	}
	return nil
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
//
// Oversized Jaeger payloads (common under load-generator traffic) are detected
// as ErrResponseTruncated. QueryTraces then retries with smaller limits and,
// if still truncated, returns an empty result so callers can keep polling for
// the marker-filtered trace instead of failing as "malformed JSON".
func QueryTraces(ctx context.Context, client HTTPDoer, jaegerBase, service string, start, end time.Time, marker string, limit int) (TracesResponse, error) {
	if limit <= 0 {
		limit = 50
	}
	limits := descendingLimits(limit)

	if strings.TrimSpace(marker) != "" {
		for _, lim := range limits {
			resp, err := fetchTraces(ctx, client, jaegerBase, service, start, end, marker, lim)
			if err == nil {
				if len(resp.Data) > 0 {
					return resp, nil
				}
				// Empty marker filter — stop shrinking; try unfiltered.
				break
			}
			if errors.Is(err, ErrResponseTruncated) {
				continue
			}
			// Network/HTTP/HTML errors on the marker query: still try unfiltered
			// (matches prior fallthrough behavior when GetJSONBytes failed).
			break
		}
	}

	var lastTrunc error
	for _, lim := range limits {
		resp, err := fetchTraces(ctx, client, jaegerBase, service, start, end, "", lim)
		if err == nil {
			return resp, nil
		}
		if errors.Is(err, ErrResponseTruncated) {
			lastTrunc = err
			continue
		}
		return TracesResponse{}, err
	}
	if lastTrunc != nil {
		// Soft-empty: keep polling; marker filter usually succeeds once the
		// diagnostic request is indexed. Do not surface truncated JSON as
		// "malformed" / telemetry_unreachable.
		return TracesResponse{Data: []Trace{}}, nil
	}
	return TracesResponse{Data: []Trace{}}, nil
}

func fetchTraces(ctx context.Context, client HTTPDoer, jaegerBase, service string, start, end time.Time, marker string, limit int) (TracesResponse, error) {
	raw, err := GetJSONBytes(ctx, client, TracesURL(jaegerBase, service, start, end, limit, marker))
	if err != nil {
		return TracesResponse{}, err
	}
	return ParseTraces(raw)
}

func descendingLimits(max int) []int {
	if max <= 0 {
		max = 50
	}
	candidates := []int{max, 20, 10, 5, 2}
	seen := map[int]struct{}{}
	out := make([]int, 0, len(candidates))
	for _, n := range candidates {
		if n <= 0 || n > max {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	if len(out) == 0 {
		return []int{max}
	}
	return out
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
