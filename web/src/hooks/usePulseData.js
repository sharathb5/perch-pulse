import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  indexLiveByGraphNode,
  liveCollectorCurrent,
  mergeIncidentsPreferLive,
  parseLiveIncidentsResponse,
  parseLiveServicesResponse,
  parseLiveStatusResponse,
  unmappedLiveServices,
} from '../lib/livePulse.js'
import {
  buildPulseOverview,
  enrichServicePulse,
  indexGraphNodeByServiceId,
  indexPulseByGraphNode,
  parseChangesResponse,
  parseIncidentsResponse,
  parseServicesResponse,
} from '../lib/pulse.js'

const POLL_MS = 8_000
const FETCH_TIMEOUT_MS = 8_000

/**
 * @param {string} url
 * @param {AbortSignal} signal
 */
async function fetchJson(url, signal) {
  const res = await fetch(url, { headers: { Accept: 'application/json' }, signal })
  const text = await res.text()
  let body
  try {
    body = text ? JSON.parse(text) : null
  } catch {
    body = null
  }
  if (!res.ok) {
    const msg = body?.error ?? body?.code ?? res.statusText ?? 'request failed'
    throw new Error(typeof msg === 'string' ? msg : JSON.stringify(msg))
  }
  return body
}

/**
 * Bounded poll of Pulse read-only APIs. Independent from graph/status health.
 * Failures never invent incidents or healthy intelligence.
 *
 * @param {{ enabled?: boolean }} [opts]
 */
export function usePulseData(opts = {}) {
  const enabled = opts.enabled !== false
  const [byGraphNode, setByGraphNode] = useState(() => new Map())
  const [servicesResponse, setServicesResponse] = useState(null)
  const [incidentsResponse, setIncidentsResponse] = useState(null)
  const [changesResponse, setChangesResponse] = useState(null)
  const [incidents, setIncidents] = useState([])
  const [changes, setChanges] = useState([])
  const [liveStatus, setLiveStatus] = useState(null)
  const [liveServicesResponse, setLiveServicesResponse] = useState(null)
  const [liveIncidents, setLiveIncidents] = useState([])
  const [liveByGraphNode, setLiveByGraphNode] = useState(() => new Map())
  const [dataSources, setDataSources] = useState(null)
  const [limitations, setLimitations] = useState([])
  const [generatedAt, setGeneratedAt] = useState(null)
  const [lastSuccessAt, setLastSuccessAt] = useState(null)
  const [loading, setLoading] = useState(enabled)
  const [error, setError] = useState(null)
  const [staleSnapshot, setStaleSnapshot] = useState(false)
  const mounted = useRef(true)
  const inFlight = useRef(false)

  const load = useCallback(async () => {
    if (!enabled) {
      return
    }
    if (inFlight.current) {
      return
    }
    inFlight.current = true
    const controller = new AbortController()
    const timer = window.setTimeout(() => controller.abort(), FETCH_TIMEOUT_MS)
    try {
      // Historical + Live Pulse are independently best-effort (GET-only). Changes optional.
      const [servicesSettled, incidentsSettled, changesSettled, liveStatusSettled, liveServicesSettled, liveIncidentsSettled] =
        await Promise.all([
          fetchJson('/api/pulse/services?limit=200', controller.signal).then(
            (body) => ({ ok: true, body }),
            (err) => ({ ok: false, error: err }),
          ),
          fetchJson('/api/pulse/incidents?limit=100', controller.signal).then(
            (body) => ({ ok: true, body }),
            (err) => ({ ok: false, error: err }),
          ),
          fetchJson('/api/pulse/changes?limit=100', controller.signal).then(
            (body) => ({ ok: true, body }),
            (err) => ({ ok: false, error: err }),
          ),
          fetchJson('/api/pulse/live/status', controller.signal).then(
            (body) => ({ ok: true, body }),
            (err) => ({ ok: false, error: err }),
          ),
          fetchJson('/api/pulse/live/services', controller.signal).then(
            (body) => ({ ok: true, body }),
            (err) => ({ ok: false, error: err }),
          ),
          fetchJson('/api/pulse/live/incidents', controller.signal).then(
            (body) => ({ ok: true, body }),
            (err) => ({ ok: false, error: err }),
          ),
        ])
      if (!mounted.current) {
        return
      }

      let servicesParsed = null
      let incidentsParsed = null
      const histWarnings = []
      if (servicesSettled.ok) {
        servicesParsed = parseServicesResponse(servicesSettled.body)
        if (!servicesParsed.ok) {
          histWarnings.push(servicesParsed.error)
          servicesParsed = null
        }
      } else {
        histWarnings.push(
          servicesSettled.error instanceof Error
            ? servicesSettled.error.message
            : 'historical services unavailable',
        )
      }
      if (incidentsSettled.ok) {
        incidentsParsed = parseIncidentsResponse(incidentsSettled.body)
        if (!incidentsParsed.ok) {
          histWarnings.push(incidentsParsed.error)
          incidentsParsed = null
        }
      } else {
        histWarnings.push(
          incidentsSettled.error instanceof Error
            ? incidentsSettled.error.message
            : 'historical incidents unavailable',
        )
      }
      // Need at least live OR historical to update; total failure keeps prior snapshot.
      // (Live continues even when historical Pulse store fails.)

      let changesParsed = null
      let changesWarning = ''
      if (changesSettled.ok) {
        changesParsed = parseChangesResponse(changesSettled.body)
        if (!changesParsed.ok) {
          changesWarning = changesParsed.error
          changesParsed = null
        }
      } else {
        const err = changesSettled.error
        changesWarning =
          err instanceof Error ? err.message : err != null ? String(err) : 'changes unavailable'
      }

      let liveStatusParsed = null
      let liveServicesParsed = null
      let liveIncidentsParsed = null
      const liveWarnings = []
      if (liveStatusSettled.ok) {
        liveStatusParsed = parseLiveStatusResponse(liveStatusSettled.body)
        if (!liveStatusParsed.ok) {
          liveWarnings.push(liveStatusParsed.error)
          liveStatusParsed = null
        }
      } else {
        liveWarnings.push('Live status unavailable this refresh')
      }
      if (liveServicesSettled.ok) {
        liveServicesParsed = parseLiveServicesResponse(liveServicesSettled.body)
        if (!liveServicesParsed.ok) {
          liveWarnings.push(liveServicesParsed.error)
          liveServicesParsed = null
        }
      }
      if (liveIncidentsSettled.ok) {
        liveIncidentsParsed = parseLiveIncidentsResponse(liveIncidentsSettled.body)
        if (!liveIncidentsParsed.ok) {
          liveWarnings.push(liveIncidentsParsed.error)
          liveIncidentsParsed = null
        }
      }

      const liveOk = liveStatusParsed != null || liveServicesParsed != null
      const histOk = servicesParsed != null || incidentsParsed != null
      if (!liveOk && !histOk) {
        throw new Error(histWarnings[0] || liveWarnings[0] || 'Pulse unavailable')
      }

      const statusForGate =
        liveStatusParsed?.response?.status ??
        liveServicesParsed?.response?.status ??
        null
      const collectorCurrent = liveCollectorCurrent(statusForGate)
      const liveIndex =
        liveServicesParsed && collectorCurrent
          ? indexLiveByGraphNode(liveServicesParsed.response)
          : new Map()
      const liveViews = liveIncidentsParsed?.response?.incidents ?? []
      const histIncidents = incidentsParsed?.response?.incidents ?? []
      const mergedIncidents = mergeIncidentsPreferLive(histIncidents, liveViews, {
        liveStatus: statusForGate,
      })

      const index = servicesParsed
        ? indexPulseByGraphNode(servicesParsed.response)
        : new Map()
      const enriched = new Map()
      for (const [nodeId, row] of index.entries()) {
        enriched.set(
          nodeId,
          enrichServicePulse(row, mergedIncidents, {
            liveService: liveIndex.get(nodeId) ?? null,
          }),
        )
      }
      // Live-mapped nodes with no historical service row still get a Pulse overlay.
      for (const [nodeId, liveRow] of liveIndex.entries()) {
        if (enriched.has(nodeId)) continue
        enriched.set(
          nodeId,
          enrichServicePulse(
            {
              serviceId: liveRow.serviceId,
              graphNode: liveRow.graphNode,
              mappingSource: liveRow.mappingSource,
              intelligence: liveRow.intelligence,
              intelligenceAvailable: true,
              freshness: liveRow.freshness,
              lastObservedAt: liveRow.lastObservedAt,
              openIncidentIds: liveRow.openIncidentIds,
              relatedIncidentIds: [],
              limitations: liveRow.limitations,
              activeHealthNote: liveRow.activeHealthNote,
            },
            mergedIncidents,
            { liveService: liveRow },
          ),
        )
      }

      if (servicesParsed || liveServicesParsed) {
        setByGraphNode(enriched)
      }
      if (liveServicesParsed && collectorCurrent) {
        setLiveByGraphNode(liveIndex)
        setLiveServicesResponse(liveServicesParsed.response)
      } else {
        // Failed / unavailable collector — clear current regression overlays and sidebar list.
        setLiveByGraphNode(new Map())
        if (liveServicesParsed && !collectorCurrent) {
          // Keep payload for debugging but zero regression flags for display consumers.
          setLiveServicesResponse({
            ...liveServicesParsed.response,
            services: (liveServicesParsed.response.services ?? []).map((s) => ({
              ...s,
              quality: 'stale',
              freshness: 'stale',
              regressionOpen: false,
            })),
          })
        } else if (liveWarnings.length) {
          setLiveServicesResponse(null)
        }
      }
      if (servicesParsed) {
        setServicesResponse(servicesParsed.response)
        setDataSources(servicesParsed.response.dataSources)
        setGeneratedAt(servicesParsed.response.generatedAt)
      }
      if (incidentsParsed) {
        setIncidentsResponse(incidentsParsed.response)
      }
      if (changesParsed) {
        setChangesResponse(changesParsed.response)
        setChanges(changesParsed.response.changes)
      }
      setIncidents(mergedIncidents)
      if (liveStatusParsed) {
        setLiveStatus(liveStatusParsed.response.status)
      }
      if (liveIncidentsParsed) {
        setLiveIncidents(liveViews)
      }
      setLimitations([
        ...(servicesParsed?.response?.limitations ?? []),
        ...histWarnings,
        ...(incidentsParsed &&
        incidentsParsed.response.count >= incidentsParsed.response.limit
          ? [`Incident list may be incomplete (page filled limit=${incidentsParsed.response.limit}).`]
          : []),
        ...(changesParsed && changesParsed.response.count >= changesParsed.response.limit
          ? [`Change list may be incomplete (page filled limit=${changesParsed.response.limit}).`]
          : []),
        ...(changesWarning
          ? [`Changes unavailable this refresh: ${changesWarning}`]
          : []),
        ...liveWarnings,
      ])
      setLastSuccessAt(new Date().toISOString())
      setStaleSnapshot(false)
      setError(histWarnings.length && !histOk ? histWarnings[0] : null)
    } catch (e) {
      if (!mounted.current) {
        return
      }
      const msg =
        e instanceof DOMException && e.name === 'AbortError'
          ? `Pulse request timed out after ${FETCH_TIMEOUT_MS}ms`
          : e instanceof Error
            ? e.message
            : String(e)
      // Keep last successful historical snapshot, but downgrade live overlays:
      // unconfirmed live_regression must not remain painted as current.
      setLiveByGraphNode((prev) => {
        if (prev.size === 0) return prev
        const next = new Map()
        for (const [k, v] of prev.entries()) {
          next.set(k, { ...v, quality: 'stale', freshness: 'stale', regressionOpen: false })
        }
        return next
      })
      setByGraphNode((prev) => {
        if (prev.size === 0) return prev
        const next = new Map()
        for (const [k, row] of prev.entries()) {
          const live = row.liveService
            ? { ...row.liveService, quality: 'stale', freshness: 'stale', regressionOpen: false }
            : null
          next.set(
            k,
            enrichServicePulse(
              { ...row, freshness: row.freshness === 'fresh' ? 'stale' : row.freshness },
              row.relatedIncidents ?? [],
              { liveService: live },
            ),
          )
        }
        return next
      })
      setError(msg)
      setStaleSnapshot(true)
    } finally {
      window.clearTimeout(timer)
      inFlight.current = false
      if (mounted.current) {
        setLoading(false)
      }
    }
  }, [enabled])

  useEffect(() => {
    mounted.current = true
    if (!enabled) {
      setLoading(false)
      return () => {
        mounted.current = false
      }
    }
    setLoading(true)
    void load()
    const id = window.setInterval(() => {
      void load()
    }, POLL_MS)
    return () => {
      mounted.current = false
      window.clearInterval(id)
    }
  }, [load, enabled])

  const getForNode = useCallback(
    (nodeId) => {
      if (nodeId == null || nodeId === '') {
        return null
      }
      return byGraphNode.get(String(nodeId)) ?? null
    },
    [byGraphNode],
  )

  const graphNodeByServiceId = useMemo(() => {
    const fromHist = indexGraphNodeByServiceId(servicesResponse)
    // Exact graph_node from live API when historical mapping is absent.
    for (const s of liveServicesResponse?.services ?? []) {
      const sid = String(s.serviceId ?? '').trim()
      const node = String(s.graphNode ?? '').trim()
      if (sid && node && !fromHist.has(sid)) {
        fromHist.set(sid, node)
      }
    }
    return fromHist
  }, [servicesResponse, liveServicesResponse])

  const overview = useMemo(
    () =>
      buildPulseOverview({
        servicesResponse,
        incidentsResponse,
        changesResponse,
      }),
    [servicesResponse, incidentsResponse, changesResponse],
  )

  const getLiveForNode = useCallback(
    (nodeId) => {
      if (nodeId == null || nodeId === '') return null
      return liveByGraphNode.get(String(nodeId)) ?? null
    },
    [liveByGraphNode],
  )

  return {
    byGraphNode,
    graphNodeByServiceId,
    servicesResponse,
    incidentsResponse,
    changesResponse,
    incidents,
    changes,
    liveStatus,
    liveServicesResponse,
    liveIncidents,
    liveByGraphNode,
    unmappedLive: unmappedLiveServices(liveServicesResponse),
    overview,
    /** True only after at least one successful Pulse poll. */
    hasSnapshot: servicesResponse != null,
    dataSources,
    limitations,
    generatedAt,
    lastSuccessAt,
    loading,
    error,
    staleSnapshot,
    refetch: load,
    getForNode,
    getLiveForNode,
  }
}
