import { useCallback, useEffect, useRef, useState } from 'react'
import {
  RUN_STATUS,
  parseRunResponse,
  pulseRunApiPath,
  pulseRunsApiPath,
} from '../lib/pulse.js'

const FETCH_TIMEOUT_MS = 8000
const POLL_MS = 1500

function isTerminal(status) {
  return (
    status === RUN_STATUS.COMPLETED ||
    status === RUN_STATUS.FAILED ||
    status === RUN_STATUS.TIMED_OUT
  )
}

/**
 * Polls a Run Pulse diagnostic while pending/running. Starting a run is explicit
 * via startRun — GET polling never creates a new execution.
 *
 * @param {string} runId
 */
export function usePulseRun(runId) {
  const [run, setRun] = useState(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)
  const [starting, setStarting] = useState(false)
  const gen = useRef(0)
  const terminalRef = useRef(false)

  const fetchRun = useCallback(async (id) => {
    if (!id) {
      setRun(null)
      setError(null)
      terminalRef.current = false
      return null
    }
    const my = ++gen.current
    setLoading(true)
    const ctrl = new AbortController()
    const timer = setTimeout(() => ctrl.abort(), FETCH_TIMEOUT_MS)
    try {
      const res = await fetch(pulseRunApiPath(id), { signal: ctrl.signal })
      const json = await res.json().catch(() => null)
      if (my !== gen.current) return null
      if (!res.ok) {
        const msg = json?.error || `Run unavailable (${res.status})`
        setError(msg)
        setRun(null)
        terminalRef.current = true
        return null
      }
      const parsed = parseRunResponse(json)
      if (!parsed.ok) {
        setError(parsed.error)
        setRun(null)
        terminalRef.current = true
        return null
      }
      setRun(parsed.response.run)
      setError(null)
      terminalRef.current = isTerminal(parsed.response.run.status)
      return parsed.response.run
    } catch (e) {
      if (my !== gen.current) return null
      setError(e?.name === 'AbortError' ? 'Run request timed out' : String(e?.message || e))
      return null
    } finally {
      clearTimeout(timer)
      if (my === gen.current) setLoading(false)
    }
  }, [])

  useEffect(() => {
    terminalRef.current = false
    if (!runId) {
      setRun(null)
      setError(null)
      return undefined
    }
    void fetchRun(runId)
    const t = setInterval(() => {
      if (terminalRef.current) return
      void fetchRun(runId)
    }, POLL_MS)
    return () => clearInterval(t)
  }, [runId, fetchRun])

  const startRun = useCallback(async (workflowId) => {
    setStarting(true)
    setError(null)
    terminalRef.current = false
    const ctrl = new AbortController()
    const timer = setTimeout(() => ctrl.abort(), FETCH_TIMEOUT_MS)
    try {
      const res = await fetch(pulseRunsApiPath(), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ workflow_id: workflowId }),
        signal: ctrl.signal,
      })
      const json = await res.json().catch(() => null)
      if (!res.ok) {
        const msg = json?.error || `Could not start diagnostic (${res.status})`
        setError(msg)
        return { ok: false, error: msg, code: json?.code }
      }
      const parsed = parseRunResponse(json)
      if (!parsed.ok) {
        setError(parsed.error)
        return { ok: false, error: parsed.error }
      }
      setRun(parsed.response.run)
      terminalRef.current = isTerminal(parsed.response.run.status)
      return { ok: true, run: parsed.response.run }
    } catch (e) {
      const msg = e?.name === 'AbortError' ? 'Start request timed out' : String(e?.message || e)
      setError(msg)
      return { ok: false, error: msg }
    } finally {
      clearTimeout(timer)
      setStarting(false)
    }
  }, [])

  return { run, loading, error, starting, startRun, refetch: () => fetchRun(runId) }
}
