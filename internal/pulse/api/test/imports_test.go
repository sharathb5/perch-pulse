package api_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAPIPackage_DoesNotImportScenario enforces ground-truth isolation.
func TestAPIPackage_DoesNotImportScenario(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".go") {
			continue
		}
		path := filepath.Join(root, ent.Name())
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(p, "internal/pulse/scenario") {
				t.Fatalf("%s imports scenario ground-truth package %q", ent.Name(), p)
			}
			if strings.Contains(p, "internal/pulse/evaluate") {
				t.Fatalf("%s must not import evaluate (label-aware): %q", ent.Name(), p)
			}
			if strings.Contains(p, "internal/pulse/changeeval") {
				t.Fatalf("%s must not import changeeval (label-aware): %q", ent.Name(), p)
			}
		}
	}
}
