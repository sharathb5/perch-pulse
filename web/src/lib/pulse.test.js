import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { deriveStatus, mapGraphToNodes } from './mappers.js'
import {
  PULSE_KIND,
  assertProbeStatusUntouched,
  containsGroundTruthLeak,
  decodeIncidentId,
  derivePulseKind,
  describePulseDataSources,
  encodeIncidentId,
  enrichServicePulse,
  indexPulseByGraphNode,
  parseIncidentDetailResponse,
  parseIncidentsResponse,
  parseServicesResponse,
  pulseIncidentApiPath,
  pulseIncidentPath,
  sortIncidentsDeterministic,
} from './pulse.js'

const fixturesDir = join(dirname(fileURLToPath(import.meta.url)), '../../fixtures')

function load(name) {
  return JSON.parse(readFileSync(join(fixturesDir, name), 'utf8'))
}

const graphOk = load('graph.ok.json')
const statusOk = load('status.ok.json')
const servicesHistorical = load('pulse.services.historical.json')
const servicesOpen = load('pulse.services.open.json')
const servicesUnavailable = load('pulse.services.unavailable.json')
const servicesEmpty = load('pulse.services.empty.json')
const incidentsHistorical = load('pulse.incidents.historical.json')
const incidentsOpen = load('pulse.incidents.open.json')
const incidentDetail = load('pulse.incident.detail.json')

describe('parseServicesResponse', () => {
  it('parses contract fixtures and indexes by graph_node only', () => {
    const parsed = parseServicesResponse(servicesHistorical)
    expect(parsed.ok).toBe(true)
    expect(parsed.response.dataSources.observations).toBe('process_memory_unavailable')
    const index = indexPulseByGraphNode(parsed.response)
    expect(index.get('api')?.serviceId).toBe('fixture/production/api')
    expect(index.get('web')?.serviceId).toBe('fixture/production/web')
    expect(index.has('Shipping Service')).toBe(false)
  })

  it('rejects structured ground-truth markers (not prose negations)', () => {
    const bad = {
      ...servicesEmpty,
      ground_truth: { scenario_id: 'leak' },
    }
    expect(containsGroundTruthLeak(bad)).toBe(true)
    expect(parseServicesResponse(bad).ok).toBe(false)
    expect(
      containsGroundTruthLeak({
        limitations: ['incident assembly does not use scenario ground-truth labels'],
      }),
    ).toBe(false)
  })

  it('handles empty Pulse services without inventing rows', () => {
    const parsed = parseServicesResponse(servicesEmpty)
    expect(parsed.ok).toBe(true)
    expect(parsed.response.services).toEqual([])
    expect(indexPulseByGraphNode(parsed.response).size).toBe(0)
  })

  it('ignores services without graph_node (no fuzzy name join)', () => {
    const raw = {
      ...servicesEmpty,
      services: [
        {
          service_id: 'fixture/production/orphan',
          intelligence: '',
          intelligence_available: false,
          freshness: 'unavailable',
          open_incident_ids: [],
        },
      ],
    }
    const parsed = parseServicesResponse(raw)
    expect(parsed.ok).toBe(true)
    expect(indexPulseByGraphNode(parsed.response).size).toBe(0)
  })
})

describe('parseIncidentsResponse — ordering and duplicates', () => {
  it('dedupes duplicate incident references and sorts deterministically', () => {
    const duped = {
      ...incidentsHistorical,
      incidents: [
        incidentsHistorical.incidents[0],
        incidentsHistorical.incidents[1],
        incidentsHistorical.incidents[0],
      ],
    }
    const parsed = parseIncidentsResponse(duped)
    expect(parsed.ok).toBe(true)
    expect(parsed.response.incidents).toHaveLength(2)
    const ids = parsed.response.incidents.map((i) => i.incidentId)
    expect(ids[0]).toBe('inc|fixture/production/api|20261010T050000.000000000Z')
    expect(new Set(ids).size).toBe(2)
  })

  it('marks recovered vs open correctly', () => {
    const parsed = parseIncidentsResponse(incidentsOpen)
    expect(parsed.ok).toBe(true)
    const open = parsed.response.incidents.find((i) => i.incidentId.includes('115000'))
    const recovered = parsed.response.incidents.find((i) => i.incidentId.includes('050000'))
    expect(open.open).toBe(true)
    expect(recovered.open).toBe(false)
    expect(recovered.recoveredAt).toBeTruthy()
  })
})

describe('current vs historical / unavailable semantics', () => {
  it('treats recovered-only related incidents as historical (not open outage)', () => {
    const services = parseServicesResponse(servicesHistorical).response
    const incidents = parseIncidentsResponse(incidentsHistorical).response.incidents
    const api = enrichServicePulse(services.services.find((s) => s.graphNode === 'api'), incidents)
    expect(api.kind).toBe(PULSE_KIND.HISTORICAL)
    expect(api.openCount).toBe(0)
    expect(api.incidentCount).toBe(1)
    expect(api.intelligenceAvailable).toBe(false)
  })

  it('marks open incident with missing live telemetry as open/stale — never healthy', () => {
    const services = parseServicesResponse(servicesOpen).response
    const incidents = parseIncidentsResponse(incidentsOpen).response.incidents
    const api = enrichServicePulse(services.services[0], incidents)
    expect(api.kind).toBe(PULSE_KIND.STALE)
    expect(api.openCount).toBe(1)
    expect(api.intelligence).not.toBe('healthy')
    expect(describePulseDataSources(services.dataSources).liveObservations).toBe(false)
  })

  it('unavailable / no incidents is not healthy', () => {
    const services = parseServicesResponse(servicesUnavailable).response
    const row = enrichServicePulse(services.services[0], [])
    expect(row.kind).toBe(PULSE_KIND.UNAVAILABLE)
    expect(derivePulseKind(services.services[0], [])).toBe(PULSE_KIND.UNAVAILABLE)
    expect(row.intelligenceAvailable).toBe(false)
  })

  it('no incidents + empty related → unavailable/unknown, not healthy', () => {
    const row = {
      serviceId: 'x',
      graphNode: 'api',
      intelligence: 'unknown',
      intelligenceAvailable: false,
      freshness: 'unavailable',
      openIncidentIds: [],
      relatedIncidentIds: [],
    }
    expect(derivePulseKind(row, [])).toBe(PULSE_KIND.UNAVAILABLE)
  })
})

describe('active probe health unaffected', () => {
  it('does not change deriveStatus / StatusPill probe status when Pulse is attached', () => {
    const nodes = mapGraphToNodes(graphOk, statusOk)
    const web = nodes.find((n) => n.id === 'web')
    const api = nodes.find((n) => n.id === 'api')
    expect(web.status).toBe('healthy')
    expect(api.status).toBe('degraded')

    const services = parseServicesResponse(servicesHistorical).response
    const incidents = parseIncidentsResponse(incidentsHistorical).response.incidents
    const pulseApi = enrichServicePulse(services.services.find((s) => s.graphNode === 'api'), incidents)

    expect(assertProbeStatusUntouched(api.status, pulseApi)).toBe('degraded')
    expect(assertProbeStatusUntouched(web.status, pulseApi)).toBe('healthy')
    expect(deriveStatus({ name: 'api', provider: 'render' }, statusOk.nodes.find((n) => n.name === 'api'))).toBe(
      'degraded',
    )
  })
})

describe('URL-safe incident navigation', () => {
  it('round-trips ids that embed / and |', () => {
    const id = 'inc|fixture/production/api|20261010T050000.000000000Z'
    const enc = encodeIncidentId(id)
    expect(enc).not.toContain('/')
    expect(enc).not.toContain('|')
    expect(decodeIncidentId(enc)).toBe(id)
    expect(pulseIncidentPath('fixture-stack', 'api', id)).toContain('?incident=')
    expect(pulseIncidentPath('fixture-stack', 'api', id)).toContain(enc)
    // API path keeps raw id (Go {id...}); must not use PathEscape that breaks mux.
    expect(pulseIncidentApiPath(id)).toBe(`/api/pulse/incidents/${id}`)
  })

  it('parses incident detail contract', () => {
    const parsed = parseIncidentDetailResponse(incidentDetail)
    expect(parsed.ok).toBe(true)
    expect(parsed.response.graphNode).toBe('api')
    expect(parsed.response.incident.open).toBe(false)
    expect(containsGroundTruthLeak(parsed.response)).toBe(false)
  })
})

describe('environment mismatch', () => {
  it('flags when incident environment differs from UI selection', () => {
    const services = parseServicesResponse(servicesHistorical).response
    const incidents = parseIncidentsResponse(incidentsHistorical).response.incidents
    const mismatched = enrichServicePulse(services.services[0], incidents, {
      expectedEnvironment: 'staging',
    })
    expect(mismatched.envMismatch).toBe(true)
    const matched = enrichServicePulse(services.services[0], incidents, {
      expectedEnvironment: 'production',
    })
    expect(matched.envMismatch).toBe(false)
  })
})

describe('sortIncidentsDeterministic', () => {
  it('orders by lastObservedAt desc then incidentId', () => {
    const sorted = sortIncidentsDeterministic([
      { incidentId: 'b', lastObservedAt: '2026-10-10T05:00:00Z' },
      { incidentId: 'a', lastObservedAt: '2026-10-10T06:00:00Z' },
      { incidentId: 'c', lastObservedAt: '2026-10-10T06:00:00Z' },
    ])
    expect(sorted.map((i) => i.incidentId)).toEqual(['a', 'c', 'b'])
  })
})

describe('API error shapes', () => {
  it('parseServicesResponse fails closed on null/invalid', () => {
    expect(parseServicesResponse(null).ok).toBe(false)
    expect(parseServicesResponse({ schema_version: 'wrong' }).ok).toBe(false)
  })
})
