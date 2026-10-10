package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yashg4509/perch/internal/config"
	"github.com/yashg4509/perch/internal/graph"
	"github.com/yashg4509/perch/internal/pulse/change"
	"github.com/yashg4509/perch/internal/pulse/correlate"
	"github.com/yashg4509/perch/internal/pulse/incident"
	"github.com/yashg4509/perch/internal/stackcontext"
	"github.com/yashg4509/perch/internal/stackstatus"
)

func newContextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Print merged stack topology + status for agents or JSON consumers",
		RunE:  runContext,
	}
	cmd.Flags().Bool("for-agent", false, "Emit plain text optimized for LLM context injection")
	return cmd
}

func runContext(cmd *cobra.Command, args []string) error {
	_ = args
	env, err := cmd.Flags().GetString("env")
	if err != nil {
		return err
	}
	jsonOut, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	forAgent, err := cmd.Flags().GetBool("for-agent")
	if err != nil {
		return err
	}
	noColor, err := cmd.Flags().GetBool("no-color")
	if err != nil {
		return err
	}
	if forAgent && jsonOut {
		return fmt.Errorf("context: use only one of --json or --for-agent")
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	perchPath, err := config.FindPerchYAML(wd)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(perchPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	cfg, err := config.Load(raw)
	if err != nil {
		return err
	}
	root := filepath.Dir(perchPath)
	reg, err := loadRegistryForProject(root)
	if err != nil {
		return err
	}

	ctx := context.Background()
	g, err := graph.Build(cfg, reg, env)
	if err != nil {
		return err
	}
	rep, err := stackstatus.Collect(ctx, cfg, env, reg, loadCollectOptions(perchPath))
	if err != nil {
		return err
	}

	at := time.Now()
	r := stackcontext.Build(at, g, rep)

	out := cmd.OutOrStdout()
	if forAgent {
		_ = noColor
		if err := writeContextForAgent(out, r); err != nil {
			return err
		}
		return appendPulseAgentContext(out)
	}
	_ = noColor
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func writeContextForAgent(w io.Writer, r *stackcontext.Report) error {
	var b strings.Builder
	_, _ = b.WriteString("Stack: ")
	b.WriteString(r.Stack)
	b.WriteString("\nEnvironment: ")
	b.WriteString(r.Environment)
	b.WriteString("\nGenerated: ")
	b.WriteString(r.GeneratedAt)
	b.WriteByte('\n')
	if r.StatusReport != nil {
		_, _ = b.WriteString("\n")
		_, _ = b.WriteString(stackstatus.FormatHuman(r.Stack, r.Environment, r.StatusReport))
	}
	_, err := w.Write([]byte(b.String()))
	return err
}

// appendPulseAgentContext adds a compact Pulse correlation brief when local
// Pulse data exists. Absence of Pulse data is not an error.
func appendPulseAgentContext(w io.Writer) error {
	dir, err := pulseDataDir(nil)
	if err != nil {
		return nil
	}
	incs, err := incident.NewFileStore(dir).List()
	if err != nil || len(incs) == 0 {
		return nil
	}
	inc := incs[len(incs)-1]
	changes, err := change.NewFileStore(dir).List()
	if err != nil {
		return nil
	}
	rep, err := correlate.Correlate(inc, changes, correlate.DefaultConfig())
	if err != nil {
		return nil
	}
	var top *change.Event
	if rep.Top1 != nil {
		if ev, ok, _ := change.NewFileStore(dir).Get(rep.Top1.ChangeID); ok {
			top = &ev
		}
	}
	_, err = fmt.Fprint(w, "\n"+correlate.FormatAgentContext(inc, rep, top))
	return err
}
