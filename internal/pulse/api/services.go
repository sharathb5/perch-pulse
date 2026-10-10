package api

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/incident"
	"github.com/yashg4509/perch/internal/pulse/observation"
)

const serviceStatusSchema = "pulse.api.service_status.v1"

const activeHealthNote = "Active reachability/latency probes remain on GET /api/status and are not represented here."

// ListServices builds Pulse intelligence rows for known service IDs.
func (r *Reader) ListServices(limit int) (ServicesResponse, error) {
	now := r.nowUTC()
	incs, err := r.Incidents.List()
	if err != nil {
		return ServicesResponse{}, fmt.Errorf("list incidents: %w", err)
	}
	changes, err := r.Changes.List()
	if err != nil {
		return ServicesResponse{}, fmt.Errorf("list changes: %w", err)
	}

	ids := map[string]struct{}{}
	for _, inc := range incs {
		ids[inc.PrimaryServiceID] = struct{}{}
		for _, sid := range inc.AffectedServiceIDs {
			if strings.TrimSpace(sid) != "" {
				ids[sid] = struct{}{}
			}
		}
	}
	for _, ch := range changes {
		for _, sid := range ch.ServiceIDs {
			if strings.TrimSpace(sid) != "" {
				ids[sid] = struct{}{}
			}
		}
	}

	sorted := make([]string, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)

	lim := limit
	if lim <= 0 {
		lim = r.ResolveLimit("")
	}
	if len(sorted) > lim {
		sorted = sorted[:lim]
	}

	out := make([]ServicePulseStatus, 0, len(sorted))
	for _, id := range sorted {
		st, err := r.serviceStatus(id, incs, now)
		if err != nil {
			return ServicesResponse{}, err
		}
		out = append(out, st)
	}

	limitations := []string{
		"Pulse intelligence is distinct from active health probes (GET /api/status).",
		"This endpoint returns a snapshot; it is not a continuous live stream.",
	}
	if r.Observations == nil {
		limitations = append(limitations,
			"Observation history is process-local and unavailable in this viz process; intelligence uses persisted incidents when present and never treats missing data as healthy.")
	}

	return ServicesResponse{
		SchemaVersion: SchemaServices,
		GeneratedAt:   now,
		DataSources: DataSources{
			Observations: r.observationSourceLabel(),
			Incidents:    "file_store",
			Changes:      "file_store",
		},
		Limitations: limitations,
		Count:       len(out),
		Limit:       lim,
		Services:    out,
	}, nil
}

func (r *Reader) serviceStatus(serviceID string, incs []incident.Incident, now time.Time) (ServicePulseStatus, error) {
	st := ServicePulseStatus{
		SchemaVersion:         serviceStatusSchema,
		ServiceID:             serviceID,
		Intelligence:          observation.StatusUnknown,
		IntelligenceAvailable: false,
		Freshness:             FreshnessUnavailable,
		OpenIncidentIDs:       []string{},
		ActiveHealthNote:      activeHealthNote,
	}
	if r.Mapper != nil {
		if node, src, ok := r.Mapper.GraphNodeForPulseServiceID(serviceID); ok {
			st.GraphNode = node
			st.MappingSource = src
		}
	}

	var related, open []string
	var latestOpenAt time.Time
	var worstOpen observation.Status
	for _, inc := range incs {
		if !incidentTouches(inc, serviceID) {
			continue
		}
		related = append(related, inc.IncidentID)
		if inc.Open() {
			open = append(open, inc.IncidentID)
			if inc.LastObservedAt.After(latestOpenAt) {
				latestOpenAt = inc.LastObservedAt
			}
			cand := intelligenceFromOpenIncident(inc)
			if severityRank(cand) > severityRank(worstOpen) {
				worstOpen = cand
			}
		}
	}
	sort.Strings(open)
	sort.Strings(related)
	if open == nil {
		open = []string{}
	}
	if related == nil {
		related = []string{}
	}
	st.OpenIncidentIDs = open
	st.RelatedIncidentIDs = related

	// Observations take precedence when present (live process memory).
	if r.Observations != nil {
		obs, ok, err := r.Observations.Latest(serviceID)
		if err != nil {
			return ServicePulseStatus{}, err
		}
		if ok {
			eff := obs.EffectiveStatus(now, r.freshnessWindow())
			st.Intelligence = eff
			st.IntelligenceAvailable = true
			st.ObservationSource = obs.Source
			t := obs.ObservedAt.UTC()
			st.LastObservedAt = &t
			age := now.Sub(t).Seconds()
			st.AgeSeconds = &age
			if obs.IsStale(now, r.freshnessWindow()) {
				st.Freshness = FreshnessStale
				st.Limitations = append(st.Limitations,
					"Observation is stale relative to freshness window; stale ≠ healthy.")
			} else {
				st.Freshness = FreshnessFresh
			}
			// Still attach open incidents as evidence refs.
			return st, nil
		}
		st.Freshness = FreshnessMissing
		st.Limitations = append(st.Limitations, "No observation stored for this service_id.")
	} else {
		st.Limitations = append(st.Limitations,
			"No process-local observation store is wired; cannot report fresh telemetry intelligence.")
	}

	// Fall back to open-incident-derived intelligence (persisted snapshot).
	if len(open) > 0 {
		st.Intelligence = worstOpen
		if st.Intelligence == observation.StatusUnknown {
			st.Intelligence = observation.StatusDegraded
		}
		st.IntelligenceAvailable = true
		t := latestOpenAt.UTC()
		st.LastObservedAt = &t
		age := now.Sub(t).Seconds()
		st.AgeSeconds = &age
		st.ObservationSource = "persisted_incident"
		if isTimestampStale(latestOpenAt, now, r.freshnessWindow()) {
			st.Freshness = FreshnessStale
			st.Limitations = append(st.Limitations,
				"Open-incident evidence is older than the freshness window; treated as stale, not healthy.")
		} else {
			st.Freshness = FreshnessFresh
		}
		st.Limitations = append(st.Limitations,
			"Intelligence derived from open persisted incidents (snapshot), not a live probe.")
		return st, nil
	}

	// No observations, no open incidents → unknown / unavailable (never healthy).
	if st.Freshness == FreshnessMissing {
		// already set
	} else {
		st.Freshness = FreshnessUnavailable
	}
	st.Intelligence = observation.StatusUnknown
	st.IntelligenceAvailable = false
	st.Limitations = append(st.Limitations,
		"Missing Pulse intelligence must not be interpreted as healthy.")
	return st, nil
}

func incidentTouches(inc incident.Incident, serviceID string) bool {
	if inc.PrimaryServiceID == serviceID {
		return true
	}
	for _, id := range inc.AffectedServiceIDs {
		if id == serviceID {
			return true
		}
	}
	return false
}

func intelligenceFromOpenIncident(inc incident.Incident) observation.Status {
	for _, sig := range inc.SignalTypes {
		switch strings.ToLower(sig) {
		case "error_rate", "outage", "availability":
			return observation.StatusUnhealthy
		}
	}
	// latency / call_rate regressions → degraded
	for _, sig := range inc.SignalTypes {
		switch strings.ToLower(sig) {
		case "latency_ms", "call_rate":
			return observation.StatusDegraded
		}
	}
	return observation.StatusDegraded
}

func isTimestampStale(at, now time.Time, freshness time.Duration) bool {
	o := observation.Observation{
		ServiceID:  "tmp",
		ObservedAt: at,
		Status:     observation.StatusDegraded,
		Source:     "tmp",
	}
	return o.IsStale(now, freshness)
}

func severityRank(s observation.Status) int {
	switch s {
	case observation.StatusUnhealthy:
		return 6
	case observation.StatusUnavailable:
		return 5
	case observation.StatusUnknown:
		return 4
	case observation.StatusDegraded:
		return 3
	case observation.StatusStale:
		return 2
	case observation.StatusHealthy:
		return 1
	default:
		return 4
	}
}
