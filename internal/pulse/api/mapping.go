package api

import (
	"strings"

	"github.com/yashg4509/perch/internal/pulse/astronomy"
)

// ServiceMapper resolves explicit graph-node ↔ Pulse service_id bindings.
// It never invents matches from display labels alone.
type ServiceMapper interface {
	// PulseServiceIDForGraphNode returns the Pulse ID for a perch.yaml node name.
	PulseServiceIDForGraphNode(graphNode string) (serviceID string, source string, ok bool)
	// GraphNodeForPulseServiceID returns the perch.yaml node for a Pulse ID.
	GraphNodeForPulseServiceID(serviceID string) (graphNode string, source string, ok bool)
}

// AstronomyMapper maps via the embedded Astronomy Shop service-mapping.yaml.
// Graph nodes are compose_service names from perch.yaml (e.g. "shipping").
// Pulse IDs are astronomy/<env>/<otel-or-infra-name>.
type AstronomyMapper struct {
	env           string
	nodeToPulse   map[string]string // compose_service -> preferred pulse id
	pulseToNode   map[string]string // pulse id -> compose_service
	mappingSource string
}

const astronomyMappingSource = "astronomy_compose"

// NewAstronomyMapper builds an explicit mapper from the embedded astronomy table.
// environment defaults to astronomy.LocalEnvironment when empty.
func NewAstronomyMapper(environment string) (*AstronomyMapper, error) {
	env := strings.TrimSpace(environment)
	if env == "" {
		env = astronomy.LocalEnvironment
	}
	m, err := astronomy.Load()
	if err != nil {
		return nil, err
	}
	am := &AstronomyMapper{
		env:           env,
		nodeToPulse:   make(map[string]string),
		pulseToNode:   make(map[string]string),
		mappingSource: astronomyMappingSource,
	}
	// Group rows by compose_service to pick a deterministic preferred ID.
	byCompose := map[string][]astronomy.Service{}
	for _, s := range m.Services {
		byCompose[s.ComposeService] = append(byCompose[s.ComposeService], s)
	}
	for compose, rows := range byCompose {
		preferred := preferMappingRow(compose, rows)
		id, err := preferred.PulseServiceID(env)
		if err != nil {
			continue
		}
		am.nodeToPulse[compose] = id
	}
	for _, s := range m.Services {
		id, err := s.PulseServiceID(env)
		if err != nil {
			continue
		}
		am.pulseToNode[id] = s.ComposeService
	}
	return am, nil
}

// preferMappingRow picks the row whose otel/infra name matches the compose
// service name when present; otherwise prefers role "app", then lexically
// smallest Pulse ID for stability.
func preferMappingRow(compose string, rows []astronomy.Service) astronomy.Service {
	if len(rows) == 1 {
		return rows[0]
	}
	for _, s := range rows {
		if s.OTELServiceName == compose || s.InfraName == compose {
			return s
		}
	}
	var app *astronomy.Service
	for i := range rows {
		if rows[i].Role == "app" {
			if app == nil || rows[i].OTELServiceName < app.OTELServiceName {
				cp := rows[i]
				app = &cp
			}
		}
	}
	if app != nil {
		return *app
	}
	best := rows[0]
	bestKey := best.OTELServiceName + best.InfraName
	for i := 1; i < len(rows); i++ {
		key := rows[i].OTELServiceName + rows[i].InfraName
		if key < bestKey {
			best = rows[i]
			bestKey = key
		}
	}
	return best
}

// PulseServiceIDForGraphNode implements ServiceMapper.
func (m *AstronomyMapper) PulseServiceIDForGraphNode(graphNode string) (string, string, bool) {
	if m == nil {
		return "", "", false
	}
	id, ok := m.nodeToPulse[strings.TrimSpace(graphNode)]
	if !ok {
		return "", "", false
	}
	return id, m.mappingSource, true
}

// GraphNodeForPulseServiceID implements ServiceMapper.
func (m *AstronomyMapper) GraphNodeForPulseServiceID(serviceID string) (string, string, bool) {
	if m == nil {
		return "", "", false
	}
	node, ok := m.pulseToNode[strings.TrimSpace(serviceID)]
	if !ok {
		return "", "", false
	}
	return node, m.mappingSource, true
}

// EmptyMapper never resolves IDs (non-astronomy stacks).
type EmptyMapper struct{}

// PulseServiceIDForGraphNode implements ServiceMapper.
func (EmptyMapper) PulseServiceIDForGraphNode(string) (string, string, bool) {
	return "", "", false
}

// GraphNodeForPulseServiceID implements ServiceMapper.
func (EmptyMapper) GraphNodeForPulseServiceID(string) (string, string, bool) {
	return "", "", false
}

// CompositeMapper tries mappers in order.
type CompositeMapper []ServiceMapper

// PulseServiceIDForGraphNode implements ServiceMapper.
func (c CompositeMapper) PulseServiceIDForGraphNode(graphNode string) (string, string, bool) {
	for _, m := range c {
		if m == nil {
			continue
		}
		if id, src, ok := m.PulseServiceIDForGraphNode(graphNode); ok {
			return id, src, true
		}
	}
	return "", "", false
}

// GraphNodeForPulseServiceID implements ServiceMapper.
func (c CompositeMapper) GraphNodeForPulseServiceID(serviceID string) (string, string, bool) {
	for _, m := range c {
		if m == nil {
			continue
		}
		if node, src, ok := m.GraphNodeForPulseServiceID(serviceID); ok {
			return node, src, true
		}
	}
	return "", "", false
}
