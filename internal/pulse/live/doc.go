// Package live runs the optional Live Pulse collector inside perch viz.
//
// When enabled via explicit server-side configuration, a single poll loop:
//  1. fetches bounded Astronomy Shop spanmetrics from a loopback Prometheus,
//  2. normalizes samples into detect.Sample / observation.Observation,
//  3. feeds the existing baseline detector,
//  4. builds/updates incidents via incident.Build + FileStore.Upsert,
//  5. publishes a concurrency-safe Snapshot for read-only HTTP handlers.
//
// Live Pulse is independent of Run Pulse (on-demand diagnostics) and of
// active stackstatus probes. Disabled or misconfigured Live Pulse reports
// unavailable — never healthy. GET handlers never issue Prometheus queries.
package live
