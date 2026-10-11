package cli

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	pulseapi "github.com/yashg4509/perch/internal/pulse/api"
	"github.com/yashg4509/perch/internal/pulse/runpulse"
)

// registerPulseAPI mounts Pulse endpoints on the viz mux (historical read APIs +
// optional Run Pulse diagnostics). Failures to resolve astronomy mapping fall
// back to an empty mapper so core Perch viz still starts without Astronomy Shop.
// env is the selected perch.yaml environment (same as --env / graph queries).
func registerPulseAPI(mux *http.ServeMux, env string) error {
	dir, err := vizPulseDataDir()
	if err != nil {
		return err
	}
	var mapper pulseapi.ServiceMapper = pulseapi.EmptyMapper{}
	if am, err := pulseapi.NewAstronomyMapper(env); err == nil {
		mapper = am
	}
	reader, err := pulseapi.NewFileReader(dir, mapper)
	if err != nil {
		return err
	}
	h := &pulseapi.Handler{Reader: reader}

	// Run Pulse: explicit env config; invalid allowlist → engine omitted (workflows 503 / unavailable).
	if cfg, err := runpulse.ConfigFromEnv(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "# → Run Pulse disabled: %v\n", err)
	} else if eng, err := runpulse.NewEngine(cfg, mapper); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "# → Run Pulse disabled: %v\n", err)
	} else {
		h.Runs = eng
	}

	h.Register(mux)
	return nil
}

// vizPulseDataDir mirrors pulseDataDir without requiring a cobra command.
func vizPulseDataDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv("PERCH_PULSE_DIR")); v != "" {
		return filepath.Clean(v), nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(wd, ".perch", "pulse"), nil
}

// pulseAPIMountError is logged but does not abort viz — Perch core must work
// even when Pulse wiring fails.
func pulseAPIMountError(err error) string {
	return fmt.Sprintf("pulse API not mounted: %v", err)
}
