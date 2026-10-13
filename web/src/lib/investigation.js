/**
 * Milestone F1 — evidence-backed investigation client helpers.
 * Does not invent root causes; preserves OBSERVED / HYPOTHESIS / UNKNOWN labels.
 */

/** @returns {string} */
export function pulseInvestigationsApiPath() {
  return '/api/pulse/investigations'
}

/**
 * @param {string} investigationId
 * @returns {string}
 */
export function pulseInvestigationApiPath(investigationId) {
  const id = String(investigationId ?? '')
  return `/api/pulse/investigations/${id}`
}

/**
 * @param {string} investigationId
 * @returns {string}
 */
export function pulseInvestigationFeedbackApiPath(investigationId) {
  return `${pulseInvestigationApiPath(investigationId)}/feedback`
}

/**
 * @param {string} investigationId
 * @returns {string}
 */
export function pulseInvestigationLLMProposeApiPath(investigationId) {
  return `${pulseInvestigationApiPath(investigationId)}/llm-propose`
}

/**
 * @param {string} investigationId
 * @returns {string}
 */
export function pulseInvestigationProposalsApiPath(investigationId) {
  return `${pulseInvestigationApiPath(investigationId)}/proposals`
}

/** @returns {string} */
export function pulseLLMStatusApiPath() {
  return '/api/pulse/llm/status'
}

/**
 * @param {string} incidentId
 * @returns {string}
 */
export function pulseInvestigationContextApiPath(incidentId) {
  const id = String(incidentId ?? '')
  return `/api/pulse/incidents/${id}/investigation-context`
}

/**
 * @param {unknown} raw
 * @returns {{ ok: true, investigation: object } | { ok: false, error: string }}
 */
export function parseInvestigationEnvelope(raw) {
  if (!raw || typeof raw !== 'object') {
    return { ok: false, error: 'investigation response missing' }
  }
  const inv = raw.investigation
  if (!inv || typeof inv !== 'object') {
    return { ok: false, error: 'investigation payload missing' }
  }
  const schema = inv.schemaVersion ?? inv.schema_version ?? ''
  if (schema && schema !== 'pulse.investigation.v1') {
    return { ok: false, error: `unsupported investigation schema ${schema}` }
  }
  if (inv.diagnosed === true) {
    return { ok: false, error: 'investigation claims diagnosed=true (rejected)' }
  }
  return {
    ok: true,
    investigation: {
      schemaVersion: inv.schemaVersion ?? 'pulse.investigation.v1',
      investigationId: inv.investigationId ?? inv.investigation_id ?? '',
      incidentId: inv.incidentId ?? inv.incident_id ?? '',
      incidentState: inv.incidentState ?? inv.incident_state ?? '',
      evidenceFreshness: inv.evidenceFreshness ?? inv.evidence_freshness ?? '',
      generationMethod: inv.generationMethod ?? inv.generation_method ?? '',
      contentFingerprint: inv.contentFingerprint ?? inv.content_fingerprint ?? '',
      diagnosed: false,
      observedSymptoms: (inv.observedSymptoms ?? inv.observed_symptoms ?? []).map(
        normalizeSymptom,
      ),
      hypotheses: (inv.hypotheses ?? []).map(normalizeHypothesis),
      missingEvidence: inv.missingEvidence ?? inv.missing_evidence ?? [],
      unknowns: inv.unknowns ?? [],
      suggestedVerificationSteps:
        inv.suggestedVerificationSteps ?? inv.suggested_verification_steps ?? [],
      limitations: inv.limitations ?? [],
      changeCandidates: inv.changeCandidates ?? inv.change_candidates ?? [],
      strongChangeCandidate: Boolean(
        inv.strongChangeCandidate ?? inv.strong_change_candidate,
      ),
      topology: inv.topology ?? null,
    },
  }
}

/**
 * @param {unknown} s
 */
function normalizeSymptom(s) {
  if (!s || typeof s !== 'object') {
    return { id: '', statement: '', classification: 'OBSERVED' }
  }
  return {
    id: s.id ?? '',
    statement: s.statement ?? '',
    classification: s.classification ?? 'OBSERVED',
    signalType: s.signalType ?? s.signal_type ?? '',
    serviceId: s.serviceId ?? s.service_id ?? '',
  }
}

/**
 * @param {unknown} h
 */
function normalizeHypothesis(h) {
  if (!h || typeof h !== 'object') {
    return {
      hypothesisId: '',
      statement: '',
      classification: 'INFERRED',
      kind: '',
      supportingEvidenceRefs: [],
      suggestedVerification: '',
    }
  }
  return {
    hypothesisId: h.hypothesisId ?? h.hypothesis_id ?? '',
    statement: h.statement ?? '',
    classification: h.classification ?? 'INFERRED',
    kind: h.kind ?? '',
    supportStatus: h.supportStatus ?? h.support_status ?? '',
    supportingEvidenceRefs: h.supportingEvidenceRefs ?? h.supporting_evidence_refs ?? [],
    counterEvidenceRefs: h.counterEvidenceRefs ?? h.counter_evidence_refs ?? [],
    missingEvidence: h.missingEvidence ?? h.missing_evidence ?? [],
    suggestedVerification: h.suggestedVerification ?? h.suggested_verification ?? '',
  }
}

export const FEEDBACK_ASSESSMENTS = [
  'useful',
  'partially_useful',
  'not_useful',
  'insufficient_evidence',
]

/**
 * @param {unknown} raw
 * @returns {{ ok: true, status: object } | { ok: false, error: string }}
 */
export function parseLLMStatusEnvelope(raw) {
  if (!raw || typeof raw !== 'object') {
    return { ok: false, error: 'llm status response missing' }
  }
  const st = raw.status
  if (!st || typeof st !== 'object') {
    return { ok: false, error: 'llm status payload missing' }
  }
  return {
    ok: true,
    status: {
      available: Boolean(st.available),
      enabled: Boolean(st.enabled),
      configured: Boolean(st.configured),
      reason: st.reason ?? '',
      model: st.model ?? '',
      endpointHost: st.endpointHost ?? st.endpoint_host ?? '',
      provider: st.provider ?? '',
    },
  }
}

/**
 * @param {unknown} raw
 * @returns {{ ok: true, proposal: object } | { ok: false, error: string }}
 */
export function parseLLMProposalEnvelope(raw) {
  if (!raw || typeof raw !== 'object') {
    return { ok: false, error: 'llm proposal response missing' }
  }
  const p = raw.proposal
  if (!p || typeof p !== 'object') {
    return { ok: false, error: 'llm proposal payload missing' }
  }
  const schema = p.schemaVersion ?? p.schema_version ?? ''
  if (schema && schema !== 'pulse.investigation.proposal.v1') {
    return { ok: false, error: `unsupported proposal schema ${schema}` }
  }
  if (p.diagnosed === true) {
    return { ok: false, error: 'proposal claims diagnosed=true (rejected)' }
  }
  return {
    ok: true,
    proposal: {
      schemaVersion: schema || 'pulse.investigation.proposal.v1',
      proposalId: p.proposalId ?? p.proposal_id ?? '',
      sourceInvestigationId: p.sourceInvestigationId ?? p.source_investigation_id ?? '',
      sourceFingerprint: p.sourceFingerprint ?? p.source_fingerprint ?? '',
      generationMethod: p.generationMethod ?? p.generation_method ?? 'llm_constrained_v1',
      diagnosed: false,
      hypotheses: (p.hypotheses ?? []).map(normalizeHypothesis),
      rejectedHypotheses: (p.rejectedHypotheses ?? p.rejected_hypotheses ?? []).map(
        normalizeHypothesis,
      ),
      missingEvidence: p.missingEvidence ?? p.missing_evidence ?? [],
      unknowns: p.unknowns ?? [],
      suggestedVerificationSteps:
        p.suggestedVerificationSteps ?? p.suggested_verification_steps ?? [],
      limitations: p.limitations ?? [],
      validationNotes: p.validationNotes ?? p.validation_notes ?? [],
      provenance: {
        provider: p.provenance?.provider ?? '',
        model: p.provenance?.model ?? '',
        endpointHost: p.provenance?.endpointHost ?? p.provenance?.endpoint_host ?? '',
      },
    },
  }
}

/**
 * @param {string} assessment
 * @returns {boolean}
 */
export function isValidFeedbackAssessment(assessment) {
  return FEEDBACK_ASSESSMENTS.includes(String(assessment ?? ''))
}

/**
 * @param {{ assessment: string, usefulHypothesisIds?: string[], rejectedHypothesisIds?: string[], rationale?: string }} input
 * @param {string[]} knownHypothesisIds
 * @returns {{ ok: true } | { ok: false, error: string }}
 */
export function validateFeedbackClient(input, knownHypothesisIds = []) {
  if (!isValidFeedbackAssessment(input?.assessment)) {
    return { ok: false, error: 'invalid assessment' }
  }
  const known = new Set(knownHypothesisIds)
  for (const id of input.usefulHypothesisIds ?? []) {
    if (!known.has(id)) return { ok: false, error: `unknown useful hypothesis ${id}` }
  }
  for (const id of input.rejectedHypothesisIds ?? []) {
    if (!known.has(id)) return { ok: false, error: `unknown rejected hypothesis ${id}` }
  }
  if (String(input.rationale ?? '').length > 2000) {
    return { ok: false, error: 'rationale too long' }
  }
  return { ok: true }
}
