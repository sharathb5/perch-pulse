import { useCallback, useEffect, useRef, useState } from 'react'
import { parseWorkflowsResponse, pulseWorkflowsApiPath } from '../lib/pulse.js'

const FETCH_TIMEOUT_MS = 8000

/**
 * Loads allowlisted Run Pulse workflows once (and on demand refetch).
 */
export function usePulseWorkflows({ enabled = true } = {}) {
  const [workflows, setWorkflows] = useState([])
  const [limitations, setLimitations] = useState([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)
  const gen = useRef(0)

  const refetch = useCallback(async () => {
    if (!enabled) return
    const my = ++gen.current
    setLoading(true)
    const ctrl = new AbortController()
    const timer = setTimeout(() => ctrl.abort(), FETCH_TIMEOUT_MS)
    try {
      const res = await fetch(pulseWorkflowsApiPath(), { signal: ctrl.signal })
      const json = await res.json().catch(() => null)
      if (my !== gen.current) return
      if (!res.ok) {
        setError(json?.error || `Workflows unavailable (${res.status})`)
        setWorkflows([])
        return
      }
      const parsed = parseWorkflowsResponse(json)
      if (!parsed.ok) {
        setError(parsed.error)
        setWorkflows([])
        return
      }
      setWorkflows(parsed.response.workflows)
      setLimitations(parsed.response.limitations)
      setError(null)
    } catch (e) {
      if (my !== gen.current) return
      setError(e?.name === 'AbortError' ? 'Workflows request timed out' : String(e?.message || e))
    } finally {
      clearTimeout(timer)
      if (my === gen.current) setLoading(false)
    }
  }, [enabled])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { workflows, limitations, loading, error, refetch }
}
