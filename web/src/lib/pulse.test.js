import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { deriveStatus, mapGraphToNodes } from './mappers.js'
import {
  INCIDENT_STATE,
  PULSE_KIND,
  PULSE_TAB,
  assertProbeStatusUntouched,
  buildDigestComparisonRows,
  buildPulseOverview,
  containsGroundTruthLeak,
  correlationCandidateLabel,
  correlationStrengthSummary,
  decodeIncidentId,
  derivePulseKind,
  describePulseDataSources,
  encodeIncidentId,
  enrichServicePulse,
  filterChanges,
  filterIncidents,
  formatSignalValue,
  indexGraphNodeByServiceId,
  indexPulseByGraphNode,
  isSimulatedChange,
  normalizePulseTab,
  parseChangeDetailResponse,
  parseChangesResponse,
  parseIncidentDetailResponse,
  parseIncidentsResponse,
  parseServicesResponse,
  pulseIncidentApiPath,
  pulseIncidentPath,
  pulseStackPath,
  sortChangesDeterministic,
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
const changesFixture = load('pulse.changes.json')
const changesEmpty = load('pulse.changes.empty.json')

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
    const path = pulseIncidentPath('fixture-stack', 'api', id)
    expect(path).toContain('incident=')
    expect(path).toContain(`pulse=${PULSE_TAB.INCIDENTS}`)
    expect(path).toContain(enc)
    // API path keeps raw id (Go {id...}); must not use PathEscape that breaks mux.
    expect(pulseIncidentApiPath(id)).toBe(`/api/pulse/incidents/${id}`)
  })

  it('pulseStackPath preserves node when closing sidebar', () => {
    expect(pulseStackPath('fixture-stack', { nodeId: 'api' })).toBe('/stack/fixture-stack/api')
    expect(normalizePulseTab('overview')).toBe(PULSE_TAB.OVERVIEW)
    expect(normalizePulseTab('nope')).toBe(null)
  })

  it('parses incident detail contract', () => {
    const parsed = parseIncidentDetailResponse(incidentDetail)
    expect(parsed.ok).toBe(true)
    expect(parsed.response.graphNode).toBe('api')
    expect(parsed.response.incident.open).toBe(false)
    expect(parsed.response.incident.state).toBe(INCIDENT_STATE.RECOVERED)
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

describe('Milestone C — overview, digests, correlation, changes', () => {
  it('builds overview counts without inventing resource metrics', () => {
    const services = parseServicesResponse(servicesHistorical).response
    const incidents = parseIncidentsResponse(incidentsHistorical).response
    const changes = parseChangesResponse(changesFixture).response
    const overview = buildPulseOverview({
      servicesResponse: services,
      incidentsResponse: incidents,
      changesResponse: changes,
    })
    expect(overview.mappedServiceCount).toBe(2)
    expect(overview.incidentCount).toBe(2)
    expect(overview.recoveredCount).toBe(2)
    expect(overview.unresolvedCount).toBe(0)
    expect(overview.mostRecentIncident?.primaryServiceId).toBe('fixture/production/api')
    expect(overview.latestChange?.simulated).toBe(true)
    expect(overview.environments).toContain('production')
    expect(overview.flags.noResourceMetrics).toBe(true)
    expect(overview.flags.unresolvedIsNotLiveOutage).toBe(true)
    expect(overview.sourceInfo.liveObservations).toBe(false)
  })

  it('discloses truncated incident/change lists', () => {
    const overview = buildPulseOverview({
      servicesResponse: parseServicesResponse(servicesEmpty).response,
      incidentsResponse: { ...parseIncidentsResponse(incidentsHistorical).response, count: 50, limit: 50 },
      changesResponse: { ...parseChangesResponse(changesFixture).response, count: 50, limit: 50 },
    })
    expect(overview.incidentListTruncated).toBe(true)
    expect(overview.changeListTruncated).toBe(true)
    expect(overview.limitations.some((l) => l.includes('may be incomplete'))).toBe(true)
  })

  it('filters recovered vs unresolved and by service', () => {
    const open = parseIncidentsResponse(incidentsOpen).response.incidents
    const unresolved = filterIncidents(open, { state: INCIDENT_STATE.UNRESOLVED })
    const recovered = filterIncidents(open, { state: INCIDENT_STATE.RECOVERED })
    expect(unresolved.every((i) => i.open)).toBe(true)
    expect(recovered.every((i) => !i.open)).toBe(true)
    const apiOnly = filterIncidents(open, { serviceId: 'fixture/production/api' })
    expect(apiOnly.every((i) => i.primaryServiceId === 'fixture/production/api')).toBe(true)
  })

  it('formats before/during/after digests and missing measurements as N/A', () => {
    const parsed = parseIncidentDetailResponse(incidentDetail)
    const rows = buildDigestComparisonRows(parsed.response.incident)
    const latency = rows.find((r) => r.signal === 'latency_ms')
    expect(latency.before.display).toContain('ms')
    expect(latency.during.display).toContain('820')
    const errorRate = rows.find((r) => r.signal === 'error_rate')
    expect(errorRate.after.display).toBe('N/A')
    expect(formatSignalValue('error_rate', 0.02)).toBe('2%')
    expect(formatSignalValue('call_rate', 1.25)).toContain('/s')
    expect(formatSignalValue('latency_ms', null)).toBe('N/A')
    expect(formatSignalValue('latency_ms', Number.NaN)).toBe('N/A')
  })

  it('keeps evidence vs inference separation and correlation ≠ probability/causation', () => {
    const parsed = parseIncidentDetailResponse(incidentDetail)
    const inc = parsed.response.incident
    expect(inc.observations.length).toBeGreaterThan(0)
    expect(inc.inferences.length).toBeGreaterThan(0)
    expect(inc.observations.join(' ')).not.toMatch(/root cause is/i)
    const corr = parsed.response.correlation
    expect(corr.strongCandidate).toBe(true)
    expect(corr.candidates[0].scoreIsProbability).toBe(false)
    expect(correlationCandidateLabel(72.5)).toBe('correlated change')
    expect(correlationCandidateLabel(10)).toBe('weak candidate')
    expect(correlationCandidateLabel(0)).toBe('no strong candidate')
    const summary = correlationStrengthSummary(corr)
    expect(summary.label).toContain('correlated')
    expect(summary.detail.toLowerCase()).toContain('not proven causation')
  })

  it('labels simulated deployment markers from contract fields', () => {
    const changes = parseChangesResponse(changesFixture).response.changes
    expect(changes.every((c) => c.simulated)).toBe(true)
    expect(changes[0].classification).toBe('simulated_deployment_marker')
    expect(
      isSimulatedChange({
        metadata: {},
        source: 'github-actions',
        summary: 'real deploy',
        evidence: [],
      }),
    ).toBe(false)
    // Substring "sim" alone must not mark simulated (Codex P2).
    expect(
      isSimulatedChange({
        metadata: {},
        source: 'simple-ci',
        summary: 'production deploy',
        evidence: [],
      }),
    ).toBe(false)
  })

  it('indexes service_id → graph_node without fuzzy matching', () => {
    const services = parseServicesResponse(servicesHistorical).response
    const bySid = indexGraphNodeByServiceId(services)
    expect(bySid.get('fixture/production/api')).toBe('api')
    expect(bySid.has('Shipping Service')).toBe(false)
  })

  it('parses changes list/detail and sorts deterministically', () => {
    const list = parseChangesResponse(changesFixture)
    expect(list.ok).toBe(true)
    expect(list.response.changes).toHaveLength(2)
    const empty = parseChangesResponse(changesEmpty)
    expect(empty.ok).toBe(true)
    expect(empty.response.changes).toEqual([])
    const sorted = sortChangesDeterministic(list.response.changes)
    expect(sorted[0].changeId).toContain('sim-fixture-api')
    const filtered = filterChanges(list.response.changes, {
      serviceId: 'fixture/production/web',
    })
    expect(filtered).toHaveLength(1)
    const detail = parseChangeDetailResponse({
      schema_version: 'pulse.api.change.v1',
      generated_at: '2026-10-10T12:00:00Z',
      data_sources: changesFixture.data_sources,
      change: changesFixture.changes[0],
      graph_nodes: ['api'],
    })
    expect(detail.ok).toBe(true)
    expect(detail.response.graphNodes).toEqual(['api'])
  })

  it('rejects ground-truth in changes and never paints empty as healthy', () => {
    expect(parseChangesResponse({ ...changesEmpty, ground_truth: {} }).ok).toBe(false)
    const overview = buildPulseOverview({
      servicesResponse: parseServicesResponse(servicesEmpty).response,
      incidentsResponse: parseIncidentsResponse({
        schema_version: 'pulse.api.incidents.v1',
        generated_at: '2026-10-10T12:00:00Z',
        data_sources: { observations: 'process_memory_unavailable', incidents: 'file_store' },
        count: 0,
        limit: 50,
        incidents: [],
      }).response,
      changesResponse: parseChangesResponse(changesEmpty).response,
    })
    expect(overview.incidentCount).toBe(0)
    expect(overview.sourceInfo.liveObservations).toBe(false)
  })
})
