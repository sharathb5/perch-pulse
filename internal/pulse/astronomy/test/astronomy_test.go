package astronomy_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/yashg4509/perch/internal/pulse/astronomy"
	"github.com/yashg4509/perch/internal/pulse/observation"
)

func TestLoad_ValidatesPinnedTopology(t *testing.T) {
	m, err := astronomy.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Demo.Pin != "3.1.0" {
		t.Fatalf("pin = %q, want 3.1.0", m.Demo.Pin)
	}
	if m.Demo.DefaultMakeTarget != "start-minimal" {
		t.Fatalf("default_make_target = %q", m.Demo.DefaultMakeTarget)
	}
	if _, ok := m.Telemetry.Surfaces["traces"]; !ok {
		t.Fatal("missing traces surface")
	}
	if _, ok := m.Telemetry.Surfaces["metrics"]; !ok {
		t.Fatal("missing metrics surface")
	}
	if _, ok := m.Telemetry.Surfaces["logs"]; !ok {
		t.Fatal("missing logs surface")
	}
}

func TestServiceID_LocalConvention(t *testing.T) {
	id, err := astronomy.ServiceID(astronomy.LocalEnvironment, "frontend")
	if err != nil {
		t.Fatal(err)
	}
	if id != "astronomy/local/frontend" {
		t.Fatalf("got %q", id)
	}
	infra, err := astronomy.InfraServiceID(astronomy.LocalEnvironment, "otel-collector")
	if err != nil {
		t.Fatal(err)
	}
	if infra != "astronomy/local/infra/otel-collector" {
		t.Fatalf("got %q", infra)
	}
}

func TestServiceID_RejectsInvalid(t *testing.T) {
	if _, err := astronomy.ServiceID("", "frontend"); err == nil {
		t.Fatal("expected empty env error")
	}
	if _, err := astronomy.ServiceID("local", "Frontend"); err == nil {
		t.Fatal("expected uppercase rejection")
	}
	if _, err := astronomy.InfraServiceID("local", ""); err == nil {
		t.Fatal("expected empty infra error")
	}
}

func TestMapping_PulseIDsAreUniqueAndObservationCompatible(t *testing.T) {
	m, err := astronomy.Load()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	seen := map[string]struct{}{}
	for _, s := range m.Services {
		id, err := s.PulseServiceID(astronomy.LocalEnvironment)
		if err != nil {
			t.Fatalf("%s: %v", s.ComposeService, err)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = struct{}{}

		obs := observation.Observation{
			ServiceID:  id,
			ObservedAt: now,
			Status:     observation.StatusUnavailable,
			Source:     "astronomy-mapping-test",
		}
		if err := obs.Validate(); err != nil {
			t.Fatalf("observation.Validate(%q): %v", id, err)
		}
	}
}

func TestMapping_DistinguishesAppAndInfra(t *testing.T) {
	m, err := astronomy.Load()
	if err != nil {
		t.Fatal(err)
	}
	var sawApp, sawInfra bool
	for _, s := range m.Services {
		id, err := s.PulseServiceID(astronomy.LocalEnvironment)
		if err != nil {
			t.Fatal(err)
		}
		if s.InfraName != "" {
			sawInfra = true
			if !strings.Contains(id, "/infra/") {
				t.Fatalf("infra id missing /infra/: %q", id)
			}
		} else {
			sawApp = true
			if strings.Contains(id, "/infra/") {
				t.Fatalf("app id unexpectedly infra-shaped: %q", id)
			}
		}
	}
	if !sawApp || !sawInfra {
		t.Fatalf("sawApp=%v sawInfra=%v", sawApp, sawInfra)
	}
}

func TestMapping_NoSecretLikeFields(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	mappingPath := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "service-mapping.yaml"))
	raw, err := os.ReadFile(mappingPath)
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(raw))
	// Mapping must not embed credential values. Demo passwords stay in upstream .env only.
	for _, bad := range []string{"password:", "api_key:", "token:", "secret:"} {
		if strings.Contains(lower, bad) {
			t.Fatalf("mapping appears to contain %q", bad)
		}
	}
}

func TestMapping_MinimalModeCorePresent(t *testing.T) {
	m, err := astronomy.Load()
	if err != nil {
		t.Fatal(err)
	}
	minimal := map[string]bool{}
	for _, s := range m.Services {
		for _, mode := range s.Modes {
			if mode == "minimal" {
				if s.OTELServiceName != "" {
					minimal[s.OTELServiceName] = true
				}
				if s.InfraName != "" {
					minimal["infra:"+s.InfraName] = true
				}
			}
		}
	}
	for _, name := range []string{"frontend", "checkout", "cart", "product-catalog", "payment"} {
		if !minimal[name] {
			t.Fatalf("minimal mode missing %s", name)
		}
	}
	if !minimal["infra:otel-collector"] || !minimal["infra:jaeger"] {
		t.Fatal("minimal mode missing collector/jaeger infra")
	}
	if minimal["kafka"] || minimal["accounting"] {
		t.Fatal("minimal mode must not include kafka/accounting")
	}
}

func TestMapping_CheckoutHasNoKafkaDepInMinimal(t *testing.T) {
	m, err := astronomy.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range m.Services {
		if s.OTELServiceName != "checkout" {
			continue
		}
		for _, dep := range append(append([]string{}, s.DependsOnOTEL...), s.DependsOnInfra...) {
			if dep == "kafka" {
				t.Fatal("checkout must not declare kafka dependency (full-mode only; use accounting/fraud edges)")
			}
		}
		return
	}
	t.Fatal("checkout not found")
}
