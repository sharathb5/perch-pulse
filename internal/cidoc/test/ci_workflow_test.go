package cidoc_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yashg4509/perch/internal/testutil"
)

func TestGitHubActionsWorkflow(t *testing.T) {
	t.Helper()
	root := testutil.RepoRoot(t)
	p := filepath.Join(root, ".github", "workflows", "ci.yml")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	s := string(b)
	for _, needle := range []string{
		"make verify",
		"name: Go",
		"GOTOOLCHAIN",
		"pull_request",
		"push",
		"main",
	} {
		if !strings.Contains(s, needle) {
			t.Errorf("ci.yml should reference %q", needle)
		}
	}
}

func TestMakefileVerifyTarget(t *testing.T) {
	t.Helper()
	root := testutil.RepoRoot(t)
	b, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	s := string(b)

	var verifyLine string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "verify:") {
			verifyLine = line
			break
		}
	}
	if verifyLine == "" {
		t.Fatal("Makefile missing verify: target")
	}

	// Dependency order is part of correctness: embed before Go compile/test.
	order := []string{
		"web-deps",
		"web-build-embed",
		"web-lint",
		"web-test",
		"go-fmt",
		"go-vet",
		"go-test",
		"provider-validate",
		"security",
	}
	pos := 0
	for _, dep := range order {
		i := strings.Index(verifyLine[pos:], dep)
		if i < 0 {
			t.Fatalf("verify dependencies missing %q (or out of order) in %q", dep, verifyLine)
		}
		pos += i + len(dep)
	}

	for _, needle := range []string{
		"web-deps:",
		"web-build-embed:",
		"web-lint:",
		"web-test:",
		"go-fmt:",
		"go-vet:",
		"go-test:",
		"provider-validate:",
		"security:",
		"npm ci",
		"npm run build:embed",
		"npm run lint",
		"npm test",
		"gofmt",
		"go vet ./...",
		"go test ./...",
		"gosec",
		"govulncheck",
		"exclude=G304",
		"GOSEC_VERSION",
		"GOVULNCHECK_VERSION",
	} {
		if !strings.Contains(s, needle) {
			t.Errorf("Makefile should reference %q", needle)
		}
	}
}
