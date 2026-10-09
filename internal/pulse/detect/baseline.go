package detect

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Stats summarizes a sample window for explainable comparison.
type Stats struct {
	Count  int
	Median float64
	MAD    float64 // median absolute deviation
	Mean   float64
	Min    float64
	Max    float64
	Start  time.Time
	End    time.Time
}

// WindowStats computes median/MAD/mean for values in [start, end] inclusive.
// Samples outside the window are ignored. Requires at least minCount samples.
func WindowStats(samples []Sample, start, end time.Time, minCount int) (Stats, error) {
	if minCount < 1 {
		minCount = 1
	}
	vals := make([]float64, 0, len(samples))
	var first, last time.Time
	for _, s := range samples {
		if s.ObservedAt.Before(start) || s.ObservedAt.After(end) {
			continue
		}
		vals = append(vals, s.Value)
		if first.IsZero() || s.ObservedAt.Before(first) {
			first = s.ObservedAt
		}
		if last.IsZero() || s.ObservedAt.After(last) {
			last = s.ObservedAt
		}
	}
	if len(vals) < minCount {
		return Stats{}, fmt.Errorf("detect: insufficient history: have %d need %d", len(vals), minCount)
	}
	sort.Float64s(vals)
	med := percentileSorted(vals, 0.5)
	devs := make([]float64, len(vals))
	sum := 0.0
	for i, v := range vals {
		devs[i] = math.Abs(v - med)
		sum += v
	}
	sort.Float64s(devs)
	mad := percentileSorted(devs, 0.5)
	return Stats{
		Count:  len(vals),
		Median: med,
		MAD:    mad,
		Mean:   sum / float64(len(vals)),
		Min:    vals[0],
		Max:    vals[len(vals)-1],
		Start:  first.UTC(),
		End:    last.UTC(),
	}, nil
}

func percentileSorted(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	idx := p * float64(len(sorted)-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return sorted[lo]
	}
	w := idx - float64(lo)
	return sorted[lo]*(1-w) + sorted[hi]*w
}

// RobustZ is (value - median) / (1.4826 * MAD), or 0 when MAD is ~0.
// When MAD is 0, falls back to relative change vs median if median > 0.
func RobustZ(value, median, mad float64) float64 {
	const eps = 1e-9
	scale := 1.4826 * mad
	if scale > eps {
		return (value - median) / scale
	}
	if math.Abs(median) > eps {
		return (value - median) / math.Abs(median)
	}
	if math.Abs(value) > eps {
		return math.Copysign(1, value)
	}
	return 0
}

// RelativeIncrease is (observed - baseline) / max(|baseline|, eps).
func RelativeIncrease(observed, baseline float64) float64 {
	const eps = 1e-9
	den := math.Abs(baseline)
	if den < eps {
		den = eps
	}
	return (observed - baseline) / den
}
