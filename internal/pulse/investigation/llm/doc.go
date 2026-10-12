// Package llm implements Milestone F2a: an optional, constrained LLM investigator.
//
// It consumes an immutable F1 investigation snapshot, builds a bounded redacted
// evidence payload, calls an OpenAI-compatible chat/completions endpoint when
// explicitly enabled, and validates structured proposals against the source
// evidence. Proposals are persisted separately and never mutate F1 snapshots,
// incidents, detector thresholds, deterministic hypotheses, or F1 feedback.
//
// Disabled by default. No provider calls occur on GET or during incident
// creation. Tests and CI use deterministic fake providers only — no paid calls.
package llm
