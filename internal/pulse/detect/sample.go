// Package detect implements telemetry-only baseline comparison and regression
// findings for Pulse. It must not import the scenario ground-truth package;
// evaluation against labels belongs in internal/pulse/evaluate.
package detect

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// SignalType is a detector input signal derived from observed telemetry.
type SignalType string

const (
	SignalLatencyMS SignalType = "latency_ms"
	SignalErrorRate SignalType = "error_rate"
	SignalCallRate  SignalType = "call_rate"
)

// Valid reports whether s is a known signal.
func (s SignalType) Valid() bool {
	switch s {
	case SignalLatencyMS, SignalErrorRate, SignalCallRate:
		return true
	default:
		return false
	}
}

// Sample is one numeric telemetry point for a service/signal at ObservedAt.
// Values must be finite and non-secret.
type Sample struct {
	ServiceID  string
	Signal     SignalType
	Value      float64
	ObservedAt time.Time
	Source     string // e.g. "prometheus"
}

// Validate checks sample invariants.
func (s Sample) Validate() error {
	if s.ServiceID == "" {
		return fmt.Errorf("detect: sample service_id is required")
	}
	if !s.Signal.Valid() {
		return fmt.Errorf("detect: invalid signal %q", s.Signal)
	}
	if s.ObservedAt.IsZero() {
		return fmt.Errorf("detect: sample observed_at is required")
	}
	if s.ObservedAt.Location() != time.UTC {
		return fmt.Errorf("detect: sample observed_at must be UTC")
	}
	if math.IsNaN(s.Value) || math.IsInf(s.Value, 0) {
		return fmt.Errorf("detect: sample value must be finite")
	}
	if s.Signal == SignalErrorRate && (s.Value < 0 || s.Value > 1) {
		return fmt.Errorf("detect: error_rate must be in [0,1]")
	}
	if (s.Signal == SignalLatencyMS || s.Signal == SignalCallRate) && s.Value < 0 {
		return fmt.Errorf("detect: %s must be >= 0", s.Signal)
	}
	if s.Source == "" {
		return fmt.Errorf("detect: sample source is required")
	}
	return nil
}

// SeriesKey identifies a service/signal series.
type SeriesKey struct {
	ServiceID string
	Signal    SignalType
}

func (k SeriesKey) String() string {
	return k.ServiceID + "|" + string(k.Signal)
}

// SortSamples orders by ObservedAt ascending, then ServiceID, Signal, Value.
func SortSamples(in []Sample) {
	sort.SliceStable(in, func(i, j int) bool {
		a, b := in[i], in[j]
		if !a.ObservedAt.Equal(b.ObservedAt) {
			return a.ObservedAt.Before(b.ObservedAt)
		}
		if a.ServiceID != b.ServiceID {
			return a.ServiceID < b.ServiceID
		}
		if a.Signal != b.Signal {
			return a.Signal < b.Signal
		}
		return a.Value < b.Value
	})
}
