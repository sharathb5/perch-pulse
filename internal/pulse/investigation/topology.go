package investigation

import (
	"fmt"
	"sort"
	"strings"

	"github.com/yashg4509/perch/internal/pulse/astronomy"
	"github.com/yashg4509/perch/internal/pulse/incident"
)

// GraphMapper resolves Pulse service IDs to graph node names when available.
type GraphMapper interface {
	GraphNodeForPulseServiceID(serviceID string) (graphNode string, mappingSource string, ok bool)
}

// BuildTopology expands one hop of astronomy depends_on around the primary service.
// Static edges are not proof that a request exercised the dependency.
func BuildTopology(inc incident.Incident, mapping astronomy.Mapping, mapper GraphMapper) TopologyContext {
	ctx := TopologyContext{
		PrimaryServiceID: inc.PrimaryServiceID,
		Depth:            1,
		Limitations: []string{
			"Topology edges come from the static Astronomy Shop mapping; they are not evidence that a live request traversed the edge.",
			"Anomalous=true only for services listed as affected in the incident snapshot.",
		},
	}

	anomalous := map[string]struct{}{}
	anomalous[inc.PrimaryServiceID] = struct{}{}
	for _, sid := range inc.AffectedServiceIDs {
		anomalous[sid] = struct{}{}
	}

	env := inc.Environment
	if env == "" {
		env = astronomy.LocalEnvironment
	}

	// Index: otel name → service id; depends_on edges.
	upstreamOf := map[string][]string{}   // service → services it depends on
	downstreamOf := map[string][]string{} // service → dependents
	known := map[string]struct{}{}
	for _, svc := range mapping.Services {
		id, err := svc.PulseServiceID(env)
		if err != nil {
			continue
		}
		known[id] = struct{}{}
		for _, dep := range svc.DependsOnOTEL {
			depID, err := astronomy.ServiceID(env, dep)
			if err != nil {
				continue
			}
			upstreamOf[id] = append(upstreamOf[id], depID)
			downstreamOf[depID] = append(downstreamOf[depID], id)
		}
	}

	addNode := func(sid, relation string, note string) {
		if sid == "" {
			return
		}
		_, isAnom := anomalous[sid]
		class := ClassObserved
		if !isAnom {
			class = ClassUnknown
		}
		node := TopologyNode{
			ServiceID:      sid,
			Relation:       relation,
			Anomalous:      isAnom,
			Classification: class,
			Note:           note,
		}
		if mapper != nil {
			if gn, _, ok := mapper.GraphNodeForPulseServiceID(sid); ok {
				node.GraphNode = gn
			}
		}
		if _, ok := known[sid]; !ok && relation != "primary" && relation != "affected" {
			node.Relation = "unmapped"
			node.Note = "service identity not present in astronomy mapping table"
			node.Classification = ClassUnknown
		}
		ctx.Nodes = append(ctx.Nodes, node)
	}

	addNode(inc.PrimaryServiceID, "primary", "primary service from incident record")
	for _, sid := range inc.AffectedServiceIDs {
		if sid == inc.PrimaryServiceID {
			continue
		}
		addNode(sid, "affected", "listed in incident affected_service_ids")
	}

	ups := uniqueSorted(upstreamOf[inc.PrimaryServiceID])
	downs := uniqueSorted(downstreamOf[inc.PrimaryServiceID])
	n := 0
	for _, sid := range ups {
		if n >= MaxTopologyNeighbors {
			ctx.Limitations = append(ctx.Limitations, fmt.Sprintf("upstream expansion truncated at %d neighbors", MaxTopologyNeighbors))
			break
		}
		if sid == inc.PrimaryServiceID {
			continue
		}
		addNode(sid, "upstream", "static depends_on: primary depends on this service")
		n++
	}
	for _, sid := range downs {
		if n >= MaxTopologyNeighbors {
			ctx.Limitations = append(ctx.Limitations, fmt.Sprintf("downstream expansion truncated at %d neighbors", MaxTopologyNeighbors))
			break
		}
		if sid == inc.PrimaryServiceID {
			continue
		}
		addNode(sid, "downstream", "static depends_on: this service depends on primary")
		n++
	}

	// Deduplicate by service_id preferring primary/affected/anomalous.
	ctx.Nodes = dedupeTopology(ctx.Nodes)
	sort.SliceStable(ctx.Nodes, func(i, j int) bool {
		ri, rj := relationRank(ctx.Nodes[i].Relation), relationRank(ctx.Nodes[j].Relation)
		if ri != rj {
			return ri < rj
		}
		return ctx.Nodes[i].ServiceID < ctx.Nodes[j].ServiceID
	})
	return ctx
}

func relationRank(r string) int {
	switch r {
	case "primary":
		return 0
	case "affected":
		return 1
	case "upstream":
		return 2
	case "downstream":
		return 3
	default:
		return 4
	}
}

func uniqueSorted(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func dedupeTopology(nodes []TopologyNode) []TopologyNode {
	best := map[string]TopologyNode{}
	order := []string{}
	for _, n := range nodes {
		prev, ok := best[n.ServiceID]
		if !ok {
			best[n.ServiceID] = n
			order = append(order, n.ServiceID)
			continue
		}
		if relationRank(n.Relation) < relationRank(prev.Relation) {
			best[n.ServiceID] = n
		} else if n.Anomalous && !prev.Anomalous {
			best[n.ServiceID] = n
		}
	}
	out := make([]TopologyNode, 0, len(order))
	for _, id := range order {
		out = append(out, best[id])
	}
	return out
}
