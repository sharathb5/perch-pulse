package live

import (
	"fmt"
	"time"

	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/observation"
)

const activeHealthNote = "Active reachability/latency probes remain on GET /api/status and are not represented here."

func signalUnit(sig detect.SignalType) string {
	switch sig {
	case detect.SignalLatencyMS:
		return "ms"
	case detect.SignalErrorRate:
		return "ratio"
	case detect.SignalCallRate:
		return "calls_per_sec"
	default:
		return ""
	}
}

func qualityFor(observedAt time.Time, now time.Time, freshness time.Duration, present bool) string {
	if !present || observedAt.IsZero() {
		return "missing"
	}
	obs := observation.Observation{ObservedAt: observedAt.UTC()}
	if obs.IsStale(now, freshness) {
		return "stale"
	}
	return "fresh"
}

func intelligenceFromState(regressionOpen bool, quality string, baselineReady bool) observation.Status {
	switch quality {
	case "missing":
		return observation.StatusUnavailable
	case "unavailable":
		return observation.StatusUnavailable
	case "stale":
		// Stale is never healthy — even with a prior baseline and no open regression.
		return observation.StatusStale
	default: // fresh
		if regressionOpen {
			return observation.StatusDegraded
		}
		if !baselineReady {
			return observation.StatusUnknown
		}
		return observation.StatusHealthy
	}
}

func observationForService(serviceID string, at time.Time, status observation.Status, summary string) (observation.Observation, error) {
	// Producers cannot store StatusStale; persist unavailable so historical
	// /api/pulse/services never treats last-known as currently healthy.
	storeStatus := status
	if storeStatus == observation.StatusStale {
		storeStatus = observation.StatusUnavailable
	}
	obs := observation.Observation{
		ServiceID:  serviceID,
		ObservedAt: at.UTC(),
		IngestedAt: at.UTC(),
		Status:     storeStatus,
		Source:     "live_prometheus",
		Evidence: &observation.Evidence{
			Ref:     "live:prometheus:spanmetrics",
			Summary: summary,
		},
	}
	if err := obs.Validate(); err != nil {
		return observation.Observation{}, err
	}
	return obs, nil
}

func summarizeSignals(signals map[detect.SignalType]float64) string {
	if len(signals) == 0 {
		return "no spanmetrics observed this poll"
	}
	return fmt.Sprintf("observed %d spanmetrics signals", len(signals))
}
