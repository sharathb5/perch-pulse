package incident

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/detect"
)

// BuildInput assembles an incident from findings and optional telemetry samples.
type BuildInput struct {
	Findings    []detect.Finding
	Samples     []detect.Sample
	Environment string
	// BeforePad / AfterPad define snapshot windows around the incident.
	BeforePad time.Duration
	AfterPad  time.Duration
}

// Build creates an incident evidence snapshot from detector findings.
// Ground-truth scenario labels must not be passed here.
func Build(in BuildInput) (Incident, error) {
	if len(in.Findings) == 0 {
		return Incident{}, fmt.Errorf("incident: findings required")
	}
	findings := append([]detect.Finding(nil), in.Findings...)
	sort.SliceStable(findings, func(i, j int) bool {
		if !findings[i].FirstDetectedAt.Equal(findings[j].FirstDetectedAt) {
			return findings[i].FirstDetectedAt.Before(findings[j].FirstDetectedAt)
		}
		return findings[i].FindingID < findings[j].FindingID
	})

	primary := findings[0]
	for _, f := range findings {
		if err := f.Validate(); err != nil {
			return Incident{}, err
		}
		if f.FirstDetectedAt.Before(primary.FirstDetectedAt) ||
			(f.FirstDetectedAt.Equal(primary.FirstDetectedAt) && f.SeverityRank() > primary.SeverityRank()) {
			primary = f
		}
	}

	beforePad := in.BeforePad
	if beforePad <= 0 {
		beforePad = 2 * time.Minute
	}
	afterPad := in.AfterPad
	if afterPad <= 0 {
		afterPad = 2 * time.Minute
	}

	first := primary.FirstDetectedAt.UTC()
	last := primary.LastObservedAt.UTC()
	var recovered *time.Time
	allPrimaryRecovered := true
	var latestRecovery *time.Time
	for _, f := range findings {
		if f.ServiceID != primary.ServiceID {
			continue
		}
		if f.LastObservedAt.After(last) {
			last = f.LastObservedAt.UTC()
		}
		if f.RecoveredAt == nil {
			allPrimaryRecovered = false
			continue
		}
		t := f.RecoveredAt.UTC()
		if latestRecovery == nil || t.After(*latestRecovery) {
			latestRecovery = &t
		}
	}
	// Only close the incident when every primary-service finding has recovered.
	if allPrimaryRecovered && latestRecovery != nil {
		recovered = latestRecovery
	}

	duringEnd := last
	if recovered != nil {
		duringEnd = recovered.UTC()
	}

	beforeWin := Window{Start: first.Add(-beforePad), End: first}
	duringWin := Window{Start: first, End: duringEnd}
	afterStart := duringEnd
	afterWin := Window{Start: afterStart, End: afterStart.Add(afterPad)}

	affected := map[string]struct{}{primary.ServiceID: {}}
	for _, f := range findings {
		affected[f.ServiceID] = struct{}{}
		for _, c := range f.CandidateImpact {
			if c.ServiceID != "" {
				affected[c.ServiceID] = struct{}{}
			}
		}
	}
	affectedIDs := sortedKeys(affected)

	var findingIDs []string
	sigSet := map[string]struct{}{}
	for _, f := range findings {
		findingIDs = append(findingIDs, f.FindingID)
		sigSet[string(f.SignalType)] = struct{}{}
	}
	signals := sortedKeys(sigSet)

	before := snapshot("before", beforeWin, primary.ServiceID, in.Samples)
	during := snapshot("during", duringWin, primary.ServiceID, in.Samples)
	after := snapshot("after", afterWin, primary.ServiceID, in.Samples)

	obs := []string{
		fmt.Sprintf("primary service %s first detected degraded at %s", primary.ServiceID, first.Format(time.RFC3339)),
		fmt.Sprintf("detector signals: %s", strings.Join(signals, ", ")),
	}
	if before.Available {
		obs = append(obs, fmt.Sprintf("before-window telemetry available (%s–%s)", beforeWin.Start.Format(time.RFC3339), beforeWin.End.Format(time.RFC3339)))
	} else {
		obs = append(obs, "before-window telemetry unavailable or empty")
	}
	if during.Available {
		obs = append(obs, fmt.Sprintf("during-window telemetry available (%s–%s)", duringWin.Start.Format(time.RFC3339), duringWin.End.Format(time.RFC3339)))
	}
	if recovered != nil {
		obs = append(obs, fmt.Sprintf("recovery observed at %s", recovered.Format(time.RFC3339)))
	} else {
		obs = append(obs, "incident still open (no recovery timestamp)")
	}

	inf := []string{
		"degradation is a detector finding from telemetry baselines (not proven root cause)",
	}
	lim := []string{
		"before/during/after digests are summaries, not full telemetry dumps",
		"incident assembly does not use scenario ground-truth labels",
		"correlation with changes is a separate step and does not prove causation",
	}
	if !before.Available {
		lim = append(lim, "missing before-window samples: cannot assert pre-incident baseline from this snapshot alone")
	}
	if !after.Available && recovered != nil {
		lim = append(lim, "missing after-window samples: recovery evidence incomplete")
	}

	env := strings.TrimSpace(in.Environment)
	if env == "" {
		env = environmentFromServiceID(primary.ServiceID)
	}

	inc := Incident{
		SchemaVersion:      SchemaVersion,
		IncidentID:         IncidentIDFor(primary.ServiceID, first),
		PrimaryServiceID:   primary.ServiceID,
		Environment:        env,
		FindingIDs:         findingIDs,
		SignalTypes:        signals,
		FirstDetectedAt:    first,
		LastObservedAt:     last,
		RecoveredAt:        recovered,
		AffectedServiceIDs: affectedIDs,
		Before:             before,
		During:             during,
		After:              after,
		Observations:       obs,
		Inferences:         inf,
		Limitations:        lim,
		Evidence: []EvidenceRef{{
			Ref:     "finding:" + primary.FindingID,
			Summary: primary.Summary,
		}},
		Summary: fmt.Sprintf("%s degradation detected via %s (evidence snapshot; not proven causation)",
			primary.ServiceID, strings.Join(signals, "/")),
	}
	if err := inc.Validate(); err != nil {
		return Incident{}, err
	}
	return inc, nil
}

func snapshot(phase string, win Window, serviceID string, samples []detect.Sample) PhaseSnapshot {
	bySignal := map[detect.SignalType][]detect.Sample{}
	for _, s := range samples {
		if s.ServiceID != serviceID {
			continue
		}
		t := s.ObservedAt.UTC()
		if t.Before(win.Start) || t.After(win.End) {
			continue
		}
		bySignal[s.Signal] = append(bySignal[s.Signal], s)
	}
	if len(bySignal) == 0 {
		return PhaseSnapshot{
			Phase:      phase,
			Window:     win,
			Available:  false,
			Limitation: "no samples in window for primary service",
		}
	}
	var digests []SampleDigest
	for sig, ss := range bySignal {
		digests = append(digests, digestOf(sig, ss))
	}
	sort.SliceStable(digests, func(i, j int) bool {
		return digests[i].Signal < digests[j].Signal
	})
	return PhaseSnapshot{
		Phase:     phase,
		Window:    win,
		Digests:   digests,
		Available: true,
	}
}

func digestOf(sig detect.SignalType, ss []detect.Sample) SampleDigest {
	detect.SortSamples(ss)
	vals := make([]float64, len(ss))
	minV, maxV := math.Inf(1), math.Inf(-1)
	for i, s := range ss {
		vals[i] = s.Value
		if s.Value < minV {
			minV = s.Value
		}
		if s.Value > maxV {
			maxV = s.Value
		}
	}
	return SampleDigest{
		Signal:  sig,
		Count:   len(ss),
		Median:  median(vals),
		Min:     minV,
		Max:     maxV,
		FirstAt: ss[0].ObservedAt.UTC(),
		LastAt:  ss[len(ss)-1].ObservedAt.UTC(),
	}
}

func median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	cp := append([]float64(nil), vals...)
	sort.Float64s(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return cp[mid]
	}
	return (cp[mid-1] + cp[mid]) / 2
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func environmentFromServiceID(id string) string {
	// astronomy/<env>/<name>
	parts := strings.Split(id, "/")
	if len(parts) >= 2 && parts[0] == "astronomy" {
		return parts[1]
	}
	return ""
}
