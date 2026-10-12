/**
 * Live Pulse API adapters — schemas must match internal/pulse/live + api live_*.
 * Do not invent CPU/memory/network fields.
 */

import { containsGroundTruthLeak } from './pulse.js'

export const LIVE_SCHEMA_STATUS = 'pulse.api.live_status.v1'
export const LIVE_SCHEMA_SERVICES = 'pulse.api.live_services.v1'
export const LIVE_SCHEMA_INCIDENTS = 'pulse.api.live_incidents.v1'

export const LIVE_STATE = Object.freeze({
  DISABLED: 'disabled',
  UNAVAILABLE: 'unavailable',
  WARMING_UP: 'warming_up',
  COLLECTING: 'collecting',
  ERROR: 'error',
})

export const LIVE_LIFECYCLE = Object.freeze({
  ACTIVE: 'active',
  RECOVERED: 'recovered',
  HISTORICAL: 'historical',
})

/**
 * @param {unknown} raw
 */
export function parseLiveStatusResponse(raw) {
  if (raw == null || typeof raw !== 'object') {
    return { ok: false, error: 'Live Pulse status response missing' }
  }
  if (containsGroundTruthLeak(raw)) {
    return { ok: false, error: 'Live Pulse status rejected: ground-truth markers' }
  }
  const schema = String(raw.schema_version ?? '')
  if (schema !== '' && schema !== LIVE_SCHEMA_STATUS) {
    return { ok: false, error: `Unexpected Live Pulse status schema: ${schema}` }
  }
  const status = normalizeLiveStatus(raw.status)
  if (!status) {
    return { ok: false, error: 'Live Pulse status.status missing' }
  }
  return {
    ok: true,
    response: {
      schemaVersion: schema || LIVE_SCHEMA_STATUS,
      generatedAt: raw.generated_at ?? null,
      limitations: Array.isArray(raw.limitations) ? raw.limitations.map(String) : [],
      status,
    },
  }
}

/**
 * @param {unknown} raw
 */
export function parseLiveServicesResponse(raw) {
  if (raw == null || typeof raw !== 'object') {
    return { ok: false, error: 'Live Pulse services response missing' }
  }
  if (containsGroundTruthLeak(raw)) {
    return { ok: false, error: 'Live Pulse services rejected: ground-truth markers' }
  }
  const schema = String(raw.schema_version ?? '')
  if (schema !== '' && schema !== LIVE_SCHEMA_SERVICES) {
    return { ok: false, error: `Unexpected Live Pulse services schema: ${schema}` }
  }
  const services = Array.isArray(raw.services)
    ? raw.services.map(normalizeLiveService).filter(Boolean)
    : []
  return {
    ok: true,
    response: {
      schemaVersion: schema || LIVE_SCHEMA_SERVICES,
      generatedAt: raw.generated_at ?? null,
      count: typeof raw.count === 'number' ? raw.count : services.length,
      services,
      status: normalizeLiveStatus(raw.status),
      limitations: Array.isArray(raw.limitations) ? raw.limitations.map(String) : [],
    },
  }
}

/**
 * @param {unknown} raw
 */
export function parseLiveIncidentsResponse(raw) {
  if (raw == null || typeof raw !== 'object') {
    return { ok: false, error: 'Live Pulse incidents response missing' }
  }
  if (containsGroundTruthLeak(raw)) {
    return { ok: false, error: 'Live Pulse incidents rejected: ground-truth markers' }
  }
  const schema = String(raw.schema_version ?? '')
  if (schema !== '' && schema !== LIVE_SCHEMA_INCIDENTS) {
    return { ok: false, error: `Unexpected Live Pulse incidents schema: ${schema}` }
  }
  const incidents = Array.isArray(raw.incidents)
    ? raw.incidents.map(normalizeLiveIncidentView).filter(Boolean)
    : []
  return {
    ok: true,
    response: {
      schemaVersion: schema || LIVE_SCHEMA_INCIDENTS,
      generatedAt: raw.generated_at ?? null,
      count: typeof raw.count === 'number' ? raw.count : incidents.length,
      incidents,
      status: normalizeLiveStatus(raw.status),
      limitations: Array.isArray(raw.limitations) ? raw.limitations.map(String) : [],
    },
  }
}

/**
 * @param {unknown} s
 */
export function normalizeLiveStatus(s) {
  if (s == null || typeof s !== 'object') return null
  return {
    schemaVersion: String(s.schema_version ?? ''),
    generatedAt: s.generated_at ?? null,
    enabled: Boolean(s.enabled),
    configured: Boolean(s.configured),
    available: Boolean(s.available),
    state: String(s.state ?? LIVE_STATE.DISABLED),
    environment: String(s.environment ?? ''),
    prometheusConfigured: Boolean(s.prometheus_configured),
    pollInterval: String(s.poll_interval ?? ''),
    sampleWindow: String(s.sample_window ?? ''),
    freshnessWindow: String(s.freshness_window ?? ''),
    lastAttemptAt: s.last_attempt_at ?? null,
    lastSuccessAt: s.last_success_at ?? null,
    lastError: String(s.last_error ?? ''),
    servicesObserved: typeof s.services_observed === 'number' ? s.services_observed : 0,
    servicesWithBaseline:
      typeof s.services_with_baseline === 'number' ? s.services_with_baseline : 0,
    activeRegressions: typeof s.active_regressions === 'number' ? s.active_regressions : 0,
    activeIncidents: typeof s.active_incidents === 'number' ? s.active_incidents : 0,
    unavailableOrUnknown:
      typeof s.unavailable_or_unknown === 'number' ? s.unavailable_or_unknown : 0,
    detectorVersion: String(s.detector_version ?? ''),
    activeHealthNote: String(s.active_health_note ?? ''),
    runPulseNote: String(s.run_pulse_note ?? ''),
    limitations: Array.isArray(s.limitations) ? s.limitations.map(String) : [],
    unavailabilityReason: String(s.unavailability_reason ?? ''),
  }
}

/**
 * @param {unknown} s
 */
export function normalizeLiveService(s) {
  if (s == null || typeof s !== 'object') return null
  const serviceId = String(s.service_id ?? '').trim()
  if (!serviceId) return null
  return {
    serviceId,
    graphNode: String(s.graph_node ?? ''),
    mappingSource: String(s.mapping_source ?? ''),
    environment: String(s.environment ?? ''),
    freshness: String(s.freshness ?? 'unavailable'),
    quality: String(s.quality ?? 'unavailable'),
    lastObservedAt: s.last_observed_at ?? null,
    intelligence: String(s.intelligence ?? ''),
    baselineReady: Boolean(s.baseline_ready),
    regressionOpen: Boolean(s.regression_open),
    openIncidentIds: Array.isArray(s.open_incident_ids) ? s.open_incident_ids.map(String) : [],
    signals: Array.isArray(s.signals) ? s.signals.map(normalizeLiveSignal).filter(Boolean) : [],
    activeHealthNote: String(s.active_health_note ?? ''),
    limitations: Array.isArray(s.limitations) ? s.limitations.map(String) : [],
  }
}

/**
 * @param {unknown} sig
 */
function normalizeLiveSignal(sig) {
  if (sig == null || typeof sig !== 'object') return null
  const signal = String(sig.signal ?? '').trim()
  if (!signal) return null
  return {
    signal,
    value: typeof sig.value === 'number' ? sig.value : null,
    unit: String(sig.unit ?? ''),
    window: String(sig.window ?? ''),
    observedAt: sig.observed_at ?? null,
    quality: String(sig.quality ?? 'missing'),
    baselineReady: Boolean(sig.baseline_ready),
    baselineValue: typeof sig.baseline_value === 'number' ? sig.baseline_value : null,
    regressionOpen: Boolean(sig.regression_open),
    regressionScore: typeof sig.regression_score === 'number' ? sig.regression_score : null,
  }
}

/**
 * @param {unknown} iv
 */
export function normalizeLiveIncidentView(iv) {
  if (iv == null || typeof iv !== 'object') return null
  const incidentId = String(iv.incident_id ?? '').trim()
  if (!incidentId) return null
  return {
    incidentId,
    lifecycle: String(iv.lifecycle ?? LIVE_LIFECYCLE.HISTORICAL),
    source: String(iv.source ?? 'historical'),
    primaryServiceId: String(iv.primary_service_id ?? ''),
    graphNode: String(iv.graph_node ?? ''),
    firstDetectedAt: iv.first_detected_at ?? null,
    lastObservedAt: iv.last_observed_at ?? null,
    recoveredAt: iv.recovered_at ?? null,
    signalTypes: Array.isArray(iv.signal_types) ? iv.signal_types.map(String) : [],
    summary: String(iv.summary ?? ''),
    limitations: Array.isArray(iv.limitations) ? iv.limitations.map(String) : [],
    // Nested incident kept for detail continuity (raw snake_case for historical detail path).
    incident: iv.incident ?? null,
  }
}

/**
 * Index live services by graph_node (explicit mapping only).
 * @param {object | null} servicesResponse
 * @returns {Map<string, object>}
 */
export function indexLiveByGraphNode(servicesResponse) {
  const map = new Map()
  for (const s of servicesResponse?.services ?? []) {
    const node = String(s.graphNode ?? '').trim()
    if (node) {
      map.set(node, s)
    }
  }
  return map
}

/**
 * Unmapped live services (no graph_node).
 * @param {object | null} servicesResponse
 */
export function unmappedLiveServices(servicesResponse) {
  return (servicesResponse?.services ?? []).filter((s) => !String(s.graphNode ?? '').trim())
}

/**
 * True when the live collector snapshot is currently usable for ACTIVE claims.
 * Retained incident views after a failed poll must not stay labeled ACTIVE.
 * @param {object | null | undefined} liveStatus — normalizeLiveStatus result
 */
export function liveCollectorCurrent(liveStatus) {
  if (!liveStatus?.enabled) return false
  if (!liveStatus.available) return false
  const state = String(liveStatus.state ?? '')
  return state === LIVE_STATE.COLLECTING || state === LIVE_STATE.WARMING_UP
}

/**
 * Merge live + historical incidents by stable incident_id (prefer live lifecycle labels).
 * @param {object[]} historical — normalized historical incidents
 * @param {object[]} liveViews — normalizeLiveIncidentView results
 * @param {{ liveStatus?: object | null }} [opts]
 */
export function mergeIncidentsPreferLive(historical = [], liveViews = [], opts = {}) {
  const collectorCurrent = liveCollectorCurrent(opts.liveStatus)
  const byId = new Map()
  for (const inc of historical) {
    if (!inc?.incidentId) continue
    // Persisted open ≠ current live activity. Keep unresolved/recovered labels
    // until a live view with source=live upgrades lifecycle.
    byId.set(inc.incidentId, {
      ...inc,
      lifecycle: inc.open ? 'unresolved' : LIVE_LIFECYCLE.RECOVERED,
      liveSource: 'historical',
    })
  }
  for (const lv of liveViews) {
    if (!lv?.incidentId) continue
    const prev = byId.get(lv.incidentId)
    const fromLiveCollector = lv.source === 'live' && collectorCurrent
    let lifecycle
    if (fromLiveCollector) {
      lifecycle = lv.lifecycle
    } else if (lv.source === 'live' && !collectorCurrent) {
      // Snapshot retained after collector failure — not current ACTIVE.
      lifecycle =
        lv.lifecycle === LIVE_LIFECYCLE.ACTIVE
          ? 'telemetry_unavailable'
          : lv.lifecycle || prev?.lifecycle || LIVE_LIFECYCLE.HISTORICAL
    } else {
      lifecycle =
        prev?.lifecycle ||
        (lv.lifecycle === LIVE_LIFECYCLE.ACTIVE ? 'unresolved' : lv.lifecycle)
    }
    byId.set(lv.incidentId, {
      ...(prev ?? {}),
      incidentId: lv.incidentId,
      primaryServiceId: lv.primaryServiceId || prev?.primaryServiceId,
      firstDetectedAt: lv.firstDetectedAt ?? prev?.firstDetectedAt,
      lastObservedAt: lv.lastObservedAt ?? prev?.lastObservedAt,
      recoveredAt: lv.recoveredAt ?? prev?.recoveredAt,
      signalTypes: lv.signalTypes?.length ? lv.signalTypes : prev?.signalTypes,
      summary: lv.summary || prev?.summary,
      open: lifecycle === LIVE_LIFECYCLE.ACTIVE || lifecycle === 'unresolved',
      lifecycle,
      liveSource: fromLiveCollector
        ? 'live'
        : lifecycle === 'telemetry_unavailable'
          ? 'stale_live'
          : prev?.liveSource === 'live'
            ? 'historical'
            : prev?.liveSource || 'historical',
      graphNode: lv.graphNode || prev?.graphNode,
      environment: prev?.environment || lv.environment,
      affectedServiceIds: prev?.affectedServiceIds,
    })
  }
  return [...byId.values()].sort((a, b) => {
    const ta = Date.parse(a.lastObservedAt ?? a.firstDetectedAt ?? '') || 0
    const tb = Date.parse(b.lastObservedAt ?? b.firstDetectedAt ?? '') || 0
    if (ta !== tb) return tb - ta
    return String(a.incidentId).localeCompare(String(b.incidentId))
  })
}

/**
 * Format a live signal for display. Missing values → unavailable (not 0%).
 * @param {object} sig
 */
/**
 * Format a numeric live metric (value or baseline) with consistent units.
 * error_rate API values are ratios (0–1); display as percent.
 * @param {string} signal
 * @param {number | null | undefined} value
 */
export function formatLiveMetricNumber(signal, value) {
  if (value == null || Number.isNaN(value)) {
    return { text: 'unavailable', unit: '' }
  }
  if (signal === 'error_rate') {
    return { text: `${(value * 100).toFixed(1)}%`, unit: 'error rate' }
  }
  if (signal === 'latency_ms') {
    return { text: `${value.toFixed(0)}`, unit: 'ms' }
  }
  if (signal === 'call_rate') {
    return { text: `${value.toFixed(2)}`, unit: 'calls/s' }
  }
  return { text: String(value), unit: '' }
}

export function formatLiveSignalValue(sig) {
  if (!sig || sig.value == null || sig.quality === 'missing') {
    return { text: 'unavailable', unit: sig?.unit ?? '' }
  }
  return formatLiveMetricNumber(sig.signal, sig.value)
}

/**
 * Format baseline with the same unit rules as the current value.
 * @param {object} sig
 */
export function formatLiveBaseline(sig) {
  if (!sig || sig.baselineValue == null || !sig.baselineReady) {
    return { text: 'unavailable', unit: '' }
  }
  return formatLiveMetricNumber(sig.signal, sig.baselineValue)
}

/**
 * Human label for live runtime state.
 * @param {object | null} status
 */
export function liveRuntimeSummary(status) {
  if (!status) {
    return { title: 'Live telemetry', detail: 'No live status yet.', tone: 'muted' }
  }
  if (!status.enabled) {
    return {
      title: 'Live telemetry disabled',
      detail: status.unavailabilityReason || 'Collector not enabled.',
      tone: 'muted',
    }
  }
  if (status.state === LIVE_STATE.WARMING_UP) {
    return {
      title: 'Live telemetry warming up',
      detail: 'Baseline still collecting — no regression claim from a single sample.',
      tone: 'warn',
    }
  }
  if (status.state === LIVE_STATE.ERROR || !status.available) {
    return {
      title: 'Live telemetry unavailable',
      detail: status.lastError || status.unavailabilityReason || 'Provider error — not healthy.',
      tone: 'warn',
    }
  }
  if (status.state === LIVE_STATE.COLLECTING) {
    return {
      title: 'Live telemetry collecting',
      detail: `Last success ${status.lastSuccessAt ?? '—'} · ${status.servicesObserved} service(s) observed`,
      tone: 'ok',
    }
  }
  return {
    title: `Live telemetry: ${status.state}`,
    detail: status.unavailabilityReason || '',
    tone: 'muted',
  }
}

/**
 * Navbar graph env (production/staging/dev) vs Live Pulse collector env.
 * Astronomy demo collectors use `local` (and similar) — those are not Perch triad
 * names and may join by graph_node with an explicit sidebar note.
 * When both sides use triad names, require an exact match.
 * @param {string} collectorEnv
 * @param {string} uiEnv
 */
export function liveEnvAllowsGraphJoin(collectorEnv, uiEnv) {
  const c = String(collectorEnv ?? '').trim()
  const u = String(uiEnv ?? '').trim()
  if (!c || !u) return true
  const triad = new Set(['production', 'staging', 'dev'])
  if (triad.has(c) && triad.has(u)) {
    return c === u
  }
  // Collector demo/local env: join by exact graph_node; UI shows collector env label.
  return true
}

/**
 * Graph indicator precedence helper for live regression vs historical Pulse.
 * Returns true when a live regression should drive the Pulse badge.
 * @param {object | null | undefined} liveService
 */
export function hasLiveRegression(liveService) {
  if (!liveService) return false
  if (liveService.quality === 'stale' || liveService.freshness === 'stale') {
    // Stale last-known regression is not current.
    return false
  }
  return Boolean(liveService.regressionOpen && liveService.baselineReady)
}
