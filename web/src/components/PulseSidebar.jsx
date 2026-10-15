import { X } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { usePulseIncident } from '../hooks/usePulseIncident.js'
import { usePulseInvestigation } from '../hooks/usePulseInvestigation.js'
import { FEEDBACK_ASSESSMENTS } from '../lib/investigation.js'
import {
  LIVE_LIFECYCLE,
  formatLiveBaseline,
  formatLiveSignalValue,
  liveRuntimeSummary,
  overlayIncidentLifecycle,
} from '../lib/livePulse.js'
import {
  INCIDENT_STATE,
  PULSE_TAB,
  buildDigestComparisonRows,
  correlationStrengthSummary,
  filterChanges,
  filterIncidents,
  formatPulseTimestamp,
  incidentStateLabel,
  pulseStackPath,
  signalUnitLabel,
  sortChangesDeterministic,
  sortIncidentsDeterministic,
} from '../lib/pulse.js'
import { PulseRunPanel } from './PulseRunPanel.jsx'

/**
 * Dedicated Pulse investigation sidebar (Milestone C).
 * Graph stays interactive; DetailPanel may remain open beside this panel.
 *
 * @param {{
 *   open: boolean,
 *   tab: string | null,
 *   incidentId: string,
 *   overview: object | null,
 *   incidents: object[],
 *   changes: object[],
 *   liveStatus?: object | null,
 *   liveServices?: object[],
 *   unmappedLive?: object[],
 *   selectedLive?: object | null,
 *   limitations?: string[],
 *   loading?: boolean,
 *   error?: string | null,
 *   staleSnapshot?: boolean,
 *   hasSnapshot?: boolean,
 *   dataSources?: object | null,
 *   graphNodeByServiceId?: Map<string, string>,
 *   graphDemo?: boolean,
 *   runId?: string,
 *   onRunHighlightsChange?: (state: object) => void,
 *   runHighlightsEnabled?: boolean,
 *   runHighlightsCleared?: boolean,
 *   onClearRunHighlights?: () => void,
 * }} props
 */
export function PulseSidebar({
  open,
  tab,
  incidentId,
  overview,
  incidents = [],
  changes = [],
  liveStatus = null,
  liveServices = [],
  unmappedLive = [],
  selectedLive = null,
  limitations = [],
  loading = false,
  error = null,
  staleSnapshot = false,
  hasSnapshot = false,
  dataSources = null,
  graphNodeByServiceId = new Map(),
  graphDemo = false,
  runId = '',
  onRunHighlightsChange,
  runHighlightsEnabled = true,
  runHighlightsCleared = false,
  onClearRunHighlights,
}) {
  const { stackName, nodeId } = useParams()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const activeTab = tab || PULSE_TAB.OVERVIEW

  const closeSidebar = () => {
    navigate(
      pulseStackPath(stackName, {
        nodeId,
        pulseTab: null,
        incidentId: null,
        runId: null,
        searchParams,
      }),
    )
  }

  const setTab = (next) => {
    navigate(
      pulseStackPath(stackName, {
        nodeId,
        pulseTab: next,
        // Incident deep-links are incidents-tab scoped; run ids persist so graph
        // highlights remain while browsing Overview/Incidents/Changes.
        incidentId: next === PULSE_TAB.INCIDENTS ? incidentId : null,
        runId: runId || null,
        searchParams,
      }),
    )
  }

  if (!open) {
    return null
  }

  return (
    <aside
      data-testid="pulse-sidebar"
      aria-label="Pulse investigation"
      className="flex h-full w-[360px] shrink-0 flex-col border-l border-gray-200 bg-white"
    >
      <div className="flex h-12 shrink-0 items-center justify-between border-b border-gray-200 px-3">
        <div>
          <div className="text-[13px] font-medium text-gray-900">Pulse</div>
          <div className="text-[10px] text-gray-400">Evidence · not live probe health</div>
        </div>
        <button
          type="button"
          data-testid="pulse-sidebar-close"
          onClick={closeSidebar}
          className="rounded p-1 text-gray-500 hover:bg-gray-100 hover:text-gray-900"
          aria-label="Close Pulse sidebar"
        >
          <X className="h-4 w-4" strokeWidth={2} />
        </button>
      </div>

      <div
        role="tablist"
        aria-label="Pulse sections"
        className="flex shrink-0 border-b border-gray-200 text-xs"
      >
        {[
          { id: PULSE_TAB.OVERVIEW, label: 'Overview' },
          { id: PULSE_TAB.INCIDENTS, label: 'Incidents' },
          { id: PULSE_TAB.CHANGES, label: 'Changes' },
          { id: PULSE_TAB.RUN, label: 'Run' },
        ].map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={activeTab === t.id}
            data-testid={`pulse-tab-${t.id}`}
            onClick={() => setTab(t.id)}
            className={`flex-1 px-2 py-2 ${
              activeTab === t.id
                ? 'border-b-2 border-gray-900 font-medium text-gray-900'
                : 'text-gray-500 hover:text-gray-800'
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      <div
        data-testid="pulse-sidebar-scroll"
        className="min-h-0 flex-1 overflow-y-auto px-3 py-2 text-[11px] text-gray-700"
      >
        {graphDemo && (
          <p data-testid="pulse-sidebar-demo" className="mb-2 text-amber-800">
            Graph is demo/last-known topology — Pulse below is not merged as live stack health.
          </p>
        )}
        {error && (
          <p data-testid="pulse-sidebar-error" role="status" className="mb-2 text-amber-800">
            {hasSnapshot
              ? `Pulse refresh failed — showing last snapshot (may be stale): ${error}`
              : `Pulse unavailable: ${error}`}
          </p>
        )}
        {staleSnapshot && !error && (
          <p data-testid="pulse-sidebar-stale" className="mb-2 text-amber-700">
            Pulse snapshot may be stale after a failed refresh.
          </p>
        )}
        {loading && !hasSnapshot && (
          <p data-testid="pulse-sidebar-loading" className="text-gray-500">
            Loading Pulse…
          </p>
        )}

        {activeTab === PULSE_TAB.OVERVIEW && (
          <OverviewPanel
            overview={hasSnapshot ? overview : null}
            limitations={limitations}
            dataSources={dataSources}
            liveStatus={liveStatus}
            liveServices={liveServices}
            unmappedLive={unmappedLive}
            selectedLive={selectedLive}
          />
        )}
        {activeTab === PULSE_TAB.INCIDENTS && (
          <IncidentsPanel
            incidents={incidents}
            incidentId={incidentId}
            limitations={limitations}
            graphNodeByServiceId={graphNodeByServiceId}
            stackName={stackName}
            nodeId={nodeId}
          />
        )}
        {activeTab === PULSE_TAB.CHANGES && (
          <ChangesPanel
            changes={changes}
            limitations={limitations}
            graphNodeByServiceId={graphNodeByServiceId}
            stackName={stackName}
            nodeId={nodeId}
          />
        )}
        {activeTab === PULSE_TAB.RUN && (
          <PulseRunPanel
            runId={runId}
            onHighlightsChange={onRunHighlightsChange}
            highlightsEnabled={runHighlightsEnabled}
            highlightsCleared={runHighlightsCleared}
            onClearHighlights={onClearRunHighlights}
          />
        )}
      </div>
    </aside>
  )
}

function OverviewPanel({
  overview,
  limitations,
  dataSources,
  liveStatus,
  liveServices = [],
  unmappedLive = [],
  selectedLive = null,
}) {
  if (!overview && !liveStatus) {
    return (
      <p data-testid="pulse-overview-empty" className="text-gray-500">
        No Pulse overview data yet.
      </p>
    )
  }
  const src = overview?.sourceInfo
  const liveSummary = liveRuntimeSummary(liveStatus)
  return (
    <div data-testid="pulse-overview" className="space-y-3">
      <section data-testid="pulse-live-telemetry" className="rounded border border-gray-100 bg-gray-50 px-2 py-2">
        <h3 className="text-[10px] uppercase tracking-wide text-gray-400">Live telemetry</h3>
        <p
          data-testid="pulse-live-runtime"
          data-live-state={liveStatus?.state ?? 'disabled'}
          className={`mt-1 font-medium ${
            liveSummary.tone === 'warn'
              ? 'text-amber-800'
              : liveSummary.tone === 'ok'
                ? 'text-gray-900'
                : 'text-gray-600'
          }`}
        >
          {liveSummary.title}
        </p>
        <p data-testid="pulse-live-runtime-detail" className="mt-0.5 text-[10px] text-gray-500">
          {liveSummary.detail}
        </p>
        {liveStatus?.enabled && (
          <dl className="mt-2 grid grid-cols-2 gap-1 font-mono text-[10px] text-gray-700">
            <div>
              <dt className="text-gray-400">observed</dt>
              <dd data-testid="pulse-live-observed">{liveStatus.servicesObserved}</dd>
            </div>
            <div>
              <dt className="text-gray-400">regressions</dt>
              <dd data-testid="pulse-live-regressions">{liveStatus.activeRegressions}</dd>
            </div>
            <div>
              <dt className="text-gray-400">active incidents</dt>
              <dd data-testid="pulse-live-active-incidents">{liveStatus.activeIncidents}</dd>
            </div>
            <div>
              <dt className="text-gray-400">unknown/n/a</dt>
              <dd data-testid="pulse-live-unknown">{liveStatus.unavailableOrUnknown}</dd>
            </div>
          </dl>
        )}
        {liveStatus?.lastSuccessAt && (
          <p className="mt-1 font-mono text-[10px] text-gray-500">
            last success {formatPulseTimestamp(liveStatus.lastSuccessAt)}
          </p>
        )}
        {liveStatus?.environment && (
          <p data-testid="pulse-live-collector-env" className="mt-1 font-mono text-[10px] text-gray-500">
            collector env {liveStatus.environment} (independent of navbar graph env)
          </p>
        )}
        <p className="mt-1 text-[10px] text-gray-400">
          Distinct from StatusPill probes and on-demand Run Pulse. Missing values are not zero errors.
        </p>
        {unmappedLive.length > 0 && (
          <p data-testid="pulse-live-unmapped" className="mt-1 text-amber-800">
            Unmapped telemetry ({unmappedLive.length}):{' '}
            {unmappedLive
              .slice(0, 4)
              .map((s) => s.serviceId)
              .join(', ')}
          </p>
        )}
        {selectedLive && (
          <div data-testid="pulse-live-selected" className="mt-2 border-t border-gray-200 pt-2">
            <h4 className="text-[10px] uppercase tracking-wide text-gray-400">
              Selected · {selectedLive.graphNode || selectedLive.serviceId}
            </h4>
            <p className="mt-0.5 text-[10px] text-gray-500">
              {selectedLive.quality}/{selectedLive.intelligence}
              {selectedLive.baselineReady ? '' : ' · baseline warming'}
              {selectedLive.regressionOpen ? ' · regression open' : ''}
            </p>
            <ul className="mt-1 space-y-0.5 font-mono text-[10px]">
              {selectedLive.signals?.map((sig) => {
                const fmt = formatLiveSignalValue(sig)
                return (
                  <li key={sig.signal} data-testid={`pulse-live-signal-${sig.signal}`}>
                    {sig.signal}:{' '}
                    {sig.value == null || sig.quality === 'missing' ? (
                      <span className="text-amber-800">unavailable</span>
                    ) : (
                      <>
                        {fmt.text} {fmt.unit}
                        {sig.quality === 'stale' ? ' (stale)' : ''}
                        {sig.baselineValue != null && sig.regressionOpen
                          ? ` · baseline ${formatLiveBaseline(sig).text}`
                          : ''}
                      </>
                    )}
                  </li>
                )
              })}
            </ul>
          </div>
        )}
        {!selectedLive && liveServices.some((s) => s.regressionOpen) && (
          <ul data-testid="pulse-live-regression-list" className="mt-2 space-y-0.5 font-mono text-[10px]">
            {liveServices
              .filter((s) => s.regressionOpen)
              .slice(0, 6)
              .map((s) => (
                <li key={s.serviceId}>
                  {s.graphNode || s.serviceId} · live regression
                </li>
              ))}
          </ul>
        )}
      </section>

      {!overview ? (
        <p className="text-gray-500">Historical Pulse overview unavailable this refresh.</p>
      ) : (
        <>
      <section>
        <h3 className="text-[10px] uppercase tracking-wide text-gray-400">Data sources</h3>
        <p className="mt-1 text-gray-800" title={src?.detail}>
          {src?.label ?? 'Pulse snapshot'}
        </p>
        <p className="mt-0.5 text-gray-500">
          Active Perch health probes remain on StatusPill /api/status. Persisted incidents are not
          proof of a current outage.
        </p>
        {dataSources?.observations === 'process_memory_unavailable' && (
          <p data-testid="pulse-overview-obs-unavailable" className="mt-1 text-amber-800">
            observations: process_memory_unavailable — not healthy.
          </p>
        )}
      </section>

      <section data-testid="pulse-overview-counts" className="grid grid-cols-2 gap-2 font-mono text-[10px]">
        <Stat label="mapped services" value={overview.mappedServiceCount} />
        <Stat label="incidents (page)" value={overview.incidentCount} />
        <Stat label="recovered" value={overview.recoveredCount} />
        <Stat label="unresolved" value={overview.unresolvedCount} />
      </section>
      <p className="text-[10px] text-gray-500">
        Unresolved means no recorded recovery on the persisted incident — not a live outage claim.
      </p>

      <section>
        <h3 className="text-[10px] uppercase tracking-wide text-gray-400">Most recent incident</h3>
        {overview.mostRecentIncident ? (
          <p data-testid="pulse-overview-latest-incident" className="mt-1">
            {incidentStateLabel(overview.mostRecentIncident)} ·{' '}
            {overview.mostRecentIncident.primaryServiceId} ·{' '}
            {formatPulseTimestamp(overview.mostRecentIncident.lastObservedAt)}
          </p>
        ) : (
          <p className="mt-1 text-gray-500">None in bounded list.</p>
        )}
      </section>

      <section>
        <h3 className="text-[10px] uppercase tracking-wide text-gray-400">Latest recorded change</h3>
        {overview.latestChange ? (
          <p data-testid="pulse-overview-latest-change" className="mt-1">
            {overview.latestChange.simulated ? 'Simulated marker · ' : ''}
            {overview.latestChange.changeType || 'change'} ·{' '}
            {formatPulseTimestamp(
              overview.latestChange.observedAt ??
                overview.latestChange.deployedAt ??
                overview.latestChange.createdAt,
            )}
          </p>
        ) : (
          <p className="mt-1 text-gray-500">None in bounded list.</p>
        )}
      </section>

      <section>
        <h3 className="text-[10px] uppercase tracking-wide text-gray-400">Environments</h3>
        <p data-testid="pulse-overview-envs" className="mt-1 font-mono text-[10px]">
          {overview.environments?.length ? overview.environments.join(', ') : '—'}
        </p>
      </section>

      {overview.latestEvidenceAt && (
        <section>
          <h3 className="text-[10px] uppercase tracking-wide text-gray-400">Latest evidence</h3>
          <p data-testid="pulse-overview-latest-evidence" className="mt-1 font-mono text-[10px]">
            {formatPulseTimestamp(overview.latestEvidenceAt)}
          </p>
        </section>
      )}

      {overview.servicesWithFindings?.length > 0 && (
        <section>
          <h3 className="text-[10px] uppercase tracking-wide text-gray-400">
            Services with recent findings
          </h3>
          <ul data-testid="pulse-overview-findings" className="mt-1 space-y-0.5 font-mono text-[10px]">
            {overview.servicesWithFindings.slice(0, 8).map((s) => (
              <li key={s.serviceId}>
                {s.graphNode || s.serviceId} · {s.intelligence}/{s.freshness}
              </li>
            ))}
          </ul>
        </section>
      )}

      <p data-testid="pulse-overview-no-metrics" className="text-[10px] text-gray-400">
        No CPU/memory/network resource cards — those metrics are not in the Pulse API contract.
      </p>

      {(limitations?.length > 0 || overview.limitations?.length > 0) && (
        <section data-testid="pulse-overview-limitations">
          <h3 className="text-[10px] uppercase tracking-wide text-gray-400">Limitations</h3>
          <ul className="mt-1 list-disc space-y-0.5 pl-4 text-gray-500">
            {[...(limitations ?? []), ...(overview.limitations ?? [])]
              .filter((v, i, a) => a.indexOf(v) === i)
              .map((l) => (
                <li key={l}>{l}</li>
              ))}
          </ul>
        </section>
      )}
        </>
      )}
    </div>
  )
}

function Stat({ label, value }) {
  return (
    <div className="rounded border border-gray-200 bg-gray-50 px-2 py-1.5">
      <div className="text-gray-400">{label}</div>
      <div className="text-sm text-gray-900">{value}</div>
    </div>
  )
}

function IncidentsPanel({
  incidents,
  incidentId,
  limitations,
  graphNodeByServiceId,
  stackName,
  nodeId,
}) {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [serviceFilter, setServiceFilter] = useState('')
  const [stateFilter, setStateFilter] = useState('all')

  const serviceOptions = useMemo(() => {
    const ids = new Set()
    for (const inc of incidents) {
      if (inc.primaryServiceId) ids.add(inc.primaryServiceId)
      for (const a of inc.affectedServiceIds ?? []) ids.add(a)
    }
    return [...ids].sort()
  }, [incidents])

  const filtered = useMemo(
    () =>
      sortIncidentsDeterministic(
        filterIncidents(incidents, {
          serviceId: serviceFilter,
          state: stateFilter,
        }),
      ),
    [incidents, serviceFilter, stateFilter],
  )

  if (incidentId) {
    const listIncident = incidents.find((i) => i.incidentId === incidentId) ?? null
    return (
      <InvestigationView
        incidentId={incidentId}
        listIncident={listIncident}
        graphNodeByServiceId={graphNodeByServiceId}
        stackName={stackName}
        nodeId={nodeId}
        onBack={() =>
          navigate(
            pulseStackPath(stackName, {
              nodeId,
              pulseTab: PULSE_TAB.INCIDENTS,
              incidentId: null,
              searchParams,
            }),
          )
        }
      />
    )
  }

  return (
    <div data-testid="pulse-incidents" className="space-y-2">
      <div className="flex flex-wrap gap-2">
        <label className="flex flex-col gap-0.5">
          <span className="text-[10px] uppercase tracking-wide text-gray-400">Service</span>
          <select
            data-testid="pulse-incident-filter-service"
            value={serviceFilter}
            onChange={(e) => setServiceFilter(e.target.value)}
            className="rounded border border-gray-200 bg-white px-1.5 py-1 text-[11px]"
          >
            <option value="">All</option>
            {serviceOptions.map((id) => (
              <option key={id} value={id}>
                {id}
              </option>
            ))}
          </select>
        </label>
        <label className="flex flex-col gap-0.5">
          <span className="text-[10px] uppercase tracking-wide text-gray-400">State</span>
          <select
            data-testid="pulse-incident-filter-state"
            value={stateFilter}
            onChange={(e) => setStateFilter(e.target.value)}
            className="rounded border border-gray-200 bg-white px-1.5 py-1 text-[11px]"
          >
            <option value="all">All</option>
            <option value={INCIDENT_STATE.RECOVERED}>Recovered</option>
            <option value={INCIDENT_STATE.UNRESOLVED}>Unresolved</option>
          </select>
        </label>
      </div>

      {limitations?.some((l) => String(l).toLowerCase().includes('may be incomplete')) && (
        <p data-testid="pulse-incidents-truncated" className="text-amber-800">
          Bounded API page — list may be incomplete at the current limit.
        </p>
      )}

      {filtered.length === 0 ? (
        <p data-testid="pulse-incidents-empty" className="text-gray-500">
          No incidents in this bounded list.
        </p>
      ) : (
        <ul data-testid="pulse-incident-browser" className="space-y-1.5">
          {filtered.map((inc) => (
            <li key={inc.incidentId}>
              <button
                type="button"
                data-testid={`pulse-sidebar-incident-${encodeURIComponent(inc.incidentId)}`}
                onClick={() =>
                  navigate(
                    pulseStackPath(stackName, {
                      nodeId,
                      pulseTab: PULSE_TAB.INCIDENTS,
                      incidentId: inc.incidentId,
                      searchParams,
                    }),
                  )
                }
                className="w-full rounded border border-gray-200 bg-gray-50 px-2 py-1.5 text-left hover:bg-gray-100"
              >
                <div className="flex items-baseline justify-between gap-2">
                  <span
                    data-testid="pulse-incident-lifecycle"
                    data-lifecycle={inc.lifecycle || (inc.open ? 'active' : 'recovered')}
                    className={
                      inc.open || inc.lifecycle === LIVE_LIFECYCLE.ACTIVE
                        ? 'rounded border border-orange-200 bg-orange-50 px-1 font-medium text-orange-900'
                        : 'rounded border border-gray-200 bg-gray-50 px-1 font-medium text-gray-700'
                    }
                  >
                    {incidentStateLabel(inc)}
                  </span>
                  <span className="font-mono text-[10px] text-gray-400">
                    {formatPulseTimestamp(inc.lastObservedAt)}
                  </span>
                </div>
                <div className="mt-0.5 truncate font-mono text-[10px] text-gray-600">
                  {inc.graphNode || inc.primaryServiceId}
                </div>
                <div className="mt-0.5 text-gray-500">
                  detected {formatPulseTimestamp(inc.firstDetectedAt)}
                  {inc.recoveredAt ? ` · recovered ${formatPulseTimestamp(inc.recoveredAt)}` : ''}
                </div>
                {inc.signalTypes?.length > 0 && (
                  <div className="mt-0.5 font-mono text-[10px] text-gray-500">
                    {inc.signalTypes.join(', ')} · affected {inc.affectedServiceIds?.length ?? 0}
                  </div>
                )}
                {inc.summary && (
                  <p className="mt-0.5 line-clamp-2 text-gray-800">{inc.summary}</p>
                )}
                {inc.lifecycle === LIVE_LIFECYCLE.ACTIVE && inc.liveSource === 'live' ? (
                  <p className="mt-0.5 text-[10px] text-orange-800">
                    ACTIVE from Live Pulse collector — telemetry-backed, not StatusPill health.
                  </p>
                ) : inc.open ? (
                  <p className="mt-0.5 text-[10px] text-amber-700">
                    Unresolved persisted record — not labeled as a live outage without fresh
                    telemetry.
                  </p>
                ) : null}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function ClassificationBadge({ label }) {
  const tone =
    label === 'OBSERVED'
      ? 'text-emerald-800 bg-emerald-50 border-emerald-200'
      : label === 'HYPOTHESIS' || label === 'INFERRED'
        ? 'text-amber-800 bg-amber-50 border-amber-200'
        : 'text-gray-600 bg-gray-50 border-gray-200'
  return (
    <span className={`inline-block rounded border px-1 py-0.5 text-[9px] font-semibold uppercase tracking-wide ${tone}`}>
      {label}
    </span>
  )
}

function EvidenceInvestigationReport({
  investigation,
  assessment,
  setAssessment,
  feedbackRationale,
  setFeedbackRationale,
  feedbackStatus,
  onSubmitFeedback,
  llmStatus,
  llmProposal,
  llmLoading,
  llmError,
  onGenerateLLM,
}) {
  const llmAvailable = Boolean(llmStatus?.available)
  return (
    <section data-testid="pulse-evidence-investigation" className="space-y-2 rounded border border-gray-200 bg-white px-2 py-2">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-[10px] uppercase tracking-wide text-gray-400">
          Deterministic investigation (F1)
        </h3>
        <ClassificationBadge label="OBSERVED" />
      </div>
      <p className="font-mono text-[10px] text-gray-500 break-all">{investigation.investigationId}</p>
      <p className="text-[10px] text-gray-500">
        State {investigation.incidentState || '—'} · freshness {investigation.evidenceFreshness || '—'} ·{' '}
        <span data-testid="pulse-method-deterministic">{investigation.generationMethod || 'deterministic'}</span>
      </p>
      <p data-testid="pulse-evidence-not-diagnosed" className="text-[10px] text-amber-800">
        Report generated ≠ diagnosed. Hypotheses are INFERRED — not proven root cause.
      </p>
      <p data-testid="pulse-method-compare-hint" className="text-[10px] text-gray-500">
        F1 is the evidence-backed baseline. Optional AI below is a separate proposal with citation
        checks — compare them; do not treat either as confirmed causation.
      </p>

      <div data-testid="pulse-evidence-symptoms">
        <div className="mb-0.5 flex items-center gap-1">
          <h4 className="text-[10px] uppercase tracking-wide text-gray-400">Symptoms</h4>
          <ClassificationBadge label="OBSERVED" />
        </div>
        <ul className="list-disc space-y-0.5 pl-4">
          {(investigation.observedSymptoms ?? []).length === 0 ? (
            <li className="text-gray-500">No observed symptoms in snapshot.</li>
          ) : (
            (investigation.observedSymptoms ?? []).map((s) => (
              <li key={s.id || s.statement}>{s.statement || s.id}</li>
            ))
          )}
        </ul>
      </div>

      <div data-testid="pulse-evidence-hypotheses">
        <div className="mb-0.5 flex items-center gap-1">
          <h4 className="text-[10px] uppercase tracking-wide text-gray-400">Hypotheses</h4>
          <ClassificationBadge label="HYPOTHESIS" />
        </div>
        <ul className="space-y-1.5">
          {(investigation.hypotheses ?? []).map((h) => (
            <li
              key={h.hypothesisId}
              data-testid={`pulse-hyp-${encodeURIComponent(h.hypothesisId)}`}
              className="rounded border border-amber-100 bg-amber-50/40 px-2 py-1.5"
            >
              <div className="font-mono text-[10px] text-gray-500">{h.hypothesisId}</div>
              <p className="text-gray-800">{h.statement}</p>
              {(h.supportingEvidenceRefs ?? []).length > 0 && (
                <p className="mt-0.5 font-mono text-[10px] text-gray-500">
                  evidence: {(h.supportingEvidenceRefs ?? []).slice(0, 4).join(', ')}
                </p>
              )}
              {h.suggestedVerification && (
                <p className="mt-0.5 text-[10px] text-gray-600">Verify: {h.suggestedVerification}</p>
              )}
            </li>
          ))}
        </ul>
      </div>

      <div data-testid="pulse-evidence-unknowns">
        <div className="mb-0.5 flex items-center gap-1">
          <h4 className="text-[10px] uppercase tracking-wide text-gray-400">Unknown / missing</h4>
          <ClassificationBadge label="UNKNOWN" />
        </div>
        <ul className="list-disc space-y-0.5 pl-4 text-gray-600">
          {[...(investigation.unknowns ?? []), ...(investigation.missingEvidence ?? [])]
            .slice(0, 8)
            .map((u) => (
              <li key={u}>{u}</li>
            ))}
        </ul>
      </div>

      <div data-testid="pulse-evidence-next-steps">
        <h4 className="text-[10px] uppercase tracking-wide text-gray-400">Suggested verification</h4>
        <ul className="mt-0.5 list-disc space-y-0.5 pl-4 text-gray-600">
          {(investigation.suggestedVerificationSteps ?? []).slice(0, 6).map((s) => (
            <li key={s}>{s}</li>
          ))}
        </ul>
      </div>

      <div data-testid="pulse-llm-investigation" className="space-y-1 border-t border-gray-100 pt-2">
        <div className="flex items-center gap-1">
          <h4 className="text-[10px] uppercase tracking-wide text-gray-400">
            Model proposal (optional LLM)
          </h4>
          <ClassificationBadge label="INFERRED" />
        </div>
        <p className="text-[10px] text-gray-500">
          Separate from F1. Citations must resolve to snapshot evidence. Unavailable when the server
          has not opted in — never invents a live model response in the UI.
        </p>
        {!llmAvailable && (
          <p data-testid="pulse-llm-unavailable" className="text-[10px] text-gray-500">
            {llmStatus?.reason ||
              'LLM investigation unavailable. Server must opt in with credentials; deterministic investigation still works.'}
          </p>
        )}
        <button
          type="button"
          data-testid="pulse-llm-generate-btn"
          onClick={onGenerateLLM}
          disabled={!llmAvailable || llmLoading}
          className="rounded border border-gray-300 bg-white px-2 py-1 text-[11px] text-gray-800 hover:bg-gray-50 disabled:opacity-50"
        >
          {llmLoading ? 'Generating AI proposal…' : 'Generate AI Investigation'}
        </button>
        {llmError && (
          <p data-testid="pulse-llm-error" className="text-red-600">
            {llmError}
          </p>
        )}
        {llmProposal && (
          <div data-testid="pulse-llm-proposal" className="space-y-1 rounded border border-amber-100 bg-amber-50/30 px-2 py-1.5">
            <p
              data-testid="pulse-f1-vs-llm"
              className="text-[10px] font-medium text-amber-900"
            >
              Comparing F1 (above) vs model proposal — mechanical citation pass ≠ semantic entailment.
            </p>
            <p className="font-mono text-[10px] text-gray-500 break-all">{llmProposal.proposalId}</p>
            <p className="text-[10px] text-gray-500">
              method <span data-testid="pulse-method-llm">{llmProposal.generationMethod}</span>
              {llmProposal.provenance?.model ? ` · model ${llmProposal.provenance.model}` : ''}
            </p>
            <p data-testid="pulse-llm-not-diagnosed" className="text-[10px] text-amber-800">
              AI proposal ≠ confirmed diagnosis.
            </p>
            <ul className="space-y-1">
              {(llmProposal.hypotheses ?? []).map((h) => (
                <li key={h.hypothesisId} className="text-gray-800">
                  <div className="font-mono text-[10px] text-gray-500">
                    {h.hypothesisId}
                    {h.supportStatus ? ` · ${h.supportStatus}` : ''}
                  </div>
                  <p>{h.statement}</p>
                  {h.supportStatus === 'validated' && (
                    <p className="text-[10px] text-gray-500">
                      Citations resolve mechanically — not proof the wording is entailed.
                    </p>
                  )}
                  {(h.supportingEvidenceRefs ?? []).length > 0 && (
                    <p className="font-mono text-[10px] text-gray-500">
                      evidence: {(h.supportingEvidenceRefs ?? []).slice(0, 4).join(', ')}
                    </p>
                  )}
                </li>
              ))}
            </ul>
            {(llmProposal.unknowns ?? []).length > 0 && (
              <ul className="list-disc pl-4 text-[10px] text-gray-600">
                {(llmProposal.unknowns ?? []).slice(0, 4).map((u) => (
                  <li key={u}>{u}</li>
                ))}
              </ul>
            )}
          </div>
        )}
      </div>

      <div data-testid="pulse-evidence-feedback" className="space-y-1 border-t border-gray-100 pt-2">
        <h4 className="text-[10px] uppercase tracking-wide text-gray-400">Feedback</h4>
        <p className="text-[10px] text-gray-500">
          Optional quality rating — not verified ground truth.
        </p>
        <label className="flex flex-col gap-0.5">
          <span className="text-[10px] text-gray-400">Assessment</span>
          <select
            data-testid="pulse-feedback-assessment"
            value={assessment}
            onChange={(e) => setAssessment(e.target.value)}
            className="rounded border border-gray-200 bg-white px-1.5 py-1 text-[11px]"
          >
            {FEEDBACK_ASSESSMENTS.map((a) => (
              <option key={a} value={a}>
                {a}
              </option>
            ))}
          </select>
        </label>
        <label className="flex flex-col gap-0.5">
          <span className="text-[10px] text-gray-400">Rationale (optional)</span>
          <textarea
            data-testid="pulse-feedback-rationale"
            value={feedbackRationale}
            onChange={(e) => setFeedbackRationale(e.target.value)}
            rows={2}
            className="rounded border border-gray-200 bg-white px-1.5 py-1 text-[11px]"
          />
        </label>
        <button
          type="button"
          data-testid="pulse-feedback-submit"
          onClick={onSubmitFeedback}
          className="rounded border border-gray-300 bg-white px-2 py-1 text-[11px] text-gray-800 hover:bg-gray-50"
        >
          Submit feedback
        </button>
        {feedbackStatus?.ok && (
          <p data-testid="pulse-feedback-ok" className="text-emerald-700">
            Feedback recorded.
          </p>
        )}
        {feedbackStatus && !feedbackStatus.ok && (
          <p data-testid="pulse-feedback-error" className="text-red-600">
            {feedbackStatus.error}
          </p>
        )}
      </div>
    </section>
  )
}

function InvestigationView({
  incidentId,
  listIncident,
  graphNodeByServiceId,
  stackName,
  nodeId,
  onBack,
}) {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const { detail, loading, error } = usePulseIncident(incidentId)
  const {
    investigation,
    loading: invLoading,
    error: invError,
    feedbackStatus,
    create: createInvestigation,
    submitFeedback,
    llmStatus,
    llmProposal,
    llmLoading,
    llmError,
    generateLLMProposal,
  } = usePulseInvestigation(incidentId)
  const [assessment, setAssessment] = useState('useful')
  const [feedbackRationale, setFeedbackRationale] = useState('')
  // Detail GET is historical file-store; overlay polled list lifecycle so ACTIVE
  // incidents do not flip to HISTORICAL when opening Investigate.
  const incident = useMemo(
    () => overlayIncidentLifecycle(detail?.incident, listIncident),
    [detail?.incident, listIncident],
  )
  const correlation = detail?.correlation
  const digestRows = useMemo(
    () => (incident ? buildDigestComparisonRows(incident) : []),
    [incident],
  )
  const corrSummary = correlationStrengthSummary(correlation)

  const selectService = (serviceId) => {
    const graphNode = graphNodeByServiceId.get(serviceId)
    if (!graphNode) return
    navigate(
      pulseStackPath(stackName, {
        nodeId: graphNode,
        pulseTab: PULSE_TAB.INCIDENTS,
        incidentId,
        searchParams,
      }),
    )
  }

  return (
    <div data-testid="pulse-investigation" className="space-y-3">
      <button
        type="button"
        data-testid="pulse-investigation-back"
        onClick={onBack}
        className="text-[11px] text-blue-600 hover:text-blue-800"
      >
        ← Back to incidents
      </button>

      {loading && <p className="text-gray-500">Loading incident…</p>}
      {error && (
        <p data-testid="pulse-investigation-error" className="text-red-600">
          Failed to load: {error}
        </p>
      )}

      {incident && (
        <>
          <section data-testid="pulse-investigation-actions" className="space-y-1">
            <button
              type="button"
              data-testid="pulse-investigate-btn"
              onClick={() => void createInvestigation()}
              disabled={invLoading}
              className="rounded border border-gray-300 bg-white px-2 py-1 text-[11px] font-medium text-gray-800 hover:bg-gray-50 disabled:opacity-50"
            >
              {invLoading ? 'Investigating…' : investigation ? 'Refresh investigation' : 'Investigate'}
            </button>
            <p className="text-[10px] text-gray-500">
              Assembles a bounded evidence snapshot — not a confirmed root cause. No LLM.
            </p>
            {invError && (
              <p data-testid="pulse-investigate-error" className="text-red-600">
                {invError}
              </p>
            )}
          </section>

          {investigation && (
            <EvidenceInvestigationReport
              investigation={investigation}
              assessment={assessment}
              setAssessment={setAssessment}
              feedbackRationale={feedbackRationale}
              setFeedbackRationale={setFeedbackRationale}
              feedbackStatus={feedbackStatus}
              onSubmitFeedback={() =>
                void submitFeedback({
                  assessment,
                  // Do not invent useful/rejected hypothesis selections.
                  usefulHypothesisIds: [],
                  rationale: feedbackRationale,
                })
              }
              llmStatus={llmStatus}
              llmProposal={llmProposal}
              llmLoading={llmLoading}
              llmError={llmError}
              onGenerateLLM={() => void generateLLMProposal()}
            />
          )}

          <section data-testid="pulse-investigation-summary" className="space-y-1">
            <h3 className="text-[10px] uppercase tracking-wide text-gray-400">Incident summary</h3>
            <div className="break-all font-mono text-[10px] text-gray-600">{incident.incidentId}</div>
            <div>
              <span data-testid="pulse-investigation-state">{incidentStateLabel(incident)}</span>
              {detail.graphNode ? ` · graph ${detail.graphNode}` : ''}
            </div>
            {!incident.open && (
              <p data-testid="pulse-investigation-historical" className="text-gray-500">
                Recovered historical record — not a current outage. After-window measurements are
                separate from the recorded recovery timestamp.
              </p>
            )}
            <dl className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5 font-mono text-[10px]">
              <dt className="text-gray-400">primary</dt>
              <dd>{incident.primaryServiceId}</dd>
              <dt className="text-gray-400">environment</dt>
              <dd>{incident.environment || '—'}</dd>
              <dt className="text-gray-400">detected</dt>
              <dd>{formatPulseTimestamp(incident.firstDetectedAt)}</dd>
              <dt className="text-gray-400">last observed</dt>
              <dd>{formatPulseTimestamp(incident.lastObservedAt)}</dd>
              <dt className="text-gray-400">recovered</dt>
              <dd>{formatPulseTimestamp(incident.recoveredAt)}</dd>
              <dt className="text-gray-400">signals</dt>
              <dd>{incident.signalTypes?.join(', ') || '—'}</dd>
            </dl>
            <p className="text-gray-800">{incident.summary}</p>
          </section>

          <section data-testid="pulse-investigation-digests">
            <h3 className="text-[10px] uppercase tracking-wide text-gray-400">
              Before / during / after
            </h3>
            <p className="mt-0.5 text-[10px] text-gray-500">
              Medians from incident digests — not interpolated time series.
            </p>
            <div className="mt-1 overflow-x-auto">
              <table className="w-full border-collapse text-left font-mono text-[10px]">
                <thead>
                  <tr className="border-b border-gray-200 text-gray-400">
                    <th className="py-1 pr-2 font-normal">Signal</th>
                    <th className="py-1 pr-2 font-normal">Before</th>
                    <th className="py-1 pr-2 font-normal">During</th>
                    <th className="py-1 font-normal">After</th>
                  </tr>
                </thead>
                <tbody>
                  {digestRows.map((row) => (
                    <tr key={row.signal} className="border-b border-gray-100 align-top">
                      <td className="py-1.5 pr-2">
                        <div>{row.signal}</div>
                        <div className="text-gray-400">{signalUnitLabel(row.signal)}</div>
                      </td>
                      {['before', 'during', 'after'].map((phase) => (
                        <td
                          key={phase}
                          data-testid={`pulse-digest-${row.signal}-${phase}`}
                          className="py-1.5 pr-2"
                        >
                          <div>{row[phase].display}</div>
                          <div className="text-gray-400">
                            n={row[phase].count ?? 'N/A'}
                          </div>
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>

          <section data-testid="pulse-investigation-observed">
            <h3 className="text-[10px] uppercase tracking-wide text-gray-400">Observed</h3>
            <ul className="mt-1 list-disc space-y-0.5 pl-4">
              {(incident.observations ?? []).map((o) => (
                <li key={o}>{o}</li>
              ))}
              {(incident.evidence ?? []).map((e) => (
                <li key={e.ref || e.summary} className="font-mono text-[10px]">
                  {e.ref}
                  {e.summary ? ` — ${e.summary}` : ''}
                </li>
              ))}
              {(incident.observations?.length ?? 0) === 0 &&
                (incident.evidence?.length ?? 0) === 0 && (
                  <li className="text-gray-500">No observation strings in snapshot.</li>
                )}
            </ul>
          </section>

          <section data-testid="pulse-investigation-inferred">
            <h3 className="text-[10px] uppercase tracking-wide text-gray-400">Inferred</h3>
            <ul className="mt-1 list-disc space-y-0.5 pl-4 text-gray-600">
              {(incident.inferences ?? []).map((o) => (
                <li key={o}>{o}</li>
              ))}
              {(incident.limitations ?? []).map((o) => (
                <li key={o}>Limitation: {o}</li>
              ))}
              {(detail.limitations ?? []).map((o) => (
                <li key={`api-${o}`}>Limitation: {o}</li>
              ))}
            </ul>
          </section>

          <section data-testid="pulse-investigation-affected">
            <h3 className="text-[10px] uppercase tracking-wide text-gray-400">Affected services</h3>
            <p className="mt-0.5 text-[10px] text-gray-500">
              Listed services are affected in the snapshot — not each a root cause.
            </p>
            <ul className="mt-1 space-y-0.5">
              {(incident.affectedServiceIds ?? []).map((sid) => {
                const graphNode = graphNodeByServiceId.get(sid)
                return (
                  <li key={sid}>
                    {graphNode ? (
                      <button
                        type="button"
                        data-testid={`pulse-affected-${encodeURIComponent(sid)}`}
                        onClick={() => selectService(sid)}
                        className="font-mono text-[10px] text-blue-600 hover:text-blue-800"
                      >
                        {sid} → graph {graphNode}
                        {nodeId === graphNode ? ' (selected)' : ''}
                      </button>
                    ) : (
                      <span className="font-mono text-[10px] text-gray-600">{sid} (unmapped)</span>
                    )}
                  </li>
                )
              })}
            </ul>
          </section>

          <section data-testid="pulse-investigation-correlation">
            <h3 className="text-[10px] uppercase tracking-wide text-gray-400">
              Change correlation
            </h3>
            <p data-testid="pulse-correlation-summary" className="mt-1 text-gray-800">
              {corrSummary.label}: {corrSummary.detail}
            </p>
            <p className="mt-0.5 text-[10px] text-gray-500">
              Correlation is not causation. Scores are not probabilities.
            </p>
            {(correlation?.candidates ?? []).length === 0 ? (
              <p className="mt-1 text-gray-500">No ranked candidates.</p>
            ) : (
              <ul className="mt-1 space-y-1.5">
                {(correlation.candidates ?? []).map((c) => (
                  <li
                    key={c.correlationId || c.changeId}
                    data-testid={`pulse-corr-${encodeURIComponent(c.changeId)}`}
                    className="rounded border border-gray-200 bg-gray-50 px-2 py-1.5"
                  >
                    <div className="flex justify-between gap-2">
                      <span className="font-medium">
                        #{c.rank ?? '?'} · {c.label}
                      </span>
                      <span className="font-mono text-[10px] text-gray-500">
                        score {c.score != null ? c.score : 'N/A'} (points)
                      </span>
                    </div>
                    <div className="mt-0.5 break-all font-mono text-[10px] text-gray-600">
                      {c.changeId}
                    </div>
                    <div className="mt-0.5 text-gray-500">
                      overlap {c.serviceOverlap || '—'}
                      {c.environmentMatch ? ' · env match' : ''}
                      {c.temporalDistanceSecs != null
                        ? ` · Δt ${Math.round(c.temporalDistanceSecs)}s`
                        : ''}
                    </div>
                    {(c.observations ?? []).slice(0, 3).map((o) => (
                      <p key={o} className="mt-0.5 text-gray-600">
                        {o}
                      </p>
                    ))}
                    {(c.inferences ?? []).slice(0, 2).map((o) => (
                      <p key={o} className="mt-0.5 text-gray-500 italic">
                        Inference: {o}
                      </p>
                    ))}
                  </li>
                ))}
              </ul>
            )}
          </section>
        </>
      )}
    </div>
  )
}

function ChangesPanel({ changes, limitations, graphNodeByServiceId, stackName, nodeId }) {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [serviceFilter, setServiceFilter] = useState('')
  const serviceOptions = useMemo(() => {
    const ids = new Set()
    for (const ch of changes) {
      for (const a of ch.serviceIds ?? []) ids.add(a)
    }
    return [...ids].sort()
  }, [changes])

  const filtered = useMemo(
    () => sortChangesDeterministic(filterChanges(changes, { serviceId: serviceFilter })),
    [changes, serviceFilter],
  )

  return (
    <div data-testid="pulse-changes" className="space-y-2">
      <p className="text-gray-500">
        Recorded change events (including those with no linked incident). Correlation ≠ causation.
      </p>
      <label className="flex flex-col gap-0.5">
        <span className="text-[10px] uppercase tracking-wide text-gray-400">Service</span>
        <select
          data-testid="pulse-change-filter-service"
          value={serviceFilter}
          onChange={(e) => setServiceFilter(e.target.value)}
          className="rounded border border-gray-200 bg-white px-1.5 py-1 text-[11px]"
        >
          <option value="">All</option>
          {serviceOptions.map((id) => (
            <option key={id} value={id}>
              {id}
            </option>
          ))}
        </select>
      </label>

      {limitations?.some((l) => String(l).toLowerCase().includes('may be incomplete')) && (
        <p data-testid="pulse-changes-truncated" className="text-amber-800">
          Bounded API page — list may be incomplete at the current limit.
        </p>
      )}

      {filtered.length === 0 ? (
        <p data-testid="pulse-changes-empty" className="text-gray-500">
          No recorded changes in this bounded list.
        </p>
      ) : (
        <ul data-testid="pulse-change-list" className="space-y-1.5">
          {filtered.map((ch) => (
            <li
              key={ch.changeId}
              data-testid={`pulse-change-${encodeURIComponent(ch.changeId)}`}
              className="rounded border border-gray-200 bg-gray-50 px-2 py-1.5"
            >
              <div className="flex items-baseline justify-between gap-2">
                <span className="font-medium text-gray-900">
                  {ch.changeType || 'change'}
                  {ch.simulated ? (
                    <span
                      data-testid="pulse-change-simulated"
                      className="ml-1 text-amber-700"
                    >
                      · simulated marker
                    </span>
                  ) : (
                    <span className="ml-1 text-gray-500">· recorded</span>
                  )}
                </span>
                <span className="font-mono text-[10px] text-gray-400">
                  {formatPulseTimestamp(ch.observedAt ?? ch.deployedAt ?? ch.createdAt)}
                </span>
              </div>
              <div className="mt-0.5 break-all font-mono text-[10px] text-gray-600">
                {ch.changeId}
              </div>
              {ch.title && <p className="mt-0.5 text-gray-800">{ch.title}</p>}
              {ch.summary && <p className="mt-0.5 text-gray-500">{ch.summary}</p>}
              <div className="mt-0.5 flex flex-wrap gap-1">
                {(ch.serviceIds ?? []).map((sid) => {
                  const graphNode = graphNodeByServiceId.get(sid)
                  if (!graphNode) {
                    return (
                      <span key={sid} className="font-mono text-[10px] text-gray-500">
                        {sid}
                      </span>
                    )
                  }
                  return (
                    <button
                      key={sid}
                      type="button"
                      onClick={() =>
                        navigate(
                          pulseStackPath(stackName, {
                            nodeId: graphNode,
                            pulseTab: PULSE_TAB.CHANGES,
                            searchParams,
                          }),
                        )
                      }
                      className="font-mono text-[10px] text-blue-600 hover:text-blue-800"
                    >
                      {graphNode}
                      {nodeId === graphNode ? ' (selected)' : ''}
                    </button>
                  )
                })}
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
