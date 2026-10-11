package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/cli"
	"github.com/yashg4509/perch/internal/provider"
	"github.com/yashg4509/perch/internal/pulse/detect"
	"github.com/yashg4509/perch/internal/pulse/incident"
	"github.com/yashg4509/perch/internal/stackstatus"
	"github.com/yashg4509/perch/internal/testutil"
)

// UNIT: original Perch CLI surface remains registered and Pulse/Databricks-optional.

func TestPerchRegression_CoreCommandsRegistered(t *testing.T) {
	root := cli.NewRootCmd()
	want := []string{"status", "graph", "context", "viz", "init", "edge", "logs", "auth", "config", "pulse"}
	have := map[string]bool{}
	for _, c := range root.Commands() {
		have[c.Name()] = true
	}
	for _, name := range want {
		if !have[name] {
			t.Fatalf("missing command %q", name)
		}
	}
	for _, name := range []string{"env", "json", "no-color"} {
		if root.PersistentFlags().Lookup(name) == nil {
			t.Fatalf("missing persistent flag --%s", name)
		}
	}
}

func TestPerchRegression_UnknownCommandFailsCleanly(t *testing.T) {
	var buf bytes.Buffer
	cmd := cli.NewRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"definitely-not-a-perch-command"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
	out := buf.String() + err.Error()
	if !strings.Contains(strings.ToLower(out), "unknown") && !strings.Contains(out, "definitely-not-a-perch-command") {
		t.Fatalf("unclear unknown-command error: %v / %q", err, buf.String())
	}
}

func TestPerchRegression_StatusGraphWithoutPulseDir(t *testing.T) {
	// UNIT: core commands work with no Pulse storage and no Databricks.
	tmp := t.TempDir()
	repo := testutil.RepoRoot(t)
	if err := os.Symlink(filepath.Join(repo, "providers"), filepath.Join(tmp, "providers")); err != nil {
		t.Skip("symlink providers:", err)
	}
	yaml := `name: pulse-off
environments:
  production:
    api: { provider: openai }
    web: { provider: vercel, project: demo }
edges:
  - web -> api
`
	if err := os.WriteFile(filepath.Join(tmp, "perch.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)
	t.Setenv("PERCH_PULSE_DIR", filepath.Join(tmp, "missing-pulse-dir"))
	t.Setenv("DATABRICKS_HOST", "")
	t.Setenv("DATABRICKS_TOKEN", "")
	prevTok := stackstatus.SetDeployableTokenFnForTest(func(*provider.Spec) (string, bool) {
		return "", false
	})
	t.Cleanup(func() { stackstatus.SetDeployableTokenFnForTest(prevTok) })
	t.Chdir(tmp)

	for _, args := range [][]string{
		{"status", "--json", "--env", "production"},
		{"graph", "--json", "--env", "production"},
		{"context", "--json", "--env", "production"},
	} {
		var buf bytes.Buffer
		cmd := cli.NewRootCmd()
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, buf.String())
		}
		if !strings.Contains(buf.String(), "{") {
			t.Fatalf("%v: expected JSON, got %q", args, buf.String())
		}
	}
}

func TestPerchRegression_AgentContextSecretRedaction(t *testing.T) {
	// UNIT: injected secret-shaped text must not appear in --for-agent output.
	tmp := t.TempDir()
	yaml := `name: agent-secret
environments:
  production:
    api: { provider: openai }
edges: []
`
	if err := os.WriteFile(filepath.Join(tmp, "perch.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	pulseDir := filepath.Join(tmp, "pulse")
	secret := "sk-abcdefghijklmnopqrstuvwxyz012345"
	first := time.Date(2026, 10, 10, 21, 0, 0, 0, time.UTC)
	inc := incident.Incident{
		SchemaVersion:      incident.SchemaVersion,
		IncidentID:         incident.IncidentIDFor("astronomy/local/shipping", first),
		PrimaryServiceID:   "astronomy/local/shipping",
		Environment:        "local",
		FindingIDs:         []string{"f-secret"},
		SignalTypes:        []string{"latency_ms"},
		FirstDetectedAt:    first,
		LastObservedAt:     first.Add(time.Minute),
		AffectedServiceIDs: []string{"astronomy/local/shipping"},
		Before:             incident.PhaseSnapshot{Phase: "before"},
		During: incident.PhaseSnapshot{
			Phase: "during", Available: true,
			Digests: []incident.SampleDigest{{Signal: detect.SignalLatencyMS, Count: 1, Median: 10, Min: 10, Max: 10, FirstAt: first, LastAt: first}},
		},
		After:        incident.PhaseSnapshot{Phase: "after"},
		Observations: []string{"observed latency; api_key=" + secret},
		Inferences:   []string{"possible deploy correlation; not proven causation"},
		Limitations:  []string{"fixture"},
		Summary:      "latency with bearer " + secret,
	}
	if err := incident.NewFileStore(pulseDir).Append(inc); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)
	t.Setenv("PERCH_PULSE_DIR", pulseDir)
	t.Setenv("PERCH_CREDENTIALS_PATH", filepath.Join(tmp, "credentials.json"))
	_ = os.WriteFile(filepath.Join(tmp, "credentials.json"), []byte("{}"), 0o600)
	prevTok := stackstatus.SetDeployableTokenFnForTest(func(*provider.Spec) (string, bool) {
		return "", false
	})
	t.Cleanup(func() { stackstatus.SetDeployableTokenFnForTest(prevTok) })
	t.Chdir(tmp)

	var buf bytes.Buffer
	cmd := cli.NewRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"context", "--for-agent", "--env", "production"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Stack: agent-secret") {
		t.Fatalf("missing base context:\n%s", out)
	}
	if !strings.Contains(out, "Environment: production") {
		t.Fatalf("base agent context corrupted:\n%s", out)
	}
	if strings.Contains(out, secret) {
		t.Fatalf("secret leaked into agent context:\n%s", out)
	}

	buf.Reset()
	cmd = cli.NewRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"pulse", "incident", inc.IncidentID})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), secret) {
		t.Fatalf("secret leaked into pulse incident JSON:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "[redacted]") {
		t.Fatalf("expected redaction marker in pulse incident JSON:\n%s", buf.String())
	}
}
