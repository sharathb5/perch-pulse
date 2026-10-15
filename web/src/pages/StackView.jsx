import { ReactFlowProvider } from '@xyflow/react'
import { X } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { liveEnvAllowsGraphJoin } from '../lib/livePulse.js'
import { envFromSearchParams } from '../lib/graphEnv.js'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { DetailPanel } from '../components/DetailPanel.jsx'
import { Navbar } from '../components/Navbar.jsx'
import { PulseSidebar } from '../components/PulseSidebar.jsx'
import { mockNodes } from '../data/mock.js'
import { PerchGraph } from '../graph/PerchGraph.jsx'
import { usePerchData } from '../hooks/usePerchData.js'
import { usePulseData } from '../hooks/usePulseData.js'
import {
  decodeIncidentId,
  decodeRunId,
  enrichServicePulse,
  normalizePulseTab,
  pulseStackPath,
} from '../lib/pulse.js'
import { RunPulseHighlightProvider } from '../lib/runPulseHighlight.jsx'

/**
 * Panel coordination (Milestone C / ADR-017):
 * DetailPanel (300px) and PulseSidebar (360px) may both be open on the right.
 * Closing Pulse preserves selected nodeId. Escape closes Pulse (or clears
 * incident) before dismissing the detail panel.
 */
export function StackView() {
  const { stackName, nodeId } = useParams()
  const [searchParams, setSearchParams] = useSearchParams()
  const navigate = useNavigate()
  // Derive from URL — avoids setState-in-effect when landing on /?env=local.
  const environment = envFromSearchParams(searchParams)
  /** When set, banner stays hidden until `error` changes to a different message. */
  const [dismissedError, setDismissedError] = useState(null)

  const setEnvironment = useCallback(
    (next) => {
      const params = new URLSearchParams(searchParams)
      if (next) {
        params.set('env', next)
      } else {
        params.delete('env')
      }
      setSearchParams(params, { replace: true })
    },
    [searchParams, setSearchParams],
  )

  const pulseTab = normalizePulseTab(searchParams.get('pulse'))
  const incidentId = decodeIncidentId(searchParams.get('incident'))
  const runId = decodeRunId(searchParams.get('run'))
  const pulseOpen = pulseTab != null || incidentId !== '' || runId !== ''

  const { nodes: flowNodes, edges: flowEdges, appName, error, refetch: refetchGraph } = usePerchData(environment)
  const {
    getForNode,
    getLiveForNode,
    graphNodeByServiceId,
    overview,
    incidents,
    changes,
    liveStatus,
    liveServicesResponse,
    unmappedLive,
    limitations,
    dataSources: pulseDataSources,
    error: pulseError,
    staleSnapshot: pulseStaleSnapshot,
    generatedAt: pulseGeneratedAt,
    lastSuccessAt: pulseLastSuccessAt,
    loading: pulseLoading,
    hasSnapshot: pulseHasSnapshot,
    refetch: refetchPulse,
  } = usePulseData()
  const stackTitle = (appName && appName.trim() !== '' ? appName : stackName) ?? ''
  const graphDemo = error != null && error !== ''

  const [runHighlight, setRunHighlight] = useState({
    enabled: false,
    graphNodes: [],
    errorNodes: [],
  })
  // Cleared highlights are scoped to a run id — changing runId auto-resets.
  const [clearedForRunId, setClearedForRunId] = useState('')
  const highlightsCleared = Boolean(runId) && clearedForRunId === runId

  const onRunHighlightsChange = useCallback((state) => {
    setRunHighlight(state)
  }, [])

  const onClearRunHighlights = useCallback(() => {
    if (runId) {
      setClearedForRunId(runId)
    }
    setRunHighlight((prev) => ({ ...prev, enabled: false }))
  }, [runId])

  const nodesWithPulse = useMemo(() => {
    // Never join Pulse onto demo/last-known mock topology — IDs may collide with real graph_node values.
    const showRun =
      !graphDemo &&
      Boolean(runId) &&
      runHighlight.enabled &&
      !highlightsCleared &&
      Array.isArray(runHighlight.graphNodes)
    const participated = new Set(showRun ? runHighlight.graphNodes : [])
    const errored = new Set(showRun ? runHighlight.errorNodes : [])

    return flowNodes.map((n) => {
      const pulse = !graphDemo ? getForNode(n.id) : null
      // Join Live Pulse by exact graph_node only. Block when triad envs disagree.
      let liveService = !graphDemo ? getLiveForNode(n.id) : null
      if (
        liveService &&
        !liveEnvAllowsGraphJoin(
          liveStatus?.environment || liveService.environment,
          environment,
        )
      ) {
        liveService = null
      }
      const enriched = pulse
        ? enrichServicePulse(pulse, pulse.relatedIncidents ?? incidents, {
            expectedEnvironment: environment,
            liveService: liveService ?? pulse.liveService ?? null,
          })
        : liveService
          ? enrichServicePulse(
              {
                serviceId: liveService.serviceId,
                graphNode: liveService.graphNode,
                mappingSource: liveService.mappingSource,
                intelligence: liveService.intelligence,
                intelligenceAvailable: true,
                freshness: liveService.freshness,
                lastObservedAt: liveService.lastObservedAt,
                openIncidentIds: liveService.openIncidentIds,
                relatedIncidentIds: [],
                limitations: liveService.limitations,
                activeHealthNote: liveService.activeHealthNote,
              },
              incidents,
              { expectedEnvironment: environment, liveService },
            )
          : null
      let runPulse = null
      if (participated.has(n.id)) {
        runPulse = { participation: errored.has(n.id) ? 'error' : 'observed' }
      }
      if (!enriched && !runPulse) {
        return n
      }
      return {
        ...n,
        data: {
          ...n.data,
          // Probe status from /api/status must remain the StatusPill source.
          status: n.data.status,
          ...(enriched ? { pulse: enriched } : {}),
          ...(runPulse ? { runPulse } : { runPulse: null }),
        },
      }
    })
  }, [
    flowNodes,
    getForNode,
    getLiveForNode,
    liveStatus,
    incidents,
    environment,
    graphDemo,
    runHighlight,
    highlightsCleared,
    runId,
  ])

  const node = useMemo(() => {
    const fromFlow = nodesWithPulse.find((n) => n.id === nodeId)?.data
    if (fromFlow) {
      return fromFlow
    }
    return mockNodes.find((n) => n.id === nodeId) ?? null
  }, [nodesWithPulse, nodeId])

  const selectedPulse = !graphDemo && nodeId ? getForNode(nodeId) : null
  const selectedLiveRaw = !graphDemo && nodeId ? getLiveForNode(nodeId) : null
  const selectedLive =
    selectedLiveRaw &&
    liveEnvAllowsGraphJoin(
      liveStatus?.environment || selectedLiveRaw.environment,
      environment,
    )
      ? selectedLiveRaw
      : null
  const selectedPulseEnriched = useMemo(() => {
    if (!selectedPulse) {
      return null
    }
    return enrichServicePulse(selectedPulse, selectedPulse.relatedIncidents ?? incidents, {
      expectedEnvironment: environment,
      liveService: selectedLive ?? null,
    })
  }, [selectedPulse, selectedLive, incidents, environment])

  const refetch = useCallback(() => {
    void refetchGraph()
    void refetchPulse()
  }, [refetchGraph, refetchPulse])

  const openPulse = useCallback(() => {
    navigate(
      pulseStackPath(stackName, {
        nodeId,
        pulseTab: pulseTab || 'overview',
        incidentId: incidentId || null,
        runId: runId || null,
      }),
    )
  }, [navigate, stackName, nodeId, pulseTab, incidentId, runId])

  useEffect(() => {
    const onKey = (e) => {
      if (e.key !== 'Escape') return
      if (pulseOpen) {
        if (incidentId) {
          navigate(
            pulseStackPath(stackName, {
              nodeId,
              pulseTab: pulseTab || 'incidents',
              incidentId: null,
              runId: runId || null,
            }),
          )
          return
        }
        if (runId) {
          navigate(
            pulseStackPath(stackName, {
              nodeId,
              pulseTab: pulseTab || 'run',
              runId: null,
            }),
          )
          return
        }
        navigate(
          pulseStackPath(stackName, {
            nodeId,
            pulseTab: null,
            incidentId: null,
            runId: null,
          }),
        )
        return
      }
      if (nodeId) {
        navigate(`/stack/${stackName}`)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [pulseOpen, incidentId, runId, navigate, stackName, nodeId, pulseTab])

  const showBanner = error != null && error !== '' && error !== dismissedError

  const runParticipated = useMemo(() => {
    const show =
      !graphDemo &&
      Boolean(runId) &&
      runHighlight.enabled &&
      !highlightsCleared &&
      Array.isArray(runHighlight.graphNodes)
    return new Set(show ? runHighlight.graphNodes : [])
  }, [graphDemo, runHighlight, highlightsCleared, runId])
  const runErrored = useMemo(() => {
    const show =
      !graphDemo &&
      Boolean(runId) &&
      runHighlight.enabled &&
      !highlightsCleared &&
      Array.isArray(runHighlight.errorNodes)
    return new Set(show ? runHighlight.errorNodes : [])
  }, [graphDemo, runHighlight, highlightsCleared, runId])

  return (
    <div className="flex h-screen flex-col bg-white">
      {showBanner && (
        <div
          data-testid="error-banner"
          className="flex shrink-0 items-center justify-between gap-3 border-b border-amber-200 bg-amber-50 px-4 py-2 text-sm text-amber-900"
        >
          <span>Could not reach perch — showing last known or demo data</span>
          <button
            type="button"
            onClick={() => setDismissedError(error)}
            className="rounded p-1 text-amber-800 hover:bg-amber-100"
            aria-label="Dismiss"
          >
            <X className="h-4 w-4" strokeWidth={2} />
          </button>
        </div>
      )}
      <Navbar
        stackName={stackTitle}
        environment={environment}
        onEnvironmentChange={setEnvironment}
        onRefresh={refetch}
        onOpenPulse={openPulse}
        pulseOpen={pulseOpen}
      />

      <div className="flex min-h-0 w-full flex-1">
        <RunPulseHighlightProvider participated={runParticipated} errored={runErrored}>
          <ReactFlowProvider>
            <div className="min-h-0 min-w-0 flex-1">
              <PerchGraph
                selectedNodeId={nodeId}
                nodes={nodesWithPulse}
                edges={flowEdges}
                layoutResetKey={environment}
              />
            </div>
          </ReactFlowProvider>
        </RunPulseHighlightProvider>

        {nodeId != null && nodeId !== '' && (
          <DetailPanel
            node={node}
            environment={environment}
            pulse={selectedPulseEnriched}
            pulseDataSources={pulseDataSources}
            pulseError={pulseError}
            pulseStaleSnapshot={pulseStaleSnapshot}
            pulseLastSuccessAt={pulseGeneratedAt ?? pulseLastSuccessAt}
            graphDemo={graphDemo}
            escapeDisabled
          />
        )}

        <PulseSidebar
          open={pulseOpen}
          tab={pulseTab || (incidentId ? 'incidents' : runId ? 'run' : 'overview')}
          incidentId={incidentId}
          runId={runId}
          overview={overview}
          incidents={incidents}
          changes={changes}
          liveStatus={liveStatus}
          liveServices={liveServicesResponse?.services ?? []}
          unmappedLive={unmappedLive}
          selectedLive={selectedLive}
          limitations={limitations}
          loading={pulseLoading}
          error={pulseError}
          staleSnapshot={pulseStaleSnapshot}
          hasSnapshot={pulseHasSnapshot}
          dataSources={pulseDataSources}
          graphNodeByServiceId={graphNodeByServiceId}
          graphDemo={graphDemo}
          onRunHighlightsChange={onRunHighlightsChange}
          runHighlightsEnabled={runHighlight.enabled && !highlightsCleared}
          runHighlightsCleared={highlightsCleared}
          onClearRunHighlights={onClearRunHighlights}
        />
      </div>
    </div>
  )
}
