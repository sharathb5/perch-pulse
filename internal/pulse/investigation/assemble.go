package investigation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/astronomy"
	"github.com/yashg4509/perch/internal/pulse/change"
	"github.com/yashg4509/perch/internal/pulse/correlate"
	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/incident"
)

// AssembleInput gathers read-only inputs for one investigation.
type AssembleInput struct {
	Incident incident.Incident
	Changes  []change.Event
	Mapping  astronomy.Mapping
	Mapper   GraphMapper
	Now      time.Time
	// LinkedRuns are optional explicitly linked Run Pulse IDs (never auto-started).
	LinkedRuns []LinkedRunPulse
	// Correlation optional precomputed report; when nil, Correlate is called.
	Correlation *correlate.Report
}

// Assemble builds a bounded investigation snapshot. Read-only; no external I/O.
func Assemble(in AssembleInput) (Investigation, error) {
	inc := in.Incident
	if err := inc.Validate(); err != nil {
		return Investigation{}, fmt.Errorf("investigation: invalid incident: %w", err)
	}
	now := in.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}

	var rep correlate.Report
	if in.Correlation != nil {
		rep = *in.Correlation
	} else {
		var err error
		rep, err = correlate.Correlate(inc, in.Changes, correlate.DefaultConfig())
		if err != nil {
			return Investigation{}, fmt.Errorf("investigation: correlate: %w", err)
		}
	}

	state := IncidentStateActive
	if inc.RecoveredAt != nil {
		state = IncidentStateRecovered
	}

	inv := Investigation{
		SchemaVersion:         SchemaVersion,
		IncidentID:            inc.IncidentID,
		Environment:           inc.Environment,
		AffectedServiceIDs:    append([]string{}, inc.AffectedServiceIDs...),
		IncidentState:         state,
		GeneratedAt:           now,
		EvidenceCollectedAt:   inc.LastObservedAt.UTC(),
		EvidenceFreshness:     FreshnessIncidentSnapshot,
		GenerationMethod:      GenerationMethodDeterministic,
		Before:                inc.Before,
		During:                inc.During,
		After:                 inc.After,
		Diagnosed:             false,
		StrongChangeCandidate: rep.StrongCandidate,
		LinkedRunPulse:        append([]LinkedRunPulse{}, in.LinkedRuns...),
	}
	sort.Strings(inv.AffectedServiceIDs)

	inv.ObservedSymptoms = buildSymptoms(inc)
	inv.BaselineComparisons = buildBaselines(inc)
	inv.Topology = BuildTopology(inc, in.Mapping, in.Mapper)
	inv.ChangeCandidates = buildChangeCandidates(rep)
	inv.MissingEvidence = buildMissing(inc, rep, in.LinkedRuns)
	inv.Unknowns = buildUnknowns(inc, rep, inv)
	inv.SourceReferences = buildSourceRefs(inc, rep)
	for _, s := range inv.ObservedSymptoms {
		inv.SourceReferences = append(inv.SourceReferences, EvidenceRef{
			Ref: s.ID, Summary: s.Statement, Classification: ClassObserved,
		})
	}
	for _, bc := range inv.BaselineComparisons {
		inv.SourceReferences = append(inv.SourceReferences, EvidenceRef{
			Ref:            fmt.Sprintf("baseline:%s:%s", bc.ServiceID, bc.SignalType),
			Summary:        "before/during digest comparison",
			Classification: bc.Classification,
		})
	}

	if len(in.LinkedRuns) > 0 {
		inv.EvidenceFreshness = FreshnessMixedCurrent
		inv.Limitations = append(inv.Limitations,
			"Linked Run Pulse results are point-in-time diagnostics and may not fall inside the incident window.",
			"Run Pulse observing request behavior is not proof of root cause.",
		)
	}

	inv.Limitations = append(inv.Limitations,
		"Investigation report generation does not confirm a root cause.",
		"Persisted snapshot reflects evidence at investigation time; it is not current live state.",
		"Correlation ranks temporal/service overlap and is not proven causation.",
		"Active probe health is separate from Pulse telemetry regressions.",
		"Absence of change events is not proof that no deployments occurred.",
	)
	inv.Limitations = append(inv.Limitations, inc.Limitations...)
	inv.Limitations = append(inv.Limitations, rep.Limitations...)
	inv.Limitations = uniquePreserveOrder(inv.Limitations)

	// Deterministic proposer seam.
	prop, err := (DeterministicProposer{}).Propose(EvidenceSnapshot{
		SchemaVersion: SchemaVersion,
		Investigation: inv,
	})
	if err != nil {
		return Investigation{}, err
	}
	inv.Hypotheses = prop.Hypotheses
	inv.Limitations = uniquePreserveOrder(append(inv.Limitations, prop.Limitations...))
	inv.SuggestedVerification = SuggestedSteps(&inv)

	fp, err := fingerprint(inv)
	if err != nil {
		return Investigation{}, err
	}
	inv.ContentFingerprint = fp
	inv.InvestigationID = investigationID(inc.IncidentID, fp, now)

	if err := inv.Validate(); err != nil {
		return Investigation{}, err
	}
	return RedactInvestigation(inv), nil
}

func buildSymptoms(inc incident.Incident) []Symptom {
	var out []Symptom
	for _, sig := range inc.SignalTypes {
		st := detect.SignalType(sig)
		if !st.Valid() {
			continue
		}
		during := digestFor(inc.During, st)
		before := digestFor(inc.Before, st)
		id := fmt.Sprintf("symptom:%s:%s", inc.PrimaryServiceID, st)
		stmt := fmt.Sprintf("%s showed anomalous %s during the recorded incident window.", shortService(inc.PrimaryServiceID), st)
		s := Symptom{
			ID:             id,
			ServiceID:      inc.PrimaryServiceID,
			SignalType:     st,
			Statement:      stmt,
			Classification: ClassObserved,
			EvidenceRefs:   []string{"phase:during", "incident:" + inc.IncidentID},
		}
		if during != nil {
			v := during.Median
			s.DuringMedian = &v
		}
		if before != nil {
			v := before.Median
			s.BeforeMedian = &v
		}
		out = append(out, s)
	}
	// Also surface digests from during that aren't in signal_types.
	for _, d := range inc.During.Digests {
		found := false
		for _, s := range out {
			if s.SignalType == d.Signal {
				found = true
				break
			}
		}
		if found {
			continue
		}
		v := d.Median
		out = append(out, Symptom{
			ID:             fmt.Sprintf("symptom:%s:%s:digest", inc.PrimaryServiceID, d.Signal),
			ServiceID:      inc.PrimaryServiceID,
			SignalType:     d.Signal,
			Statement:      fmt.Sprintf("During-window digest available for %s (median %.4g, n=%d).", d.Signal, d.Median, d.Count),
			Classification: ClassObserved,
			EvidenceRefs:   []string{"phase:during"},
			DuringMedian:   &v,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func buildBaselines(inc incident.Incident) []BaselineComparison {
	var out []BaselineComparison
	signals := map[detect.SignalType]struct{}{}
	for _, d := range inc.Before.Digests {
		signals[d.Signal] = struct{}{}
	}
	for _, d := range inc.During.Digests {
		signals[d.Signal] = struct{}{}
	}
	var keys []detect.SignalType
	for s := range signals {
		keys = append(keys, s)
	}
	sort.SliceStable(keys, func(i, j int) bool { return keys[i] < keys[j] })

	for _, sig := range keys {
		b := digestFor(inc.Before, sig)
		d := digestFor(inc.During, sig)
		bc := BaselineComparison{
			ServiceID:      inc.PrimaryServiceID,
			SignalType:     sig,
			Classification: ClassUnknown,
		}
		if b != nil {
			v := b.Median
			bc.BeforeMedian = &v
			bc.BeforeCount = b.Count
		}
		if d != nil {
			v := d.Median
			bc.DuringMedian = &v
			bc.DuringCount = d.Count
		}
		if b == nil || d == nil || b.Count == 0 || d.Count == 0 {
			bc.Limitation = "baseline comparison unavailable: missing before or during digest"
			bc.Direction = "unknown"
			out = append(out, bc)
			continue
		}
		bc.Classification = ClassObserved
		bc.Direction = direction(*bc.BeforeMedian, *bc.DuringMedian)
		out = append(out, bc)
	}
	return out
}

func direction(before, during float64) string {
	const eps = 1e-9
	delta := during - before
	switch {
	case delta > eps:
		return "increase"
	case delta < -eps:
		return "decrease"
	default:
		return "flat"
	}
}

func digestFor(phase incident.PhaseSnapshot, sig detect.SignalType) *incident.SampleDigest {
	for i := range phase.Digests {
		if phase.Digests[i].Signal == sig {
			return &phase.Digests[i]
		}
	}
	return nil
}

func buildChangeCandidates(rep correlate.Report) []ChangeCandidateRef {
	var out []ChangeCandidateRef
	cands := rep.Candidates
	if len(cands) > MaxChangeCandidates {
		cands = cands[:MaxChangeCandidates]
	}
	for _, c := range cands {
		bd := c.ScoreBreakdown
		out = append(out, ChangeCandidateRef{
			ChangeID:             c.ChangeID,
			Rank:                 c.Rank,
			Score:                c.Score,
			ScorerVersion:        c.ScorerVersion,
			ServiceOverlap:       string(c.ServiceOverlap),
			TemporalDistanceSecs: c.TemporalDistanceSecs,
			EnvironmentMatch:     c.EnvironmentMatch,
			RankingBasis: fmt.Sprintf(
				"%s: temporal=%.1f primary=%.1f topology=%.1f environment=%.1f (not a probability)",
				c.ScorerVersion, bd.TemporalPoints, bd.PrimaryPoints, bd.TopologyPoints, bd.EnvironmentPoints,
			),
			Classification: ClassInferred,
			ScoreBreakdown: &bd,
			Observations:   append([]string{}, c.Observations...),
			Inferences:     append([]string{}, c.Inferences...),
			Limitations:    append([]string{}, c.Limitations...),
		})
	}
	return out
}

func buildMissing(inc incident.Incident, rep correlate.Report, runs []LinkedRunPulse) []string {
	var miss []string
	if !inc.Before.Available {
		miss = append(miss, "before-window digests unavailable")
	}
	if !inc.During.Available {
		miss = append(miss, "during-window digests unavailable")
	}
	if !inc.After.Available && inc.RecoveredAt != nil {
		miss = append(miss, "after-window digests unavailable for recovered incident")
	}
	if len(rep.Candidates) == 0 {
		miss = append(miss, "no ranked change candidates in lookback window")
	}
	if len(runs) == 0 {
		miss = append(miss, "no explicitly linked Run Pulse diagnostic")
	}
	miss = append(miss, "distributed traces confirming dependency edges during the incident window")
	return miss
}

func buildUnknowns(inc incident.Incident, rep correlate.Report, inv Investigation) []string {
	var u []string
	u = append(u, "Root cause is UNKNOWN — investigation does not diagnose the incident.")
	if !rep.StrongCandidate {
		u = append(u, "No strong change correlation candidate.")
	}
	if !inc.Before.Available {
		u = append(u, "Baseline (before window) insufficient for comparison.")
	}
	hasMappedNeighbor := false
	for _, n := range inv.Topology.Nodes {
		if n.Relation == "upstream" || n.Relation == "downstream" {
			hasMappedNeighbor = true
			break
		}
	}
	if !hasMappedNeighbor {
		u = append(u, "No mapped upstream/downstream neighbors for topology expansion.")
	}
	return u
}

func buildSourceRefs(inc incident.Incident, rep correlate.Report) []EvidenceRef {
	refs := []EvidenceRef{
		{Ref: "incident:" + inc.IncidentID, Summary: "persisted Pulse incident snapshot", Classification: ClassObserved},
		{Ref: "phase:before", Summary: "before-window digests", Classification: classAvail(inc.Before.Available)},
		{Ref: "phase:during", Summary: "during-window digests", Classification: classAvail(inc.During.Available)},
		{Ref: "phase:after", Summary: "after-window digests", Classification: classAvail(inc.After.Available)},
		{Ref: "correlation:report", Summary: "on-read correlation report " + rep.ScorerVersion, Classification: ClassInferred},
		{Ref: "correlation:top1", Summary: "top ranked correlation candidate (not causation)", Classification: ClassInferred},
		{Ref: "topology:primary", Summary: "static astronomy topology expansion", Classification: ClassUnknown},
	}
	for _, fid := range inc.FindingIDs {
		refs = append(refs, EvidenceRef{
			Ref:            "finding:" + fid,
			Summary:        "detector finding id referenced by incident",
			Classification: ClassObserved,
		})
	}
	for _, c := range rep.Candidates {
		if c.Rank > MaxChangeCandidates {
			break
		}
		refs = append(refs, EvidenceRef{
			Ref:            "change:" + c.ChangeID,
			Summary:        fmt.Sprintf("ranked change candidate #%d score=%.1f", c.Rank, c.Score),
			Classification: ClassInferred,
		})
	}
	for _, e := range inc.Evidence {
		refs = append(refs, EvidenceRef{
			Ref:            e.Ref,
			Summary:        e.Summary,
			Classification: ClassObserved,
		})
	}
	return refs
}

func classAvail(ok bool) Classification {
	if ok {
		return ClassObserved
	}
	return ClassUnknown
}

func fingerprint(inv Investigation) (string, error) {
	// Stable subset excluding generated IDs. Includes phase digests and topology
	// so reuse_identical cannot return a stale snapshot after evidence changes.
	type fp struct {
		IncidentID string                 `json:"incident_id"`
		State      string                 `json:"incident_state"`
		EvidenceAt time.Time              `json:"evidence_collected_at"`
		Symptoms   []Symptom              `json:"observed_symptoms"`
		Baselines  []BaselineComparison   `json:"baseline_comparisons"`
		Before     incident.PhaseSnapshot `json:"before"`
		During     incident.PhaseSnapshot `json:"during"`
		After      incident.PhaseSnapshot `json:"after"`
		Topology   TopologyContext        `json:"topology"`
		Changes    []ChangeCandidateRef   `json:"change_candidates"`
		Strong     bool                   `json:"strong_change_candidate"`
		Hypotheses []Hypothesis           `json:"hypotheses"`
		Missing    []string               `json:"missing_evidence"`
		Method     string                 `json:"generation_method"`
		LinkedRuns []LinkedRunPulse       `json:"linked_runs"`
		SourceRefs []EvidenceRef          `json:"source_references"`
	}
	raw, err := json.Marshal(fp{
		IncidentID: inv.IncidentID,
		State:      inv.IncidentState,
		EvidenceAt: inv.EvidenceCollectedAt,
		Symptoms:   inv.ObservedSymptoms,
		Baselines:  inv.BaselineComparisons,
		Before:     inv.Before,
		During:     inv.During,
		After:      inv.After,
		Topology:   inv.Topology,
		Changes:    inv.ChangeCandidates,
		Strong:     inv.StrongChangeCandidate,
		Hypotheses: inv.Hypotheses,
		Missing:    inv.MissingEvidence,
		Method:     inv.GenerationMethod,
		LinkedRuns: inv.LinkedRunPulse,
		SourceRefs: inv.SourceReferences,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:16]), nil
}

func investigationID(incidentID, fp string, now time.Time) string {
	ts := now.UTC().Format("20060102T150405.000000000Z")
	return fmt.Sprintf("inv|%s|%s|%s", sanitizeID(incidentID), fp[:12], ts)
}

// IncidentStateLabel maps open/recovered to API-facing labels.
func IncidentStateLabel(inc incident.Incident) string {
	if inc.Open() {
		return IncidentStateActive
	}
	return IncidentStateRecovered
}

// Ensure unused import guard if strings needed.
var _ = strings.TrimSpace
