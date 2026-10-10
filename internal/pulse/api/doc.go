// Package api exposes a read-only Pulse HTTP surface for perch viz.
//
// Milestone A (UI integration) mounts these handlers on the existing localhost
// viz server. It does not replace /api/graph, /api/status, /api/logs, or
// /api/credentials, and it does not start background telemetry collectors.
//
// Data sources (explicit split):
//   - Incidents and changes: persisted under the Pulse data directory
//     (.perch/pulse or $PERCH_PULSE_DIR) via incident.FileStore / change.FileStore.
//   - Observations: process-local store.Store (typically empty in the viz
//     process). Missing observations are reported as unavailable/unknown — never
//     as healthy.
//
// Responses are snapshot reads of persisted artifacts (plus on-read correlation).
// They are not a continuous live stream.
//
// This package never imports internal/pulse/scenario (ground-truth isolation).
package api
