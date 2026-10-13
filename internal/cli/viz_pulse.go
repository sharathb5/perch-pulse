package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	pulseapi "github.com/yashg4509/perch/internal/pulse/api"
	"github.com/yashg4509/perch/internal/pulse/investigation"
	pulsellm "github.com/yashg4509/perch/internal/pulse/investigation/llm"
	"github.com/yashg4509/perch/internal/pulse/live"
	"github.com/yashg4509/perch/internal/pulse/runpulse"
)

// pulseRuntime holds optional long-lived Pulse subsystems for viz shutdown.
type pulseRuntime struct {
	live *live.Runtime
}

func (p *pulseRuntime) Stop() {
	if p == nil || p.live == nil {
		return
	}
	p.live.Stop()
}

// registerPulseAPI mounts Pulse endpoints on the viz mux (historical read APIs +
// optional Run Pulse diagnostics + optional Live Pulse collector). Failures to
// resolve astronomy mapping fall back to an empty mapper so core Perch viz still
// starts without Astronomy Shop. env is the selected perch.yaml environment.
// ctx cancels the Live Pulse collector when the viz server stops.
func registerPulseAPI(mux *http.ServeMux, env string, ctx context.Context) (*pulseRuntime, error) {
	dir, err := vizPulseDataDir()
	if err != nil {
		return nil, err
	}
	var mapper pulseapi.ServiceMapper = pulseapi.EmptyMapper{}
	if am, err := pulseapi.NewAstronomyMapper(env); err == nil {
		mapper = am
	}
	reader, err := pulseapi.NewFileReader(dir, mapper)
	if err != nil {
		return nil, err
	}
	h := &pulseapi.Handler{Reader: reader}
	invStore := investigation.NewFileStore(dir)
	h.Investigations = &investigation.Service{
		Incidents: reader.Incidents,
		Changes:   reader.Changes,
		Store:     invStore,
		Mapper:    mapper,
	}
	rt := &pulseRuntime{}

	// LLM investigator (F2a): explicit opt-in; never required for deterministic F1.
	if lcfg, err := pulsellm.ConfigFromEnv(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "# → Pulse LLM investigator disabled: %v\n", err)
	} else if lcfg.Available() {
		h.LLM = &pulsellm.Service{
			Config:         lcfg,
			Investigations: pulsellm.InvestigationAdapter{Store: invStore},
			Store:          pulsellm.NewFileStore(dir),
			Client:         pulsellm.NewHTTPClientFromConfig(lcfg),
		}
		_, _ = fmt.Fprintf(os.Stderr, "# → Pulse LLM investigator enabled (model %s)\n", lcfg.Model)
	} else if lcfg.Enabled {
		_, _ = fmt.Fprintf(os.Stderr, "# → Pulse LLM investigator unavailable: %s\n", lcfg.UnavailabilityReason())
	}

	// Run Pulse: explicit env config; invalid allowlist → engine omitted (workflows 503 / unavailable).
	if cfg, err := runpulse.ConfigFromEnv(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "# → Run Pulse disabled: %v\n", err)
	} else if eng, err := runpulse.NewEngine(cfg, mapper); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "# → Run Pulse disabled: %v\n", err)
	} else {
		h.Runs = eng
	}

	// Live Pulse: opt-in continuous telemetry; independent of Run Pulse.
	if lcfg, err := live.ConfigFromEnv(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "# → Live Pulse disabled: %v\n", err)
	} else if lcfg.Available() {
		liveRT, err := live.NewRuntime(live.Options{
			Config:   lcfg,
			PulseDir: dir,
			Mapper:   mapper,
		})
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "# → Live Pulse disabled: %v\n", err)
		} else {
			reader.WithObservationStore(liveRT.ObservationStore())
			h.Live = liveRT
			rt.live = liveRT
			if ctx == nil {
				ctx = context.Background()
			}
			liveRT.Start(ctx)
			_, _ = fmt.Fprintf(os.Stderr, "# → Live Pulse enabled (poll %s → %s)\n", lcfg.PollInterval, lcfg.PrometheusURL)
		}
	} else if lcfg.Enabled {
		_, _ = fmt.Fprintf(os.Stderr, "# → Live Pulse unavailable: %s\n", lcfg.UnavailabilityReason())
	}

	h.Register(mux)
	return rt, nil
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
