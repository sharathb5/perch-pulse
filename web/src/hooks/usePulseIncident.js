import { useEffect, useRef, useState } from 'react'
import { parseIncidentDetailResponse, pulseIncidentApiPath } from '../lib/pulse.js'

const FETCH_TIMEOUT_MS = 8_000

/**
 * Fetch a single Pulse incident detail when `incidentId` is set.
 * Uses request generation + AbortController so rapid ID switches cannot stick
 * on a skipped in-flight request or show a previous incident.
 * @param {string} incidentId
 */
export function usePulseIncident(incidentId) {
  const [detail, setDetail] = useState(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)
  const genRef = useRef(0)

  useEffect(() => {
    const id = String(incidentId ?? '').trim()
    const gen = ++genRef.current
    if (id === '') {
      setDetail(null)
      setError(null)
      setLoading(false)
      return undefined
    }

    const controller = new AbortController()
    const timer = window.setTimeout(() => controller.abort(), FETCH_TIMEOUT_MS)
    setLoading(true)
    setError(null)
    setDetail(null)

    const run = async () => {
      try {
        const res = await fetch(pulseIncidentApiPath(id), {
          headers: { Accept: 'application/json' },
          signal: controller.signal,
        })
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
        const parsed = parseIncidentDetailResponse(body)
        if (!parsed.ok) {
          throw new Error(parsed.error)
        }
        if (gen !== genRef.current) {
          return
        }
        setDetail(parsed.response)
      } catch (e) {
        if (gen !== genRef.current) {
          return
        }
        if (e instanceof DOMException && e.name === 'AbortError') {
          setError(`Pulse incident request timed out after ${FETCH_TIMEOUT_MS}ms`)
        } else {
          setError(e instanceof Error ? e.message : String(e))
        }
        setDetail(null)
      } finally {
        window.clearTimeout(timer)
        if (gen === genRef.current) {
          setLoading(false)
        }
      }
    }
    void run()
    return () => {
      controller.abort()
      window.clearTimeout(timer)
    }
  }, [incidentId])

  return { detail, loading, error }
}
