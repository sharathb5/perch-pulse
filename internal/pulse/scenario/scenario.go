// Package scenario defines the Phase 2 ground-truth fault-injection contract.
//
// Ground-truth labels are intentionally separate from Pulse observations and
// the observation store. Detector code must not import this package as an
// implicit source of the correct answer; evaluation harnesses load records
// explicitly.
//
// This package does not implement anomaly detection, AI investigation, or
// Databricks backends.
package scenario

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	_ "embed"

	"github.com/yashg4509/perch/internal/pulse/astronomy"
	"gopkg.in/yaml.v3"
)

//go:embed catalog.yaml
var catalogYAML []byte

const (
	// SchemaVersion is the persisted ground-truth record schema.
	SchemaVersion = "pulse.scenario.v1"

	// CatalogSchemaVersion is the embedded catalog document schema.
	CatalogSchemaVersion = "pulse.scenario.catalog.v1"

	// DefaultFlagdAPIBase is the Astronomy Shop flagd-ui API via Envoy.
	DefaultFlagdAPIBase = "http://127.0.0.1:8080/feature/api"
)

// Type classifies the injected failure mode (or neutral change).
type Type string

const (
	TypeLatency          Type = "latency"
	TypeErrorRate        Type = "error_rate"
	TypeDependencyOutage Type = "dependency_outage"
	TypeNeutral          Type = "neutral"
)

// Classification separates faults from negative controls.
type Classification string

const (
	ClassificationFault   Classification = "fault"
	ClassificationControl Classification = "control"
)

// State is the harness lifecycle for a single run.
type State string

const (
	StateIdle    State = "idle"
	StateActive  State = "active"
	StateStopped State = "stopped"
	StateFailed  State = "failed"
)

// MechanismKind identifies how a scenario is applied.
type MechanismKind string

const (
	MechanismFlagd MechanismKind = "flagd"
)

var (
	scenarioIDRe = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
	secretKeyRe  = regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key|private[_-]?key|credential|authorization|bearer)`)
)

// Mechanism describes the reversible injection method.
type Mechanism struct {
	Kind          MechanismKind `json:"kind" yaml:"kind"`
	Flag          string        `json:"flag,omitempty" yaml:"flag,omitempty"`
	ActiveVariant string        `json:"active_variant,omitempty" yaml:"active_variant,omitempty"`
	IdleVariant   string        `json:"idle_variant,omitempty" yaml:"idle_variant,omitempty"`
}

// Definition is one catalog scenario (template, not a run).
type Definition struct {
	ID                         string         `json:"id" yaml:"id"`
	Type                       Type           `json:"type" yaml:"type"`
	Classification             Classification `json:"classification" yaml:"classification"`
	TargetOTEL                 string         `json:"target_otel" yaml:"target_otel"`
	Description                string         `json:"description" yaml:"description"`
	Mechanism                  Mechanism      `json:"mechanism" yaml:"mechanism"`
	ExpectedAffectedOTEL       []string       `json:"expected_affected_otel" yaml:"expected_affected_otel"`
	ExpectedDependencyPathOTEL []string       `json:"expected_dependency_path_otel" yaml:"expected_dependency_path_otel"`
	OperatorNotes              string         `json:"operator_notes,omitempty" yaml:"operator_notes,omitempty"`
}

// Catalog is the embedded scenario catalog.
type Catalog struct {
	SchemaVersion string       `json:"schema_version" yaml:"schema_version"`
	DemoPin       string       `json:"demo_pin" yaml:"demo_pin"`
	Environment   string       `json:"environment" yaml:"environment"`
	FlagdAPIBase  string       `json:"flagd_api_base" yaml:"flagd_api_base"`
	Scenarios     []Definition `json:"scenarios" yaml:"scenarios"`
}

// Record is a versioned ground-truth run (labels for later evaluation).
//
// It must never be written into observation.Observation or store.Store.
type Record struct {
	SchemaVersion              string            `json:"schema_version"`
	ScenarioID                 string            `json:"scenario_id"`
	RunID                      string            `json:"run_id"`
	ScenarioType               Type              `json:"scenario_type"`
	Classification             Classification    `json:"classification"`
	Environment                string            `json:"environment"`
	TargetServiceID            string            `json:"target_service_id"`
	StartTime                  time.Time         `json:"start_time"`
	EndTime                    *time.Time        `json:"end_time,omitempty"`
	InjectedParameters         map[string]string `json:"injected_parameters"`
	ExpectedAffectedServiceIDs []string          `json:"expected_affected_service_ids"`
	ExpectedDependencyPathIDs  []string          `json:"expected_dependency_path_service_ids"`
	State                      State             `json:"state"`
	Recovered                  *bool             `json:"recovered,omitempty"`
	OperatorNotes              string            `json:"operator_notes,omitempty"`
	EvidenceRefs               []string          `json:"evidence_refs,omitempty"`
	Mechanism                  Mechanism         `json:"mechanism"`
	ActivationVerified         bool              `json:"activation_verified"`
	RecoveryVerified           bool              `json:"recovery_verified"`
}

// LoadCatalog parses and validates the embedded catalog.
func LoadCatalog() (Catalog, error) {
	var c Catalog
	if err := yaml.Unmarshal(catalogYAML, &c); err != nil {
		return Catalog{}, fmt.Errorf("scenario: parse catalog: %w", err)
	}
	if err := c.Validate(); err != nil {
		return Catalog{}, err
	}
	return c, nil
}

// Get returns a definition by id.
func (c Catalog) Get(id string) (Definition, error) {
	id = strings.TrimSpace(id)
	for _, s := range c.Scenarios {
		if s.ID == id {
			return s, nil
		}
	}
	return Definition{}, fmt.Errorf("scenario: unknown scenario id %q", id)
}

// Validate checks catalog invariants.
func (c Catalog) Validate() error {
	if c.SchemaVersion != CatalogSchemaVersion {
		return fmt.Errorf("scenario: catalog schema_version must be %q", CatalogSchemaVersion)
	}
	if c.DemoPin == "" {
		return fmt.Errorf("scenario: demo_pin is required")
	}
	if c.Environment == "" {
		return fmt.Errorf("scenario: environment is required")
	}
	if len(c.Scenarios) == 0 {
		return fmt.Errorf("scenario: scenarios must not be empty")
	}

	seen := map[string]struct{}{}
	hasControl := false
	typesSeen := map[Type]bool{}

	for i, s := range c.Scenarios {
		if err := s.Validate(c.Environment); err != nil {
			return fmt.Errorf("scenario: scenarios[%d] (%s): %w", i, s.ID, err)
		}
		if _, dup := seen[s.ID]; dup {
			return fmt.Errorf("scenario: duplicate scenario id %q", s.ID)
		}
		seen[s.ID] = struct{}{}
		typesSeen[s.Type] = true
		if s.Classification == ClassificationControl {
			hasControl = true
		}
	}
	if !hasControl {
		return fmt.Errorf("scenario: catalog must include at least one control (negative) scenario")
	}
	for _, need := range []Type{TypeLatency, TypeErrorRate, TypeDependencyOutage, TypeNeutral} {
		if !typesSeen[need] {
			return fmt.Errorf("scenario: catalog missing required type %q", need)
		}
	}
	return nil
}

// Validate checks a single definition.
func (d Definition) Validate(environment string) error {
	if !scenarioIDRe.MatchString(d.ID) {
		return fmt.Errorf("invalid id %q", d.ID)
	}
	switch d.Type {
	case TypeLatency, TypeErrorRate, TypeDependencyOutage, TypeNeutral:
	default:
		return fmt.Errorf("invalid type %q", d.Type)
	}
	switch d.Classification {
	case ClassificationFault, ClassificationControl:
	default:
		return fmt.Errorf("invalid classification %q", d.Classification)
	}
	if d.Type == TypeNeutral && d.Classification != ClassificationControl {
		return fmt.Errorf("neutral type must be classification=control")
	}
	if d.Classification == ClassificationControl && d.Type != TypeNeutral {
		return fmt.Errorf("control classification requires type=neutral")
	}
	if d.Classification == ClassificationFault && d.Type == TypeNeutral {
		return fmt.Errorf("fault classification cannot use type=neutral")
	}
	if strings.TrimSpace(d.TargetOTEL) == "" {
		return fmt.Errorf("target_otel is required")
	}
	if _, err := astronomy.ServiceID(environment, d.TargetOTEL); err != nil {
		return fmt.Errorf("target_otel: %w", err)
	}
	if err := d.Mechanism.Validate(); err != nil {
		return err
	}
	for _, name := range d.ExpectedAffectedOTEL {
		if _, err := astronomy.ServiceID(environment, name); err != nil {
			return fmt.Errorf("expected_affected_otel %q: %w", name, err)
		}
	}
	for _, name := range d.ExpectedDependencyPathOTEL {
		if _, err := astronomy.ServiceID(environment, name); err != nil {
			return fmt.Errorf("expected_dependency_path_otel %q: %w", name, err)
		}
	}
	if d.Classification == ClassificationControl && len(d.ExpectedAffectedOTEL) != 0 {
		return fmt.Errorf("control scenarios must not list expected_affected_otel (no fault blast radius)")
	}
	return nil
}

// Validate checks mechanism fields.
func (m Mechanism) Validate() error {
	if m.Kind != MechanismFlagd {
		return fmt.Errorf("unsupported mechanism kind %q", m.Kind)
	}
	if strings.TrimSpace(m.Flag) == "" {
		return fmt.Errorf("flag is required")
	}
	if strings.TrimSpace(m.ActiveVariant) == "" {
		return fmt.Errorf("active_variant is required")
	}
	if strings.TrimSpace(m.IdleVariant) == "" {
		return fmt.Errorf("idle_variant is required")
	}
	if m.ActiveVariant == m.IdleVariant {
		return fmt.Errorf("active_variant and idle_variant must differ")
	}
	return nil
}

// Validate checks a persisted ground-truth record.
func (r Record) Validate() error {
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("scenario: schema_version must be %q", r.SchemaVersion)
	}
	if !scenarioIDRe.MatchString(r.ScenarioID) {
		return fmt.Errorf("scenario: invalid scenario_id %q", r.ScenarioID)
	}
	if strings.TrimSpace(r.RunID) == "" {
		return fmt.Errorf("scenario: run_id is required")
	}
	switch r.ScenarioType {
	case TypeLatency, TypeErrorRate, TypeDependencyOutage, TypeNeutral:
	default:
		return fmt.Errorf("scenario: invalid scenario_type %q", r.ScenarioType)
	}
	switch r.Classification {
	case ClassificationFault, ClassificationControl:
	default:
		return fmt.Errorf("scenario: invalid classification %q", r.Classification)
	}
	if r.Environment == "" {
		return fmt.Errorf("scenario: environment is required")
	}
	if err := astronomy.ValidateServiceIDShape(r.TargetServiceID); err != nil {
		return fmt.Errorf("scenario: target_service_id: %w", err)
	}
	if r.StartTime.IsZero() {
		return fmt.Errorf("scenario: start_time is required")
	}
	if r.StartTime.Location() != time.UTC {
		return fmt.Errorf("scenario: start_time must be UTC")
	}
	switch r.State {
	case StateIdle, StateActive, StateStopped, StateFailed:
	default:
		return fmt.Errorf("scenario: invalid state %q", r.State)
	}
	if r.State == StateActive && r.EndTime != nil {
		return fmt.Errorf("scenario: active run must not have end_time")
	}
	if r.State == StateStopped {
		if r.EndTime == nil || r.EndTime.IsZero() {
			return fmt.Errorf("scenario: stopped run requires end_time")
		}
		if r.EndTime.Location() != time.UTC {
			return fmt.Errorf("scenario: end_time must be UTC")
		}
		if r.EndTime.Before(r.StartTime) {
			return fmt.Errorf("scenario: end_time before start_time")
		}
	}
	if err := r.Mechanism.Validate(); err != nil {
		return fmt.Errorf("scenario: mechanism: %w", err)
	}
	if err := rejectSecrets(r.InjectedParameters); err != nil {
		return err
	}
	for _, id := range r.ExpectedAffectedServiceIDs {
		if err := astronomy.ValidateServiceIDShape(id); err != nil {
			return fmt.Errorf("scenario: expected_affected_service_ids: %w", err)
		}
	}
	for _, id := range r.ExpectedDependencyPathIDs {
		if err := astronomy.ValidateServiceIDShape(id); err != nil {
			return fmt.Errorf("scenario: expected_dependency_path_service_ids: %w", err)
		}
	}
	if r.Classification == ClassificationControl && len(r.ExpectedAffectedServiceIDs) != 0 {
		return fmt.Errorf("scenario: control record must not list expected affected services")
	}
	return nil
}

// RunIDFor builds a deterministic run id from scenario id and start time.
func RunIDFor(scenarioID string, start time.Time) string {
	return scenarioID + "-" + start.UTC().Format("20060102T150405.000000000Z")
}

// NewRecord builds a ground-truth record from a catalog definition at start.
func NewRecord(def Definition, environment string, start time.Time) (Record, error) {
	if err := def.Validate(environment); err != nil {
		return Record{}, err
	}
	start = start.UTC()
	target, err := astronomy.ServiceID(environment, def.TargetOTEL)
	if err != nil {
		return Record{}, err
	}
	affected, err := mapOTELNames(environment, def.ExpectedAffectedOTEL)
	if err != nil {
		return Record{}, err
	}
	path, err := mapOTELNames(environment, def.ExpectedDependencyPathOTEL)
	if err != nil {
		return Record{}, err
	}
	params := map[string]string{
		"flag":           def.Mechanism.Flag,
		"active_variant": def.Mechanism.ActiveVariant,
		"idle_variant":   def.Mechanism.IdleVariant,
		"mechanism_kind": string(def.Mechanism.Kind),
	}
	if err := rejectSecrets(params); err != nil {
		return Record{}, err
	}
	r := Record{
		SchemaVersion:              SchemaVersion,
		ScenarioID:                 def.ID,
		RunID:                      RunIDFor(def.ID, start),
		ScenarioType:               def.Type,
		Classification:             def.Classification,
		Environment:                environment,
		TargetServiceID:            target,
		StartTime:                  start,
		InjectedParameters:         params,
		ExpectedAffectedServiceIDs: affected,
		ExpectedDependencyPathIDs:  path,
		State:                      StateIdle,
		OperatorNotes:              strings.TrimSpace(def.OperatorNotes),
		Mechanism:                  def.Mechanism,
	}
	if err := r.Validate(); err != nil {
		return Record{}, err
	}
	return r, nil
}

func mapOTELNames(environment string, names []string) ([]string, error) {
	out := make([]string, 0, len(names))
	for _, n := range names {
		id, err := astronomy.ServiceID(environment, n)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func rejectSecrets(params map[string]string) error {
	for k, v := range params {
		if secretKeyRe.MatchString(k) {
			return fmt.Errorf("scenario: injected parameter key %q looks like a secret field", k)
		}
		if secretKeyRe.MatchString(v) {
			return fmt.Errorf("scenario: injected parameter %q value looks like a secret", k)
		}
		// Soft length guard — flag variants are short.
		if len(v) > 128 {
			return fmt.Errorf("scenario: injected parameter %q value too long", k)
		}
	}
	return nil
}
