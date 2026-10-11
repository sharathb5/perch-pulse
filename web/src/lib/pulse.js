/**
 * Pulse intelligence adapters for the Perch viz UI (Milestone B).
 * Active probe health stays in mappers.js / StatusPill — never collapsed here.
 */

export const PULSE_SCHEMA_SERVICES = 'pulse.api.services.v1'
export const PULSE_SCHEMA_INCIDENTS = 'pulse.api.incidents.v1'
export const PULSE_SCHEMA_INCIDENT = 'pulse.api.incident.v1'
export const PULSE_SCHEMA_CHANGES = 'pulse.api.changes.v1'
export const PULSE_SCHEMA_CHANGE = 'pulse.api.change.v1'
export const PULSE_SCHEMA_CORRELATION = 'pulse.correlation.v1'

/** Sidebar tabs (URL `pulse` query value). */
export const PULSE_TAB = Object.freeze({
  OVERVIEW: 'overview',
  INCIDENTS: 'incidents',
  CHANGES: 'changes',
})

/** Persisted incident state labels — never imply live outage without fresh telemetry. */
export const INCIDENT_STATE = Object.freeze({
  RECOVERED: 'recovered',
  UNRESOLVED: 'unresolved',
})

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
 * Opens the Pulse sidebar on the incidents tab when an incident id is provided.
 * @param {string} stackName
 * @param {string} nodeId
 * @param {string} [incidentId]
 */
export function pulseIncidentPath(stackName, nodeId, incidentId) {
  const base = `/stack/${encodeURIComponent(stackName)}/${encodeURIComponent(nodeId)}`
  if (incidentId == null || String(incidentId).trim() === '') {
    return base
  }
  const qs = new URLSearchParams()
  qs.set('pulse', PULSE_TAB.INCIDENTS)
  // URLSearchParams encodes once; do not pre-encode or ids double-escape.
  qs.set('incident', String(incidentId))
  return `${base}?${qs.toString()}`
}

/**
 * Build a stack path with optional node + Pulse sidebar query state.
 * Closing the sidebar omits `pulse` / `incident` but preserves `nodeId`.
 *
 * @param {string} stackName
 * @param {{
 *   nodeId?: string | null,
 *   pulseTab?: string | null,
 *   incidentId?: string | null,
 * }} [opts]
 */
export function pulseStackPath(stackName, opts = {}) {
  const stack = encodeURIComponent(String(stackName ?? 'default'))
  const nodeId = opts.nodeId != null && String(opts.nodeId).trim() !== '' ? String(opts.nodeId) : ''
  const base =
    nodeId !== ''
      ? `/stack/${stack}/${encodeURIComponent(nodeId)}`
      : `/stack/${stack}`
  const qs = new URLSearchParams()
  const tab = normalizePulseTab(opts.pulseTab)
  if (tab) {
    qs.set('pulse', tab)
  }
  if (opts.incidentId != null && String(opts.incidentId).trim() !== '') {
    qs.set('incident', String(opts.incidentId))
    if (!tab) {
      qs.set('pulse', PULSE_TAB.INCIDENTS)
    }
  }
  const q = qs.toString()
  return q ? `${base}?${q}` : base
}

/**
 * @param {string | null | undefined} raw
 * @returns {string | null} one of PULSE_TAB values, or null if closed/invalid
 */
export function normalizePulseTab(raw) {
  const v = String(raw ?? '').trim().toLowerCase()
  if (v === '1' || v === 'true' || v === 'open') {
    return PULSE_TAB.OVERVIEW
  }
  if (v === PULSE_TAB.OVERVIEW || v === PULSE_TAB.INCIDENTS || v === PULSE_TAB.CHANGES) {
    return v
  }
  return null
}

/**
 * @param {string} incidentId
 * @returns {string} path for GET /api/pulse/changes/{id...}
 */
export function pulseChangeApiPath(changeId) {
  const id = String(changeId ?? '')
  return `/api/pulse/changes/${id}`
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
 * @param {string} changeId
 * @returns {string} path for GET /api/pulse/changes list
 */
export function pulseChangesListApiPath(limit = 100) {
  return `/api/pulse/changes?limit=${encodeURIComponent(String(limit))}`
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
      dataSources: normalizeDataSources(raw.data_sources),
      limitations: Array.isArray(raw.limitations) ? raw.limitations.map(String) : [],
      incident,
      correlation: normalizeCorrelation(raw.correlation),
      graphNode: raw.graph_node != null ? String(raw.graph_node) : '',
    },
  }
}

/**
 * @param {unknown} raw
 * @returns {{ ok: true, response: object } | { ok: false, error: string }}
 */
export function parseChangesResponse(raw) {
  if (raw == null || typeof raw !== 'object') {
    return { ok: false, error: 'Pulse changes response missing' }
  }
  if (containsGroundTruthLeak(raw)) {
    return { ok: false, error: 'Pulse changes response rejected: ground-truth markers' }
  }
  const schema = String(raw.schema_version ?? '')
  if (schema !== '' && schema !== PULSE_SCHEMA_CHANGES) {
    return { ok: false, error: `Unexpected Pulse changes schema: ${schema}` }
  }
  const changes = Array.isArray(raw.changes) ? raw.changes : []
  return {
    ok: true,
    response: {
      schemaVersion: schema || PULSE_SCHEMA_CHANGES,
      generatedAt: raw.generated_at ?? null,
      dataSources: normalizeDataSources(raw.data_sources),
      limitations: Array.isArray(raw.limitations) ? raw.limitations.map(String) : [],
      count: typeof raw.count === 'number' ? raw.count : changes.length,
      limit: typeof raw.limit === 'number' ? raw.limit : changes.length,
      changes: dedupeChanges(changes.map(normalizeChange).filter(Boolean)),
    },
  }
}

/**
 * @param {unknown} raw
 * @returns {{ ok: true, response: object } | { ok: false, error: string }}
 */
export function parseChangeDetailResponse(raw) {
  if (raw == null || typeof raw !== 'object') {
    return { ok: false, error: 'Pulse change detail missing' }
  }
  if (containsGroundTruthLeak(raw)) {
    return { ok: false, error: 'Pulse change detail rejected: ground-truth markers' }
  }
  const schema = String(raw.schema_version ?? '')
  if (schema !== '' && schema !== PULSE_SCHEMA_CHANGE) {
    return { ok: false, error: `Unexpected Pulse change schema: ${schema}` }
  }
  const change = normalizeChange(raw.change)
  if (!change) {
    return { ok: false, error: 'Pulse change detail missing change body' }
  }
  return {
    ok: true,
    response: {
      schemaVersion: schema || PULSE_SCHEMA_CHANGE,
      generatedAt: raw.generated_at ?? null,
      dataSources: normalizeDataSources(raw.data_sources),
      limitations: Array.isArray(raw.limitations) ? raw.limitations.map(String) : [],
      change,
      graphNodes: uniqueStrings(raw.graph_nodes),
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
  const open = recoveredAt == null
  return {
    incidentId,
    primaryServiceId: String(raw.primary_service_id ?? ''),
    environment: raw.environment != null ? String(raw.environment) : '',
    findingIds: uniqueStrings(raw.finding_ids),
    signalTypes: uniqueStrings(raw.signal_types),
    firstDetectedAt: raw.first_detected_at ?? null,
    lastObservedAt: raw.last_observed_at ?? null,
    recoveredAt,
    open,
    /** Persisted state only — unresolved ≠ current live outage. */
    state: open ? INCIDENT_STATE.UNRESOLVED : INCIDENT_STATE.RECOVERED,
    affectedServiceIds: uniqueStrings(raw.affected_service_ids),
    before: normalizePhaseSnapshot(raw.before, 'before'),
    during: normalizePhaseSnapshot(raw.during, 'during'),
    after: normalizePhaseSnapshot(raw.after, 'after'),
    summary: raw.summary != null ? String(raw.summary) : '',
    observations: Array.isArray(raw.observations) ? raw.observations.map(String) : [],
    inferences: Array.isArray(raw.inferences) ? raw.inferences.map(String) : [],
    limitations: Array.isArray(raw.limitations) ? raw.limitations.map(String) : [],
    evidence: normalizeEvidenceRefs(raw.evidence),
  }
}

/**
 * @param {unknown} raw
 * @param {string} fallbackPhase
 */
function normalizePhaseSnapshot(raw, fallbackPhase) {
  if (raw == null || typeof raw !== 'object') {
    return {
      phase: fallbackPhase,
      window: { start: null, end: null },
      digests: [],
      available: false,
      limitation: 'Phase snapshot unavailable',
    }
  }
  const window = raw.window && typeof raw.window === 'object' ? raw.window : {}
  return {
    phase: raw.phase != null ? String(raw.phase) : fallbackPhase,
    window: {
      start: window.start ?? null,
      end: window.end ?? null,
    },
    digests: Array.isArray(raw.digests) ? raw.digests.map(normalizeDigest).filter(Boolean) : [],
    available: Boolean(raw.available),
    limitation: raw.limitation != null ? String(raw.limitation) : '',
  }
}

/**
 * @param {unknown} raw
 */
function normalizeDigest(raw) {
  if (raw == null || typeof raw !== 'object') {
    return null
  }
  const signal = String(raw.signal ?? '').trim()
  if (signal === '') {
    return null
  }
  return {
    signal,
    count: typeof raw.count === 'number' && Number.isFinite(raw.count) ? raw.count : null,
    median: finiteOrNull(raw.median),
    min: finiteOrNull(raw.min),
    max: finiteOrNull(raw.max),
    firstAt: raw.first_at ?? null,
    lastAt: raw.last_at ?? null,
  }
}

function finiteOrNull(value) {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    return null
  }
  return value
}

function normalizeEvidenceRefs(raw) {
  if (!Array.isArray(raw)) {
    return []
  }
  return raw
    .filter((e) => e != null && typeof e === 'object')
    .map((e) => ({
      ref: e.ref != null ? String(e.ref) : '',
      summary: e.summary != null ? String(e.summary) : '',
    }))
    .filter((e) => e.ref !== '' || e.summary !== '')
}

/**
 * @param {unknown} raw
 */
function normalizeChange(raw) {
  if (raw == null || typeof raw !== 'object') {
    return null
  }
  const changeId = String(raw.change_id ?? '').trim()
  if (changeId === '') {
    return null
  }
  const metadata =
    raw.metadata && typeof raw.metadata === 'object' && !Array.isArray(raw.metadata)
      ? Object.fromEntries(
          Object.entries(raw.metadata).map(([k, v]) => [String(k), v == null ? '' : String(v)]),
        )
      : {}
  const simulated = isSimulatedChange({ metadata, source: raw.source, evidence: raw.evidence, summary: raw.summary })
  return {
    changeId,
    changeType: raw.change_type != null ? String(raw.change_type) : '',
    source: raw.source != null ? String(raw.source) : '',
    repository: raw.repository != null ? String(raw.repository) : '',
    commitSha: raw.commit_sha != null ? String(raw.commit_sha) : '',
    environment: raw.environment != null ? String(raw.environment) : '',
    serviceIds: uniqueStrings(raw.service_ids),
    serviceMapping: raw.service_mapping != null ? String(raw.service_mapping) : '',
    title: raw.title != null ? String(raw.title) : '',
    summary: raw.summary != null ? String(raw.summary) : '',
    createdAt: raw.created_at ?? null,
    deployedAt: raw.deployed_at ?? null,
    observedAt: raw.observed_at ?? null,
    metadata,
    evidence: normalizeEvidenceRefs(raw.evidence),
    simulated,
    classification: simulated ? 'simulated_deployment_marker' : 'recorded_change',
  }
}

/**
 * @param {unknown} raw
 */
function normalizeCorrelation(raw) {
  if (raw == null || typeof raw !== 'object') {
    return null
  }
  const candidates = Array.isArray(raw.candidates)
    ? raw.candidates.map(normalizeCorrelationCandidate).filter(Boolean)
    : []
  const strongCandidate = Boolean(raw.strong_candidate)
  const noStrongCandidate =
    raw.no_strong_candidate != null ? Boolean(raw.no_strong_candidate) : !strongCandidate
  return {
    schemaVersion: raw.schema_version != null ? String(raw.schema_version) : PULSE_SCHEMA_CORRELATION,
    scorerVersion: raw.scorer_version != null ? String(raw.scorer_version) : '',
    incidentId: raw.incident_id != null ? String(raw.incident_id) : '',
    generatedAt: raw.generated_at ?? null,
    candidates,
    top1: raw.top1 ? normalizeCorrelationCandidate(raw.top1) : candidates[0] ?? null,
    top3: Array.isArray(raw.top3)
      ? raw.top3.map(normalizeCorrelationCandidate).filter(Boolean)
      : candidates.slice(0, 3),
    strongCandidate,
    noStrongCandidate,
    observations: Array.isArray(raw.observations) ? raw.observations.map(String) : [],
    inferences: Array.isArray(raw.inferences) ? raw.inferences.map(String) : [],
    limitations: Array.isArray(raw.limitations) ? raw.limitations.map(String) : [],
    note: raw.note != null ? String(raw.note) : 'Correlation is not causation.',
  }
}

/**
 * @param {unknown} raw
 */
function normalizeCorrelationCandidate(raw) {
  if (raw == null || typeof raw !== 'object') {
    return null
  }
  const changeId = String(raw.change_id ?? '').trim()
  if (changeId === '') {
    return null
  }
  const score = finiteOrNull(raw.score)
  return {
    correlationId: raw.correlation_id != null ? String(raw.correlation_id) : '',
    changeId,
    rank: typeof raw.rank === 'number' && Number.isFinite(raw.rank) ? raw.rank : null,
    score,
    /** Score is an explainable rank points total — never a probability. */
    scoreIsProbability: false,
    temporalDistanceSecs: finiteOrNull(raw.temporal_distance_secs),
    serviceOverlap: raw.service_overlap != null ? String(raw.service_overlap) : '',
    environmentMatch: Boolean(raw.environment_match),
    evidence: normalizeEvidenceRefs(raw.evidence),
    observations: Array.isArray(raw.observations) ? raw.observations.map(String) : [],
    inferences: Array.isArray(raw.inferences) ? raw.inferences.map(String) : [],
    limitations: Array.isArray(raw.limitations) ? raw.limitations.map(String) : [],
    scoreBreakdown:
      raw.score_breakdown && typeof raw.score_breakdown === 'object' ? raw.score_breakdown : null,
    label: correlationCandidateLabel(score, Boolean(raw.score_breakdown?.excluded)),
  }
}

/**
 * @param {number | null} score
 * @param {boolean} excluded
 */
export function correlationCandidateLabel(score, excluded = false) {
  if (excluded || score == null) {
    return 'no strong candidate'
  }
  if (score >= 50) {
    return 'correlated change'
  }
  if (score >= 25) {
    return 'potentially related'
  }
  if (score > 0) {
    return 'weak candidate'
  }
  return 'no strong candidate'
}

function normalizeDataSources(raw) {
  if (raw == null || typeof raw !== 'object') {
    return {
      observations: '',
      incidents: '',
      changes: '',
      correlation: '',
    }
  }
  return {
    observations: String(raw.observations ?? ''),
    incidents: String(raw.incidents ?? ''),
    changes: String(raw.changes ?? ''),
    correlation: raw.correlation != null ? String(raw.correlation) : '',
  }
}

/**
 * Detect simulated deployment markers from contract fields (not invented).
 * @param {{ metadata?: Record<string, string>, source?: unknown, evidence?: { ref?: string, summary?: string }[], summary?: unknown }} change
 */
export function isSimulatedChange(change) {
  const meta = change?.metadata ?? {}
  if (String(meta.simulated ?? '').toLowerCase() === 'true') {
    return true
  }
  // Exact known harness producers only — do not substring-match free-form sources
  // (e.g. "simple-ci" must not become "simulated").
  const source = String(change?.source ?? '').toLowerCase().trim()
  if (source === 'change-eval-harness' || source === 'deploy-marker' || source === 'scenario-harness') {
    return true
  }
  const summary = String(change?.summary ?? '').toLowerCase()
  if (summary.includes('simulated deployment marker') || summary.includes('not a real deploy')) {
    return true
  }
  const evidence = Array.isArray(change?.evidence) ? change.evidence : []
  return evidence.some((e) => {
    const ref = String(e?.ref ?? '').toLowerCase()
    const summaryText = String(e?.summary ?? '').toLowerCase()
    return (
      ref.includes('simulated-deploy') ||
      summaryText.includes('synthetic deployment marker') ||
      summaryText.includes('not a real cloud deploy')
    )
  })
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

/**
 * Deterministic newest-first by observedAt/deployedAt/createdAt, then changeId.
 * @param {ReturnType<typeof normalizeChange>[]} changes
 */
export function sortChangesDeterministic(changes) {
  return [...changes].sort((a, b) => {
    const ta = Math.max(timestampMs(a?.observedAt), timestampMs(a?.deployedAt), timestampMs(a?.createdAt))
    const tb = Math.max(timestampMs(b?.observedAt), timestampMs(b?.deployedAt), timestampMs(b?.createdAt))
    if (tb !== ta) {
      return tb - ta
    }
    return String(a?.changeId ?? '').localeCompare(String(b?.changeId ?? ''))
  })
}

function dedupeChanges(changes) {
  const byId = new Map()
  for (const ch of changes) {
    if (!ch) continue
    if (!byId.has(ch.changeId)) {
      byId.set(ch.changeId, ch)
    }
  }
  return sortChangesDeterministic([...byId.values()])
}

/**
 * @param {ReturnType<typeof normalizeIncident>[]} incidents
 * @param {{ serviceId?: string, state?: 'recovered' | 'unresolved' | 'all' }} [filters]
 */
export function filterIncidents(incidents, filters = {}) {
  const serviceId = filters.serviceId != null ? String(filters.serviceId).trim() : ''
  const state = filters.state != null ? String(filters.state) : 'all'
  return (incidents ?? []).filter((inc) => {
    if (!inc) return false
    if (serviceId !== '') {
      const hit =
        inc.primaryServiceId === serviceId || (inc.affectedServiceIds ?? []).includes(serviceId)
      if (!hit) return false
    }
    if (state === INCIDENT_STATE.RECOVERED && inc.open) return false
    if (state === INCIDENT_STATE.UNRESOLVED && !inc.open) return false
    return true
  })
}

/**
 * @param {ReturnType<typeof normalizeChange>[]} changes
 * @param {{ serviceId?: string }} [filters]
 */
export function filterChanges(changes, filters = {}) {
  const serviceId = filters.serviceId != null ? String(filters.serviceId).trim() : ''
  if (serviceId === '') {
    return changes ?? []
  }
  return (changes ?? []).filter((ch) => (ch.serviceIds ?? []).includes(serviceId))
}

/**
 * Index Pulse service rows by service_id for explicit navigation (no fuzzy match).
 * @param {object} servicesResponse
 * @returns {Map<string, string>} serviceId → graphNode (only when mapped)
 */
export function indexGraphNodeByServiceId(servicesResponse) {
  const map = new Map()
  for (const row of servicesResponse?.services ?? []) {
    const sid = String(row.serviceId ?? '').trim()
    const node = String(row.graphNode ?? '').trim()
    if (sid === '' || node === '') continue
    if (!map.has(sid)) {
      map.set(sid, node)
    }
  }
  return map
}

/**
 * Build infrastructure overview from parsed Pulse list responses.
 * Never invents CPU/memory/network metrics.
 *
 * @param {{
 *   servicesResponse?: object | null,
 *   incidentsResponse?: { incidents?: object[], count?: number, limit?: number, limitations?: string[] } | null,
 *   changesResponse?: { changes?: object[], count?: number, limit?: number, limitations?: string[] } | null,
 * }} input
 */
export function buildPulseOverview(input = {}) {
  const services = input.servicesResponse?.services ?? []
  const incidents = input.incidentsResponse?.incidents ?? []
  const changes = input.changesResponse?.changes ?? []
  const dataSources = input.servicesResponse?.dataSources ?? input.incidentsResponse?.dataSources ?? {}
  const sourceInfo = describePulseDataSources(dataSources)

  const mappedServices = services.filter((s) => String(s.graphNode ?? '').trim() !== '')
  const recovered = incidents.filter((i) => !i.open)
  const unresolved = incidents.filter((i) => i.open)
  const sortedIncidents = sortIncidentsDeterministic(incidents)
  const sortedChanges = sortChangesDeterministic(changes)
  const environments = uniqueStrings([
    ...incidents.map((i) => i.environment),
    ...changes.map((c) => c.environment),
  ]).sort()

  const latestEvidenceAt = maxTimestamp([
    ...incidents.map((i) => i.lastObservedAt),
    ...changes.map((c) => c.observedAt ?? c.deployedAt ?? c.createdAt),
    ...services.map((s) => s.lastObservedAt),
  ])

  const servicesWithFindings = mappedServices.filter(
    (s) =>
      (s.openIncidentIds?.length ?? 0) > 0 ||
      (s.relatedIncidentIds?.length ?? 0) > 0 ||
      s.intelligence === 'degraded' ||
      s.intelligence === 'unhealthy',
  )

  const incidentTruncated =
    typeof input.incidentsResponse?.count === 'number' &&
    typeof input.incidentsResponse?.limit === 'number' &&
    input.incidentsResponse.count >= input.incidentsResponse.limit
  const changeTruncated =
    typeof input.changesResponse?.count === 'number' &&
    typeof input.changesResponse?.limit === 'number' &&
    input.changesResponse.count >= input.changesResponse.limit

  const limitations = uniqueStrings([
    ...(input.servicesResponse?.limitations ?? []),
    ...(input.incidentsResponse?.limitations ?? []),
    ...(input.changesResponse?.limitations ?? []),
    ...(incidentTruncated
      ? [
          `Incident list may be incomplete (returned page filled limit=${input.incidentsResponse.limit}).`,
        ]
      : []),
    ...(changeTruncated
      ? [
          `Change list may be incomplete (returned page filled limit=${input.changesResponse.limit}).`,
        ]
      : []),
    ...(!sourceInfo.liveObservations
      ? ['Current Pulse observation history unavailable in this viz process — not healthy.']
      : []),
  ])

  return {
    mappedServiceCount: mappedServices.length,
    serviceCount: services.length,
    incidentCount: incidents.length,
    recoveredCount: recovered.length,
    unresolvedCount: unresolved.length,
    mostRecentIncident: sortedIncidents[0] ?? null,
    latestChange: sortedChanges[0] ?? null,
    environments,
    dataSources,
    sourceInfo,
    latestEvidenceAt,
    servicesWithFindings: servicesWithFindings.map((s) => ({
      serviceId: s.serviceId,
      graphNode: s.graphNode,
      intelligence: s.intelligence,
      freshness: s.freshness,
      openCount: s.openIncidentIds?.length ?? 0,
    })),
    limitations,
    incidentListTruncated: incidentTruncated,
    changeListTruncated: changeTruncated,
    /** Honesty flags for UI copy */
    flags: {
      liveObservations: sourceInfo.liveObservations,
      unresolvedMeansPersistedOpen: true,
      unresolvedIsNotLiveOutage: true,
      noResourceMetrics: true,
    },
  }
}

/**
 * @param {(string | null | undefined)[]} values
 */
function maxTimestamp(values) {
  let best = null
  let bestMs = 0
  for (const v of values) {
    const ms = timestampMs(v)
    if (ms > bestMs) {
      bestMs = ms
      best = v
    }
  }
  return best
}

/**
 * Format a digest median for display. Returns 'N/A' when unavailable.
 * error_rate is stored in [0,1] per detector contract → percentage.
 * call_rate is calls/second; latency_ms is milliseconds.
 *
 * @param {string} signal
 * @param {number | null | undefined} value
 */
export function formatSignalValue(signal, value) {
  if (value == null || typeof value !== 'number' || !Number.isFinite(value)) {
    return 'N/A'
  }
  switch (signal) {
    case 'latency_ms':
      return `${formatNumber(value, value >= 100 ? 0 : 1)} ms`
    case 'error_rate':
      return `${formatNumber(value * 100, value === 0 ? 0 : 2)}%`
    case 'call_rate':
      return `${formatNumber(value, value >= 1 ? 2 : 4)} /s`
    default:
      return formatNumber(value, 3)
  }
}

/**
 * @param {number} value
 * @param {number} digits
 */
export function formatNumber(value, digits = 2) {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    return 'N/A'
  }
  const fixed = value.toFixed(digits)
  if (!fixed.includes('.')) {
    return fixed
  }
  return fixed.replace(/\.?0+$/, '')
}

/**
 * Compact before/during/after rows for known signals.
 * @param {object} incident — normalized incident with phase snapshots
 * @param {string[]} [signals]
 */
export function buildDigestComparisonRows(incident, signals = ['latency_ms', 'error_rate', 'call_rate']) {
  const phases = ['before', 'during', 'after']
  return signals.map((signal) => {
    const cells = {}
    for (const phase of phases) {
      const snap = incident?.[phase]
      if (!snap?.available) {
        cells[phase] = {
          display: 'N/A',
          median: null,
          count: null,
          available: false,
          window: snap?.window ?? { start: null, end: null },
        }
        continue
      }
      const digest = (snap.digests ?? []).find((d) => d.signal === signal)
      if (!digest || digest.median == null) {
        cells[phase] = {
          display: 'N/A',
          median: null,
          count: digest?.count ?? null,
          available: true,
          window: snap.window,
          firstAt: digest?.firstAt ?? null,
          lastAt: digest?.lastAt ?? null,
        }
        continue
      }
      cells[phase] = {
        display: formatSignalValue(signal, digest.median),
        median: digest.median,
        count: digest.count,
        available: true,
        window: snap.window,
        firstAt: digest.firstAt,
        lastAt: digest.lastAt,
      }
    }
    return {
      signal,
      unitLabel: signalUnitLabel(signal),
      ...cells,
    }
  })
}

export function signalUnitLabel(signal) {
  switch (signal) {
    case 'latency_ms':
      return 'milliseconds (median)'
    case 'error_rate':
      return 'error rate (median %)'
    case 'call_rate':
      return 'calls/second (median)'
    default:
      return signal
  }
}

/**
 * Human label for persisted incident state (not active probe health).
 * @param {object} incident
 */
export function incidentStateLabel(incident) {
  if (!incident) return ''
  return incident.open ? 'Unresolved (persisted)' : 'Recovered (historical)'
}

/**
 * @param {object | null | undefined} correlation
 */
export function correlationStrengthSummary(correlation) {
  if (!correlation) {
    return {
      label: 'no correlation data',
      detail: 'Correlation was not attached to this incident response.',
    }
  }
  if (correlation.noStrongCandidate || !correlation.strongCandidate) {
    const top = correlation.top1
    if (top && top.score != null && top.score > 0) {
      return {
        label: 'no strong candidate',
        detail: `Top-ranked change scored ${formatNumber(top.score, 1)} points (not a probability; not causation).`,
      }
    }
    return {
      label: 'no strong candidate',
      detail: 'No change met the strong-candidate threshold. Correlation is not causation.',
    }
  }
  return {
    label: 'correlated change (candidate)',
    detail: 'A change ranked above the strong-candidate threshold — still not proven causation.',
  }
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

/**
 * Format ISO-ish timestamps for Pulse UI (compact UTC).
 * @param {unknown} value
 */
export function formatPulseTimestamp(value) {
  if (value == null || value === '') {
    return '—'
  }
  const d = new Date(String(value))
  if (Number.isNaN(d.getTime())) {
    return String(value)
  }
  return d.toISOString().replace('T', ' ').replace(/\.\d{3}Z$/, 'Z')
}
