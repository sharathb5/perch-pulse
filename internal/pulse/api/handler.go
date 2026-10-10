package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/yashg4509/perch/internal/pulse/change"
	"github.com/yashg4509/perch/internal/pulse/correlate"
	"github.com/yashg4509/perch/internal/pulse/incident"
)

// Handler serves read-only Pulse JSON on the existing viz mux.
type Handler struct {
	Reader *Reader
}

// Register mounts Pulse routes on mux. Only GET is registered.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/pulse/services", h.handleServices)
	mux.HandleFunc("GET /api/pulse/incidents", h.handleIncidents)
	// {id...} because Pulse incident/change IDs embed '/' (e.g. astronomy/local/shipping).
	mux.HandleFunc("GET /api/pulse/incidents/{id...}", h.handleIncidentDetail)
	mux.HandleFunc("GET /api/pulse/changes", h.handleChanges)
	mux.HandleFunc("GET /api/pulse/changes/{id...}", h.handleChangeDetail)
}

func (h *Handler) handleServices(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Reader == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "pulse_unavailable", "Pulse reader is not configured")
		return
	}
	limit := h.Reader.ResolveLimit(r.URL.Query().Get("limit"))
	resp, err := h.Reader.ListServices(limit)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleIncidents(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Reader == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "pulse_unavailable", "Pulse reader is not configured")
		return
	}
	limit := h.Reader.ResolveLimit(r.URL.Query().Get("limit"))
	list, err := h.Reader.Incidents.List()
	if err != nil {
		writeStoreError(w, err)
		return
	}
	// Newest first; stable tie-break on incident_id.
	sort.SliceStable(list, func(i, j int) bool {
		if !list[i].FirstDetectedAt.Equal(list[j].FirstDetectedAt) {
			return list[i].FirstDetectedAt.After(list[j].FirstDetectedAt)
		}
		return list[i].IncidentID < list[j].IncidentID
	})
	if len(list) > limit {
		list = list[:limit]
	}
	out := make([]incident.Incident, 0, len(list))
	for i := range list {
		out = append(out, RedactIncident(list[i]))
	}
	resp := IncidentsResponse{
		SchemaVersion: SchemaIncidents,
		GeneratedAt:   h.Reader.nowUTC(),
		DataSources: DataSources{
			Observations: h.Reader.observationSourceLabel(),
			Incidents:    "file_store",
			Changes:      "file_store",
		},
		Limitations: []string{
			"Incident list is a persisted snapshot under the Pulse data directory.",
			"Evaluation labels and harness records are not included in this API.",
		},
		Count:     len(out),
		Limit:     limit,
		Incidents: out,
	}
	writeAPIJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleIncidentDetail(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Reader == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "pulse_unavailable", "Pulse reader is not configured")
		return
	}
	id := r.PathValue("id")
	if err := ValidateResourceID(id); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	inc, ok, err := h.Reader.Incidents.Get(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found", fmt.Sprintf("incident %q not found", id))
		return
	}
	inc = RedactIncident(inc)

	changes, err := h.Reader.Changes.List()
	if err != nil {
		writeStoreError(w, err)
		return
	}
	for i := range changes {
		changes[i] = RedactChange(changes[i])
	}
	rep, err := correlate.Correlate(inc, changes, correlate.DefaultConfig())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "correlation_failed", err.Error())
		return
	}

	resp := IncidentDetailResponse{
		SchemaVersion: SchemaIncident,
		GeneratedAt:   h.Reader.nowUTC(),
		DataSources: DataSources{
			Observations: h.Reader.observationSourceLabel(),
			Incidents:    "file_store",
			Changes:      "file_store",
			Correlation:  "on_read",
		},
		Limitations: []string{
			"Correlation ranks temporal/service overlap; it is not proven causation.",
			"Incident detail is a persisted snapshot; not a continuous live stream.",
		},
		Incident:    inc,
		Correlation: &rep,
	}
	if h.Reader.Mapper != nil {
		if node, _, mapped := h.Reader.Mapper.GraphNodeForPulseServiceID(inc.PrimaryServiceID); mapped {
			resp.GraphNode = node
		}
	}
	writeAPIJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleChanges(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Reader == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "pulse_unavailable", "Pulse reader is not configured")
		return
	}
	limit := h.Reader.ResolveLimit(r.URL.Query().Get("limit"))
	list, err := h.Reader.Changes.List()
	if err != nil {
		writeStoreError(w, err)
		return
	}
	sort.SliceStable(list, func(i, j int) bool {
		ti, tj := list[i].ObservedAt, list[j].ObservedAt
		if list[i].DeployedAt != nil {
			ti = *list[i].DeployedAt
		}
		if list[j].DeployedAt != nil {
			tj = *list[j].DeployedAt
		}
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return list[i].ChangeID < list[j].ChangeID
	})
	if len(list) > limit {
		list = list[:limit]
	}
	out := make([]change.Event, 0, len(list))
	for i := range list {
		out = append(out, RedactChange(list[i]))
	}
	resp := ChangesResponse{
		SchemaVersion: SchemaChanges,
		GeneratedAt:   h.Reader.nowUTC(),
		DataSources: DataSources{
			Observations: h.Reader.observationSourceLabel(),
			Incidents:    "file_store",
			Changes:      "file_store",
		},
		Limitations: []string{
			"Change list is a persisted snapshot under the Pulse data directory.",
		},
		Count:   len(out),
		Limit:   limit,
		Changes: out,
	}
	writeAPIJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleChangeDetail(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Reader == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "pulse_unavailable", "Pulse reader is not configured")
		return
	}
	id := r.PathValue("id")
	if err := ValidateResourceID(id); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	ev, ok, err := h.Reader.Changes.Get(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found", fmt.Sprintf("change %q not found", id))
		return
	}
	ev = RedactChange(ev)
	resp := ChangeDetailResponse{
		SchemaVersion: SchemaChange,
		GeneratedAt:   h.Reader.nowUTC(),
		DataSources: DataSources{
			Observations: h.Reader.observationSourceLabel(),
			Incidents:    "file_store",
			Changes:      "file_store",
		},
		Change: ev,
	}
	if h.Reader.Mapper != nil {
		seen := map[string]struct{}{}
		for _, sid := range ev.ServiceIDs {
			if node, _, mapped := h.Reader.Mapper.GraphNodeForPulseServiceID(sid); mapped {
				if _, dup := seen[node]; !dup {
					seen[node] = struct{}{}
					resp.GraphNodes = append(resp.GraphNodes, node)
				}
			}
		}
		sort.Strings(resp.GraphNodes)
	}
	writeAPIJSON(w, http.StatusOK, resp)
}

func writeAPIJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

func writeAPIError(w http.ResponseWriter, status int, code, msg string) {
	writeAPIJSON(w, status, ErrorBody{
		SchemaVersion: SchemaError,
		Error:         msg,
		Code:          code,
	})
}

func writeStoreError(w http.ResponseWriter, err error) {
	msg := err.Error()
	code := "store_error"
	status := http.StatusInternalServerError
	var lims []string
	if strings.Contains(msg, "invalid character") ||
		strings.Contains(msg, "unexpected end of JSON") ||
		strings.Contains(msg, "cannot unmarshal") {
		code = "corrupted_artifact"
		lims = append(lims, "A persisted Pulse artifact could not be parsed; refusing to invent substitute data.")
	}
	writeAPIJSON(w, status, ErrorBody{
		SchemaVersion: SchemaError,
		Error:         msg,
		Code:          code,
		Limitations:   lims,
	})
}

// Ensure correlate is referenced for reviewers scanning imports.
var _ = correlate.SchemaVersion
