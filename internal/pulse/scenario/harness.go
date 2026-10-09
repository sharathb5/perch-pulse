package scenario

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrAlreadyActive is returned when Start is called while a run is active or
// awaiting recovery.
var ErrAlreadyActive = errors.New("scenario: a scenario run is already active")

// ErrNotActive is returned when Stop is called with no active run.
var ErrNotActive = errors.New("scenario: no active scenario run")

// ErrDuplicateStop is returned when Stop targets a run that is already stopped.
var ErrDuplicateStop = errors.New("scenario: scenario run already stopped")

// Clock supplies wall time (injectable for tests).
type Clock func() time.Time

// Harness starts/stops catalog scenarios and persists ground-truth records.
//
// Records are written only under ResultsDir. They are never appended to the
// Pulse observation store.
type Harness struct {
	Catalog    Catalog
	Flagd      *FlagdClient
	ResultsDir string
	Now        Clock
}

// NewHarness constructs a harness. resultsDir must be non-empty.
func NewHarness(catalog Catalog, flagd *FlagdClient, resultsDir string, now Clock) (*Harness, error) {
	if resultsDir == "" {
		return nil, fmt.Errorf("scenario: resultsDir is required")
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if flagd == nil {
		base := catalog.FlagdAPIBase
		if base == "" {
			base = DefaultFlagdAPIBase
		}
		flagd = NewFlagdClient(base)
	}
	if err := os.MkdirAll(resultsDir, 0o750); err != nil {
		return nil, fmt.Errorf("scenario: create results dir: %w", err)
	}
	return &Harness{
		Catalog:    catalog,
		Flagd:      flagd,
		ResultsDir: resultsDir,
		Now:        now,
	}, nil
}

// List returns catalog definitions.
func (h *Harness) List() []Definition {
	out := make([]Definition, len(h.Catalog.Scenarios))
	copy(out, h.Catalog.Scenarios)
	return out
}

// ActivePath is the lock file for the currently active run.
func (h *Harness) ActivePath() string {
	return filepath.Join(h.ResultsDir, "active.json")
}

// LoadActive returns the active record, or nil if none.
func (h *Harness) LoadActive() (*Record, error) {
	path := h.ActivePath()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var r Record
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("scenario: parse active.json: %w", err)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return &r, nil
}

// Start activates a scenario. Fails if another run is already active.
//
// The active.json lock is claimed with O_EXCL before flag mutation so concurrent
// starts cannot both inject faults.
func (h *Harness) Start(ctx context.Context, scenarioID string) (*Record, error) {
	active, err := h.LoadActive()
	if err != nil {
		return nil, err
	}
	if active != nil {
		return nil, fmt.Errorf("%w (%s state=%s)", ErrAlreadyActive, active.ScenarioID, active.State)
	}

	def, err := h.Catalog.Get(scenarioID)
	if err != nil {
		return nil, err
	}
	start := h.Now().UTC()
	rec, err := NewRecord(def, h.Catalog.Environment, start)
	if err != nil {
		return nil, err
	}
	rec.State = StateActive
	rec.EvidenceRefs = append(rec.EvidenceRefs, h.Flagd.BaseURL+"/read")
	if err := rec.Validate(); err != nil {
		return nil, err
	}

	// Claim lock before mutating remote flags.
	if err := h.claimActiveExclusive(&rec); err != nil {
		return nil, err
	}

	if err := h.Flagd.SetVariant(ctx, def.Mechanism.Flag, def.Mechanism.ActiveVariant); err != nil {
		rec.State = StateFailed
		rec.OperatorNotes = joinNotes(rec.OperatorNotes, "start failed: "+err.Error())
		_ = h.saveActive(&rec)
		_ = h.saveRun(rec)
		return nil, err
	}

	if _, err := h.Flagd.WaitForVariant(ctx, def.Mechanism.Flag, def.Mechanism.ActiveVariant); err != nil {
		rec.State = StateFailed
		rec.OperatorNotes = joinNotes(rec.OperatorNotes, "activation verify failed: "+err.Error())
		_ = h.Flagd.SetVariant(ctx, def.Mechanism.Flag, def.Mechanism.IdleVariant)
		_, _ = h.Flagd.WaitForVariant(ctx, def.Mechanism.Flag, def.Mechanism.IdleVariant)
		_ = h.saveActive(&rec)
		_ = h.saveRun(rec)
		return nil, err
	}

	rec.ActivationVerified = true
	if err := h.saveActive(&rec); err != nil {
		return nil, err
	}
	if err := h.saveRun(rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// Stop reverts the active scenario (or scenarioID if it matches active).
//
// Runs in StateFailed remain stoppable so recovery can be retried until the
// idle variant is confirmed. active.json is removed only after verified recovery.
func (h *Harness) Stop(ctx context.Context, scenarioID string) (*Record, error) {
	active, err := h.LoadActive()
	if err != nil {
		return nil, err
	}
	if active == nil {
		return nil, ErrNotActive
	}
	if scenarioID != "" && scenarioID != active.ScenarioID {
		return nil, fmt.Errorf("scenario: active run is %q, not %q", active.ScenarioID, scenarioID)
	}
	if active.State == StateStopped {
		return nil, ErrDuplicateStop
	}
	if active.State != StateActive && active.State != StateFailed {
		return nil, fmt.Errorf("scenario: cannot stop run in state %q", active.State)
	}

	def, err := h.Catalog.Get(active.ScenarioID)
	if err != nil {
		return nil, err
	}

	if err := h.Flagd.SetVariant(ctx, def.Mechanism.Flag, def.Mechanism.IdleVariant); err != nil {
		active.State = StateFailed
		active.EndTime = nil
		active.OperatorNotes = joinNotes(active.OperatorNotes, "stop failed: "+err.Error())
		_ = h.saveRun(*active)
		_ = h.saveActive(active)
		return nil, err
	}

	got, err := h.Flagd.WaitForVariant(ctx, def.Mechanism.Flag, def.Mechanism.IdleVariant)
	recovered := err == nil && got == def.Mechanism.IdleVariant
	active.Recovered = &recovered
	active.RecoveryVerified = recovered
	active.EvidenceRefs = appendUnique(active.EvidenceRefs, h.Flagd.BaseURL+"/read")

	if !recovered {
		active.State = StateFailed
		active.EndTime = nil
		if err != nil {
			active.OperatorNotes = joinNotes(active.OperatorNotes, "recovery verify failed: "+err.Error())
		} else {
			active.OperatorNotes = joinNotes(active.OperatorNotes,
				fmt.Sprintf("recovery mismatch: want idle %q got %q", def.Mechanism.IdleVariant, got))
		}
		_ = h.saveRun(*active)
		_ = h.saveActive(active)
		return nil, fmt.Errorf("scenario: recovery not confirmed for %s (retry stop)", active.ScenarioID)
	}

	end := h.Now().UTC()
	active.EndTime = &end
	active.State = StateStopped
	if err := active.Validate(); err != nil {
		return nil, err
	}
	if err := h.saveRun(*active); err != nil {
		return nil, err
	}
	if err := os.Remove(h.ActivePath()); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return active, nil
}

// claimActiveExclusive creates active.json with O_EXCL so only one starter wins.
func (h *Harness) claimActiveExclusive(r *Record) error {
	path := h.ActivePath()
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%w", ErrAlreadyActive)
		}
		return err
	}
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func (h *Harness) saveActive(r *Record) error {
	return writeJSONAtomic(h.ActivePath(), r)
}

func (h *Harness) saveRun(r Record) error {
	path := filepath.Join(h.ResultsDir, r.RunID+".json")
	return writeJSONAtomic(path, r)
}

func writeJSONAtomic(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func joinNotes(a, b string) string {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "; " + b
}

func appendUnique(in []string, v string) []string {
	for _, x := range in {
		if x == v {
			return in
		}
	}
	return append(in, v)
}
