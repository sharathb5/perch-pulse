package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yashg4509/perch/internal/config"
	"github.com/yashg4509/perch/internal/testutil"
)

// Regression: Astronomy Shop perch.yaml must validate, including checkout → email.
// Previously the edge referenced email without declaring the node (Validate rejects unknown edge endpoints).
func TestAstronomyShopPerchYAML_loadsAndValidates(t *testing.T) {
	path := filepath.Join(testutil.RepoRoot(t), "examples", "astronomy-shop", "perch.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read astronomy perch.yaml: %v", err)
	}
	cfg, err := config.Load(raw)
	if err != nil {
		t.Fatalf("Load astronomy perch.yaml: %v", err)
	}
	local, ok := cfg.Environments["local"]
	if !ok {
		t.Fatal("expected environments.local")
	}
	if _, ok := local["email"]; !ok {
		t.Fatal("expected email node (checkout → email edge requires it)")
	}
	if _, ok := local["checkout"]; !ok {
		t.Fatal("expected checkout node")
	}
	found := false
	for _, e := range cfg.Edges {
		if e.From == "checkout" && e.To == "email" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected edge checkout → email (canonical OTEL checkout depends_on email)")
	}
}
