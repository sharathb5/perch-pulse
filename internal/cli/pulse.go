package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	pulseapi "github.com/yashg4509/perch/internal/pulse/api"
	"github.com/yashg4509/perch/internal/pulse/change"
	"github.com/yashg4509/perch/internal/pulse/correlate"
	"github.com/yashg4509/perch/internal/pulse/export"
	"github.com/yashg4509/perch/internal/pulse/incident"
	"github.com/yashg4509/perch/internal/pulse/investigation"
	"github.com/yashg4509/perch/internal/pulse/investigation/eval"
	pulsellm "github.com/yashg4509/perch/internal/pulse/investigation/llm"
)

func newPulseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pulse",
		Short: "Pulse local change/incident correlation (optional; no Databricks)",
	}
	cmd.PersistentFlags().String("pulse-dir", "", "Pulse data directory (default: .perch/pulse or $PERCH_PULSE_DIR)")
	cmd.AddCommand(newPulseChangeRecordCmd())
	cmd.AddCommand(newPulseChangesCmd())
	cmd.AddCommand(newPulseChangeGetCmd())
	cmd.AddCommand(newPulseIncidentsCmd())
	cmd.AddCommand(newPulseIncidentGetCmd())
	cmd.AddCommand(newPulseContextCmd())
	cmd.AddCommand(newPulseInvestigateCmd())
	cmd.AddCommand(newPulseInvestigationContextCmd())
	cmd.AddCommand(newPulseLLMStatusCmd())
	cmd.AddCommand(newPulseLLMProposeCmd())
	cmd.AddCommand(newPulseInvestigationEvalCmd())
	cmd.AddCommand(newPulseExportAnalyticsCmd())
	return cmd
}

func pulseDataDir(cmd *cobra.Command) (string, error) {
	if v := strings.TrimSpace(os.Getenv("PERCH_PULSE_DIR")); v != "" {
		return v, nil
	}
	flag := ""
	if cmd != nil {
		f, err := cmd.Flags().GetString("pulse-dir")
		if err == nil {
			flag = strings.TrimSpace(f)
		}
		if flag == "" && cmd.Parent() != nil {
			f, err = cmd.Parent().PersistentFlags().GetString("pulse-dir")
			if err == nil {
				flag = strings.TrimSpace(f)
			}
		}
	}
	if flag != "" {
		return flag, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(wd, ".perch", "pulse"), nil
}

func newPulseChangeRecordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "change-record",
		Short: "Record a local change/deployment event for correlation",
		RunE:  runPulseChangeRecord,
	}
	cmd.Flags().String("type", "deployment", "change type: commit|pull_request_merge|deployment")
	cmd.Flags().StringSlice("service", nil, "affected Pulse service ID (repeatable)")
	cmd.Flags().String("commit", "", "commit SHA")
	cmd.Flags().String("repo", "", "repository name or URL (non-secret)")
	cmd.Flags().String("branch", "", "branch name")
	cmd.Flags().String("env", "local", "environment (e.g. local)")
	cmd.Flags().String("title", "", "short title")
	cmd.Flags().String("summary", "", "short summary (no causation claims)")
	cmd.Flags().Bool("simulated", false, "mark as simulated deploy marker (demo)")
	cmd.Flags().String("deployed-at", "", "RFC3339 deploy time (default: now)")
	return cmd
}

func runPulseChangeRecord(cmd *cobra.Command, args []string) error {
	_ = args
	typStr, _ := cmd.Flags().GetString("type")
	services, _ := cmd.Flags().GetStringSlice("service")
	commit, _ := cmd.Flags().GetString("commit")
	repo, _ := cmd.Flags().GetString("repo")
	branch, _ := cmd.Flags().GetString("branch")
	env, _ := cmd.Flags().GetString("env")
	title, _ := cmd.Flags().GetString("title")
	summary, _ := cmd.Flags().GetString("summary")
	simulated, _ := cmd.Flags().GetBool("simulated")
	deployedAtStr, _ := cmd.Flags().GetString("deployed-at")

	var typ change.ChangeType
	switch typStr {
	case "commit":
		typ = change.ChangeCommit
	case "pull_request_merge", "pr", "pull_request":
		typ = change.ChangePRMerge
	case "deployment", "deploy":
		typ = change.ChangeDeployment
	default:
		return fmt.Errorf("pulse: unknown change type %q", typStr)
	}
	if len(services) == 0 {
		return fmt.Errorf("pulse: at least one --service is required")
	}

	var deployedAt *time.Time
	if deployedAtStr != "" {
		t, err := time.Parse(time.RFC3339, deployedAtStr)
		if err != nil {
			return fmt.Errorf("pulse: --deployed-at: %w", err)
		}
		t = t.UTC()
		deployedAt = &t
	}

	ev, err := change.Record(change.RecordInput{
		Type:        typ,
		ServiceIDs:  services,
		Environment: env,
		Repository:  repo,
		CommitSHA:   commit,
		Branch:      branch,
		Title:       title,
		Summary:     summary,
		DeployedAt:  deployedAt,
		Simulated:   simulated,
		Source:      "cli",
	})
	if err != nil {
		return err
	}
	dir, err := pulseDataDir(cmd)
	if err != nil {
		return err
	}
	store := change.NewFileStore(dir)
	if err := store.Append(ev); err != nil {
		return err
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(ev)
}

func newPulseChangesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "changes",
		Short: "List recorded change events",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			dir, err := pulseDataDir(cmd)
			if err != nil {
				return err
			}
			list, err := change.NewFileStore(dir).List()
			if err != nil {
				return err
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(list)
		},
	}
}

func newPulseChangeGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "change",
		Short: "Show one change event by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := pulseDataDir(cmd)
			if err != nil {
				return err
			}
			ev, ok, err := change.NewFileStore(dir).Get(args[0])
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("pulse: change %q not found", args[0])
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(pulseapi.RedactChange(ev))
		},
	}
}

func newPulseIncidentsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "incidents",
		Short: "List recorded incidents",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			dir, err := pulseDataDir(cmd)
			if err != nil {
				return err
			}
			list, err := incident.NewFileStore(dir).List()
			if err != nil {
				return err
			}
			for i := range list {
				list[i] = pulseapi.RedactIncident(list[i])
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(list)
		},
	}
}

func newPulseIncidentGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "incident",
		Short: "Show one incident; optional --changes for correlation",
		Args:  cobra.ExactArgs(1),
		RunE:  runPulseIncidentGet,
	}
	cmd.Flags().Bool("changes", false, "Include ranked change correlation candidates")
	cmd.Flags().Bool("for-agent", false, "Compact agent-oriented text")
	return cmd
}

func runPulseIncidentGet(cmd *cobra.Command, args []string) error {
	dir, err := pulseDataDir(cmd)
	if err != nil {
		return err
	}
	withChanges, _ := cmd.Flags().GetBool("changes")
	forAgent, _ := cmd.Flags().GetBool("for-agent")
	inc, ok, err := incident.NewFileStore(dir).Get(args[0])
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("pulse: incident %q not found", args[0])
	}
	inc = pulseapi.RedactIncident(inc)
	if !withChanges && !forAgent {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(inc)
	}
	changes, err := change.NewFileStore(dir).List()
	if err != nil {
		return err
	}
	for i := range changes {
		changes[i] = pulseapi.RedactChange(changes[i])
	}
	rep, err := correlate.Correlate(inc, changes, correlate.DefaultConfig())
	if err != nil {
		return err
	}
	var top *change.Event
	if rep.Top1 != nil {
		if ev, ok, _ := change.NewFileStore(dir).Get(rep.Top1.ChangeID); ok {
			redacted := pulseapi.RedactChange(ev)
			top = &redacted
		}
	}
	if forAgent {
		_, err := fmt.Fprint(cmd.OutOrStdout(), correlate.FormatAgentContext(inc, rep, top))
		return err
	}
	out := map[string]any{
		"incident":    inc,
		"correlation": rep,
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func newPulseContextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Emit Pulse incident/change context for agents",
		RunE:  runPulseContext,
	}
	cmd.Flags().Bool("for-agent", true, "Emit plain text for agent injection")
	cmd.Flags().String("incident", "", "Incident ID (default: latest)")
	return cmd
}

func runPulseContext(cmd *cobra.Command, args []string) error {
	_ = args
	dir, err := pulseDataDir(cmd)
	if err != nil {
		return err
	}
	id, _ := cmd.Flags().GetString("incident")
	store := incident.NewFileStore(dir)
	var inc incident.Incident
	if id != "" {
		var ok bool
		inc, ok, err = store.Get(id)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("pulse: incident %q not found", id)
		}
	} else {
		list, err := store.List()
		if err != nil {
			return err
		}
		if len(list) == 0 {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Pulse: no incidents recorded.")
			return err
		}
		inc = list[len(list)-1]
	}
	changes, err := change.NewFileStore(dir).List()
	if err != nil {
		return err
	}
	inc = pulseapi.RedactIncident(inc)
	for i := range changes {
		changes[i] = pulseapi.RedactChange(changes[i])
	}
	rep, err := correlate.Correlate(inc, changes, correlate.DefaultConfig())
	if err != nil {
		return err
	}
	var top *change.Event
	if rep.Top1 != nil {
		if ev, ok, _ := change.NewFileStore(dir).Get(rep.Top1.ChangeID); ok {
			redacted := pulseapi.RedactChange(ev)
			top = &redacted
		}
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), correlate.FormatAgentContext(inc, rep, top))
	return err
}

func newPulseInvestigateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "investigate",
		Short: "Assemble a versioned evidence-backed investigation for an incident (no LLM)",
		Args:  cobra.ExactArgs(1),
		RunE:  runPulseInvestigate,
	}
	cmd.Flags().Bool("reuse-identical", true, "Return existing snapshot when content fingerprint matches")
	cmd.Flags().Bool("for-agent", false, "Emit agent-context JSON instead of full investigation")
	return cmd
}

func runPulseInvestigate(cmd *cobra.Command, args []string) error {
	dir, err := pulseDataDir(cmd)
	if err != nil {
		return err
	}
	reuse, _ := cmd.Flags().GetBool("reuse-identical")
	forAgent, _ := cmd.Flags().GetBool("for-agent")
	svc := &investigation.Service{
		Incidents: incident.NewFileStore(dir),
		Changes:   change.NewFileStore(dir),
		Store:     investigation.NewFileStore(dir),
	}
	inv, err := svc.Create(investigation.CreateOptions{
		IncidentID:     args[0],
		ReuseIdentical: reuse,
	})
	if err != nil {
		return err
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	if forAgent {
		return enc.Encode(investigation.BuildAgentContext(inv))
	}
	return enc.Encode(inv)
}

func newPulseInvestigationContextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "investigation-context",
		Short: "Emit FACTS/HYPOTHESES/UNKNOWN/NEXT STEPS agent context for an incident",
		Args:  cobra.ExactArgs(1),
		RunE:  runPulseInvestigationContext,
	}
	cmd.Flags().Bool("json", false, "Emit versioned agent-context JSON instead of plain text")
	return cmd
}

func newPulseLLMStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "llm-status",
		Short: "Show whether optional LLM investigation is configured (no secrets)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := pulsellm.ConfigFromEnv()
			if err != nil {
				return err
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(cfg.Status())
		},
	}
}

func newPulseLLMProposeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "llm-propose",
		Short: "Generate a constrained LLM investigation proposal for an F1 investigation id (opt-in)",
		Args:  cobra.ExactArgs(1),
		RunE:  runPulseLLMPropose,
	}
	cmd.Flags().String("expected-fingerprint", "", "Reject if F1 snapshot fingerprint differs")
	return cmd
}

func newPulseInvestigationEvalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "investigation-eval",
		Short: "Offline evaluation of F1 investigations vs stored F2a proposals (no provider calls)",
		Args:  cobra.NoArgs,
		RunE:  runPulseInvestigationEval,
	}
	cmd.Flags().String("out-dir", "", "Write report.json and report.md under this directory")
	cmd.Flags().Bool("json", false, "Print report JSON to stdout (default: markdown summary)")
	return cmd
}

func newPulseExportAnalyticsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export-analytics",
		Short: "Incrementally export investigation analytics JSONL (local; no Databricks)",
		Args:  cobra.NoArgs,
		RunE:  runPulseExportAnalytics,
	}
	cmd.Flags().String("out-dir", "", "Export directory for versioned JSONL + checkpoint (required)")
	cmd.Flags().String("allowed-root", "", "Reject out-dir outside this root (default: cwd)")
	cmd.Flags().Int("max-batch", export.DefaultMaxBatchRecords, "Max new rows per run across datasets")
	cmd.Flags().String("eval-report", "", "Optional investigation-eval report.json to include")
	cmd.Flags().String("incident-provenance", string(export.ProvenanceObservedLocal),
		"Provenance class for pulse-dir incidents/investigations (observed_local|historical_demo|synthetic_fixture|unknown)")
	cmd.Flags().Bool("inventory", false, "Print data inventory markdown and exit")
	cmd.Flags().Bool("json", false, "Print export result JSON")
	return cmd
}

func runPulseExportAnalytics(cmd *cobra.Command, _ []string) error {
	if inv, _ := cmd.Flags().GetBool("inventory"); inv {
		_, err := fmt.Fprint(cmd.OutOrStdout(), export.InventoryMarkdown())
		return err
	}
	outDir, _ := cmd.Flags().GetString("out-dir")
	if strings.TrimSpace(outDir) == "" {
		return fmt.Errorf("export-analytics: --out-dir is required")
	}
	dir, err := pulseDataDir(cmd)
	if err != nil {
		return err
	}
	allowed, _ := cmd.Flags().GetString("allowed-root")
	maxBatch, _ := cmd.Flags().GetInt("max-batch")
	evalReport, _ := cmd.Flags().GetString("eval-report")
	prov, _ := cmd.Flags().GetString("incident-provenance")
	res, err := export.Run(export.Options{
		PulseDir:           dir,
		OutDir:             outDir,
		AllowedRoot:        allowed,
		MaxBatchRecords:    maxBatch,
		EvalReportPath:     evalReport,
		IncidentProvenance: export.ProvenanceClass(prov),
	})
	if err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(),
		"export_schema=%s\nout=%s\nbytes≈%d\nbatch_capped=%v\ncheckpoint=%s\nquality=%s\n",
		res.ExportSchema, res.ExportDir, res.BytesWritten, res.BatchCapped, res.CheckpointPath, res.QualityReportPath)
	if err != nil {
		return err
	}
	for _, ds := range export.SupportedDatasets {
		added := res.RowsAdded[ds]
		skipped := res.RowsSkipped[ds]
		if added == 0 && skipped == 0 {
			continue
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s: added=%d skipped_idempotent=%d\n", ds, added, skipped)
	}
	if len(res.CorruptSkipped) > 0 {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "corrupt_skipped=%d\n", len(res.CorruptSkipped))
	}
	return nil
}

func runPulseInvestigationEval(cmd *cobra.Command, _ []string) error {
	ds, err := eval.BuiltinDataset()
	if err != nil {
		return err
	}
	rep, err := eval.Run(eval.RunOptions{Dataset: ds})
	if err != nil {
		return err
	}
	outDir, _ := cmd.Flags().GetString("out-dir")
	if outDir != "" {
		if err := os.MkdirAll(outDir, 0o750); err != nil {
			return err
		}
		raw, err := rep.JSON()
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(outDir, "report.json"), raw, 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(outDir, "report.md"), []byte(rep.Markdown()), 0o600); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s/report.json and report.md\n", outDir)
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), rep.Markdown())
	return err
}

func runPulseLLMPropose(cmd *cobra.Command, args []string) error {
	dir, err := pulseDataDir(cmd)
	if err != nil {
		return err
	}
	cfg, err := pulsellm.ConfigFromEnv()
	if err != nil {
		return err
	}
	if !cfg.Available() {
		return fmt.Errorf("%s", cfg.UnavailabilityReason())
	}
	fp, _ := cmd.Flags().GetString("expected-fingerprint")
	invStore := investigation.NewFileStore(dir)
	svc := &pulsellm.Service{
		Config:         cfg,
		Investigations: pulsellm.InvestigationAdapter{Store: invStore},
		Store:          pulsellm.NewFileStore(dir),
		Client:         pulsellm.NewHTTPClientFromConfig(cfg),
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	prop, err := svc.Propose(ctx, pulsellm.ProposeOptions{
		InvestigationID:     args[0],
		ExpectedFingerprint: strings.TrimSpace(fp),
	})
	if err != nil {
		return err
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(prop)
}

func runPulseInvestigationContext(cmd *cobra.Command, args []string) error {
	dir, err := pulseDataDir(cmd)
	if err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	svc := &investigation.Service{
		Incidents: incident.NewFileStore(dir),
		Changes:   change.NewFileStore(dir),
		Store:     investigation.NewFileStore(dir),
	}
	// CLI may assemble (explicit local action); HTTP GET context remains read-only.
	inv, err := svc.Create(investigation.CreateOptions{IncidentID: args[0], ReuseIdentical: true})
	if err != nil {
		return err
	}
	ctx := investigation.BuildAgentContext(inv)
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(ctx)
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), investigation.FormatAgentContextText(inv))
	return err
}
