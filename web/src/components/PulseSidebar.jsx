import { X } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { usePulseIncident } from '../hooks/usePulseIncident.js'
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
 *   limitations?: string[],
 *   loading?: boolean,
 *   error?: string | null,
 *   staleSnapshot?: boolean,
 *   hasSnapshot?: boolean,
 *   dataSources?: object | null,
 *   graphNodeByServiceId?: Map<string, string>,
 *   graphDemo?: boolean,
 * }} props
 */
export function PulseSidebar({
  open,
  tab,
  incidentId,
  overview,
  incidents = [],
  changes = [],
  limitations = [],
  loading = false,
  error = null,
  staleSnapshot = false,
  hasSnapshot = false,
  dataSources = null,
  graphNodeByServiceId = new Map(),
  graphDemo = false,
}) {
  const { stackName, nodeId } = useParams()
  const navigate = useNavigate()
  const activeTab = tab || PULSE_TAB.OVERVIEW

  const closeSidebar = () => {
    navigate(
      pulseStackPath(stackName, {
        nodeId,
        pulseTab: null,
        incidentId: null,
      }),
    )
  }

  const setTab = (next) => {
    navigate(
      pulseStackPath(stackName, {
        nodeId,
        pulseTab: next,
        incidentId: next === PULSE_TAB.INCIDENTS ? incidentId : null,
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

      <div className="min-h-0 flex-1 overflow-y-auto px-3 py-2 text-[11px] text-gray-700">
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
      </div>
    </aside>
  )
}

function OverviewPanel({ overview, limitations, dataSources }) {
  if (!overview) {
    return (
      <p data-testid="pulse-overview-empty" className="text-gray-500">
        No Pulse overview data yet.
      </p>
    )
  }
  const src = overview.sourceInfo
  return (
    <div data-testid="pulse-overview" className="space-y-3">
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
    return (
      <InvestigationView
        incidentId={incidentId}
        graphNodeByServiceId={graphNodeByServiceId}
        stackName={stackName}
        nodeId={nodeId}
        onBack={() =>
          navigate(
            pulseStackPath(stackName, {
              nodeId,
              pulseTab: PULSE_TAB.INCIDENTS,
              incidentId: null,
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
                    }),
                  )
                }
                className="w-full rounded border border-gray-200 bg-gray-50 px-2 py-1.5 text-left hover:bg-gray-100"
              >
                <div className="flex items-baseline justify-between gap-2">
                  <span className="font-medium text-gray-900">
                    {inc.open ? 'Unresolved' : 'Recovered'}
                  </span>
                  <span className="font-mono text-[10px] text-gray-400">
                    {formatPulseTimestamp(inc.lastObservedAt)}
                  </span>
                </div>
                <div className="mt-0.5 truncate font-mono text-[10px] text-gray-600">
                  {inc.primaryServiceId}
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
                {inc.open && (
                  <p className="mt-0.5 text-[10px] text-amber-700">
                    Unresolved persisted record — not labeled as a live outage without fresh
                    telemetry.
                  </p>
                )}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function InvestigationView({ incidentId, graphNodeByServiceId, stackName, nodeId, onBack }) {
  const navigate = useNavigate()
  const { detail, loading, error } = usePulseIncident(incidentId)
  const incident = detail?.incident
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
