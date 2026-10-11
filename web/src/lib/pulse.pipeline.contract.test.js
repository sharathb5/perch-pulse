import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import {
  buildDigestComparisonRows,
  buildPulseOverview,
  containsGroundTruthLeak,
  correlationStrengthSummary,
  isSimulatedChange,
  parseChangeDetailResponse,
  parseChangesResponse,
  parseIncidentDetailResponse,
  parseIncidentsResponse,
  parseServicesResponse,
} from './pulse.js'

/**
 * UNIT (frontend contract): consumes the golden JSON produced by the Go
 * INTEGRATION pipeline (samples→detector→incident→API).
 * Regenerate: go test ./internal/pulse/pipeline/test -update-pipeline-fixture
 */
const fixturesDir = join(dirname(fileURLToPath(import.meta.url)), '../../fixtures')
const contract = JSON.parse(readFileSync(join(fixturesDir, 'pulse.pipeline.contract.json'), 'utf8'))

describe('Go pipeline contract fixture (Milestone C.5)', () => {
  it('parses services/incidents/detail/changes without ground-truth leakage', () => {
    for (const [name, payload] of Object.entries(contract)) {
      expect(containsGroundTruthLeak(payload), name).toBe(false)
    }
    const services = parseServicesResponse(contract.services)
    expect(services.ok).toBe(true)
    const incidents = parseIncidentsResponse(contract.incidents)
    expect(incidents.ok).toBe(true)
    expect(incidents.response.count).toBeGreaterThan(0)
    const detail = parseIncidentDetailResponse(contract.incident)
    expect(detail.ok).toBe(true)
    expect(detail.response.incident.primaryServiceId).toBe('astronomy/local/shipping')
    expect(detail.response.incident.during?.available).toBe(true)
    const changes = parseChangesResponse(contract.changes)
    expect(changes.ok).toBe(true)
    expect(changes.response.changes.some((c) => isSimulatedChange(c))).toBe(true)
  })

  it('preserves before/during/after digests for investigation UI', () => {
    const detail = parseIncidentDetailResponse(contract.incident)
    expect(detail.ok).toBe(true)
    const rows = buildDigestComparisonRows(detail.response.incident)
    expect(rows.length).toBeGreaterThan(0)
    const latency = rows.find((r) => r.signal === 'latency_ms')
    expect(latency).toBeTruthy()
    // During median must be the elevated regression value from the Go pipeline (~5000ms).
    expect(latency.during?.median).toBeGreaterThanOrEqual(1000)
  })

  it('keeps correlation ranking and non-causation language', () => {
    const detail = parseIncidentDetailResponse(contract.incident)
    expect(detail.ok).toBe(true)
    const corr = detail.response.correlation
    expect(corr).toBeTruthy()
    expect(corr.top1 || corr.candidates?.length).toBeTruthy()
    const summary = correlationStrengthSummary(corr)
    const text = `${summary.label} ${summary.detail}`.toLowerCase()
    expect(text).toMatch(/not.*causation|not causation/)
    expect(text).not.toMatch(/proven cause|root cause is/)
  })

  it('overview counts agree with API envelopes', () => {
    const services = parseServicesResponse(contract.services).response
    const incidents = parseIncidentsResponse(contract.incidents).response
    const changes = parseChangesResponse(contract.changes).response
    const overview = buildPulseOverview({
      servicesResponse: services,
      incidentsResponse: incidents,
      changesResponse: changes,
    })
    expect(overview.incidentCount).toBe(incidents.count)
    expect(overview.incidentCount).toBe(incidents.incidents.length)
    expect(overview.latestChange).toBeTruthy()
    expect(changes.changes.length).toBeGreaterThan(0)
    expect(overview.sourceInfo.liveObservations).toBe(false)
  })

  it('rejects malformed contract-shaped payloads safely', () => {
    expect(parseServicesResponse({ ...contract.services, ground_truth: { scenario_id: 'x' } }).ok).toBe(
      false,
    )
    expect(parseIncidentDetailResponse({ not: 'valid' }).ok).toBe(false)
    expect(parseChangeDetailResponse({ schema_version: 'nope' }).ok).toBe(false)
  })
})
