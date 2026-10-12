package investigation

// EvidenceSnapshot is the narrow input seam for investigation proposers.
// F1 uses DeterministicProposer only; future LLM proposers must consume this
// versioned snapshot and emit Proposal without mutating stored incidents.
type EvidenceSnapshot struct {
	SchemaVersion string        `json:"schema_version"`
	Investigation Investigation `json:"investigation_draft"` // pre-hypothesis assembly
}

// Proposal is the versioned output seam for a proposer.
type Proposal struct {
	SchemaVersion string       `json:"schema_version"`
	Method        string       `json:"generation_method"`
	Hypotheses    []Hypothesis `json:"hypotheses"`
	Limitations   []string     `json:"limitations"`
}

// Proposer generates hypotheses from an evidence snapshot.
// Implementations must not call external networks in F1.
type Proposer interface {
	Propose(snap EvidenceSnapshot) (Proposal, error)
}

// DeterministicProposer is the only F1 implementation.
type DeterministicProposer struct{}

// Propose runs the rules engine on the draft investigation fields.
func (DeterministicProposer) Propose(snap EvidenceSnapshot) (Proposal, error) {
	inv := snap.Investigation
	hyps := GenerateHypotheses(&inv)
	return Proposal{
		SchemaVersion: SchemaVersion,
		Method:        GenerationMethodDeterministic,
		Hypotheses:    hyps,
		Limitations: []string{
			"Deterministic rules only — no LLM was used.",
			"Hypotheses are INFERRED and are not confirmed root causes.",
		},
	}, nil
}
