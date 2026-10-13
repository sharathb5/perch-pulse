// Package export provides a local, Databricks-ready analytical export of Pulse
// investigation and evaluation records (Milestone F3a).
//
// It reads immutable FileStore JSON under a pulse data directory, projects
// allowlisted fields into versioned JSONL datasets, and maintains an incremental
// checkpoint for idempotent, resumable exports. It does not provision Databricks,
// load Delta tables, or call cloud APIs.
package export
