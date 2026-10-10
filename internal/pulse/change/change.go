// Package change defines typed Pulse change/deployment events and a local
// store for ingesting them. Correlation consumes these events; this package
// does not import scenario ground truth or claim causation.
package change

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

const (
	// SchemaVersion is the persisted change-event schema.
	SchemaVersion = "pulse.change.v1"
)

// ChangeType classifies the software change.
type ChangeType string

const (
	ChangeCommit     ChangeType = "commit"
	ChangePRMerge    ChangeType = "pull_request_merge"
	ChangeDeployment ChangeType = "deployment"
)

// Valid reports whether t is a supported change type.
func (t ChangeType) Valid() bool {
	switch t {
	case ChangeCommit, ChangePRMerge, ChangeDeployment:
		return true
	default:
		return false
	}
}

// EvidenceRef is a non-secret pointer to supporting artifacts.
type EvidenceRef struct {
	Ref     string `json:"ref"`
	Summary string `json:"summary"`
}

// MappingUncertainty records when affected services are incomplete or guessed.
type MappingUncertainty string

const (
	MappingExplicit   MappingUncertainty = "explicit"   // service IDs provided by caller
	MappingConfigured MappingUncertainty = "configured" // path/repo rules applied
	MappingUnknown    MappingUncertainty = "unknown"    // no reliable mapping
	MappingPartial    MappingUncertainty = "partial"    // some paths mapped, others not
)

// Event is a versioned software change (commit, PR merge, or deployment).
// It is intentionally generic for future GitHub/CI/cloud integrations.
type Event struct {
	SchemaVersion string     `json:"schema_version"`
	ChangeID      string     `json:"change_id"`
	ChangeType    ChangeType `json:"change_type"`
	// Source identifies the producer (e.g. "cli", "deploy-marker", "local-git").
	Source string `json:"source"`

	Repository string `json:"repository,omitempty"`
	CommitSHA  string `json:"commit_sha,omitempty"`
	ParentSHA  string `json:"parent_sha,omitempty"`
	Branch     string `json:"branch,omitempty"`
	PRNumber   *int   `json:"pr_number,omitempty"`

	// Environment is the deploy/runtime environment when known (e.g. "local").
	Environment string `json:"environment,omitempty"`
	// ServiceIDs are explicitly or mapped potentially affected services.
	ServiceIDs []string `json:"service_ids,omitempty"`
	// ServiceMapping records how ServiceIDs were obtained.
	ServiceMapping MappingUncertainty `json:"service_mapping,omitempty"`

	Actor   string `json:"actor,omitempty"`
	Title   string `json:"title,omitempty"`
	Summary string `json:"summary,omitempty"`

	// CreatedAt is when the change was authored/created (UTC).
	CreatedAt time.Time `json:"created_at"`
	// DeployedAt is when the change was deployed, if applicable (UTC).
	DeployedAt *time.Time `json:"deployed_at,omitempty"`
	// ObservedAt is when Pulse recorded the event (UTC).
	ObservedAt time.Time `json:"observed_at"`

	// Metadata holds non-secret key/value refs (never credentials).
	Metadata map[string]string `json:"metadata,omitempty"`
	Evidence []EvidenceRef     `json:"evidence,omitempty"`

	// FilesChanged is optional lightweight commit metadata (paths only).
	FilesChanged []string `json:"files_changed,omitempty"`
}

var forbiddenFieldSubstrings = []string{
	"secret", "token", "password", "credential", "apikey", "api_key", "auth",
}

// ValidateReportsSecretFieldNames guards the exported shape against secret-like fields.
func ValidateReportsSecretFieldNames() error {
	for _, t := range []reflect.Type{reflect.TypeOf(Event{}), reflect.TypeOf(EvidenceRef{})} {
		for i := 0; i < t.NumField(); i++ {
			name := strings.ToLower(t.Field(i).Name)
			for _, bad := range forbiddenFieldSubstrings {
				if strings.Contains(name, bad) {
					return fmt.Errorf("change: type %s field %s looks like a secret holder", t.Name(), t.Field(i).Name)
				}
			}
		}
	}
	return nil
}

// Validate checks change-event invariants.
func (e Event) Validate() error {
	if e.SchemaVersion != SchemaVersion {
		return fmt.Errorf("change: schema_version must be %q", SchemaVersion)
	}
	if strings.TrimSpace(e.ChangeID) == "" {
		return fmt.Errorf("change: change_id is required")
	}
	if !e.ChangeType.Valid() {
		return fmt.Errorf("change: invalid change_type %q", e.ChangeType)
	}
	if strings.TrimSpace(e.Source) == "" {
		return fmt.Errorf("change: source is required")
	}
	if e.CreatedAt.IsZero() {
		return fmt.Errorf("change: created_at is required")
	}
	if e.ObservedAt.IsZero() {
		return fmt.Errorf("change: observed_at is required")
	}
	if e.CreatedAt.Location() != time.UTC || e.ObservedAt.Location() != time.UTC {
		return fmt.Errorf("change: timestamps must be UTC")
	}
	if e.DeployedAt != nil {
		if e.DeployedAt.Location() != time.UTC {
			return fmt.Errorf("change: deployed_at must be UTC")
		}
	}
	if e.ServiceMapping != "" {
		switch e.ServiceMapping {
		case MappingExplicit, MappingConfigured, MappingUnknown, MappingPartial:
		default:
			return fmt.Errorf("change: invalid service_mapping %q", e.ServiceMapping)
		}
	}
	if len(e.ServiceIDs) == 0 && e.ServiceMapping == "" {
		// Allow empty services only when uncertainty is recorded.
		return fmt.Errorf("change: service_ids empty requires service_mapping=%q", MappingUnknown)
	}
	if len(e.ServiceIDs) == 0 && e.ServiceMapping != MappingUnknown {
		return fmt.Errorf("change: empty service_ids requires service_mapping=%q", MappingUnknown)
	}
	for _, id := range e.ServiceIDs {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("change: service_ids must not contain empty entries")
		}
	}
	lower := strings.ToLower(e.Summary + " " + e.Title)
	if claimsCausation(lower) {
		return fmt.Errorf("change: title/summary must not claim causation")
	}
	return nil
}

func claimsCausation(lower string) bool {
	if strings.Contains(lower, "caused by") ||
		strings.Contains(lower, "deployment caused") ||
		strings.Contains(lower, "change caused") ||
		strings.Contains(lower, "root cause is") {
		return true
	}
	if strings.Contains(lower, "proven root cause") &&
		!strings.Contains(lower, "not proven") &&
		!strings.Contains(lower, "unproven") {
		return true
	}
	if strings.Contains(lower, "root cause") &&
		!strings.Contains(lower, "not") &&
		!strings.Contains(lower, "unproven") {
		return true
	}
	return false
}

// EffectiveTime is the best timestamp for temporal correlation:
// DeployedAt if set, else ObservedAt, else CreatedAt.
func (e Event) EffectiveTime() time.Time {
	if e.DeployedAt != nil && !e.DeployedAt.IsZero() {
		return e.DeployedAt.UTC()
	}
	if !e.ObservedAt.IsZero() {
		return e.ObservedAt.UTC()
	}
	return e.CreatedAt.UTC()
}

// ChangeIDFor builds a deterministic ID from type, commit/source key, and time.
func ChangeIDFor(t ChangeType, key string, at time.Time) string {
	key = strings.TrimSpace(key)
	if key == "" {
		key = "unknown"
	}
	return fmt.Sprintf("%s|%s|%s", t, key, at.UTC().Format("20060102T150405.000000000Z"))
}

// Compare orders events for deterministic listing: EffectiveTime, then ChangeID.
func Compare(a, b Event) int {
	ta, tb := a.EffectiveTime(), b.EffectiveTime()
	if !ta.Equal(tb) {
		if ta.Before(tb) {
			return -1
		}
		return 1
	}
	return strings.Compare(a.ChangeID, b.ChangeID)
}
