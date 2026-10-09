package detect

import "time"

// Config tunes the explainable baseline detector.
//
// Defaults favor demo-scale Astronomy Shop traffic: short rolling windows,
// relative latency change with an absolute floor, and absolute error-rate
// deltas. Thresholds are not derived from ground-truth labels.
type Config struct {
	// BaselineWindow is how far back (before the current window) baseline samples come from.
	BaselineWindow time.Duration
	// CurrentWindow is the recent observation window compared to baseline.
	CurrentWindow time.Duration
	// MinBaselineSamples required before emitting findings.
	MinBaselineSamples int
	// MinCurrentSamples required in the current window.
	MinCurrentSamples int

	// LatencyRelThreshold: fire when relative increase >= this (e.g. 1.0 = +100%).
	LatencyRelThreshold float64
	// LatencyAbsMinMS: also require absolute median increase of at least this many ms.
	LatencyAbsMinMS float64
	// LatencyRobustZ: optional MAD z-score gate (0 disables).
	LatencyRobustZ float64
	// LatencyMaxMS ignores absurd aggregated values from sparse histograms.
	LatencyMaxMS float64
	// MinCallRateForLatency requires concurrent call_rate median >= this.
	MinCallRateForLatency float64

	// ErrorRateAbsDelta: fire when error_rate median rises by at least this.
	ErrorRateAbsDelta float64
	// ErrorRateMin: current error_rate must also be >= this.
	ErrorRateMin float64

	// OutageErrorRate: fire availability finding when error_rate >= this.
	OutageErrorRate float64
	// OutageCallRateDrop: fire when call_rate drops by this relative fraction.
	OutageCallRateDrop float64

	DetectorVersion string
}

// DefaultConfig returns demo-appropriate defaults.
func DefaultConfig() Config {
	return Config{
		BaselineWindow:        3 * time.Minute,
		CurrentWindow:         45 * time.Second,
		MinBaselineSamples:    4,
		MinCurrentSamples:     2,
		LatencyRelThreshold:   1.0,   // +100%
		LatencyAbsMinMS:       1000,  // p99 ambient swings are hundreds of ms; real demo faults are seconds
		LatencyRobustZ:        3.0,   // robust confirmation when MAD > 0
		LatencyMaxMS:          30000, // allow demo 10s fault p99 while rejecting absurd outliers
		MinCallRateForLatency: 0.05,
		ErrorRateAbsDelta:     0.10, // +10 percentage points
		ErrorRateMin:          0.05,
		OutageErrorRate:       0.35,
		OutageCallRateDrop:    0.55, // 55% throughput drop
		DetectorVersion:       DetectorVersion,
	}
}
