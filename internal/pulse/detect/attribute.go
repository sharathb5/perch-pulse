package detect

import (
	"fmt"
	"sort"

	"github.com/yashg4509/perch/internal/pulse/astronomy"
)

// Attribute attaches ranked impact candidates using detector scores and the
// Astronomy Shop topology map. Topology comes from astronomy mapping only —
// never from scenario ground-truth labels.
func Attribute(findings []Finding, mapping astronomy.Mapping) []Finding {
	if len(findings) == 0 {
		return findings
	}
	// Build dependents: service A depends_on B ⇒ B's downstream includes A.
	downstream := map[string][]string{}
	env := astronomy.LocalEnvironment
	for _, svc := range mapping.Services {
		if svc.OTELServiceName == "" {
			continue
		}
		id, err := astronomy.ServiceID(env, svc.OTELServiceName)
		if err != nil {
			continue
		}
		for _, dep := range svc.DependsOnOTEL {
			depID, err := astronomy.ServiceID(env, dep)
			if err != nil {
				continue
			}
			downstream[depID] = append(downstream[depID], id)
		}
	}

	// Open findings only for ranking primary.
	type scored struct {
		idx   int
		score float64
		id    string
	}
	var open []scored
	for i, f := range findings {
		if !f.Open() {
			continue
		}
		open = append(open, scored{idx: i, score: f.Score, id: f.ServiceID})
	}
	sort.SliceStable(open, func(i, j int) bool {
		if open[i].score != open[j].score {
			return open[i].score > open[j].score
		}
		return open[i].id < open[j].id
	})

	degraded := map[string]float64{}
	for _, s := range open {
		if s.score > degraded[s.id] {
			degraded[s.id] = s.score
		}
	}

	out := make([]Finding, len(findings))
	copy(out, findings)
	for rank, s := range open {
		f := out[s.idx]
		cands := []ImpactCandidate{{
			ServiceID: f.ServiceID,
			Rank:      1,
			Score:     f.Score,
			Relation:  "primary",
			Note:      "service with strongest observed degradation for this finding",
		}}
		// Also-degraded peers (not primary service).
		type peer struct {
			id    string
			score float64
		}
		var peers []peer
		for id, sc := range degraded {
			if id == f.ServiceID {
				continue
			}
			peers = append(peers, peer{id: id, score: sc})
		}
		sort.SliceStable(peers, func(i, j int) bool {
			if peers[i].score != peers[j].score {
				return peers[i].score > peers[j].score
			}
			return peers[i].id < peers[j].id
		})
		r := 2
		for _, p := range peers {
			cands = append(cands, ImpactCandidate{
				ServiceID: p.id,
				Rank:      r,
				Score:     p.score,
				Relation:  "also_degraded",
				Note:      "also showed observed degradation in the same evaluation tick",
			})
			r++
		}
		// Topology downstream of this finding's service.
		for _, dep := range uniqueSorted(downstream[f.ServiceID]) {
			already := false
			for _, c := range cands {
				if c.ServiceID == dep {
					already = true
					break
				}
			}
			if already {
				continue
			}
			cands = append(cands, ImpactCandidate{
				ServiceID: dep,
				Rank:      r,
				Score:     0,
				Relation:  "downstream_of_primary",
				Note:      fmt.Sprintf("configured dependent of %s; likely affected if primary degradation persists (correlation, not causation)", f.ServiceID),
			})
			r++
		}
		// Cap candidates for stable eval.
		if len(cands) > 5 {
			cands = cands[:5]
		}
		f.CandidateImpact = cands
		// Annotate global rank among open findings on the primary note.
		if rank == 0 && len(f.CandidateImpact) > 0 {
			f.CandidateImpact[0].Note = "top-ranked degraded service this tick; " + f.CandidateImpact[0].Note
		}
		out[s.idx] = f
	}
	return out
}

func uniqueSorted(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
