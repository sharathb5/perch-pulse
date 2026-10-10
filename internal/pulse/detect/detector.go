package detect

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// Detector maintains per-series history and emits regression findings from
// telemetry alone. It never accepts scenario labels.
type Detector struct {
	mu      sync.Mutex
	cfg     Config
	series  map[SeriesKey][]Sample
	open    map[string]Finding // finding_id -> open finding
	byKey   map[SeriesKey]string
	nowFunc func() time.Time
}

// New returns a detector with the given config.
func New(cfg Config) *Detector {
	def := DefaultConfig()
	if cfg.BaselineWindow <= 0 {
		cfg.BaselineWindow = def.BaselineWindow
	}
	if cfg.CurrentWindow <= 0 {
		cfg.CurrentWindow = def.CurrentWindow
	}
	if cfg.MinBaselineSamples <= 0 {
		cfg.MinBaselineSamples = def.MinBaselineSamples
	}
	if cfg.MinCurrentSamples <= 0 {
		cfg.MinCurrentSamples = def.MinCurrentSamples
	}
	if cfg.DetectorVersion == "" {
		cfg.DetectorVersion = def.DetectorVersion
	}
	if cfg.LatencyRelThreshold == 0 {
		cfg.LatencyRelThreshold = def.LatencyRelThreshold
	}
	if cfg.LatencyAbsMinMS == 0 {
		cfg.LatencyAbsMinMS = def.LatencyAbsMinMS
	}
	if cfg.ErrorRateAbsDelta == 0 {
		cfg.ErrorRateAbsDelta = def.ErrorRateAbsDelta
	}
	if cfg.ErrorRateMin == 0 {
		cfg.ErrorRateMin = def.ErrorRateMin
	}
	if cfg.OutageErrorRate == 0 {
		cfg.OutageErrorRate = def.OutageErrorRate
	}
	if cfg.OutageCallRateDrop == 0 {
		cfg.OutageCallRateDrop = def.OutageCallRateDrop
	}
	return &Detector{
		cfg:     cfg,
		series:  map[SeriesKey][]Sample{},
		open:    map[string]Finding{},
		byKey:   map[SeriesKey]string{},
		nowFunc: func() time.Time { return time.Now().UTC() },
	}
}

// SetClock injects time for tests.
func (d *Detector) SetClock(now func() time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if now == nil {
		d.nowFunc = func() time.Time { return time.Now().UTC() }
		return
	}
	d.nowFunc = now
}

// Ingest appends validated samples. Out-of-order samples are sorted into place.
// Exact duplicates (same service, signal, time, value, source) are ignored.
// Conflicting values at the same timestamp replace the prior sample.
func (d *Detector) Ingest(samples []Sample) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, s := range samples {
		s.ObservedAt = s.ObservedAt.UTC()
		if err := s.Validate(); err != nil {
			return err
		}
		key := SeriesKey{ServiceID: s.ServiceID, Signal: s.Signal}
		cur := d.series[key]
		exactDup := false
		replaced := false
		for i, prev := range cur {
			if !prev.ObservedAt.Equal(s.ObservedAt) {
				continue
			}
			if prev.Value == s.Value && prev.Source == s.Source {
				exactDup = true
				break
			}
			// Same timestamp, different value: keep the latest ingest (event integrity).
			cur[i] = s
			replaced = true
			break
		}
		if exactDup {
			continue
		}
		if !replaced {
			cur = append(cur, s)
		}
		SortSamples(cur)
		// Bound memory: keep ~2x baseline+current worth by time.
		cutoff := s.ObservedAt.Add(-(d.cfg.BaselineWindow + d.cfg.CurrentWindow + time.Minute))
		trimmed := cur[:0]
		for _, x := range cur {
			if !x.ObservedAt.Before(cutoff) {
				trimmed = append(trimmed, x)
			}
		}
		d.series[key] = trimmed
	}
	return nil
}

// SeriesCount returns how many distinct series are stored.
func (d *Detector) SeriesCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.series)
}

// Evaluate compares current vs baseline windows and updates open findings.
// Returns all currently open findings plus any that recovered on this tick.
func (d *Detector) Evaluate(now time.Time) ([]Finding, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now = now.UTC()
	if now.IsZero() {
		now = d.nowFunc().UTC()
	}

	type hit struct {
		key      SeriesKey
		obs      Stats
		base     Stats
		score    float64
		thresh   float64
		severity Severity
		summary  string
	}
	var hits []hit

	curStart := now.Add(-d.cfg.CurrentWindow)
	baseEnd := curStart
	baseStart := baseEnd.Add(-d.cfg.BaselineWindow)

	// Keys with enough history to decide fire vs not-fire. Insufficient history
	// must not look like recovery (stale ≠ healthy).
	decidable := map[SeriesKey]struct{}{}

	for key, samples := range d.series {
		obs, err := WindowStats(samples, curStart, now, d.cfg.MinCurrentSamples)
		if err != nil {
			continue
		}
		base, err := WindowStats(samples, baseStart, baseEnd, d.cfg.MinBaselineSamples)
		if err != nil {
			continue
		}
		decidable[key] = struct{}{}
		var callObs *Stats
		if key.Signal == SignalLatencyMS {
			if cr, ok := d.series[SeriesKey{ServiceID: key.ServiceID, Signal: SignalCallRate}]; ok {
				if st, err := WindowStats(cr, curStart, now, d.cfg.MinCurrentSamples); err == nil {
					callObs = &st
				}
			}
		}
		fire, score, thresh, sev, summary := d.cfg.decide(key.Signal, obs, base, callObs)
		if !fire {
			continue
		}
		hits = append(hits, hit{key: key, obs: obs, base: base, score: score, thresh: thresh, severity: sev, summary: summary})
	}

	// Rank hits by score descending for attribution.
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].key.String() < hits[j].key.String()
	})

	activeKeys := map[SeriesKey]struct{}{}
	out := make([]Finding, 0, len(hits)+len(d.open))

	for _, h := range hits {
		activeKeys[h.key] = struct{}{}
		id, ok := d.byKey[h.key]
		var f Finding
		if ok {
			f = d.open[id]
			f.LastObservedAt = now
			f.ObservedValue = h.obs.Median
			f.BaselineValue = h.base.Median
			f.Score = h.score
			f.Threshold = h.thresh
			f.Severity = h.severity
			f.ObservedWindow = Window{Start: h.obs.Start, End: h.obs.End}
			f.BaselineWindow = Window{Start: h.base.Start, End: h.base.End}
			f.Summary = h.summary
			f.RecoveredAt = nil
		} else {
			f = Finding{
				SchemaVersion:   SchemaVersion,
				FindingID:       FindingIDFor(h.key.ServiceID, h.key.Signal, now),
				DetectorVersion: d.cfg.DetectorVersion,
				ServiceID:       h.key.ServiceID,
				SignalType:      h.key.Signal,
				ObservedWindow:  Window{Start: h.obs.Start, End: h.obs.End},
				BaselineWindow:  Window{Start: h.base.Start, End: h.base.End},
				ObservedValue:   h.obs.Median,
				BaselineValue:   h.base.Median,
				Threshold:       h.thresh,
				Score:           h.score,
				Severity:        h.severity,
				FirstDetectedAt: now,
				LastObservedAt:  now,
				Evidence: []EvidenceRef{{
					Ref:     "telemetry:" + string(h.key.Signal),
					Summary: fmt.Sprintf("current_median=%.4f baseline_median=%.4f", h.obs.Median, h.base.Median),
				}},
				Summary: h.summary,
			}
			d.byKey[h.key] = f.FindingID
		}
		d.open[f.FindingID] = f
	}

	// Recover findings only when we could re-evaluate the series and it no longer fires.
	for key, id := range d.byKey {
		if _, still := activeKeys[key]; still {
			continue
		}
		if _, ok := decidable[key]; !ok {
			// Insufficient / missing current telemetry — leave finding open.
			continue
		}
		f := d.open[id]
		if f.RecoveredAt == nil {
			rec := now
			f.RecoveredAt = &rec
			f.LastObservedAt = now
			d.open[id] = f
		}
		delete(d.byKey, key)
	}

	for _, f := range d.open {
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].FindingID < out[j].FindingID
	})
	return out, nil
}

// OpenFindings returns currently open (unrecovered) findings.
func (d *Detector) OpenFindings() []Finding {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Finding, 0, len(d.byKey))
	for _, id := range d.byKey {
		out = append(out, d.open[id])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].FindingID < out[j].FindingID })
	return out
}

func (c Config) decide(sig SignalType, obs, base Stats, callObs *Stats) (fire bool, score, thresh float64, sev Severity, summary string) {
	switch sig {
	case SignalLatencyMS:
		maxMS := c.LatencyMaxMS
		if maxMS <= 0 {
			maxMS = 30000
		}
		if obs.Median > maxMS || base.Median > maxMS {
			return false, 0, c.LatencyRelThreshold, "", ""
		}
		// Near-zero baselines make relative increase meaningless (and explode scores).
		if base.Median < 1.0 {
			return false, 0, c.LatencyRelThreshold, "", ""
		}
		if c.MinCallRateForLatency > 0 {
			if callObs == nil || callObs.Median < c.MinCallRateForLatency {
				return false, 0, c.LatencyRelThreshold, "", ""
			}
		}
		rel := RelativeIncrease(obs.Median, base.Median)
		abs := obs.Median - base.Median
		z := RobustZ(obs.Median, base.Median, base.MAD)
		thresh = c.LatencyRelThreshold
		score = math.Max(rel, 0)
		if abs >= c.LatencyAbsMinMS && rel >= c.LatencyRelThreshold {
			if c.LatencyRobustZ > 0 && base.MAD > 1e-9 && z < c.LatencyRobustZ {
				return false, score, thresh, "", ""
			}
			sev = SeverityWarning
			if rel >= 3 || abs >= 500 {
				sev = SeverityCritical
			}
			summary = fmt.Sprintf("observed latency degradation: current_median_ms=%.2f baseline_median_ms=%.2f rel_increase=%.2f (correlated change, not proven causation)",
				obs.Median, base.Median, rel)
			return true, score, thresh, sev, summary
		}
		return false, score, thresh, "", ""

	case SignalErrorRate:
		delta := obs.Median - base.Median
		thresh = c.ErrorRateAbsDelta
		score = math.Max(delta, 0)
		if delta >= c.ErrorRateAbsDelta && obs.Median >= c.ErrorRateMin {
			sev = SeverityWarning
			if obs.Median >= c.OutageErrorRate {
				sev = SeverityCritical
			}
			summary = fmt.Sprintf("observed elevated error rate: current_median=%.3f baseline_median=%.3f delta=%.3f (likely affected service; correlation only, not causation)",
				obs.Median, base.Median, delta)
			return true, score, thresh, sev, summary
		}
		return false, score, thresh, "", ""

	case SignalCallRate:
		// Availability/outage proxy: sharp drop in call throughput.
		if base.Median <= 1e-9 {
			return false, 0, c.OutageCallRateDrop, "", ""
		}
		drop := 1 - (obs.Median / base.Median)
		thresh = c.OutageCallRateDrop
		score = math.Max(drop, 0)
		if drop >= c.OutageCallRateDrop {
			summary = fmt.Sprintf("observed call-rate drop suggesting reduced availability: current_median=%.4f baseline_median=%.4f drop=%.2f (correlated change)",
				obs.Median, base.Median, drop)
			return true, score, thresh, SeverityCritical, summary
		}
		return false, score, thresh, "", ""
	default:
		return false, 0, 0, "", ""
	}
}
