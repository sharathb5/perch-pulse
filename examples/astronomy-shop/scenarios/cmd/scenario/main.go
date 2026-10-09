// Command scenario drives Astronomy Shop ground-truth fault-injection runs.
//
// Usage:
//
//	go run ./examples/astronomy-shop/scenarios/cmd/scenario list
//	go run ./examples/astronomy-shop/scenarios/cmd/scenario start error-payment
//	go run ./examples/astronomy-shop/scenarios/cmd/scenario stop
//	go run ./examples/astronomy-shop/scenarios/cmd/scenario status
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yashg4509/perch/internal/pulse/scenario"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: scenario <list|start|stop|status> [scenario-id]")
	}
	cmd := args[0]

	catalog, err := scenario.LoadCatalog()
	if err != nil {
		return err
	}
	resultsDir := os.Getenv("PERCH_SCENARIO_RESULTS_DIR")
	if resultsDir == "" {
		root, err := repoRoot()
		if err != nil {
			return err
		}
		resultsDir = filepath.Join(root, "examples", "astronomy-shop", "scenarios", "results")
	}
	base := os.Getenv("PERCH_FLAGD_API_BASE")
	if base == "" {
		base = catalog.FlagdAPIBase
	}
	h, err := scenario.NewHarness(catalog, scenario.NewFlagdClient(base), resultsDir, nil)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch cmd {
	case "list":
		for _, s := range h.List() {
			fmt.Printf("%-28s  %-18s  %-8s  target=%s  flag=%s→%s\n",
				s.ID, s.Type, s.Classification, s.TargetOTEL, s.Mechanism.Flag, s.Mechanism.ActiveVariant)
		}
		return nil
	case "start":
		if len(args) < 2 {
			return fmt.Errorf("usage: scenario start <scenario-id>")
		}
		rec, err := h.Start(ctx, args[1])
		if err != nil {
			return err
		}
		return printJSON(rec)
	case "stop":
		id := ""
		if len(args) >= 2 {
			id = args[1]
		}
		rec, err := h.Stop(ctx, id)
		if err != nil {
			return err
		}
		return printJSON(rec)
	case "status":
		rec, err := h.LoadActive()
		if err != nil {
			return err
		}
		if rec == nil {
			fmt.Println(`{"active":false}`)
			return nil
		}
		return printJSON(map[string]any{"active": true, "run": rec})
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find go.mod from %s", wd)
		}
		dir = parent
	}
}
