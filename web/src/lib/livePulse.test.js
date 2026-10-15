import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import {
  LIVE_SCHEMA_SERVICES,
  LIVE_SCHEMA_STATUS,
  formatLiveSignalValue,
  hasLiveRegression,
  mergeIncidentsPreferLive,
  overlayIncidentLifecycle,
  parseLiveIncidentsResponse,
  parseLiveServicesResponse,
  parseLiveStatusResponse,
} from './livePulse.js'
import { PULSE_KIND, derivePulseKind, enrichServicePulse } from './pulse.js'

const fixtures = join(dirname(fileURLToPath(import.meta.url)), '..', '..', 'fixtures')

function load(name) {
  return JSON.parse(readFileSync(join(fixtures, name), 'utf8'))
}

describe('livePulse contracts (Go-generated fixtures)', () => {
  it('parses disabled live status as unavailable not healthy', () => {
    const parsed = parseLiveStatusResponse(load('pulse.live.status.disabled.json'))
    expect(parsed.ok).toBe(true)
    expect(parsed.response.schemaVersion).toBe(LIVE_SCHEMA_STATUS)
    expect(parsed.response.status.enabled).toBe(false)
    expect(parsed.response.status.available).toBe(false)
    expect(parsed.response.status.state).toBe('disabled')
  })

  it('parses collecting status and regression services from Go fixtures', () => {
    const status = parseLiveStatusResponse(load('pulse.live.status.collecting.json'))
    expect(status.ok).toBe(true)
    expect(status.response.status.state).toBe('collecting')
    expect(status.response.status.activeRegressions).toBeGreaterThanOrEqual(1)

    const services = parseLiveServicesResponse(load('pulse.live.services.regression.json'))
    expect(services.ok).toBe(true)
    expect(services.response.schemaVersion).toBe(LIVE_SCHEMA_SERVICES)
    const payment = services.response.services.find((s) => s.graphNode === 'payment')
    expect(payment).toBeTruthy()
    expect(payment.regressionOpen).toBe(true)
    expect(hasLiveRegression(payment)).toBe(true)
    const lat = payment.signals.find((s) => s.signal === 'latency_ms')
    expect(lat.value).toBe(5000)
    expect(formatLiveSignalValue(lat).unit).toBe('ms')
  })

  it('does not treat missing signal as zero errors', () => {
    const fmt = formatLiveSignalValue({ signal: 'error_rate', quality: 'missing', value: null })
    expect(fmt.text).toBe('unavailable')
  })

  it('stale live regression is not current live_regression kind', () => {
    const live = {
      regressionOpen: true,
      baselineReady: true,
      quality: 'stale',
      freshness: 'stale',
    }
    expect(hasLiveRegression(live)).toBe(false)
    const kind = derivePulseKind(
      { openIncidentIds: [], freshness: 'stale', intelligenceAvailable: false },
      [],
      live,
    )
    expect(kind).toBe(PULSE_KIND.STALE)
  })

  it('live regression precedes historical open badge', () => {
    const kind = derivePulseKind(
      {
        openIncidentIds: ['inc|x'],
        freshness: 'fresh',
        intelligenceAvailable: true,
        intelligence: 'degraded',
      },
      [{ incidentId: 'inc|x', open: true }],
      {
        regressionOpen: true,
        baselineReady: true,
        quality: 'fresh',
        freshness: 'fresh',
      },
    )
    expect(kind).toBe(PULSE_KIND.LIVE_REGRESSION)
  })

  it('dedupes live+historical incidents by id', () => {
    const live = parseLiveIncidentsResponse(load('pulse.live.incidents.active.json'))
    const status = parseLiveStatusResponse(load('pulse.live.status.collecting.json'))
    expect(live.ok).toBe(true)
    expect(status.ok).toBe(true)
    const id = live.response.incidents[0].incidentId
    const merged = mergeIncidentsPreferLive(
      [
        {
          incidentId: id,
          open: true,
          primaryServiceId: 'astronomy/local/payment',
          firstDetectedAt: '2026-01-01T00:00:00Z',
          lastObservedAt: '2026-01-01T00:00:00Z',
        },
      ],
      live.response.incidents,
      { liveStatus: status.response.status },
    )
    expect(merged.filter((i) => i.incidentId === id)).toHaveLength(1)
    expect(merged[0].lifecycle).toBe('active')
    expect(merged[0].liveSource).toBe('live')
  })

  it('overlayIncidentLifecycle copies list ACTIVE onto detail without inventing ids', () => {
    const detail = {
      incidentId: 'inc|astronomy/local/payment|1',
      open: true,
      primaryServiceId: 'astronomy/local/payment',
    }
    const list = {
      incidentId: 'inc|astronomy/local/payment|1',
      open: true,
      lifecycle: 'active',
      liveSource: 'live',
    }
    const overlaid = overlayIncidentLifecycle(detail, list)
    expect(overlaid.lifecycle).toBe('active')
    expect(overlaid.liveSource).toBe('live')
    expect(overlayIncidentLifecycle(detail, null)).toEqual(detail)
    expect(
      overlayIncidentLifecycle(detail, { ...list, incidentId: 'other' }).lifecycle,
    ).toBeUndefined()
  })

  it('does not keep ACTIVE when collector is unavailable', () => {
    const live = parseLiveIncidentsResponse(load('pulse.live.incidents.active.json'))
    const merged = mergeIncidentsPreferLive([], live.response.incidents, {
      liveStatus: {
        enabled: true,
        available: false,
        state: 'error',
      },
    })
    expect(merged[0].lifecycle).toBe('telemetry_unavailable')
    expect(merged[0].liveSource).not.toBe('live')
  })

  it('enrichServicePulse attaches liveService and live_regression kind', () => {
    const services = parseLiveServicesResponse(load('pulse.live.services.regression.json'))
    const live = services.response.services[0]
    const enriched = enrichServicePulse(
      {
        serviceId: live.serviceId,
        graphNode: live.graphNode,
        openIncidentIds: live.openIncidentIds,
        freshness: 'fresh',
        intelligence: 'degraded',
        intelligenceAvailable: true,
      },
      [],
      { liveService: live },
    )
    expect(enriched.kind).toBe(PULSE_KIND.LIVE_REGRESSION)
    expect(enriched.liveService.serviceId).toBe(live.serviceId)
  })

  it('warming status does not claim healthy', () => {
    const warm = parseLiveStatusResponse(load('pulse.live.status.warming.json'))
    expect(warm.ok).toBe(true)
    expect(warm.response.status.available).toBe(true)
    expect(['warming_up', 'collecting']).toContain(warm.response.status.state)
  })
})
