package pipeline

import (
	"time"

	"github.com/yashg4509/perch/internal/pulse/detect"
)

// SynthSeries builds evenly spaced samples for one service/signal.
func SynthSeries(serviceID string, signal detect.SignalType, base time.Time, value float64, step time.Duration, n int, source string) []detect.Sample {
	out := make([]detect.Sample, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, detect.Sample{
			ServiceID:  serviceID,
			Signal:     signal,
			Value:      value,
			ObservedAt: base.Add(time.Duration(i) * step),
			Source:     source,
		})
	}
	return out
}

// LatencyBaselineThenSpike: healthy ~200ms baseline, then ~5000ms regression.
func LatencyBaselineThenSpike(serviceID string, base time.Time) []detect.Sample {
	var samples []detect.Sample
	for i := 0; i < 6; i++ {
		t0 := base.Add(time.Duration(i) * 20 * time.Second)
		samples = append(samples,
			detect.Sample{ServiceID: serviceID, Signal: detect.SignalLatencyMS, Value: 200, ObservedAt: t0, Source: "pipeline-test"},
			detect.Sample{ServiceID: serviceID, Signal: detect.SignalCallRate, Value: 0.5, ObservedAt: t0, Source: "pipeline-test"},
		)
	}
	for i := 0; i < 3; i++ {
		t0 := base.Add(2*time.Minute + time.Duration(i)*10*time.Second)
		samples = append(samples,
			detect.Sample{ServiceID: serviceID, Signal: detect.SignalLatencyMS, Value: 5000, ObservedAt: t0, Source: "pipeline-test"},
			detect.Sample{ServiceID: serviceID, Signal: detect.SignalCallRate, Value: 0.5, ObservedAt: t0, Source: "pipeline-test"},
		)
	}
	return samples
}

// ErrorRateBaselineThenSpike: low errors then elevated rate (+ call drop for outage).
func ErrorRateBaselineThenSpike(serviceID string, base time.Time) []detect.Sample {
	var samples []detect.Sample
	for i := 0; i < 6; i++ {
		t0 := base.Add(time.Duration(i) * 20 * time.Second)
		samples = append(samples,
			detect.Sample{ServiceID: serviceID, Signal: detect.SignalErrorRate, Value: 0.01, ObservedAt: t0, Source: "pipeline-test"},
			detect.Sample{ServiceID: serviceID, Signal: detect.SignalCallRate, Value: 1.0, ObservedAt: t0, Source: "pipeline-test"},
		)
	}
	for i := 0; i < 3; i++ {
		t0 := base.Add(2*time.Minute + time.Duration(i)*10*time.Second)
		samples = append(samples,
			detect.Sample{ServiceID: serviceID, Signal: detect.SignalErrorRate, Value: 0.55, ObservedAt: t0, Source: "pipeline-test"},
			detect.Sample{ServiceID: serviceID, Signal: detect.SignalCallRate, Value: 0.2, ObservedAt: t0, Source: "pipeline-test"},
		)
	}
	return samples
}

// HealthySteady: control telemetry that must not create incidents.
func HealthySteady(serviceID string, base time.Time) []detect.Sample {
	var samples []detect.Sample
	for i := 0; i < 10; i++ {
		t0 := base.Add(time.Duration(i) * 15 * time.Second)
		samples = append(samples,
			detect.Sample{ServiceID: serviceID, Signal: detect.SignalLatencyMS, Value: 40 + float64(i%3), ObservedAt: t0, Source: "pipeline-test"},
			detect.Sample{ServiceID: serviceID, Signal: detect.SignalErrorRate, Value: 0.0, ObservedAt: t0, Source: "pipeline-test"},
			detect.Sample{ServiceID: serviceID, Signal: detect.SignalCallRate, Value: 0.5, ObservedAt: t0, Source: "pipeline-test"},
		)
	}
	return samples
}

// CallRateDropOnly: throughput collapse without necessarily elevating error_rate first.
func CallRateDropOnly(serviceID string, base time.Time) []detect.Sample {
	var samples []detect.Sample
	for i := 0; i < 6; i++ {
		t0 := base.Add(time.Duration(i) * 20 * time.Second)
		samples = append(samples,
			detect.Sample{ServiceID: serviceID, Signal: detect.SignalCallRate, Value: 1.0, ObservedAt: t0, Source: "pipeline-test"},
			detect.Sample{ServiceID: serviceID, Signal: detect.SignalErrorRate, Value: 0.02, ObservedAt: t0, Source: "pipeline-test"},
		)
	}
	for i := 0; i < 3; i++ {
		t0 := base.Add(2*time.Minute + time.Duration(i)*10*time.Second)
		samples = append(samples,
			detect.Sample{ServiceID: serviceID, Signal: detect.SignalCallRate, Value: 0.3, ObservedAt: t0, Source: "pipeline-test"},
			detect.Sample{ServiceID: serviceID, Signal: detect.SignalErrorRate, Value: 0.02, ObservedAt: t0, Source: "pipeline-test"},
		)
	}
	return samples
}
