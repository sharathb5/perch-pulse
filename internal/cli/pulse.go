package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yashg4509/perch/internal/pulse/change"
	"github.com/yashg4509/perch/internal/pulse/correlate"
	"github.com/yashg4509/perch/internal/pulse/incident"
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
			return enc.Encode(ev)
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
	if !withChanges && !forAgent {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(inc)
	}
	changes, err := change.NewFileStore(dir).List()
	if err != nil {
		return err
	}
	rep, err := correlate.Correlate(inc, changes, correlate.DefaultConfig())
	if err != nil {
		return err
	}
	var top *change.Event
	if rep.Top1 != nil {
		if ev, ok, _ := change.NewFileStore(dir).Get(rep.Top1.ChangeID); ok {
			top = &ev
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
	rep, err := correlate.Correlate(inc, changes, correlate.DefaultConfig())
	if err != nil {
		return err
	}
	var top *change.Event
	if rep.Top1 != nil {
		if ev, ok, _ := change.NewFileStore(dir).Get(rep.Top1.ChangeID); ok {
			top = &ev
		}
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), correlate.FormatAgentContext(inc, rep, top))
	return err
}
