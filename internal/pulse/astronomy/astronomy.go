// Package astronomy defines the Phase 2 OpenTelemetry Astronomy Shop topology
// mapping into Perch/Pulse service identities.
//
// It does not start Docker, scrape telemetry, or alter observation/store
// semantics. Callers use ServiceID helpers when adapting future collectors.
package astronomy

import (
	"fmt"
	"regexp"
	"strings"

	_ "embed"

	"gopkg.in/yaml.v3"
)

//go:embed service-mapping.yaml
var mappingYAML []byte

const (
	// LocalEnvironment is the Pulse environment segment for Docker Compose local runs.
	LocalEnvironment = "local"

	// IDPrefix is the stable product/system prefix for Astronomy Shop identities.
	IDPrefix = "astronomy"
)

var (
	serviceIDRe = regexp.MustCompile(`^astronomy/[a-z0-9-]+/(?:infra/[a-z0-9-]+|[a-z0-9-]+)$`)
	nameRe      = regexp.MustCompile(`^[a-z0-9-]+$`)
)

// Mapping is the parsed service-mapping.yaml document.
type Mapping struct {
	Demo      DemoConfig      `yaml:"demo"`
	ServiceID ServiceIDConfig `yaml:"service_id"`
	Services  []Service       `yaml:"services"`
	Telemetry TelemetryConfig `yaml:"telemetry"`
}

// DemoConfig identifies the pinned upstream demo.
type DemoConfig struct {
	Upstream          string `yaml:"upstream"`
	Pin               string `yaml:"pin"`
	Docs              string `yaml:"docs"`
	DefaultMakeTarget string `yaml:"default_make_target"`
	OTELServiceNS     string `yaml:"otel_service_namespace"`
	FrontendURL       string `yaml:"frontend_url"`
}

// ServiceIDConfig documents the identity pattern.
type ServiceIDConfig struct {
	Pattern          string `yaml:"pattern"`
	LocalEnvironment string `yaml:"local_environment"`
}

// Service is one mapped Astronomy Shop component.
type Service struct {
	OTELServiceName string   `yaml:"otel_service_name"`
	InfraName       string   `yaml:"infra_name"`
	ComposeService  string   `yaml:"compose_service"`
	Role            string   `yaml:"role"`
	Visibility      string   `yaml:"visibility"`
	Modes           []string `yaml:"modes"`
	Description     string   `yaml:"description"`
	DependsOnOTEL   []string `yaml:"depends_on_otel"`
	DependsOnInfra  []string `yaml:"depends_on_infra"`
}

// TelemetryConfig summarizes traces/metrics/logs surfaces.
type TelemetryConfig struct {
	ExportProtocol string             `yaml:"export_protocol"`
	CollectorGRPC  string             `yaml:"collector_grpc"`
	CollectorHTTP  string             `yaml:"collector_http"`
	Surfaces       map[string]Surface `yaml:"surfaces"`
	Notes          []string           `yaml:"notes"`
}

// Surface is one telemetry path (traces, metrics, or logs).
type Surface struct {
	Path string `yaml:"path"`
	UI   string `yaml:"ui"`
}

// Load returns the embedded mapping document.
func Load() (Mapping, error) {
	var m Mapping
	if err := yaml.Unmarshal(mappingYAML, &m); err != nil {
		return Mapping{}, fmt.Errorf("astronomy: parse mapping: %w", err)
	}
	if err := m.Validate(); err != nil {
		return Mapping{}, err
	}
	return m, nil
}

// ServiceID builds astronomy/<environment>/<name>.
func ServiceID(environment, name string) (string, error) {
	environment = strings.TrimSpace(environment)
	name = strings.TrimSpace(name)
	if environment == "" || !nameRe.MatchString(environment) {
		return "", fmt.Errorf("astronomy: invalid environment %q", environment)
	}
	if name == "" || !nameRe.MatchString(name) {
		return "", fmt.Errorf("astronomy: invalid name %q", name)
	}
	id := IDPrefix + "/" + environment + "/" + name
	if !serviceIDRe.MatchString(id) {
		return "", fmt.Errorf("astronomy: invalid service id %q", id)
	}
	return id, nil
}

// InfraServiceID builds astronomy/<environment>/infra/<component>.
func InfraServiceID(environment, component string) (string, error) {
	environment = strings.TrimSpace(environment)
	component = strings.TrimSpace(component)
	if environment == "" || !nameRe.MatchString(environment) {
		return "", fmt.Errorf("astronomy: invalid environment %q", environment)
	}
	if component == "" || !nameRe.MatchString(component) {
		return "", fmt.Errorf("astronomy: invalid infra component %q", component)
	}
	id := IDPrefix + "/" + environment + "/infra/" + component
	if !serviceIDRe.MatchString(id) {
		return "", fmt.Errorf("astronomy: invalid infra service id %q", id)
	}
	return id, nil
}

// PulseServiceID returns the mapped Pulse service_id for this service in env.
func (s Service) PulseServiceID(environment string) (string, error) {
	if s.InfraName != "" {
		return InfraServiceID(environment, s.InfraName)
	}
	if s.OTELServiceName == "" {
		return "", fmt.Errorf("astronomy: service %q missing otel_service_name and infra_name", s.ComposeService)
	}
	return ServiceID(environment, s.OTELServiceName)
}

// Validate checks mapping invariants used by Phase 2 setup docs.
func (m Mapping) Validate() error {
	if m.Demo.Upstream == "" || m.Demo.Pin == "" {
		return fmt.Errorf("astronomy: demo.upstream and demo.pin are required")
	}
	if m.ServiceID.LocalEnvironment != LocalEnvironment {
		return fmt.Errorf("astronomy: local_environment must be %q", LocalEnvironment)
	}
	if len(m.Services) == 0 {
		return fmt.Errorf("astronomy: services must not be empty")
	}
	for _, key := range []string{"traces", "metrics", "logs"} {
		if _, ok := m.Telemetry.Surfaces[key]; !ok {
			return fmt.Errorf("astronomy: telemetry.surfaces.%s is required", key)
		}
	}

	seenIDs := map[string]string{}
	seenComposeRole := map[string]string{}
	otelNames := map[string]struct{}{}
	infraNames := map[string]struct{}{}

	for i, s := range m.Services {
		if strings.TrimSpace(s.ComposeService) == "" {
			return fmt.Errorf("astronomy: services[%d] compose_service is required", i)
		}
		switch s.Visibility {
		case "external", "internal":
		default:
			return fmt.Errorf("astronomy: service %q visibility %q invalid", s.ComposeService, s.Visibility)
		}
		if len(s.Modes) == 0 {
			return fmt.Errorf("astronomy: service %q modes required", s.ComposeService)
		}
		for _, mode := range s.Modes {
			switch mode {
			case "minimal", "full", "agentic":
			default:
				return fmt.Errorf("astronomy: service %q unknown mode %q", s.ComposeService, mode)
			}
		}

		hasOTEL := strings.TrimSpace(s.OTELServiceName) != ""
		hasInfra := strings.TrimSpace(s.InfraName) != ""
		if hasOTEL == hasInfra {
			return fmt.Errorf("astronomy: service %q must set exactly one of otel_service_name or infra_name", s.ComposeService)
		}
		if hasOTEL {
			if !nameRe.MatchString(s.OTELServiceName) {
				return fmt.Errorf("astronomy: invalid otel_service_name %q", s.OTELServiceName)
			}
			if _, dup := otelNames[s.OTELServiceName]; dup {
				return fmt.Errorf("astronomy: duplicate otel_service_name %q", s.OTELServiceName)
			}
			otelNames[s.OTELServiceName] = struct{}{}
		}
		if hasInfra {
			if !nameRe.MatchString(s.InfraName) {
				return fmt.Errorf("astronomy: invalid infra_name %q", s.InfraName)
			}
			if _, dup := infraNames[s.InfraName]; dup {
				return fmt.Errorf("astronomy: duplicate infra_name %q", s.InfraName)
			}
			infraNames[s.InfraName] = struct{}{}
			if s.Role != "infra" {
				return fmt.Errorf("astronomy: infra entry %q must have role infra", s.InfraName)
			}
		}

		id, err := s.PulseServiceID(LocalEnvironment)
		if err != nil {
			return err
		}
		if prev, ok := seenIDs[id]; ok {
			return fmt.Errorf("astronomy: duplicate service_id %q (%s and %s)", id, prev, s.ComposeService)
		}
		seenIDs[id] = s.ComposeService

		key := s.ComposeService + "|" + s.Role + "|" + s.OTELServiceName + "|" + s.InfraName
		if prev, ok := seenComposeRole[key]; ok {
			return fmt.Errorf("astronomy: duplicate mapping row for %s / prior %s", key, prev)
		}
		seenComposeRole[key] = id
	}

	// Required core services for minimal mode must be present.
	requiredOTEL := []string{
		"frontend-proxy", "frontend", "frontend-web", "checkout", "cart",
		"product-catalog", "payment", "shipping", "currency", "recommendation",
		"ad", "email", "quote", "load-generator", "flagd",
	}
	for _, name := range requiredOTEL {
		if _, ok := otelNames[name]; !ok {
			return fmt.Errorf("astronomy: missing required otel service %q", name)
		}
	}
	requiredInfra := []string{"otel-collector", "jaeger", "prometheus", "opensearch", "grafana", "astronomy-db", "valkey-cart"}
	for _, name := range requiredInfra {
		if _, ok := infraNames[name]; !ok {
			return fmt.Errorf("astronomy: missing required infra %q", name)
		}
	}

	byOTEL := map[string]Service{}
	byInfra := map[string]Service{}
	for _, s := range m.Services {
		if s.OTELServiceName != "" {
			byOTEL[s.OTELServiceName] = s
		}
		if s.InfraName != "" {
			byInfra[s.InfraName] = s
		}
	}

	// Dependency references must exist and share at least one deployment mode.
	for _, s := range m.Services {
		for _, dep := range s.DependsOnOTEL {
			target, ok := byOTEL[dep]
			if !ok {
				return fmt.Errorf("astronomy: %s depends_on_otel unknown %q", s.ComposeService, dep)
			}
			if !modesOverlap(s.Modes, target.Modes) {
				return fmt.Errorf("astronomy: %s depends_on_otel %q has no shared mode (source=%v target=%v)",
					s.ComposeService, dep, s.Modes, target.Modes)
			}
		}
		for _, dep := range s.DependsOnInfra {
			target, ok := byInfra[dep]
			if !ok {
				return fmt.Errorf("astronomy: %s depends_on_infra unknown %q (infra deps must use infra_name entries, not otel-named services)",
					s.ComposeService, dep)
			}
			if !modesOverlap(s.Modes, target.Modes) {
				return fmt.Errorf("astronomy: %s depends_on_infra %q has no shared mode (source=%v target=%v)",
					s.ComposeService, dep, s.Modes, target.Modes)
			}
		}
	}
	return nil
}

func modesOverlap(a, b []string) bool {
	set := map[string]struct{}{}
	for _, m := range a {
		set[m] = struct{}{}
	}
	for _, m := range b {
		if _, ok := set[m]; ok {
			return true
		}
	}
	return false
}

// MustLocalServiceID returns astronomy/local/<name> or panics (tests/fixtures only).
func MustLocalServiceID(name string) string {
	id, err := ServiceID(LocalEnvironment, name)
	if err != nil {
		panic(err)
	}
	return id
}
