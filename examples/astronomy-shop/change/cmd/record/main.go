// Command record writes a simulated Pulse deployment change event (no real deploy).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/yashg4509/perch/internal/pulse/change"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: record <service-id> [commit]\n")
		os.Exit(2)
	}
	service := os.Args[1]
	commit := fmt.Sprintf("demo-%d", time.Now().UTC().Unix())
	if len(os.Args) >= 3 {
		commit = os.Args[2]
	}
	dir := os.Getenv("PERCH_PULSE_DIR")
	if dir == "" {
		fmt.Fprintln(os.Stderr, "PERCH_PULSE_DIR is required")
		os.Exit(2)
	}
	env := os.Getenv("PERCH_CHANGE_ENV")
	if env == "" {
		env = "local"
	}
	ev, err := change.Record(change.RecordInput{
		Type:        change.ChangeDeployment,
		ServiceIDs:  []string{service},
		Environment: env,
		CommitSHA:   commit,
		Simulated:   true,
		Source:      "deploy-marker",
		Title:       "Simulated Astronomy Shop deploy marker",
		Summary:     "Simulated deployment marker for correlation demo (not a real deploy)",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
	if err := change.NewFileStore(dir).Append(ev); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(ev)
}
