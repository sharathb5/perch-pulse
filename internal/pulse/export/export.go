package export

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/yashg4509/perch/internal/pulse/incident"
	"github.com/yashg4509/perch/internal/pulse/investigation"
	"github.com/yashg4509/perch/internal/pulse/investigation/eval"
	"github.com/yashg4509/perch/internal/pulse/investigation/llm"
)

// Options configures an incremental analytical export.
type Options struct {
	PulseDir           string
	OutDir             string
	AllowedRoot        string // empty => cwd
	MaxBatchRecords    int
	MaxOutputBytes     int64
	EvalReportPath     string          // optional pulse.investigation.eval_report.v1 JSON
	IncidentProvenance ProvenanceClass // default observed_local
	Now                func() time.Time
}

// Result summarizes one export run.
type Result struct {
	ExportDir         string         `json:"export_dir"`
	ExportSchema      string         `json:"export_schema_version"`
	ExportedAt        time.Time      `json:"exported_at"`
	RowsAdded         map[string]int `json:"rows_added"`
	RowsSkipped       map[string]int `json:"rows_skipped_idempotent"`
	CorruptSkipped    []string       `json:"corrupt_skipped,omitempty"`
	BytesWritten      int64          `json:"bytes_written_approx"`
	CheckpointPath    string         `json:"checkpoint_path"`
	ManifestPath      string         `json:"manifest_path"`
	QualityReportPath string         `json:"quality_report_path,omitempty"`
	BatchCapped       bool           `json:"batch_capped"`
}

// Manifest is written beside JSONL files after a successful commit.
type Manifest struct {
	SchemaVersion       string       `json:"schema_version"`
	ExportSchemaVersion string       `json:"export_schema_version"`
	ExportedAt          time.Time    `json:"exported_at"`
	PulseDirFingerprint string       `json:"pulse_dir_note"`
	Datasets            []ManifestDS `json:"datasets"`
	Limitations         []string     `json:"limitations"`
}

// ManifestDS counts rows per dataset after commit.
type ManifestDS struct {
	Name     string `json:"name"`
	RowCount int    `json:"row_count"`
	File     string `json:"file"`
}

// Run executes an incremental export. Source pulse records are never modified.
func Run(opts Options) (Result, error) {
	if strings.TrimSpace(opts.PulseDir) == "" {
		return Result{}, fmt.Errorf("export: pulse-dir is required")
	}
	if opts.MaxBatchRecords <= 0 {
		opts.MaxBatchRecords = DefaultMaxBatchRecords
	}
	if opts.MaxOutputBytes <= 0 {
		opts.MaxOutputBytes = DefaultMaxOutputBytes
	}
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	exportedAt := nowFn().UTC()
	prov := opts.IncidentProvenance
	if prov == "" {
		prov = ProvenanceObservedLocal
	}
	if !validProvenance(prov) {
		return Result{}, fmt.Errorf("export: invalid incident provenance %q", prov)
	}

	outDir, err := ResolveExportDir(opts.OutDir, opts.AllowedRoot)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return Result{}, fmt.Errorf("export: mkdir: %w", err)
	}
	removeStaleCheckpointTmp(outDir)

	cp, err := LoadCheckpoint(outDir)
	if err != nil {
		return Result{}, err
	}
	// Heal checkpoint from on-disk JSONL so a crash between rename and checkpoint
	// save does not re-append identical rows on retry.
	cp, err = reconcileCheckpointWithJSONL(outDir, cp)
	if err != nil {
		return Result{}, err
	}

	incStore := incident.NewFileStore(opts.PulseDir)
	invStore := investigation.NewFileStore(opts.PulseDir)
	propStore := llm.NewFileStore(opts.PulseDir)

	incidents, err := incStore.List()
	if err != nil {
		return Result{}, fmt.Errorf("export: list incidents: %w", err)
	}
	invs, err := invStore.ListAll()
	if err != nil {
		return Result{}, fmt.Errorf("export: list investigations: %w", err)
	}
	invFB, err := invStore.ListAllFeedback()
	if err != nil {
		return Result{}, fmt.Errorf("export: list investigation feedback: %w", err)
	}
	props, err := propStore.ListAll()
	if err != nil {
		return Result{}, fmt.Errorf("export: list proposals: %w", err)
	}
	// Provenance must reflect the verified source. Refusing synthetic labels on a
	// non-empty pulse-dir prevents demo/export paths from mislabeling real
	// ~/.perch (or any persisted) incident/investigation records.
	if prov == ProvenanceSyntheticFixture &&
		(len(incidents) > 0 || len(invs) > 0 || len(invFB) > 0 || len(props) > 0) {
		return Result{}, fmt.Errorf(
			"export: refusing incident-provenance=%s with non-empty pulse-dir %q (%d incidents, %d investigations, %d feedback, %d proposals); use observed_local or historical_demo, or export from an empty/fixture-only --pulse-dir",
			prov, opts.PulseDir, len(incidents), len(invs), len(invFB), len(props),
		)
	}
	propFB, err := propStore.ListAllFeedback()
	if err != nil {
		return Result{}, fmt.Errorf("export: list proposal feedback: %w", err)
	}

	var corrupt []string
	incRows, sigRows, c1, err := incidentRows(incidents, prov, exportedAt)
	if err != nil {
		return Result{}, err
	}
	corrupt = append(corrupt, c1...)
	invRows, hypInv, citeRows, c2, err := investigationRows(invs, prov, exportedAt)
	if err != nil {
		return Result{}, err
	}
	corrupt = append(corrupt, c2...)
	propRows, hypProp, c3, err := proposalRows(props, exportedAt)
	if err != nil {
		return Result{}, err
	}
	corrupt = append(corrupt, c3...)
	fbInv, c4 := feedbackFromInvestigation(invFB, exportedAt)
	corrupt = append(corrupt, c4...)
	fbProp, c5 := feedbackFromProposal(propFB, exportedAt)
	corrupt = append(corrupt, c5...)

	hypRows := append(hypInv, hypProp...)
	sort.SliceStable(hypRows, func(i, j int) bool { return hypRows[i].DedupeKey < hypRows[j].DedupeKey })
	fbRows := append(fbInv, fbProp...)
	sort.SliceStable(fbRows, func(i, j int) bool { return fbRows[i].DedupeKey < fbRows[j].DedupeKey })

	var evalRun *EvalRunRow
	var evalCases []EvalCaseResultRow
	if strings.TrimSpace(opts.EvalReportPath) != "" {
		rep, err := loadEvalReport(opts.EvalReportPath)
		if err != nil {
			return Result{}, err
		}
		run, cases, err := evalRows(rep, exportedAt)
		if err != nil {
			return Result{}, err
		}
		evalRun = &run
		evalCases = cases
	}

	type pending struct {
		dataset string
		rows    []any
		keys    map[string]string
		skipped int
	}
	build := func(dataset string, getKeyFP func(i int) (string, string), materialize func(i int) any, n int) pending {
		p := pending{dataset: dataset, keys: map[string]string{}}
		for i := 0; i < n; i++ {
			k, fp := getKeyFP(i)
			if !cp.ShouldExport(dataset, k, fp) {
				p.skipped++
				continue
			}
			p.rows = append(p.rows, materialize(i))
			p.keys[k] = fp
		}
		return p
	}

	pendings := []pending{
		build(DatasetIncidents,
			func(i int) (string, string) { return incRows[i].DedupeKey, incRows[i].ContentFingerprint },
			func(i int) any { return incRows[i] }, len(incRows)),
		build(DatasetIncidentSignals,
			func(i int) (string, string) { return sigRows[i].DedupeKey, sigRows[i].ContentFingerprint },
			func(i int) any { return sigRows[i] }, len(sigRows)),
		build(DatasetInvestigations,
			func(i int) (string, string) { return invRows[i].DedupeKey, invRows[i].ContentFingerprint },
			func(i int) any { return invRows[i] }, len(invRows)),
		build(DatasetHypotheses,
			func(i int) (string, string) { return hypRows[i].DedupeKey, hypRows[i].ContentFingerprint },
			func(i int) any { return hypRows[i] }, len(hypRows)),
		build(DatasetCitations,
			func(i int) (string, string) { return citeRows[i].DedupeKey, citeRows[i].ContentFingerprint },
			func(i int) any { return citeRows[i] }, len(citeRows)),
		build(DatasetInvestigationFeedback,
			func(i int) (string, string) { return fbRows[i].DedupeKey, fbRows[i].ContentFingerprint },
			func(i int) any { return fbRows[i] }, len(fbRows)),
		build(DatasetProposals,
			func(i int) (string, string) { return propRows[i].DedupeKey, propRows[i].ContentFingerprint },
			func(i int) any { return propRows[i] }, len(propRows)),
	}
	if evalRun != nil {
		pendings = append(pendings, build(DatasetEvalRuns,
			func(i int) (string, string) { return evalRun.DedupeKey, evalRun.ContentFingerprint },
			func(i int) any { return *evalRun }, 1))
		pendings = append(pendings, build(DatasetEvalCaseResults,
			func(i int) (string, string) { return evalCases[i].DedupeKey, evalCases[i].ContentFingerprint },
			func(i int) any { return evalCases[i] }, len(evalCases)))
	}

	// Apply batch + byte caps across datasets in stable dataset order.
	batchLeft := opts.MaxBatchRecords
	var bytesApprox int64
	batchCapped := false
	res := Result{
		ExportDir:      outDir,
		ExportSchema:   ExportSchemaVersion,
		ExportedAt:     exportedAt,
		RowsAdded:      map[string]int{},
		RowsSkipped:    map[string]int{},
		CorruptSkipped: corrupt,
	}

	newCP := cp
	for _, p := range pendings {
		res.RowsSkipped[p.dataset] = p.skipped
		if len(p.rows) == 0 {
			continue
		}
		rows := p.rows
		keys := p.keys
		if len(rows) > batchLeft {
			batchCapped = true
			rows = rows[:batchLeft]
			keys = map[string]string{}
			for _, r := range rows {
				env, err := rowEnvelope(r)
				if err != nil {
					return Result{}, err
				}
				keys[env.DedupeKey] = env.ContentFingerprint
			}
		}
		if batchLeft <= 0 {
			batchCapped = true
			break
		}
		// Rough size check before write.
		approx := int64(0)
		for _, r := range rows {
			raw, _ := json.Marshal(r)
			approx += int64(len(raw) + 1)
		}
		if bytesApprox+approx > opts.MaxOutputBytes {
			batchCapped = true
			// shrink until under budget
			for len(rows) > 0 && bytesApprox+approx > opts.MaxOutputBytes {
				rows = rows[:len(rows)-1]
				approx = 0
				for _, r := range rows {
					raw, _ := json.Marshal(r)
					approx += int64(len(raw) + 1)
				}
			}
			keys = map[string]string{}
			for _, r := range rows {
				env, err := rowEnvelope(r)
				if err != nil {
					return Result{}, err
				}
				keys[env.DedupeKey] = env.ContentFingerprint
			}
			if len(rows) == 0 {
				break
			}
		}
		n, written, err := appendJSONLAtomic(outDir, p.dataset, rows)
		if err != nil {
			// Failed commit: do not advance checkpoint for this dataset.
			return Result{}, err
		}
		bytesApprox += written
		batchLeft -= n
		res.RowsAdded[p.dataset] = n
		newCP = newCP.WithCommitted(p.dataset, keys, exportedAt)
		// Persist checkpoint after each dataset so interrupt mid-run loses at most
		// the current dataset's progress (JSONL reconcile still covers rename/cp gap).
		if err := SaveCheckpointAtomic(outDir, newCP); err != nil {
			return Result{}, err
		}
	}

	manifest, err := writeManifest(outDir, exportedAt, newCP)
	if err != nil {
		return Result{}, err
	}
	qr, err := ValidateExport(outDir)
	if err != nil {
		return Result{}, fmt.Errorf("export: post-commit validation: %w", err)
	}
	qrPath, err := writeQualityReport(outDir, qr)
	if err != nil {
		return Result{}, err
	}
	if !qr.OK {
		return Result{}, fmt.Errorf("export: quality report failed (%d issues); see %s", len(qr.Issues), qrPath)
	}

	cpPath, _ := CheckpointPath(outDir)
	res.BytesWritten = bytesApprox
	res.CheckpointPath = cpPath
	res.ManifestPath = manifest
	res.QualityReportPath = qrPath
	res.BatchCapped = batchCapped
	return res, nil
}

func reconcileCheckpointWithJSONL(exportDir string, cp Checkpoint) (Checkpoint, error) {
	now := time.Now().UTC()
	out := cp
	if out.Datasets == nil {
		out.Datasets = map[string]DatasetCheckpoint{}
	}
	for _, ds := range SupportedDatasets {
		idx, err := loadJSONLKeyIndex(exportDir, ds)
		if err != nil {
			return Checkpoint{}, err
		}
		if len(idx) == 0 {
			continue
		}
		out = out.WithCommitted(ds, idx, now)
	}
	return out, nil
}

func rowEnvelope(r any) (Envelope, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return Envelope{}, err
	}
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return Envelope{}, err
	}
	if env.DedupeKey == "" || env.ContentFingerprint == "" {
		return Envelope{}, fmt.Errorf("export: row missing dedupe_key/fingerprint")
	}
	return env, nil
}

func validProvenance(p ProvenanceClass) bool {
	switch p {
	case ProvenanceSyntheticFixture, ProvenanceHistoricalDemo, ProvenanceObservedLocal,
		ProvenanceHumanAssessment, ProvenanceEvalAnnotation, ProvenanceModelGenerated, ProvenanceUnknown:
		return true
	default:
		return false
	}
}

func loadEvalReport(path string) (eval.Report, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return eval.Report{}, fmt.Errorf("export: read eval report: %w", err)
	}
	var rep eval.Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		return eval.Report{}, fmt.Errorf("export: parse eval report: %w", err)
	}
	return rep, nil
}

func writeManifest(exportDir string, exportedAt time.Time, cp Checkpoint) (string, error) {
	path, err := SafeJoin(exportDir, "export-manifest.json")
	if err != nil {
		return "", err
	}
	tmp, err := SafeJoin(exportDir, "export-manifest.json.tmp")
	if err != nil {
		return "", err
	}
	m := Manifest{
		SchemaVersion:       ManifestSchemaVersion,
		ExportSchemaVersion: ExportSchemaVersion,
		ExportedAt:          exportedAt.UTC(),
		PulseDirFingerprint: "local FileStore read-only; sources not hashed for privacy",
		Limitations: []string{
			"F3a produces local JSONL only; no Databricks/Delta load is performed.",
			"Hypothesis statement text is hashed, not exported.",
			"Provider prompts/responses and raw logs are never exported.",
			"Synthetic eval rows must not be joined as live incidents without provenance filters.",
		},
	}
	for _, name := range SupportedDatasets {
		ds := cp.Datasets[name]
		file, _ := DatasetJSONLName(name)
		m.Datasets = append(m.Datasets, ManifestDS{Name: name, RowCount: ds.RowCount, File: file})
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return path, nil
}

func writeQualityReport(exportDir string, qr QualityReport) (string, error) {
	path, err := SafeJoin(exportDir, "quality-report.json")
	if err != nil {
		return "", err
	}
	tmp, err := SafeJoin(exportDir, "quality-report.json.tmp")
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(qr, "", "  ")
	if err != nil {
		return "", err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return path, nil
}
