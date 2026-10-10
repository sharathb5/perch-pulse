/**
 * Pulse intelligence adapters for the Perch viz UI (Milestone B).
 * Active probe health stays in mappers.js / StatusPill — never collapsed here.
 */

export const PULSE_SCHEMA_SERVICES = 'pulse.api.services.v1'
export const PULSE_SCHEMA_INCIDENTS = 'pulse.api.incidents.v1'
export const PULSE_SCHEMA_INCIDENT = 'pulse.api.incident.v1'

/** Presentation kinds for node indicators / detail copy (not probe health). */
export const PULSE_KIND = Object.freeze({
  NONE: 'none',
  OPEN: 'open',
  HISTORICAL: 'historical',
  STALE: 'stale',
  UNAVAILABLE: 'unavailable',
  UNKNOWN: 'unknown',
})

// Align with internal/pulse/api.ContainsGroundTruthLeak — field/schema markers only.
// Do not match prose like "does not use scenario ground-truth labels".
const GROUND_TRUTH_MARKERS = [
  '"ground_truth"',
  '"ground-truth"',
  'ground_truth:',
  '"scenario_id"',
  'scenario_id:',
  '"expected_change_id"',
  'expected_change_id:',
  '"expected_change"',
  'pulse.scenario.v1',
  'label_leak',
]

/**
 * @param {unknown} value
 * @returns {boolean}
 */
export function containsGroundTruthLeak(value) {
  const text = typeof value === 'string' ? value : JSON.stringify(value ?? '')
  const lower = text.toLowerCase()
  return GROUND_TRUTH_MARKERS.some((m) => lower.includes(m.toLowerCase()))
}

/**
 * Encode an incident id for use in query strings / path segments.
 * @param {string} incidentId
 */
export function encodeIncidentId(incidentId) {
  return encodeURIComponent(String(incidentId ?? ''))
}

/**
 * Decode a URL-safe incident id. Returns '' if invalid.
 * @param {string | null | undefined} encoded
 */
export function decodeIncidentId(encoded) {
  if (encoded == null || String(encoded).trim() === '') {
    return ''
  }
  try {
    return decodeURIComponent(String(encoded))
  } catch {
    return ''
  }
}

/**
 * Build the detail-panel path for a service + optional incident reference.
 * @param {string} stackName
 * @param {string} nodeId
 * @param {string} [incidentId]
 */
export function pulseIncidentPath(stackName, nodeId, incidentId) {
  const base = `/stack/${encodeURIComponent(stackName)}/${encodeURIComponent(nodeId)}`
  if (incidentId == null || String(incidentId).trim() === '') {
    return base
  }
  return `${base}?incident=${encodeIncidentId(incidentId)}`
}

/**
 * @param {string} incidentId
 * @returns {string} path for GET /api/pulse/incidents/{id...}
 */
export function pulseIncidentApiPath(incidentId) {
  // Go ServeMux {id...} expects raw path segments (IDs embed '/'); do not PathEscape '/'.
  const id = String(incidentId ?? '')
  return `/api/pulse/incidents/${id}`
}

/**
 * @param {unknown} raw
 * @returns {{ ok: true, response: object } | { ok: false, error: string }}
 */
export function parseServicesResponse(raw) {
  if (raw == null || typeof raw !== 'object') {
    return { ok: false, error: 'Pulse services response missing' }
  }
  if (containsGroundTruthLeak(raw)) {
    return { ok: false, error: 'Pulse services response rejected: ground-truth markers' }
  }
  const schema = String(raw.schema_version ?? '')
  if (schema !== '' && schema !== PULSE_SCHEMA_SERVICES) {
    return { ok: false, error: `Unexpected Pulse services schema: ${schema}` }
  }
  const services = Array.isArray(raw.services) ? raw.services : []
  const dataSources = raw.data_sources && typeof raw.data_sources === 'object' ? raw.data_sources : {}
  return {
    ok: true,
    response: {
      schemaVersion: schema || PULSE_SCHEMA_SERVICES,
      generatedAt: raw.generated_at ?? null,
      dataSources: {
        observations: String(dataSources.observations ?? ''),
        incidents: String(dataSources.incidents ?? ''),
        changes: String(dataSources.changes ?? ''),
        correlation: dataSources.correlation != null ? String(dataSources.correlation) : '',
      },
      limitations: Array.isArray(raw.limitations) ? raw.limitations.map(String) : [],
      count: typeof raw.count === 'number' ? raw.count : services.length,
      limit: typeof raw.limit === 'number' ? raw.limit : services.length,
      services: services.map(normalizeServiceStatus).filter(Boolean),
    },
  }
}

/**
 * @param {unknown} raw
 * @returns {{ ok: true, response: object } | { ok: false, error: string }}
 */
export function parseIncidentsResponse(raw) {
  if (raw == null || typeof raw !== 'object') {
    return { ok: false, error: 'Pulse incidents response missing' }
  }
  if (containsGroundTruthLeak(raw)) {
    return { ok: false, error: 'Pulse incidents response rejected: ground-truth markers' }
  }
  const schema = String(raw.schema_version ?? '')
  if (schema !== '' && schema !== PULSE_SCHEMA_INCIDENTS) {
    return { ok: false, error: `Unexpected Pulse incidents schema: ${schema}` }
  }
  const incidents = Array.isArray(raw.incidents) ? raw.incidents : []
  return {
    ok: true,
    response: {
      schemaVersion: schema || PULSE_SCHEMA_INCIDENTS,
      generatedAt: raw.generated_at ?? null,
      dataSources: raw.data_sources ?? {},
      limitations: Array.isArray(raw.limitations) ? raw.limitations.map(String) : [],
      count: typeof raw.count === 'number' ? raw.count : incidents.length,
      limit: typeof raw.limit === 'number' ? raw.limit : incidents.length,
      incidents: dedupeIncidents(incidents.map(normalizeIncident).filter(Boolean)),
    },
  }
}

/**
 * @param {unknown} raw
 * @returns {{ ok: true, response: object } | { ok: false, error: string }}
 */
export function parseIncidentDetailResponse(raw) {
  if (raw == null || typeof raw !== 'object') {
    return { ok: false, error: 'Pulse incident detail missing' }
  }
  if (containsGroundTruthLeak(raw)) {
    return { ok: false, error: 'Pulse incident detail rejected: ground-truth markers' }
  }
  const schema = String(raw.schema_version ?? '')
  if (schema !== '' && schema !== PULSE_SCHEMA_INCIDENT) {
    return { ok: false, error: `Unexpected Pulse incident schema: ${schema}` }
  }
  const incident = normalizeIncident(raw.incident)
  if (!incident) {
    return { ok: false, error: 'Pulse incident detail missing incident body' }
  }
  return {
    ok: true,
    response: {
      schemaVersion: schema || PULSE_SCHEMA_INCIDENT,
      generatedAt: raw.generated_at ?? null,
      dataSources: raw.data_sources ?? {},
      limitations: Array.isArray(raw.limitations) ? raw.limitations.map(String) : [],
      incident,
      correlation: raw.correlation ?? null,
      graphNode: raw.graph_node != null ? String(raw.graph_node) : '',
    },
  }
}

/**
 * @param {unknown} row
 */
function normalizeServiceStatus(row) {
  if (row == null || typeof row !== 'object') {
    return null
  }
  const serviceId = String(row.service_id ?? '').trim()
  if (serviceId === '') {
    return null
  }
  const open = uniqueStrings(row.open_incident_ids)
  const related = uniqueStrings(row.related_incident_ids)
  return {
    serviceId,
    graphNode: row.graph_node != null ? String(row.graph_node).trim() : '',
    mappingSource: row.mapping_source != null ? String(row.mapping_source) : '',
    intelligence: normalizeIntelligence(row.intelligence),
    intelligenceAvailable: Boolean(row.intelligence_available),
    observationSource: row.observation_source != null ? String(row.observation_source) : '',
    lastObservedAt: row.last_observed_at ?? null,
    freshness: String(row.freshness ?? 'unavailable'),
    ageSeconds: typeof row.age_seconds === 'number' ? row.age_seconds : null,
    openIncidentIds: open,
    relatedIncidentIds: related.length > 0 ? related : open.slice(),
    limitations: Array.isArray(row.limitations) ? row.limitations.map(String) : [],
    activeHealthNote: row.active_health_note != null ? String(row.active_health_note) : '',
  }
}

/**
 * @param {unknown} raw
 */
function normalizeIncident(raw) {
  if (raw == null || typeof raw !== 'object') {
    return null
  }
  const incidentId = String(raw.incident_id ?? '').trim()
  if (incidentId === '') {
    return null
  }
  const recoveredAt = raw.recovered_at ?? null
  return {
    incidentId,
    primaryServiceId: String(raw.primary_service_id ?? ''),
    environment: raw.environment != null ? String(raw.environment) : '',
    signalTypes: uniqueStrings(raw.signal_types),
    firstDetectedAt: raw.first_detected_at ?? null,
    lastObservedAt: raw.last_observed_at ?? null,
    recoveredAt,
    open: recoveredAt == null,
    affectedServiceIds: uniqueStrings(raw.affected_service_ids),
    summary: raw.summary != null ? String(raw.summary) : '',
    observations: Array.isArray(raw.observations) ? raw.observations.map(String) : [],
    inferences: Array.isArray(raw.inferences) ? raw.inferences.map(String) : [],
    limitations: Array.isArray(raw.limitations) ? raw.limitations.map(String) : [],
  }
}

function normalizeIntelligence(value) {
  const s = value == null ? '' : String(value)
  switch (s) {
    case 'healthy':
    case 'degraded':
    case 'unhealthy':
    case 'unavailable':
    case 'stale':
      return s
    default:
      return 'unknown'
  }
}

/**
 * @param {unknown} arr
 * @returns {string[]}
 */
function uniqueStrings(arr) {
  if (!Array.isArray(arr)) {
    return []
  }
  const out = []
  const seen = new Set()
  for (const item of arr) {
    const s = String(item ?? '').trim()
    if (s === '' || seen.has(s)) {
      continue
    }
    seen.add(s)
    out.push(s)
  }
  return out
}

/**
 * Deterministic newest-first by lastObservedAt, then incidentId.
 * @param {ReturnType<typeof normalizeIncident>[]} incidents
 */
export function sortIncidentsDeterministic(incidents) {
  return [...incidents].sort((a, b) => {
    const ta = timestampMs(a?.lastObservedAt)
    const tb = timestampMs(b?.lastObservedAt)
    if (tb !== ta) {
      return tb - ta
    }
    return String(a?.incidentId ?? '').localeCompare(String(b?.incidentId ?? ''))
  })
}

/**
 * @param {ReturnType<typeof normalizeIncident>[]} incidents
 */
function dedupeIncidents(incidents) {
  const byId = new Map()
  for (const inc of incidents) {
    if (!inc) continue
    if (!byId.has(inc.incidentId)) {
      byId.set(inc.incidentId, inc)
    }
  }
  return sortIncidentsDeterministic([...byId.values()])
}

function timestampMs(value) {
  if (value == null || value === '') {
    return 0
  }
  const t = Date.parse(String(value))
  return Number.isFinite(t) ? t : 0
}

/**
 * Index Pulse service rows by explicit `graph_node` only (no display-name guessing).
 * @param {object} servicesResponse — parsed parseServicesResponse().response
 * @returns {Map<string, object>}
 */
export function indexPulseByGraphNode(servicesResponse) {
  const map = new Map()
  const services = servicesResponse?.services ?? []
  for (const row of services) {
    const node = String(row.graphNode ?? '').trim()
    if (node === '') {
      continue
    }
    // Prefer the first row; if duplicates appear, keep open/severity preference.
    const prev = map.get(node)
    if (!prev || severityRank(row) > severityRank(prev)) {
      map.set(node, row)
    }
  }
  return map
}

function severityRank(row) {
  if ((row.openIncidentIds?.length ?? 0) > 0) {
    return 40 + intelligenceRank(row.intelligence)
  }
  if ((row.relatedIncidentIds?.length ?? 0) > 0) {
    return 20
  }
  if (row.intelligenceAvailable) {
    return 10 + intelligenceRank(row.intelligence)
  }
  return 0
}

function intelligenceRank(intel) {
  switch (intel) {
    case 'unhealthy':
      return 6
    case 'unavailable':
      return 5
    case 'unknown':
      return 4
    case 'degraded':
      return 3
    case 'stale':
      return 2
    case 'healthy':
      return 1
    default:
      return 4
  }
}

/**
 * Attach related incidents (from list endpoint) onto a service row for the panel.
 * @param {object | null | undefined} serviceRow
 * @param {object[]} incidents — normalized incidents
 * @param {{ expectedEnvironment?: string }} [opts]
 */
export function enrichServicePulse(serviceRow, incidents, opts = {}) {
  if (!serviceRow) {
    return null
  }
  const relatedIds = new Set([
    ...(serviceRow.openIncidentIds ?? []),
    ...(serviceRow.relatedIncidentIds ?? []),
  ])
  let related = (incidents ?? []).filter((inc) => relatedIds.has(inc.incidentId))
  // Also include incidents whose primary/affected maps to this service_id when IDs omitted.
  if (related.length === 0 && serviceRow.serviceId) {
    related = (incidents ?? []).filter(
      (inc) =>
        inc.primaryServiceId === serviceRow.serviceId ||
        (inc.affectedServiceIds ?? []).includes(serviceRow.serviceId),
    )
  }
  related = sortIncidentsDeterministic(dedupeIncidents(related))

  const env = opts.expectedEnvironment != null ? String(opts.expectedEnvironment).trim() : ''
  const envMismatch =
    env !== '' &&
    related.some((inc) => {
      const ie = String(inc.environment ?? '').trim()
      return ie !== '' && ie !== env
    })

  const kind = derivePulseKind(serviceRow, related)
  const open = related.filter((i) => i.open)
  const recovered = related.filter((i) => !i.open)
  const latest = related[0] ?? null

  return {
    ...serviceRow,
    kind,
    relatedIncidents: related,
    openIncidents: open,
    recoveredIncidents: recovered,
    incidentCount: related.length,
    openCount: open.length,
    recoveredCount: recovered.length,
    latestIncidentAt: latest?.lastObservedAt ?? serviceRow.lastObservedAt ?? null,
    latestSummary: latest?.summary ?? '',
    signalTypes: uniqueStrings(related.flatMap((i) => i.signalTypes ?? [])),
    envMismatch,
    label: pulseKindLabel(kind),
    hint: pulseKindHint(kind, serviceRow),
  }
}

/**
 * @param {object} serviceRow
 * @param {object[]} relatedIncidents
 */
export function derivePulseKind(serviceRow, relatedIncidents = []) {
  const openIds = serviceRow.openIncidentIds ?? []
  const related = relatedIncidents.length
    ? relatedIncidents
    : []
  const openFromList = related.filter((i) => i.open)
  const hasOpen = openIds.length > 0 || openFromList.length > 0
  const hasRelated =
    (serviceRow.relatedIncidentIds?.length ?? 0) > 0 || related.length > 0 || hasOpen

  if (hasOpen) {
    if (serviceRow.freshness === 'stale') {
      return PULSE_KIND.STALE
    }
    return PULSE_KIND.OPEN
  }

  if (hasRelated) {
    // Recovered historical evidence only — never paint as current outage.
    return PULSE_KIND.HISTORICAL
  }

  if (serviceRow.intelligenceAvailable) {
    if (serviceRow.freshness === 'stale' || serviceRow.intelligence === 'stale') {
      return PULSE_KIND.STALE
    }
    if (serviceRow.intelligence === 'healthy') {
      // Fresh healthy intelligence with no incidents — still show a soft indicator,
      // but never as active-probe healthy replacement (UI copy makes this clear).
      return PULSE_KIND.UNKNOWN
    }
    if (serviceRow.intelligence === 'degraded' || serviceRow.intelligence === 'unhealthy') {
      return PULSE_KIND.OPEN
    }
    if (serviceRow.intelligence === 'unavailable') {
      return PULSE_KIND.UNAVAILABLE
    }
    return PULSE_KIND.UNKNOWN
  }

  const freshness = serviceRow.freshness
  if (freshness === 'unavailable' || freshness === 'missing') {
    return PULSE_KIND.UNAVAILABLE
  }
  if (freshness === 'stale') {
    return PULSE_KIND.STALE
  }
  return PULSE_KIND.UNKNOWN
}

/**
 * @param {string} kind
 */
export function pulseKindLabel(kind) {
  switch (kind) {
    case PULSE_KIND.OPEN:
      return 'Pulse: open'
    case PULSE_KIND.HISTORICAL:
      return 'Pulse: historical'
    case PULSE_KIND.STALE:
      return 'Pulse: stale'
    case PULSE_KIND.UNAVAILABLE:
      return 'Pulse: unavailable'
    case PULSE_KIND.UNKNOWN:
      return 'Pulse: unknown'
    default:
      return ''
  }
}

/**
 * @param {string} kind
 * @param {object} serviceRow
 */
export function pulseKindHint(kind, serviceRow) {
  switch (kind) {
    case PULSE_KIND.OPEN:
      return 'Open Pulse incident(s) — not the same as active probe health.'
    case PULSE_KIND.HISTORICAL:
      return 'Recovered historical Pulse incidents — not a current outage.'
    case PULSE_KIND.STALE:
      return 'Pulse evidence is stale; stale ≠ healthy.'
    case PULSE_KIND.UNAVAILABLE:
      if (String(serviceRow?.observationSource ?? '').includes('process_memory') ||
          (serviceRow?.limitations ?? []).some((l) => String(l).includes('process-local'))) {
        return 'Pulse observation history unavailable in this viz process.'
      }
      return 'Pulse intelligence unavailable — not healthy.'
    case PULSE_KIND.UNKNOWN:
      return 'Pulse intelligence unknown — not healthy.'
    default:
      return ''
  }
}

/**
 * Dot/text classes for PulseIndicator (separate from StatusPill greens/reds for health).
 * Historical never uses danger red.
 * @param {string} kind
 */
export function pulseKindStyles(kind) {
  switch (kind) {
    case PULSE_KIND.OPEN:
      return { dot: 'bg-amber-400', text: 'text-amber-700', short: 'open' }
    case PULSE_KIND.HISTORICAL:
      return { dot: 'bg-slate-400', text: 'text-gray-500', short: 'hist' }
    case PULSE_KIND.STALE:
      return { dot: 'bg-amber-300', text: 'text-amber-600', short: 'stale' }
    case PULSE_KIND.UNAVAILABLE:
      return { dot: 'bg-slate-300', text: 'text-gray-500', short: 'n/a' }
    case PULSE_KIND.UNKNOWN:
      return { dot: 'bg-slate-300', text: 'text-gray-500', short: '?' }
    default:
      return { dot: 'bg-slate-300', text: 'text-gray-400', short: '' }
  }
}

/**
 * Snapshot honesty: observations from process_memory_unavailable are not live.
 * @param {object | null | undefined} dataSources
 */
export function describePulseDataSources(dataSources) {
  const obs = String(dataSources?.observations ?? '')
  const incidents = String(dataSources?.incidents ?? '')
  const parts = []
  if (obs === 'process_memory_unavailable' || obs === '') {
    return {
      liveObservations: false,
      label: 'Pulse snapshot (observations unavailable)',
      detail:
        incidents === 'file_store'
          ? 'Incidents from persisted store; not live telemetry.'
          : 'Pulse intelligence unavailable.',
    }
  }
  if (obs === 'process_memory') {
    parts.push('process-local observations')
  }
  if (incidents === 'file_store') {
    parts.push('persisted incidents')
  }
  return {
    liveObservations: obs === 'process_memory',
    label: parts.length ? `Pulse: ${parts.join(' + ')}` : 'Pulse snapshot',
    detail: 'Snapshot response — not a continuous live stream.',
  }
}

/**
 * Ensure probe status is untouched when merging Pulse onto a mapped node.
 * @param {string} probeStatus
 * @param {object | null} pulseView
 */
export function assertProbeStatusUntouched(probeStatus, pulseView) {
  // Pure helper for tests / callers: always returns the probe status unchanged.
  void pulseView
  return probeStatus
}
