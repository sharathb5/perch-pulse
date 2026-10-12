package export

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Checkpoint tracks exported dedupe keys and content fingerprints per dataset.
// It advances only after a successful atomic commit of JSONL + manifest updates.
type Checkpoint struct {
	SchemaVersion       string                       `json:"schema_version"`
	ExportSchemaVersion string                       `json:"export_schema_version"`
	UpdatedAt           time.Time                    `json:"updated_at"`
	Datasets            map[string]DatasetCheckpoint `json:"datasets"`
}

// DatasetCheckpoint stores idempotency state for one JSONL dataset.
type DatasetCheckpoint struct {
	// Keys maps dedupe_key -> content_fingerprint for already-committed rows.
	Keys     map[string]string `json:"keys"`
	LastKey  string            `json:"last_key,omitempty"`
	RowCount int               `json:"row_count"`
}

const checkpointFileName = "checkpoint.json"
const checkpointTmpName = "checkpoint.json.tmp"

// LoadCheckpoint reads checkpoint.json or returns an empty valid checkpoint.
func LoadCheckpoint(exportDir string) (Checkpoint, error) {
	path, err := SafeJoin(exportDir, checkpointFileName)
	if err != nil {
		return Checkpoint{}, err
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return emptyCheckpoint(), nil
	}
	if err != nil {
		return Checkpoint{}, fmt.Errorf("export: read checkpoint: %w", err)
	}
	var cp Checkpoint
	if err := json.Unmarshal(raw, &cp); err != nil {
		return Checkpoint{}, fmt.Errorf("export: corrupt checkpoint: %w", err)
	}
	if cp.SchemaVersion != CheckpointSchemaVersion {
		return Checkpoint{}, fmt.Errorf("export: checkpoint schema_version %q unsupported (want %q)", cp.SchemaVersion, CheckpointSchemaVersion)
	}
	if cp.ExportSchemaVersion != "" && cp.ExportSchemaVersion != ExportSchemaVersion {
		return Checkpoint{}, fmt.Errorf("export: checkpoint export_schema_version %q != %q", cp.ExportSchemaVersion, ExportSchemaVersion)
	}
	if cp.Datasets == nil {
		cp.Datasets = map[string]DatasetCheckpoint{}
	}
	for name, ds := range cp.Datasets {
		if ds.Keys == nil {
			ds.Keys = map[string]string{}
			cp.Datasets[name] = ds
		}
	}
	return cp, nil
}

func emptyCheckpoint() Checkpoint {
	return Checkpoint{
		SchemaVersion:       CheckpointSchemaVersion,
		ExportSchemaVersion: ExportSchemaVersion,
		UpdatedAt:           time.Time{},
		Datasets:            map[string]DatasetCheckpoint{},
	}
}

// ShouldExport reports whether a row should be written (new or content changed).
func (cp Checkpoint) ShouldExport(dataset, dedupeKey, fingerprint string) bool {
	ds, ok := cp.Datasets[dataset]
	if !ok || ds.Keys == nil {
		return true
	}
	prev, ok := ds.Keys[dedupeKey]
	if !ok {
		return true
	}
	return prev != fingerprint
}

// WithCommitted returns a copy of cp with keys from committed marked as exported.
func (cp Checkpoint) WithCommitted(dataset string, committed map[string]string, now time.Time) Checkpoint {
	out := Checkpoint{
		SchemaVersion:       CheckpointSchemaVersion,
		ExportSchemaVersion: ExportSchemaVersion,
		UpdatedAt:           now.UTC(),
		Datasets:            map[string]DatasetCheckpoint{},
	}
	for k, v := range cp.Datasets {
		keys := map[string]string{}
		for kk, vv := range v.Keys {
			keys[kk] = vv
		}
		out.Datasets[k] = DatasetCheckpoint{
			Keys:     keys,
			LastKey:  v.LastKey,
			RowCount: v.RowCount,
		}
	}
	ds := out.Datasets[dataset]
	if ds.Keys == nil {
		ds.Keys = map[string]string{}
	}
	last := ds.LastKey
	for k, fp := range committed {
		if _, existed := ds.Keys[k]; !existed {
			ds.RowCount++
		}
		ds.Keys[k] = fp
		if k > last {
			last = k
		}
	}
	ds.LastKey = last
	out.Datasets[dataset] = ds
	return out
}

// SaveCheckpointAtomic writes checkpoint.json via temp + rename.
func SaveCheckpointAtomic(exportDir string, cp Checkpoint) error {
	path, err := SafeJoin(exportDir, checkpointFileName)
	if err != nil {
		return err
	}
	tmp, err := SafeJoin(exportDir, checkpointTmpName)
	if err != nil {
		return err
	}
	cp.SchemaVersion = CheckpointSchemaVersion
	cp.ExportSchemaVersion = ExportSchemaVersion
	raw, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("export: write checkpoint tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("export: commit checkpoint: %w", err)
	}
	return nil
}

// CheckpointPath returns the checkpoint file path under exportDir.
func CheckpointPath(exportDir string) (string, error) {
	return SafeJoin(exportDir, checkpointFileName)
}

// removeStaleCheckpointTmp clears an interrupted checkpoint write without advancing state.
func removeStaleCheckpointTmp(exportDir string) {
	tmp, err := SafeJoin(exportDir, checkpointTmpName)
	if err != nil {
		return
	}
	_ = os.Remove(tmp)
}
