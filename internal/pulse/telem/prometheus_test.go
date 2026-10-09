package telem

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/pulse/detect"
)

func TestPrometheusCollectMapsServices(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/query", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		var results []map[string]any
		switch {
		case strings.Contains(q, "histogram_quantile") && strings.Contains(q, "duration_milliseconds_bucket"):
			results = []map[string]any{{
				"metric": map[string]string{"service_name": "payment"},
				"value":  []any{1.0, "120.5"},
			}}
		case strings.Contains(q, `status_code="STATUS_CODE_ERROR"`):
			// Omit payment entirely to ensure defaultZero emits 0 for error_rate
			// when testing missing series — except we also want a positive case.
			results = []map[string]any{{
				"metric": map[string]string{"service_name": "checkout"},
				"value":  []any{1.0, "0.25"},
			}}
		default:
			results = []map[string]any{{
				"metric": map[string]string{"service_name": "payment"},
				"value":  []any{1.0, "0.5"},
			}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data":   map[string]any{"resultType": "vector", "result": results},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := NewPrometheus(srv.URL)
	p.Services = []string{"payment"}
	at := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	samples, err := p.Collect(context.Background(), at)
	if err != nil {
		t.Fatal(err)
	}
	// latency + call_rate + error_rate(default 0 because calls observed)
	if len(samples) != 3 {
		t.Fatalf("samples=%d %+v", len(samples), samples)
	}
	seen := map[detect.SignalType]float64{}
	for _, s := range samples {
		if err := s.Validate(); err != nil {
			t.Fatal(err)
		}
		seen[s.Signal] = s.Value
	}
	if seen[detect.SignalLatencyMS] != 120.5 {
		t.Fatalf("latency %v", seen)
	}
	if seen[detect.SignalErrorRate] != 0 {
		t.Fatalf("expected default-zero error_rate when calls present, got %v", seen)
	}
	if seen[detect.SignalCallRate] != 0.5 {
		t.Fatalf("call_rate %v", seen)
	}
}

func TestPrometheusOmitsUnobservedServiceZeros(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/query", func(w http.ResponseWriter, r *http.Request) {
		// No series for any query — must not invent healthy zeros.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data":   map[string]any{"resultType": "vector", "result": []any{}},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	p := NewPrometheus(srv.URL)
	p.Services = []string{"payment"}
	samples, err := p.Collect(context.Background(), time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 0 {
		t.Fatalf("expected no invented samples, got %+v", samples)
	}
}
