package graph_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yashg4509/perch/internal/config"
	"github.com/yashg4509/perch/internal/graph"
	"github.com/yashg4509/perch/internal/provider"
	"github.com/yashg4509/perch/internal/testutil"
)

// UNIT: Astronomy Shop topology builds stable identities without provider credentials.

func TestAstronomyShopGraph_BuildsStableNodes(t *testing.T) {
	root := testutil.RepoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "examples", "astronomy-shop", "perch.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(raw)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := provider.LoadRegistry(filepath.Join(root, "providers"))
	if err != nil {
		t.Fatal(err)
	}
	g, err := graph.Build(cfg, reg, "local")
	if err != nil {
		t.Fatal(err)
	}
	if g.AppName != "perch-astronomy-shop" || g.Environment != "local" {
		t.Fatalf("%+v", g)
	}
	want := []string{"checkout", "frontend", "payment", "shipping", "email"}
	have := map[string]bool{}
	for _, n := range g.Nodes {
		have[n.Name] = true
		if n.Name == "" {
			t.Fatal("empty node name")
		}
	}
	for _, name := range want {
		if !have[name] {
			t.Fatalf("missing node %q", name)
		}
	}
	// Missing optional cloud credentials must not matter — custom providers only.
	for _, n := range g.Nodes {
		if n.Provider != "custom" {
			t.Fatalf("astronomy shop node %q provider=%q want custom", n.Name, n.Provider)
		}
	}
}

func TestGraph_InvalidEdgeRejectedAtConfig(t *testing.T) {
	_, err := config.Load([]byte(`
name: bad
environments:
  production:
    a: { provider: openai }
edges:
  - a -> missing
`))
	if err == nil {
		t.Fatal("expected invalid edge rejection")
	}
}
