import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  buildPulseOverview,
  enrichServicePulse,
  indexGraphNodeByServiceId,
  indexPulseByGraphNode,
  parseChangesResponse,
  parseIncidentsResponse,
  parseServicesResponse,
} from '../lib/pulse.js'

const POLL_MS = 15_000
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
      // Services + incidents are required for graph Pulse overlays.
      // Changes are best-effort: a changes-store failure must not wipe service/incident data.
      const [servicesRaw, incidentsRaw, changesSettled] = await Promise.all([
        fetchJson('/api/pulse/services?limit=200', controller.signal),
        fetchJson('/api/pulse/incidents?limit=100', controller.signal),
        fetchJson('/api/pulse/changes?limit=100', controller.signal).then(
          (body) => ({ ok: true, body }),
          (err) => ({ ok: false, error: err }),
        ),
      ])
      if (!mounted.current) {
        return
      }
      const servicesParsed = parseServicesResponse(servicesRaw)
      if (!servicesParsed.ok) {
        throw new Error(servicesParsed.error)
      }
      const incidentsParsed = parseIncidentsResponse(incidentsRaw)
      if (!incidentsParsed.ok) {
        throw new Error(incidentsParsed.error)
      }

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

      const index = indexPulseByGraphNode(servicesParsed.response)
      const enriched = new Map()
      for (const [nodeId, row] of index.entries()) {
        enriched.set(nodeId, enrichServicePulse(row, incidentsParsed.response.incidents))
      }

      setByGraphNode(enriched)
      setServicesResponse(servicesParsed.response)
      setIncidentsResponse(incidentsParsed.response)
      if (changesParsed) {
        setChangesResponse(changesParsed.response)
        setChanges(changesParsed.response.changes)
      }
      setIncidents(incidentsParsed.response.incidents)
      setDataSources(servicesParsed.response.dataSources)
      setLimitations([
        ...servicesParsed.response.limitations,
        ...(incidentsParsed.response.count >= incidentsParsed.response.limit
          ? [`Incident list may be incomplete (page filled limit=${incidentsParsed.response.limit}).`]
          : []),
        ...(changesParsed && changesParsed.response.count >= changesParsed.response.limit
          ? [`Change list may be incomplete (page filled limit=${changesParsed.response.limit}).`]
          : []),
        ...(changesWarning
          ? [`Changes unavailable this refresh: ${changesWarning}`]
          : []),
      ])
      setGeneratedAt(servicesParsed.response.generatedAt)
      setLastSuccessAt(new Date().toISOString())
      setStaleSnapshot(false)
      setError(null)
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
      // Keep last successful Pulse snapshot if any, but mark it stale / errored.
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

  const graphNodeByServiceId = useMemo(
    () => indexGraphNodeByServiceId(servicesResponse),
    [servicesResponse],
  )

  const overview = useMemo(
    () =>
      buildPulseOverview({
        servicesResponse,
        incidentsResponse,
        changesResponse,
      }),
    [servicesResponse, incidentsResponse, changesResponse],
  )

  return {
    byGraphNode,
    graphNodeByServiceId,
    servicesResponse,
    incidentsResponse,
    changesResponse,
    incidents,
    changes,
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
  }
}
