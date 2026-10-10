package cli

import (
	"github.com/spf13/cobra"
)

// NewRootCmd builds the perch Cobra tree (persistent flags: --env, --json, --no-color).
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "perch",
		Short: "Explore and debug multi-service deployment stacks",
		Long:  "perch — stack TUI (run with no args), JSON subcommands, and Homebrew-friendly binary. See https://github.com/yashg4509/perch",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			return runRootTUI(cmd)
		},
	}

	pf := root.PersistentFlags()
	pf.String("env", "production", "Environment name in perch.yaml")
	pf.Bool("json", false, "Emit JSON to stdout")
	pf.Bool("no-color", false, "Disable ANSI colors (human output)")

	root.AddCommand(newInitCmd())
	root.AddCommand(newAuthCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newContextCmd())
	root.AddCommand(newPulseCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newGraphCmd())
	root.AddCommand(newEdgeCmd())
	root.AddCommand(newVizCmd())
	root.AddCommand(newLogsCmd())
	return root
}
