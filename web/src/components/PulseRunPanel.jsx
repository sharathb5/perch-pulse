import { useEffect } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { usePulseRun } from '../hooks/usePulseRun.js'
import { usePulseWorkflows } from '../hooks/usePulseWorkflows.js'
import {
  RUN_STATUS,
  formatPulseTimestamp,
  pulseStackPath,
  runFailureSummary,
  runPhaseLabel,
} from '../lib/pulse.js'

/**
 * Run Pulse tab: allowlisted on-demand diagnostics (Milestone E).
 * Distinct from historical incidents and active /api/status probes.
 *
 * @param {{
 *   runId: string,
 *   onHighlightsChange?: (state: { enabled: boolean, graphNodes: string[], errorNodes: string[] }) => void,
 *   highlightsEnabled?: boolean,
 *   highlightsCleared?: boolean,
 *   onClearHighlights?: () => void,
 * }} props
 */
export function PulseRunPanel({
  runId,
  onHighlightsChange,
  highlightsEnabled = true,
  highlightsCleared = false,
  onClearHighlights,
}) {
  const { stackName, nodeId } = useParams()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const { workflows, loading: wfLoading, error: wfError } = usePulseWorkflows({ enabled: true })
  const { run, loading, error, starting, startRun } = usePulseRun(runId)

  return (
    <div data-testid="pulse-run-panel" className="space-y-3">
      <p className="text-[10px] text-gray-500">
        On-demand diagnostic — not continuous monitoring, not incident detection, and not active
        probe health.
      </p>
      {wfError && (
        <p data-testid="pulse-run-workflows-error" className="text-amber-800">
          Workflow catalog unavailable: {wfError}
        </p>
      )}
      {error && (
        <p data-testid="pulse-run-error" className="text-amber-800" role="status">
          {error}
        </p>
      )}

      {!runId && (
        <WorkflowPicker
          workflows={workflows}
          loading={wfLoading || starting}
          onStart={async (id) => {
            const res = await startRun(id)
            if (res.ok && res.run?.id) {
              navigate(
                pulseStackPath(stackName, {
                  nodeId,
                  pulseTab: 'run',
                  runId: res.run.id,
                  searchParams,
                }),
              )
            }
          }}
        />
      )}

      {runId && (
        <RunDetail
          run={run}
          loading={loading}
          runId={runId}
          highlightsEnabled={highlightsEnabled}
          onClearHighlights={onClearHighlights}
          onBack={() =>
            navigate(
              pulseStackPath(stackName, { nodeId, pulseTab: 'run', runId: null, searchParams }),
            )
          }
          onSelectNode={(graphNode) =>
            navigate(
              pulseStackPath(stackName, {
                nodeId: graphNode,
                pulseTab: 'run',
                runId,
                searchParams,
              }),
            )
          }
        />
      )}

      <HighlightSync
        run={run}
        onHighlightsChange={onHighlightsChange}
        highlightsCleared={highlightsCleared}
      />
    </div>
  )
}

/** Syncs completed-run graph targets to StackView. */
function HighlightSync({ run, onHighlightsChange, highlightsCleared }) {
  useEffect(() => {
    if (typeof onHighlightsChange !== 'function') return
    // Do not re-enable after the user clears highlights for this run.
    if (highlightsCleared) {
      return
    }
    if (run?.status === RUN_STATUS.COMPLETED) {
      onHighlightsChange({
        enabled: true,
        graphNodes: run.graphNodes ?? [],
        errorNodes: run.graphNodesWithErrors ?? [],
      })
    } else {
      onHighlightsChange({ enabled: false, graphNodes: [], errorNodes: [] })
    }
  }, [run, onHighlightsChange, highlightsCleared])
  return null
}

function WorkflowPicker({ workflows, loading, onStart }) {
  if (loading && workflows.length === 0) {
    return <p data-testid="pulse-run-workflows-loading">Loading workflows…</p>
  }
  if (workflows.length === 0) {
    return (
      <p data-testid="pulse-run-no-workflows" className="text-gray-500">
        No diagnostic workflows are registered on this server.
      </p>
    )
  }
  return (
    <ul data-testid="pulse-run-workflows" className="space-y-2">
      {workflows.map((wf) => (
        <li
          key={wf.id}
          data-testid={`pulse-run-workflow-${wf.id}`}
          className="rounded border border-gray-200 p-2"
        >
          <div className="text-[12px] font-medium text-gray-900">{wf.displayName}</div>
          <div className="mt-0.5 text-[10px] text-gray-500">{wf.description}</div>
          <div className="mt-1 space-y-0.5 text-[10px] text-gray-600">
            <div>Environment: {wf.environment || '—'}</div>
            <div>Request: {wf.requestSummary || '—'}</div>
            <div>Signals: {(wf.supportedSignals || []).join(', ') || '—'}</div>
            {wf.expectedAppServices?.length > 0 && (
              <div>
                Validation identities (not drawn as visited): {wf.expectedAppServices.join(', ')}
              </div>
            )}
          </div>
          {!wf.available ? (
            <p
              data-testid={`pulse-run-unavailable-${wf.id}`}
              className="mt-2 text-[11px] text-amber-800"
            >
              Unavailable: {wf.unavailabilityReason || 'not configured'}
            </p>
          ) : (
            <button
              type="button"
              data-testid={`pulse-run-start-${wf.id}`}
              disabled={loading}
              onClick={() => void onStart(wf.id)}
              className="mt-2 rounded border border-gray-300 bg-gray-50 px-2 py-1 text-[11px] font-medium text-gray-900 hover:bg-gray-100 disabled:opacity-50"
            >
              Start diagnostic
            </button>
          )}
        </li>
      ))}
    </ul>
  )
}

function RunDetail({
  run,
  loading,
  runId,
  highlightsEnabled,
  onClearHighlights,
  onBack,
  onSelectNode,
}) {
  if (!run && loading) {
    return <p data-testid="pulse-run-loading">Loading diagnostic {runId}…</p>
  }
  if (!run) {
    return (
      <div data-testid="pulse-run-missing">
        <p className="text-amber-800">
          Run not found (expired, never existed, or lost after viz restart).
        </p>
        <button
          type="button"
          data-testid="pulse-run-back"
          onClick={onBack}
          className="mt-2 text-[11px] text-gray-700 underline"
        >
          Back to workflows
        </button>
      </div>
    )
  }

  const running =
    run.status === RUN_STATUS.PENDING || run.status === RUN_STATUS.RUNNING
  const completed = run.status === RUN_STATUS.COMPLETED
  const failed =
    run.status === RUN_STATUS.FAILED || run.status === RUN_STATUS.TIMED_OUT

  return (
    <div data-testid="pulse-run-detail" className="space-y-2">
      <div className="flex items-start justify-between gap-2">
        <div>
          <div className="text-[12px] font-medium text-gray-900">{run.displayName}</div>
          <div className="text-[10px] text-gray-500">
            {run.id} · started {formatPulseTimestamp(run.startedAt)}
          </div>
        </div>
        <button
          type="button"
          data-testid="pulse-run-back"
          onClick={onBack}
          className="text-[11px] text-gray-700 underline"
        >
          Workflows
        </button>
      </div>

      <div data-testid="pulse-run-status" data-status={run.status} className="text-[11px]">
        Status:{' '}
        <span className={completed ? 'text-gray-900' : failed ? 'text-amber-800' : 'text-gray-700'}>
          {run.status}
        </span>
      </div>

      {running && (
        <p data-testid="pulse-run-phase" className="text-[11px] text-gray-700">
          {runPhaseLabel(run.phase)}
        </p>
      )}

      {failed && (
        <p data-testid="pulse-run-failure" className="text-amber-800">
          {runFailureSummary(run)}
        </p>
      )}

      {run.request && (
        <section data-testid="pulse-run-request" className="rounded border border-gray-100 p-2">
          <div className="text-[10px] font-medium uppercase tracking-wide text-gray-400">
            Observed request
          </div>
          <div className="mt-1 space-y-0.5 text-[11px]">
            <div>
              {run.request.method} → HTTP {run.request.statusCode ?? '—'}{' '}
              {run.request.ok ? '(200 OK)' : '(not successful)'}
            </div>
            <div>
              Server-measured elapsed:{' '}
              {run.request.elapsedMs != null ? `${run.request.elapsedMs.toFixed(1)} ms` : '—'}
            </div>
            <div className="truncate text-gray-500" title={run.request.urlRedacted}>
              {run.request.urlRedacted}
            </div>
          </div>
        </section>
      )}

      {completed && run.trace && (
        <section data-testid="pulse-run-trace" className="space-y-2">
          <div className="rounded border border-gray-100 p-2">
            <div className="text-[10px] font-medium uppercase tracking-wide text-gray-400">
              Observed trace
            </div>
            <div className="mt-1 space-y-0.5 text-[11px]">
              <div>Trace ID: {run.trace.traceId || '—'}</div>
              <div>
                Services in marked subgraph: {run.trace.observedServices?.length ?? 0} (
                {(run.trace.observedServices || []).join(', ') || '—'})
              </div>
              <div>
                Spans: {run.trace.subgraphSpanCount} in subgraph / {run.trace.spanCount} total
              </div>
              <div>
                Trace wall time (subgraph):{' '}
                {run.trace.traceDurationMs != null
                  ? `${run.trace.traceDurationMs.toFixed(1)} ms`
                  : 'unknown'}
              </div>
              <div>
                Cross-service edge:{' '}
                {run.trace.hasCrossServiceEdge ? 'observed' : 'missing evidence'}
              </div>
            </div>
          </div>

          <div data-testid="pulse-run-services">
            <div className="text-[10px] font-medium uppercase tracking-wide text-gray-400">
              Participating services (from trace)
            </div>
            <ul className="mt-1 space-y-1">
              {(run.services || []).map((s) => (
                <li
                  key={s.otelServiceName}
                  className="flex items-center justify-between gap-2 text-[11px]"
                >
                  <span>
                    {s.otelServiceName}
                    {s.hasError ? ' · error spans' : ''}
                    {!s.mapped ? ' · unmapped' : ''}
                  </span>
                  {s.mapped && s.graphNode ? (
                    <button
                      type="button"
                      data-testid={`pulse-run-service-${s.graphNode}`}
                      onClick={() => onSelectNode(s.graphNode)}
                      className="text-gray-700 underline"
                    >
                      {s.graphNode}
                    </button>
                  ) : (
                    <span className="text-gray-400">no graph node</span>
                  )}
                </li>
              ))}
            </ul>
          </div>

          {run.unmappedServices?.length > 0 && (
            <p data-testid="pulse-run-unmapped" className="text-[11px] text-amber-800">
              Unmapped evidence: {run.unmappedServices.join(', ')}
            </p>
          )}

          {(run.trace.services || []).length > 0 && (
            <div data-testid="pulse-run-timing">
              <div className="text-[10px] font-medium uppercase tracking-wide text-gray-400">
                Per-service span timing (individual spans — not overlapping sum)
              </div>
              <table className="mt-1 w-full text-left text-[10px] text-gray-700">
                <thead>
                  <tr className="text-gray-400">
                    <th className="py-0.5 font-medium">Service</th>
                    <th className="py-0.5 font-medium">Spans</th>
                    <th className="py-0.5 font-medium">Longest span</th>
                  </tr>
                </thead>
                <tbody>
                  {run.trace.services.map((s) => (
                    <tr key={s.otelServiceName}>
                      <td className="py-0.5">{s.otelServiceName}</td>
                      <td className="py-0.5">{s.spanCount}</td>
                      <td className="py-0.5">
                        {s.longestSpanDurationMs != null
                          ? `${s.longestSpanDurationMs.toFixed(1)} ms`
                          : '—'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {(run.trace.crossServiceEdges || []).length > 0 && (
            <div data-testid="pulse-run-edges">
              <div className="text-[10px] font-medium uppercase tracking-wide text-gray-400">
                Cross-service relationships
              </div>
              <ul className="mt-1 space-y-0.5 text-[11px]">
                {run.trace.crossServiceEdges.map((e, i) => (
                  <li key={`${e.parentService}-${e.childService}-${i}`}>
                    {e.parentService} → {e.childService}
                  </li>
                ))}
              </ul>
            </div>
          )}

          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              data-testid="pulse-run-clear-highlights"
              onClick={onClearHighlights}
              disabled={!highlightsEnabled}
              className="rounded border border-gray-300 px-2 py-1 text-[11px] text-gray-800 hover:bg-gray-50 disabled:opacity-40"
            >
              Clear graph highlights
            </button>
          </div>
        </section>
      )}

      {(run.warnings || []).length > 0 && (
        <ul data-testid="pulse-run-warnings" className="list-disc pl-4 text-[10px] text-amber-800">
          {run.warnings.map((w) => (
            <li key={w}>{w}</li>
          ))}
        </ul>
      )}
    </div>
  )
}
