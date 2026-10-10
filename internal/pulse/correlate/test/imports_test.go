package correlate_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestCorrelateDoesNotImportScenario(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, "github.com/yashg4509/perch/internal/pulse/correlate").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "internal/pulse/scenario") {
			t.Fatalf("correlate must not import scenario; imports:\n%s", out)
		}
	}
}

func TestDetectDoesNotImportChangeOrCorrelate(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, "github.com/yashg4509/perch/internal/pulse/detect").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	blob := string(out)
	for _, bad := range []string{"internal/pulse/change", "internal/pulse/correlate", "internal/pulse/scenario", "internal/pulse/changeeval"} {
		if strings.Contains(blob, bad) {
			t.Fatalf("detect must not import %s; imports:\n%s", bad, blob)
		}
	}
}
