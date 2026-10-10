package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/pulse/change"
	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/incident"
)

func TestPulseChangeRecordAndIncidentChanges(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PERCH_PULSE_DIR", dir)

	root := NewRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{
		"pulse", "change-record",
		"--type", "deployment",
		"--service", "astronomy/local/payment",
		"--commit", "abc123",
		"--env", "local",
		"--simulated",
		"--deployed-at", time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("change-record: %v\n%s", err, buf.String())
	}
	var ev change.Event
	if err := json.Unmarshal(buf.Bytes(), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Metadata["simulated"] != "true" {
		t.Fatalf("expected simulated marker: %+v", ev)
	}

	at := time.Date(2026, 10, 9, 12, 0, 40, 0, time.UTC)
	f := detect.Finding{
		SchemaVersion: detect.SchemaVersion, FindingID: detect.FindingIDFor("astronomy/local/payment", detect.SignalErrorRate, at),
		DetectorVersion: detect.DetectorVersion, ServiceID: "astronomy/local/payment", SignalType: detect.SignalErrorRate,
		ObservedWindow: detect.Window{Start: at.Add(-40 * time.Second), End: at},
		BaselineWindow: detect.Window{Start: at.Add(-3 * time.Minute), End: at.Add(-40 * time.Second)},
		ObservedValue:  0.5, BaselineValue: 0.01, Threshold: 0.05, Score: 2,
		Severity: detect.SeverityCritical, FirstDetectedAt: at, LastObservedAt: at.Add(10 * time.Second),
		Summary: "observed elevated error rate (correlation only, not causation)",
	}
	inc, err := incident.Build(incident.BuildInput{Findings: []detect.Finding{f}, Environment: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if err := incident.NewFileStore(dir).Append(inc); err != nil {
		t.Fatal(err)
	}

	root2 := NewRootCmd()
	buf2 := &bytes.Buffer{}
	root2.SetOut(buf2)
	root2.SetErr(buf2)
	root2.SetArgs([]string{"pulse", "incident", inc.IncidentID, "--changes", "--for-agent"})
	if err := root2.Execute(); err != nil {
		t.Fatalf("incident --changes: %v\n%s", err, buf2.String())
	}
	out := buf2.String()
	if !bytes.Contains(buf2.Bytes(), []byte("not proven causation")) && !bytes.Contains(buf2.Bytes(), []byte("not proven")) {
		t.Fatalf("agent text missing caveat: %s", out)
	}
	if filepath.Clean(dir) == "" {
		t.Fatal("dir")
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "changes"))
	if len(entries) == 0 {
		t.Fatal("expected change files")
	}
}
