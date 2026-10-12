// Package investigation assembles bounded, versioned evidence-backed
// investigation snapshots from existing Pulse incidents (Milestone F1).
//
// Investigations are read-only by default: they do not run Run Pulse, scrape
// Prometheus, or perform remediation. Hypotheses are deterministic inferences
// classified as INFERRED — never proven root causes. Correlation is never
// presented as causation.
//
// Schema: pulse.investigation.v1
// Feedback: pulse.investigation.feedback.v1
// Generation method (F1): deterministic_rules_v1
//
// Future LLM proposers may implement Proposer against the same EvidenceSnapshot
// input; F1 ships only DeterministicProposer. No external AI APIs are called.
package investigation
