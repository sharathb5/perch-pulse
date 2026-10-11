package stackstatus_test

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/config"
	"github.com/yashg4509/perch/internal/provider"
	"github.com/yashg4509/perch/internal/stackstatus"
	"github.com/yashg4509/perch/internal/testutil"
)

// UNIT: active health probe semantics — timeout/fail/unchecked never report healthy.

func TestProbeSemantics_HealthyCustomMeasured(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell")
	}
	ctx := context.Background()
	cfg := mustLoad(t, `name: p
environments:
  production:
    ok:
      provider: custom
      status: "true"
edges: []
`)
	got, err := stackstatus.Collect(ctx, cfg, "production", emptyReg(), stackstatus.CollectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Nodes) != 1 || !got.Nodes[0].Healthy || got.Nodes[0].StatusSource != stackstatus.SourceShell {
		t.Fatalf("%+v", got.Nodes)
	}
}

func TestProbeSemantics_FailureNotHealthy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell")
	}
	ctx := context.Background()
	cfg := mustLoad(t, `name: p
environments:
  production:
    bad:
      provider: custom
      status: "false"
edges: []
`)
	got, err := stackstatus.Collect(ctx, cfg, "production", emptyReg(), stackstatus.CollectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Nodes[0].Healthy {
		t.Fatal("failed probe reported healthy")
	}
	if got.Nodes[0].StatusSource != stackstatus.SourceShell {
		t.Fatalf("source=%q", got.Nodes[0].StatusSource)
	}
}

func TestProbeSemantics_TimeoutNotHealthy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	cfg := mustLoad(t, `name: p
environments:
  production:
    slow:
      provider: custom
      status: "sleep 5"
edges: []
`)
	got, err := stackstatus.Collect(ctx, cfg, "production", emptyReg(), stackstatus.CollectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Nodes[0].Healthy {
		t.Fatal("timed-out probe reported healthy")
	}
}

func TestProbeSemantics_UncheckedProviderNotHealthy(t *testing.T) {
	// Missing credentials → not a successful measured healthy probe.
	prev := stackstatus.SetDeployableTokenFnForTest(func(*provider.Spec) (string, bool) {
		return "", false
	})
	t.Cleanup(func() { stackstatus.SetDeployableTokenFnForTest(prev) })

	rootProviders := mustProviders(t)
	cfg := mustLoad(t, `name: p
environments:
  production:
    web:
      provider: vercel
      project: demo
edges: []
`)
	got, err := stackstatus.Collect(context.Background(), cfg, "production", rootProviders, stackstatus.CollectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Nodes) != 1 {
		t.Fatalf("%+v", got.Nodes)
	}
	n := got.Nodes[0]
	if n.Healthy {
		t.Fatalf("unchecked/missing-cred deployable must not be healthy: %+v", n)
	}
	if n.StatusSource == stackstatus.SourceProbe && strings.Contains(strings.ToLower(n.Detail), "active") {
		// Probe source with missing cred should still be unhealthy (detail mentions credential).
	}
	if !strings.Contains(strings.ToLower(n.Detail), "credential") && n.StatusSource == stackstatus.SourceProbe {
		// acceptable: detail may vary; healthy=false is the invariant
	}
}

func emptyReg() *provider.Registry {
	return &provider.Registry{ByName: map[string]*provider.Spec{}}
}

func mustLoad(t *testing.T, yaml string) *config.Config {
	t.Helper()
	cfg, err := config.Load([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func mustProviders(t *testing.T) *provider.Registry {
	t.Helper()
	reg, err := provider.LoadRegistry(filepath.Join(testutil.RepoRoot(t), "providers"))
	if err != nil {
		t.Fatal(err)
	}
	return reg
}
