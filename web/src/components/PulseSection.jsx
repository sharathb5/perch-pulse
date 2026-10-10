import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { usePulseIncident } from '../hooks/usePulseIncident.js'
import {
  PULSE_KIND,
  decodeIncidentId,
  describePulseDataSources,
  pulseIncidentPath,
  pulseKindLabel,
} from '../lib/pulse.js'

function formatTs(value) {
  if (value == null || value === '') {
    return '—'
  }
  const d = new Date(String(value))
  if (Number.isNaN(d.getTime())) {
    return String(value)
  }
  return d.toISOString().replace('T', ' ').replace(/\.\d{3}Z$/, 'Z')
}

/**
 * Compact Pulse intelligence block for DetailPanel (Milestone B — not full investigation).
 * @param {{
 *   pulse: object | null,
 *   dataSources?: object | null,
 *   pulseError?: string | null,
 *   staleSnapshot?: boolean,
 *   lastSuccessAt?: string | null,
 *   graphDemo?: boolean,
 * }} props
 */
export function PulseSection({
  pulse,
  dataSources,
  pulseError,
  staleSnapshot = false,
  lastSuccessAt,
  graphDemo = false,
}) {
  const { stackName, nodeId } = useParams()
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const selectedIncidentId = decodeIncidentId(searchParams.get('incident'))
  const { detail, loading: detailLoading, error: detailError } = usePulseIncident(selectedIncidentId)
  const sourceInfo = describePulseDataSources(dataSources)
  const incidentNodeMismatch =
    detail?.graphNode &&
    nodeId &&
    String(detail.graphNode) !== String(nodeId) &&
    !(pulse?.relatedIncidentIds ?? []).includes(detail?.incident?.incidentId)

  const openIncident = (incidentId) => {
    navigate(pulseIncidentPath(stackName, nodeId, incidentId))
  }

  const clearIncident = () => {
    navigate(pulseIncidentPath(stackName, nodeId))
  }

  return (
    <div data-testid="pulse-section" className="border-b border-gray-200 px-3 py-2">
      <div className="flex items-baseline justify-between gap-2">
        <div className="text-[10px] uppercase tracking-wide text-gray-400">Pulse intelligence</div>
        {lastSuccessAt && (
          <div
            data-testid="pulse-refreshed-at"
            className="truncate text-[10px] text-gray-400"
            title={`Pulse snapshot generated at ${lastSuccessAt}`}
          >
            snapshot {formatTs(lastSuccessAt)}
          </div>
        )}
      </div>

      {graphDemo && (
        <p data-testid="pulse-demo-disclaimer" className="mt-1 text-[11px] text-amber-800">
          Graph is showing last-known or demo topology — Pulse below is not merged as live stack health.
        </p>
      )}

      <p className="mt-1 text-[11px] text-gray-500" title={sourceInfo.detail}>
        {sourceInfo.label}
      </p>

      {pulseError && (
        <p
          data-testid="pulse-error"
          role="status"
          aria-live="polite"
          className="mt-1.5 text-[11px] text-amber-800"
        >
          {pulse
            ? `Pulse refresh failed — showing last snapshot (may be stale): ${pulseError}`
            : `Pulse unavailable: ${pulseError}`}
        </p>
      )}
      {staleSnapshot && pulse && !pulseError && (
        <p data-testid="pulse-stale-flag" className="mt-1 text-[10px] text-amber-700">
          Pulse snapshot may be stale after a failed refresh.
        </p>
      )}

      {!pulse && !pulseError && (
        <p data-testid="pulse-empty" className="mt-1.5 text-[11px] text-gray-500">
          No Pulse mapping for this service.
        </p>
      )}

      {pulse && (
        <div className="mt-1.5 space-y-1.5 text-[11px] text-gray-700">
          <div className="flex flex-wrap items-baseline justify-between gap-x-2 gap-y-0.5">
            <span data-testid="pulse-kind" data-pulse-kind={pulse.kind} className="font-medium text-gray-900">
              {pulseKindLabel(pulse.kind) || 'Pulse'}
            </span>
            <span className="font-mono text-[10px] text-gray-400" title={pulse.serviceId}>
              {pulse.serviceId}
            </span>
          </div>
          <p className="text-gray-500">{pulse.hint}</p>
          {pulse.envMismatch && (
            <p data-testid="pulse-env-mismatch" className="text-amber-700">
              Some incidents list a different environment than the current header selection.
            </p>
          )}
          <div className="grid grid-cols-2 gap-x-2 gap-y-1 font-mono text-[10px]">
            <div>
              <span className="text-gray-400">incidents</span> {pulse.incidentCount}
            </div>
            <div>
              <span className="text-gray-400">open</span> {pulse.openCount}
            </div>
            <div className="col-span-2">
              <span className="text-gray-400">latest</span> {formatTs(pulse.latestIncidentAt)}
            </div>
            <div className="col-span-2">
              <span className="text-gray-400">freshness</span> {pulse.freshness}
              {pulse.intelligenceAvailable ? ` · intel ${pulse.intelligence}` : ' · intel unavailable'}
            </div>
            {pulse.signalTypes?.length > 0 && (
              <div className="col-span-2">
                <span className="text-gray-400">signals</span> {pulse.signalTypes.join(', ')}
              </div>
            )}
          </div>
          {pulse.latestSummary !== '' && (
            <p className="text-[11px] leading-snug text-gray-800" title={pulse.latestSummary}>
              {pulse.latestSummary}
            </p>
          )}
          {pulse.kind === PULSE_KIND.HISTORICAL && (
            <p className="text-[10px] text-gray-500">Recovered incidents are historical — not a current outage.</p>
          )}

          {pulse.relatedIncidents?.length > 0 && (
            <ul data-testid="pulse-incident-list" className="mt-1 space-y-1">
              {pulse.relatedIncidents.slice(0, 5).map((inc) => (
                <li key={inc.incidentId}>
                  <button
                    type="button"
                    data-testid={`pulse-incident-link-${encodeURIComponent(inc.incidentId)}`}
                    onClick={() => openIncident(inc.incidentId)}
                    className="w-full truncate rounded border border-gray-200 bg-gray-50 px-1.5 py-1 text-left font-mono text-[10px] text-blue-600 hover:bg-gray-100 hover:text-blue-800"
                    title={inc.incidentId}
                  >
                    {inc.open ? 'open' : 'recovered'} · {formatTs(inc.lastObservedAt)}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {selectedIncidentId !== '' && (
        <div
          data-testid="pulse-incident-ref"
          className="mt-2 rounded-md border border-gray-200 bg-gray-50 p-2 text-[11px] text-gray-700"
        >
          <div className="flex items-start justify-between gap-2">
            <div className="text-[10px] uppercase tracking-wide text-gray-400">Incident reference</div>
            <button
              type="button"
              onClick={clearIncident}
              className="text-[10px] text-gray-500 hover:text-gray-800"
            >
              Close
            </button>
          </div>
          {detailLoading && <p className="mt-1 text-gray-500">Loading incident…</p>}
          {detailError && <p className="mt-1 text-red-600">Failed to load: {detailError}</p>}
          {detail?.incident && incidentNodeMismatch && (
            <p data-testid="pulse-incident-mismatch" className="mt-1 text-amber-800">
              This incident maps to graph node “{detail.graphNode}”, not “{nodeId}”. Showing reference only —
              do not treat it as this service’s active health.
            </p>
          )}
          {detail?.incident && (
            <div className="mt-1 space-y-1">
              <div className="break-all font-mono text-[10px] text-gray-600">{detail.incident.incidentId}</div>
              <div>
                {detail.incident.open ? (
                  <span className="text-amber-700">open</span>
                ) : (
                  <span className="text-gray-600">recovered</span>
                )}
                {detail.graphNode ? ` · graph ${detail.graphNode}` : ''}
              </div>
              {detail.incident.signalTypes?.length > 0 && (
                <div className="font-mono text-[10px]">signals: {detail.incident.signalTypes.join(', ')}</div>
              )}
              <p className="leading-snug text-gray-800">{detail.incident.summary}</p>
              <p className="text-[10px] text-gray-500">
                Detected {formatTs(detail.incident.firstDetectedAt)} · last{' '}
                {formatTs(detail.incident.lastObservedAt)}
                {detail.incident.recoveredAt ? ` · recovered ${formatTs(detail.incident.recoveredAt)}` : ''}
              </p>
              {detail.correlation && (
                <p className="text-[10px] text-gray-500">
                  Correlation attached (not causation). Full investigation is Milestone C.
                </p>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
