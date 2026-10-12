import { describe, expect, it } from 'vitest'
import createFixture from '../../fixtures/pulse.investigation.create.json'
import {
  FEEDBACK_ASSESSMENTS,
  isValidFeedbackAssessment,
  parseInvestigationEnvelope,
  parseLLMProposalEnvelope,
  parseLLMStatusEnvelope,
  pulseInvestigationApiPath,
  pulseInvestigationContextApiPath,
  pulseInvestigationFeedbackApiPath,
  pulseInvestigationLLMProposeApiPath,
  pulseLLMStatusApiPath,
  validateFeedbackClient,
} from './investigation.js'

describe('investigation helpers', () => {
  it('parses create fixture with OBSERVED/INFERRED/UNKNOWN and diagnosed=false', () => {
    const parsed = parseInvestigationEnvelope(createFixture)
    expect(parsed.ok).toBe(true)
    expect(parsed.investigation.diagnosed).toBe(false)
    expect(parsed.investigation.hypotheses[0].classification).toBe('INFERRED')
    expect(parsed.investigation.unknowns[0]).toMatch(/UNKNOWN/i)
    expect(parsed.investigation.observedSymptoms.length).toBeGreaterThan(0)
  })

  it('rejects diagnosed=true envelopes', () => {
    const bad = structuredClone(createFixture)
    bad.investigation.diagnosed = true
    expect(parseInvestigationEnvelope(bad).ok).toBe(false)
  })

  it('validates feedback assessments and hypothesis ids', () => {
    expect(FEEDBACK_ASSESSMENTS).toContain('useful')
    expect(isValidFeedbackAssessment('useful')).toBe(true)
    expect(isValidFeedbackAssessment('bogus')).toBe(false)
    const hyp = 'hyp|latency_regression|fixture_production_api'
    expect(
      validateFeedbackClient({ assessment: 'useful', usefulHypothesisIds: [hyp] }, [hyp]).ok,
    ).toBe(true)
    expect(
      validateFeedbackClient({ assessment: 'useful', usefulHypothesisIds: ['nope'] }, [hyp]).ok,
    ).toBe(false)
  })

  it('builds investigation API paths without escaping embedded slashes', () => {
    const id = 'inv|inc|a/b|fp|ts'
    expect(pulseInvestigationApiPath(id)).toBe(`/api/pulse/investigations/${id}`)
    expect(pulseInvestigationFeedbackApiPath(id)).toBe(`/api/pulse/investigations/${id}/feedback`)
    expect(pulseInvestigationLLMProposeApiPath(id)).toBe(
      `/api/pulse/investigations/${id}/llm-propose`,
    )
    expect(pulseInvestigationContextApiPath('inc|a/b')).toBe(
      '/api/pulse/incidents/inc|a/b/investigation-context',
    )
    expect(pulseLLMStatusApiPath()).toBe('/api/pulse/llm/status')
  })

  it('parses LLM status and rejects diagnosed proposals', () => {
    const st = parseLLMStatusEnvelope({
      status: { available: false, enabled: false, reason: 'disabled' },
    })
    expect(st.ok).toBe(true)
    expect(st.status.available).toBe(false)
    const bad = parseLLMProposalEnvelope({
      proposal: {
        schema_version: 'pulse.investigation.proposal.v1',
        diagnosed: true,
        hypotheses: [],
      },
    })
    expect(bad.ok).toBe(false)
    const good = parseLLMProposalEnvelope({
      proposal: {
        schema_version: 'pulse.investigation.proposal.v1',
        proposal_id: 'prop|x',
        diagnosed: false,
        generation_method: 'llm_constrained_v1',
        hypotheses: [
          {
            hypothesis_id: 'hyp|1',
            statement: 'INFERRED claim',
            classification: 'INFERRED',
            supporting_evidence_refs: ['phase:during'],
          },
        ],
        unknowns: ['Root cause is UNKNOWN'],
      },
    })
    expect(good.ok).toBe(true)
    expect(good.proposal.diagnosed).toBe(false)
  })
})
