// Package api exposes a read-only Pulse HTTP surface for perch viz.
//
// Milestone A (UI integration) mounts these handlers on the existing localhost
// viz server. It does not replace /api/graph, /api/status, /api/logs, or
// /api/credentials. Optional Live Pulse collection is started by the CLI wiring
// layer (internal/cli), not by this package.
//
// Data sources (explicit split):
//   - Incidents and changes: persisted under the Pulse data directory
//     (.perch/pulse or $PERCH_PULSE_DIR) via incident.FileStore / change.FileStore.
//   - Observations: process-local store.Store (wired when Live Pulse is enabled).
//     Missing observations are reported as unavailable/unknown — never as healthy.
//   - Live snapshot: optional LiveSource for GET /api/pulse/live/* (collector
//     cache only; handlers never query Prometheus).
//
// Historical responses are snapshot reads of persisted artifacts (plus on-read
// correlation). Live routes expose the collector snapshot when configured.
//
// This package never imports internal/pulse/scenario (ground-truth isolation).
package api
