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
		// Must match go.mod toolchain (not language `go` line via go-version-file).
		"go-version: '1.27.2'",
		"cancel-in-progress: true",
		"make perch-regression",
		"make pulse-integration",
		"timeout-minutes: 30",
	} {
		if !strings.Contains(s, needle) {
			t.Errorf("ci.yml should reference %q", needle)
		}
	}
}

func TestAstronomySmokeWorkflow_DispatchOnly(t *testing.T) {
	t.Helper()
	root := testutil.RepoRoot(t)
	b, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "astronomy-smoke.yml"))
	if err != nil {
		t.Fatalf("read astronomy-smoke.yml: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "workflow_dispatch") {
		t.Fatal("astronomy-smoke must be workflow_dispatch")
	}
	if strings.Contains(s, "schedule:") {
		t.Fatal("astronomy-smoke must not be scheduled yet")
	}
	if strings.Contains(s, "pull_request:") {
		t.Fatal("astronomy-smoke must not run on pull_request")
	}
	for _, needle := range []string{
		"confirm",
		"ci-smoke.sh",
		"ASTRONOMY_DEMO_PIN",
		"timeout-minutes: 45",
		"ASTRONOMY_TRACE_DEADLINE_SECS",
		"ASTRONOMY_RUN_PULSE_DEADLINE_SECS",
		"if-no-files-found: ignore",
	} {
		if !strings.Contains(s, needle) {
			t.Errorf("astronomy-smoke.yml should reference %q", needle)
		}
	}
	if strings.Contains(s, "continue-on-error: true") {
		t.Fatal("astronomy-smoke must not mask failures with continue-on-error")
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
		"GO_TOOLCHAIN",
		"go1.27.2",
		"perch-regression:",
		"pulse-integration:",
	} {
		if !strings.Contains(s, needle) {
			t.Errorf("Makefile should reference %q", needle)
		}
	}
}
