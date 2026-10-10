// Package correlate ranks change events against Pulse incidents using
// explainable temporal and service-overlap factors.
//
// Correlation is NOT causation. Scores never use scenario ground-truth labels
// and must not influence detector behavior.
package correlate

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/change"
	"github.com/yashg4509/perch/internal/pulse/incident"
)

const (
	// SchemaVersion is the persisted correlation result schema.
	SchemaVersion = "pulse.correlation.v1"

	// ScorerVersion identifies this explainable scorer.
	ScorerVersion = "temporal-service-overlap-v1"
)

// Config controls lookback and score weights. Weights should sum to ~100.
type Config struct {
	// MaxLookback is how far before the incident a change may still score.
	MaxLookback time.Duration
	// WeightTemporal max points for proximity (linear decay over MaxLookback).
	WeightTemporal float64
	// WeightPrimary max points when change services include incident primary.
	WeightPrimary float64
	// WeightTopology max points when change overlaps affected topology only.
	WeightTopology float64
	// WeightEnvironment points when environments match (both non-empty).
	WeightEnvironment float64
	// StrongScoreThreshold: scores below this are weak / "no strong candidate".
	StrongScoreThreshold float64
	// ExcludePostIncident drops changes at/after incident FirstDetectedAt.
	ExcludePostIncident bool
}

// DefaultConfig returns demo-oriented explainable defaults.
func DefaultConfig() Config {
	return Config{
		MaxLookback:          30 * time.Minute,
		WeightTemporal:       40,
		WeightPrimary:        35,
		WeightTopology:       15,
		WeightEnvironment:    10,
		StrongScoreThreshold: 50,
		ExcludePostIncident:  true,
	}
}

// EvidenceRef is a non-secret pointer to supporting artifacts.
type EvidenceRef struct {
	Ref     string `json:"ref"`
	Summary string `json:"summary"`
}

// ServiceOverlap classifies how change services relate to the incident.
type ServiceOverlap string

const (
	OverlapPrimary  ServiceOverlap = "primary"
	OverlapTopology ServiceOverlap = "topology"
	OverlapNone     ServiceOverlap = "none"
)

// Result is one ranked incident↔change correlation candidate.
type Result struct {
	SchemaVersion        string         `json:"schema_version"`
	CorrelationID        string         `json:"correlation_id"`
	ScorerVersion        string         `json:"scorer_version"`
	IncidentID           string         `json:"incident_id"`
	ChangeID             string         `json:"change_id"`
	Rank                 int            `json:"rank"`
	Score                float64        `json:"score"`
	TemporalDistanceSecs float64        `json:"temporal_distance_secs"`
	ServiceOverlap       ServiceOverlap `json:"service_overlap"`
	EnvironmentMatch     bool           `json:"environment_match"`
	Evidence             []EvidenceRef  `json:"evidence,omitempty"`
	Observations         []string       `json:"observations"`
	Inferences           []string       `json:"inferences"`
	Limitations          []string       `json:"limitations"`
	ScoreBreakdown       ScoreBreakdown `json:"score_breakdown"`
}

// ScoreBreakdown documents how the score was formed (explainable, not opaque).
type ScoreBreakdown struct {
	TemporalPoints    float64 `json:"temporal_points"`
	PrimaryPoints     float64 `json:"primary_points"`
	TopologyPoints    float64 `json:"topology_points"`
	EnvironmentPoints float64 `json:"environment_points"`
	Excluded          bool    `json:"excluded"`
	ExcludeReason     string  `json:"exclude_reason,omitempty"`
}

// Report is the full correlation output for one incident.
type Report struct {
	SchemaVersion     string    `json:"schema_version"`
	ScorerVersion     string    `json:"scorer_version"`
	IncidentID        string    `json:"incident_id"`
	GeneratedAt       time.Time `json:"generated_at"`
	Candidates        []Result  `json:"candidates"`
	Top1              *Result   `json:"top1,omitempty"`
	Top3              []Result  `json:"top3,omitempty"`
	StrongCandidate   bool      `json:"strong_candidate"`
	NoStrongCandidate bool      `json:"no_strong_candidate"`
	Observations      []string  `json:"observations"`
	Inferences        []string  `json:"inferences"`
	Limitations       []string  `json:"limitations"`
}

// Correlate ranks change events against a single incident.
// Returns an empty candidate list (with no_strong_candidate) when nothing plausible.
func Correlate(inc incident.Incident, changes []change.Event, cfg Config) (Report, error) {
	if err := inc.Validate(); err != nil {
		return Report{}, err
	}
	cfg = normalize(cfg)
	now := time.Now().UTC()

	var scored []Result
	for _, ch := range changes {
		if err := ch.Validate(); err != nil {
			return Report{}, fmt.Errorf("correlate: invalid change: %w", err)
		}
		r, ok := scoreOne(inc, ch, cfg)
		if !ok {
			continue
		}
		scored = append(scored, r)
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		if scored[i].TemporalDistanceSecs != scored[j].TemporalDistanceSecs {
			return scored[i].TemporalDistanceSecs < scored[j].TemporalDistanceSecs
		}
		return scored[i].ChangeID < scored[j].ChangeID
	})
	for i := range scored {
		scored[i].Rank = i + 1
		scored[i].CorrelationID = fmt.Sprintf("corr|%s|%s|%d", inc.IncidentID, scored[i].ChangeID, scored[i].Rank)
	}

	rep := Report{
		SchemaVersion: SchemaVersion,
		ScorerVersion: ScorerVersion,
		IncidentID:    inc.IncidentID,
		GeneratedAt:   now,
		Candidates:    scored,
		Limitations: []string{
			"correlation is not proven causation",
			"scores use temporal proximity and service overlap only",
			"scenario ground-truth labels are never used in scoring",
		},
	}

	if len(scored) > 0 {
		top := scored[0]
		rep.Top1 = &top
		n := 3
		if len(scored) < n {
			n = len(scored)
		}
		rep.Top3 = append([]Result(nil), scored[:n]...)
		rep.StrongCandidate = top.Score >= cfg.StrongScoreThreshold &&
			(top.ServiceOverlap == OverlapPrimary || top.ServiceOverlap == OverlapTopology)
	}
	rep.NoStrongCandidate = !rep.StrongCandidate

	rep.Observations = []string{
		fmt.Sprintf("incident %s primary service %s first detected at %s",
			inc.IncidentID, inc.PrimaryServiceID, inc.FirstDetectedAt.UTC().Format(time.RFC3339)),
		fmt.Sprintf("evaluated %d change events; %d candidates after filters", len(changes), len(scored)),
	}
	if rep.Top1 != nil {
		rep.Observations = append(rep.Observations,
			fmt.Sprintf("top candidate change %s score=%.1f temporal_distance=%.0fs overlap=%s",
				rep.Top1.ChangeID, rep.Top1.Score, rep.Top1.TemporalDistanceSecs, rep.Top1.ServiceOverlap))
	}
	if rep.StrongCandidate {
		rep.Inferences = []string{
			"top-ranked change is a strong temporal/service correlation candidate (not proven causation)",
		}
	} else {
		rep.Inferences = []string{
			"no strong change candidate found for this incident",
		}
		rep.Observations = append(rep.Observations, "no strong candidate meeting score and service-overlap thresholds")
	}

	return rep, nil
}

func normalize(cfg Config) Config {
	def := DefaultConfig()
	if cfg == (Config{}) {
		return def
	}
	if cfg.MaxLookback <= 0 {
		cfg.MaxLookback = def.MaxLookback
	}
	if cfg.WeightTemporal == 0 {
		cfg.WeightTemporal = def.WeightTemporal
	}
	if cfg.WeightPrimary == 0 {
		cfg.WeightPrimary = def.WeightPrimary
	}
	if cfg.WeightTopology == 0 {
		cfg.WeightTopology = def.WeightTopology
	}
	if cfg.WeightEnvironment == 0 {
		cfg.WeightEnvironment = def.WeightEnvironment
	}
	if cfg.StrongScoreThreshold == 0 {
		cfg.StrongScoreThreshold = def.StrongScoreThreshold
	}
	// ExcludePostIncident is a bool; zero value is false. Callers that want
	// the safe default should pass DefaultConfig() (ExcludePostIncident=true).
	return cfg
}

func scoreOne(inc incident.Incident, ch change.Event, cfg Config) (Result, bool) {
	incidentAt := inc.FirstDetectedAt.UTC()
	changeAt := ch.EffectiveTime()
	dist := incidentAt.Sub(changeAt)

	bd := ScoreBreakdown{}
	r := Result{
		SchemaVersion: SchemaVersion,
		ScorerVersion: ScorerVersion,
		IncidentID:    inc.IncidentID,
		ChangeID:      ch.ChangeID,
	}

	if cfg.ExcludePostIncident && !changeAt.Before(incidentAt) {
		bd.Excluded = true
		bd.ExcludeReason = "change at or after incident onset"
		return Result{}, false
	}
	if dist < 0 {
		// change after incident
		bd.Excluded = true
		bd.ExcludeReason = "negative temporal distance (change after incident)"
		return Result{}, false
	}
	if dist > cfg.MaxLookback {
		bd.Excluded = true
		bd.ExcludeReason = "outside max lookback window"
		return Result{}, false
	}

	envMatch := false
	if inc.Environment != "" && ch.Environment != "" {
		if !strings.EqualFold(inc.Environment, ch.Environment) {
			bd.Excluded = true
			bd.ExcludeReason = "environment mismatch"
			return Result{}, false
		}
		envMatch = true
		bd.EnvironmentPoints = cfg.WeightEnvironment
	} else if inc.Environment != "" && ch.Environment != "" {
		envMatch = true
	}

	// Temporal: linear decay from WeightTemporal at dist=0 to 0 at MaxLookback.
	frac := 1.0 - float64(dist)/float64(cfg.MaxLookback)
	if frac < 0 {
		frac = 0
	}
	bd.TemporalPoints = cfg.WeightTemporal * frac

	overlap := OverlapNone
	primaryHit := contains(ch.ServiceIDs, inc.PrimaryServiceID)
	topoHit := false
	if !primaryHit {
		for _, id := range ch.ServiceIDs {
			if contains(inc.AffectedServiceIDs, id) {
				topoHit = true
				break
			}
		}
	}
	switch {
	case primaryHit:
		overlap = OverlapPrimary
		bd.PrimaryPoints = cfg.WeightPrimary
	case topoHit:
		overlap = OverlapTopology
		bd.TopologyPoints = cfg.WeightTopology
	}

	score := bd.TemporalPoints + bd.PrimaryPoints + bd.TopologyPoints + bd.EnvironmentPoints

	obs := []string{
		fmt.Sprintf("change %s (%s) effective at %s", ch.ChangeID, ch.ChangeType, changeAt.Format(time.RFC3339)),
		fmt.Sprintf("incident detected at %s (%.0fs later)", incidentAt.Format(time.RFC3339), dist.Seconds()),
		fmt.Sprintf("change service_ids=%v mapping=%s", ch.ServiceIDs, ch.ServiceMapping),
		fmt.Sprintf("incident primary service=%s", inc.PrimaryServiceID),
	}
	if envMatch {
		obs = append(obs, fmt.Sprintf("environment match: %s", inc.Environment))
	}

	inf := []string{}
	lim := []string{
		"NOT PROVEN: change did not necessarily cause the incident",
		"score reflects temporal proximity and service overlap only",
	}
	if overlap == OverlapNone {
		inf = append(inf, "change has no service overlap with incident; weak candidate")
		lim = append(lim, "unrelated service change — do not treat as attribution")
	} else if score >= cfg.StrongScoreThreshold {
		inf = append(inf, "change is a strong candidate related change (correlation, not causation)")
	} else {
		inf = append(inf, "change is a weak temporal/service candidate")
	}
	if ch.ServiceMapping == change.MappingUnknown || ch.ServiceMapping == change.MappingPartial {
		lim = append(lim, "service mapping uncertainty: "+string(ch.ServiceMapping))
	}
	if ch.Metadata["simulated"] == "true" {
		obs = append(obs, "change is a simulated deployment marker (not a real cloud deploy)")
		lim = append(lim, "simulated marker — represents a modeled change event for demo/eval")
	}

	r.Score = score
	r.TemporalDistanceSecs = dist.Seconds()
	r.ServiceOverlap = overlap
	r.EnvironmentMatch = envMatch
	r.Observations = obs
	r.Inferences = inf
	r.Limitations = lim
	r.ScoreBreakdown = bd
	r.Evidence = []EvidenceRef{
		{Ref: "change:" + ch.ChangeID, Summary: nonEmpty(ch.Title, ch.Summary, string(ch.ChangeType))},
		{Ref: "incident:" + inc.IncidentID, Summary: inc.Summary},
	}
	return r, true
}

func contains(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func nonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
