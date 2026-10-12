import { useCallback, useEffect, useState } from 'react'
import {
  parseInvestigationEnvelope,
  pulseInvestigationFeedbackApiPath,
  pulseInvestigationsApiPath,
  validateFeedbackClient,
} from '../lib/investigation.js'

const FETCH_TIMEOUT_MS = 12_000

/**
 * Create / hold an evidence-backed investigation for an incident (F1).
 * @param {string} incidentId
 */
export function usePulseInvestigation(incidentId) {
  const [investigation, setInvestigation] = useState(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)
  const [feedbackStatus, setFeedbackStatus] = useState(null)

  useEffect(() => {
    setInvestigation(null)
    setError(null)
    setFeedbackStatus(null)
    setLoading(false)
  }, [incidentId])

  const create = useCallback(async () => {
    const id = String(incidentId ?? '').trim()
    if (!id) {
      setError('incident id required')
      return null
    }
    setLoading(true)
    setError(null)
    setFeedbackStatus(null)
    const controller = new AbortController()
    const timer = window.setTimeout(() => controller.abort(), FETCH_TIMEOUT_MS)
    try {
      const res = await fetch(pulseInvestigationsApiPath(), {
        method: 'POST',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ incident_id: id, reuse_identical: true }),
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
      const parsed = parseInvestigationEnvelope(body)
      if (!parsed.ok) throw new Error(parsed.error)
      setInvestigation(parsed.investigation)
      return parsed.investigation
    } catch (e) {
      if (e instanceof DOMException && e.name === 'AbortError') {
        setError(`Investigation request timed out after ${FETCH_TIMEOUT_MS}ms`)
      } else {
        setError(e instanceof Error ? e.message : String(e))
      }
      setInvestigation(null)
      return null
    } finally {
      window.clearTimeout(timer)
      setLoading(false)
    }
  }, [incidentId])

  const submitFeedback = useCallback(
    async ({ assessment, usefulHypothesisIds = [], rationale = '' }) => {
      if (!investigation?.investigationId) {
        setFeedbackStatus({ ok: false, error: 'no investigation loaded' })
        return false
      }
      const known = (investigation.hypotheses ?? []).map((h) => h.hypothesisId)
      const client = validateFeedbackClient(
        { assessment, usefulHypothesisIds, rationale },
        known,
      )
      if (!client.ok) {
        setFeedbackStatus({ ok: false, error: client.error })
        return false
      }
      setFeedbackStatus(null)
      try {
        const res = await fetch(pulseInvestigationFeedbackApiPath(investigation.investigationId), {
          method: 'POST',
          headers: {
            Accept: 'application/json',
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({
            assessment,
            useful_hypothesis_ids: usefulHypothesisIds,
            rationale,
          }),
        })
        const text = await res.text()
        let body
        try {
          body = text ? JSON.parse(text) : null
        } catch {
          body = null
        }
        if (!res.ok) {
          const msg = body?.error ?? body?.code ?? res.statusText ?? 'feedback failed'
          throw new Error(typeof msg === 'string' ? msg : JSON.stringify(msg))
        }
        setFeedbackStatus({ ok: true })
        return true
      } catch (e) {
        setFeedbackStatus({
          ok: false,
          error: e instanceof Error ? e.message : String(e),
        })
        return false
      }
    },
    [investigation],
  )

  return {
    investigation,
    loading,
    error,
    feedbackStatus,
    create,
    submitFeedback,
  }
}
