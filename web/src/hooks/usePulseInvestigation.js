import { useCallback, useEffect, useState } from 'react'
import {
  parseInvestigationEnvelope,
  parseLLMProposalEnvelope,
  parseLLMStatusEnvelope,
  pulseInvestigationFeedbackApiPath,
  pulseInvestigationLLMProposeApiPath,
  pulseInvestigationsApiPath,
  pulseLLMStatusApiPath,
  validateFeedbackClient,
} from '../lib/investigation.js'

const FETCH_TIMEOUT_MS = 12_000
const LLM_FETCH_TIMEOUT_MS = 45_000

/**
 * Create / hold an evidence-backed investigation for an incident (F1).
 * Optional LLM proposal generation is explicit and never runs on GET.
 * @param {string} incidentId
 */
export function usePulseInvestigation(incidentId) {
  const [investigation, setInvestigation] = useState(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)
  const [feedbackStatus, setFeedbackStatus] = useState(null)
  const [llmStatus, setLlmStatus] = useState(null)
  const [llmProposal, setLlmProposal] = useState(null)
  const [llmLoading, setLlmLoading] = useState(false)
  const [llmError, setLlmError] = useState(null)

  useEffect(() => {
    setInvestigation(null)
    setError(null)
    setFeedbackStatus(null)
    setLoading(false)
    setLlmProposal(null)
    setLlmError(null)
    setLlmLoading(false)
  }, [incidentId])

  useEffect(() => {
    let cancelled = false
    const controller = new AbortController()
    const timer = window.setTimeout(() => controller.abort(), FETCH_TIMEOUT_MS)
    ;(async () => {
      try {
        const res = await fetch(pulseLLMStatusApiPath(), {
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
        if (!res.ok || cancelled) return
        const parsed = parseLLMStatusEnvelope(body)
        if (parsed.ok && !cancelled) setLlmStatus(parsed.status)
      } catch {
        if (!cancelled) {
          setLlmStatus({
            available: false,
            enabled: false,
            configured: false,
            reason: 'LLM status unavailable',
          })
        }
      } finally {
        window.clearTimeout(timer)
      }
    })()
    return () => {
      cancelled = true
      controller.abort()
      window.clearTimeout(timer)
    }
  }, [])

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

  const generateLLMProposal = useCallback(async () => {
    if (!investigation?.investigationId) {
      setLlmError('Create a deterministic investigation first')
      return null
    }
    setLlmLoading(true)
    setLlmError(null)
    const controller = new AbortController()
    const timer = window.setTimeout(() => controller.abort(), LLM_FETCH_TIMEOUT_MS)
    try {
      const res = await fetch(pulseInvestigationLLMProposeApiPath(investigation.investigationId), {
        method: 'POST',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          expected_fingerprint: investigation.contentFingerprint ?? undefined,
        }),
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
        const msg = body?.error ?? body?.code ?? res.statusText ?? 'LLM propose failed'
        throw new Error(typeof msg === 'string' ? msg : JSON.stringify(msg))
      }
      const parsed = parseLLMProposalEnvelope(body)
      if (!parsed.ok) throw new Error(parsed.error)
      setLlmProposal(parsed.proposal)
      return parsed.proposal
    } catch (e) {
      if (e instanceof DOMException && e.name === 'AbortError') {
        setLlmError(`LLM proposal timed out after ${LLM_FETCH_TIMEOUT_MS}ms`)
      } else {
        setLlmError(e instanceof Error ? e.message : String(e))
      }
      return null
    } finally {
      window.clearTimeout(timer)
      setLlmLoading(false)
    }
  }, [investigation])

  return {
    investigation,
    loading,
    error,
    feedbackStatus,
    create,
    submitFeedback,
    llmStatus,
    llmProposal,
    llmLoading,
    llmError,
    generateLLMProposal,
  }
}
