package api

import (
	"net/http"
	"time"

	"github.com/yashg4509/perch/internal/pulse/live"
)

// LiveSource is the optional Live Pulse snapshot provider.
type LiveSource interface {
	Snapshot() live.Snapshot
}

// registerLiveRoutes mounts read-only live Pulse endpoints.
func (h *Handler) registerLiveRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/pulse/live/status", h.handleLiveStatus)
	mux.HandleFunc("GET /api/pulse/live/services", h.handleLiveServices)
	mux.HandleFunc("GET /api/pulse/live/incidents", h.handleLiveIncidents)
}

func (h *Handler) liveSnapshot() live.Snapshot {
	if h != nil && h.Live != nil {
		return h.Live.Snapshot()
	}
	now := time.Now().UTC()
	if h != nil && h.Reader != nil {
		now = h.Reader.nowUTC()
	}
	return live.DisabledSnapshot(now, "Live Pulse is not mounted in this viz process.")
}

func (h *Handler) handleLiveStatus(w http.ResponseWriter, r *http.Request) {
	_ = r
	snap := h.liveSnapshot()
	writeAPIJSON(w, http.StatusOK, LiveStatusResponse{
		SchemaVersion: live.SchemaLiveStatus,
		GeneratedAt:   snap.Status.GeneratedAt,
		Status:        snap.Status,
		Limitations:   snap.Status.Limitations,
	})
}

func (h *Handler) handleLiveServices(w http.ResponseWriter, r *http.Request) {
	_ = r
	snap := h.liveSnapshot()
	writeAPIJSON(w, http.StatusOK, LiveServicesResponse{
		SchemaVersion: live.SchemaLiveServices,
		GeneratedAt:   snap.Status.GeneratedAt,
		Count:         len(snap.Services),
		Services:      snap.Services,
		Status:        snap.Status,
		Limitations: []string{
			"Live service signals come from the server-side collector snapshot.",
			"GET does not query Prometheus; missing values are not zero errors.",
			"Active health probes remain on GET /api/status.",
		},
	})
}

func (h *Handler) handleLiveIncidents(w http.ResponseWriter, r *http.Request) {
	_ = r
	snap := h.liveSnapshot()
	// Redact incident payloads.
	incs := make([]live.IncidentView, 0, len(snap.Incidents))
	for _, iv := range snap.Incidents {
		iv.Incident = RedactIncident(iv.Incident)
		incs = append(incs, iv)
	}
	writeAPIJSON(w, http.StatusOK, LiveIncidentsResponse{
		SchemaVersion: live.SchemaLiveIncidents,
		GeneratedAt:   snap.Status.GeneratedAt,
		Count:         len(incs),
		Incidents:     incs,
		Status:        snap.Status,
		Limitations: []string{
			"Active live incidents are labeled separately from recovered/historical snapshots.",
			"Deduplicated by stable incident_id across live and persisted views.",
		},
	})
}
