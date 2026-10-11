package tracesmoke_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/pulse/astronomy/tracesmoke"
)

func TestGetJSONBytes_DetectsTruncation(t *testing.T) {
	huge := bytes.Repeat([]byte("a"), tracesmoke.MaxResponseBytes+64)
	// Valid-looking JSON prefix so Content-Type checks pass; size triggers truncation.
	body := append([]byte(`{"data":"`), huge...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	_, err := tracesmoke.GetJSONBytes(t.Context(), srv.Client(), srv.URL)
	if err == nil || !errors.Is(err, tracesmoke.ErrResponseTruncated) {
		t.Fatalf("got %v, want ErrResponseTruncated", err)
	}
}

func TestGetJSONBytes_RejectsHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>proxy error</html>"))
	}))
	defer srv.Close()
	_, err := tracesmoke.GetJSONBytes(t.Context(), srv.Client(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "non-JSON") {
		t.Fatalf("got %v", err)
	}
}

func TestQueryTraces_MarkerPreferred(t *testing.T) {
	marker := "perch-run-pulse/run_test"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.RawQuery, "tags=") {
			_, _ = w.Write([]byte(`{"data":[{"traceID":"abc","spans":[],"processes":{}}]}`))
			return
		}
		t.Fatalf("unexpected unfiltered query: %s", r.URL.RawQuery)
	}))
	defer srv.Close()

	resp, err := tracesmoke.QueryTraces(t.Context(), srv.Client(), srv.URL, "frontend",
		time.Now().Add(-time.Minute), time.Now(), marker, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 || resp.Data[0].TraceID != "abc" {
		t.Fatalf("%+v", resp.Data)
	}
}

func TestQueryTraces_TruncatedUnfilteredSoftEmpty(t *testing.T) {
	// Marker filter empty; unfiltered always oversized → soft empty (nil error).
	huge := make([]byte, tracesmoke.MaxResponseBytes+128)
	for i := range huge {
		huge[i] = 'x'
	}
	payload := append([]byte(`{"data":[`), bytes.Repeat([]byte(`{"traceID":"t","spans":[],"processes":{}},`), 200)...)
	payload = append(payload, huge...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.RawQuery, "tags=") {
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	resp, err := tracesmoke.QueryTraces(t.Context(), srv.Client(), srv.URL, "frontend",
		time.Now().Add(-time.Minute), time.Now(), "perch-run-pulse/run_x", 50)
	if err != nil {
		t.Fatalf("expected soft-empty nil error, got %v", err)
	}
	if len(resp.Data) != 0 {
		t.Fatalf("want empty data, got %d traces", len(resp.Data))
	}
}

func TestQueryTraces_AdaptiveLimitRecovers(t *testing.T) {
	var sawLimits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		lim := r.URL.Query().Get("limit")
		sawLimits = append(sawLimits, lim)
		if strings.Contains(r.URL.RawQuery, "tags=") {
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		if lim == "50" || lim == "20" {
			body := bytes.Repeat([]byte("y"), tracesmoke.MaxResponseBytes+8)
			_, _ = w.Write(append([]byte(`{"data":"`), body...))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"traceID":"ok","spans":[],"processes":{}}]}`))
	}))
	defer srv.Close()

	resp, err := tracesmoke.QueryTraces(t.Context(), srv.Client(), srv.URL, "frontend",
		time.Now().Add(-time.Minute), time.Now(), "marker", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 || resp.Data[0].TraceID != "ok" {
		t.Fatalf("%+v limits=%v", resp.Data, sawLimits)
	}
	joined := strings.Join(sawLimits, ",")
	if !strings.Contains(joined, "50") || !strings.Contains(joined, "10") {
		t.Fatalf("expected adaptive limits, saw %v", sawLimits)
	}
}

func TestQueryTraces_TrulyMalformedStillFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":`)) // complete short body, invalid JSON
	}))
	defer srv.Close()
	_, err := tracesmoke.QueryTraces(t.Context(), srv.Client(), srv.URL, "frontend",
		time.Now().Add(-time.Minute), time.Now(), "", 5)
	if err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("got %v", err)
	}
}

func TestQueryTraces_EmptyMarkerFallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.RawQuery, "tags=") {
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		enc := json.NewEncoder(w)
		_ = enc.Encode(map[string]any{
			"data": []any{
				map[string]any{"traceID": "fallback", "spans": []any{}, "processes": map[string]any{}},
			},
		})
	}))
	defer srv.Close()
	resp, err := tracesmoke.QueryTraces(t.Context(), srv.Client(), srv.URL, "frontend",
		time.Now().Add(-time.Minute), time.Now(), "missing-marker", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 || resp.Data[0].TraceID != "fallback" {
		t.Fatalf("%+v", resp.Data)
	}
}
