// Package tracesmoke qualifies Jaeger API responses for Astronomy Shop CI smoke.
//
// It parses the Jaeger HTTP JSON schema used by the OpenTelemetry Demo UI
// (/jaeger/ui/api/…) and decides whether a trace proves multi-service execution
// for a safe synthetic workflow. Live Docker orchestration lives outside this
// package (examples/astronomy-shop).
package tracesmoke

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ServicesResponse is Jaeger GET /api/services.
type ServicesResponse struct {
	Data   []string `json:"data"`
	Total  int      `json:"total"`
	Limit  int      `json:"limit"`
	Offset int      `json:"offset"`
	Errors any      `json:"errors"`
}

// TracesResponse is Jaeger GET /api/traces.
type TracesResponse struct {
	Data   []Trace `json:"data"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
	Errors any     `json:"errors"`
}

// Trace is one Jaeger trace object.
type Trace struct {
	TraceID   string             `json:"traceID"`
	Spans     []Span             `json:"spans"`
	Processes map[string]Process `json:"processes"`
	Warnings  any                `json:"warnings"`
}

// Span is one Jaeger span.
type Span struct {
	TraceID       string      `json:"traceID"`
	SpanID        string      `json:"spanID"`
	OperationName string      `json:"operationName"`
	References    []Reference `json:"references"`
	StartTime     int64       `json:"startTime"` // microseconds since epoch
	Duration      int64       `json:"duration"`
	Tags          []Tag       `json:"tags"`
	ProcessID     string      `json:"processID"`
}

// Reference is a Jaeger span reference (typically CHILD_OF).
type Reference struct {
	RefType string `json:"refType"`
	TraceID string `json:"traceID"`
	SpanID  string `json:"spanID"`
}

// Tag is a Jaeger span/process tag. Value may be string, bool, float, or int.
type Tag struct {
	Key   string `json:"key"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

// Process is a Jaeger process entry (service identity metadata).
type Process struct {
	ServiceName string `json:"serviceName"`
	Tags        []Tag  `json:"tags"`
}

// ParseServices decodes a Jaeger services API body.
// Malformed JSON fails immediately (retry cannot help).
func ParseServices(raw []byte) (ServicesResponse, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return ServicesResponse{}, fmt.Errorf("empty Jaeger services response")
	}
	var resp ServicesResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return ServicesResponse{}, fmt.Errorf("malformed Jaeger services JSON: %w", err)
	}
	if resp.Data == nil {
		return ServicesResponse{}, fmt.Errorf("malformed Jaeger services JSON: missing data array")
	}
	return resp, nil
}

// ParseTraces decodes a Jaeger traces API body.
// Malformed JSON fails immediately (retry cannot help).
func ParseTraces(raw []byte) (TracesResponse, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return TracesResponse{}, fmt.Errorf("empty Jaeger traces response")
	}
	var resp TracesResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return TracesResponse{}, fmt.Errorf("malformed Jaeger traces JSON: %w", err)
	}
	if resp.Data == nil {
		return TracesResponse{}, fmt.Errorf("malformed Jaeger traces JSON: missing data array")
	}
	return resp, nil
}

// ServiceNames returns distinct non-empty process service names for a trace.
func (t Trace) ServiceNames() []string {
	seen := map[string]struct{}{}
	var out []string
	for _, p := range t.Processes {
		name := strings.TrimSpace(p.ServiceName)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// EarliestStart returns the earliest span start time, or zero if none.
func (t Trace) EarliestStart() time.Time {
	var min int64
	for _, s := range t.Spans {
		if s.StartTime <= 0 {
			continue
		}
		if min == 0 || s.StartTime < min {
			min = s.StartTime
		}
	}
	if min == 0 {
		return time.Time{}
	}
	return time.UnixMicro(min).UTC()
}

// TagString returns the string form of a tag value, if present.
func TagString(tags []Tag, key string) string {
	for _, tag := range tags {
		if tag.Key != key {
			continue
		}
		switch v := tag.Value.(type) {
		case string:
			return v
		case float64:
			return fmt.Sprintf("%v", v)
		case bool:
			return fmt.Sprintf("%t", v)
		case json.Number:
			return v.String()
		default:
			if v == nil {
				return ""
			}
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}
