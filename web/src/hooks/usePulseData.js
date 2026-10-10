import { useCallback, useEffect, useRef, useState } from 'react'
import {
  enrichServicePulse,
  indexPulseByGraphNode,
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
  const [incidents, setIncidents] = useState([])
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
      const [servicesRaw, incidentsRaw] = await Promise.all([
        fetchJson('/api/pulse/services?limit=200', controller.signal),
        fetchJson('/api/pulse/incidents?limit=100', controller.signal),
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

      const index = indexPulseByGraphNode(servicesParsed.response)
      const enriched = new Map()
      for (const [nodeId, row] of index.entries()) {
        enriched.set(nodeId, enrichServicePulse(row, incidentsParsed.response.incidents))
      }

      setByGraphNode(enriched)
      setIncidents(incidentsParsed.response.incidents)
      setDataSources(servicesParsed.response.dataSources)
      setLimitations([
        ...servicesParsed.response.limitations,
        ...(incidentsParsed.response.count >= incidentsParsed.response.limit
          ? [`Incident list truncated at limit=${incidentsParsed.response.limit}.`]
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

  return {
    byGraphNode,
    incidents,
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
