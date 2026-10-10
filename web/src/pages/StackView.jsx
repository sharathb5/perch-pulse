import { ReactFlowProvider } from '@xyflow/react'
import { X } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
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
  enrichServicePulse,
  normalizePulseTab,
  pulseStackPath,
} from '../lib/pulse.js'

/**
 * Panel coordination (Milestone C / ADR-017):
 * DetailPanel (300px) and PulseSidebar (360px) may both be open on the right.
 * Closing Pulse preserves selected nodeId. Escape closes Pulse (or clears
 * incident) before dismissing the detail panel.
 */
export function StackView() {
  const { stackName, nodeId } = useParams()
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const [environment, setEnvironment] = useState('production')
  /** When set, banner stays hidden until `error` changes to a different message. */
  const [dismissedError, setDismissedError] = useState(null)

  const pulseTab = normalizePulseTab(searchParams.get('pulse'))
  const incidentId = decodeIncidentId(searchParams.get('incident'))
  const pulseOpen = pulseTab != null || incidentId !== ''

  const { nodes: flowNodes, edges: flowEdges, appName, error, refetch: refetchGraph } = usePerchData(environment)
  const {
    getForNode,
    graphNodeByServiceId,
    overview,
    incidents,
    changes,
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

  const nodesWithPulse = useMemo(() => {
    // Never join Pulse onto demo/last-known mock topology — IDs may collide with real graph_node values.
    if (graphDemo) {
      return flowNodes
    }
    return flowNodes.map((n) => {
      const pulse = getForNode(n.id)
      if (!pulse) {
        return n
      }
      const enriched = enrichServicePulse(pulse, pulse.relatedIncidents ?? [], {
        expectedEnvironment: environment,
      })
      return {
        ...n,
        data: {
          ...n.data,
          // Probe status from /api/status must remain the StatusPill source.
          status: n.data.status,
          pulse: enriched,
        },
      }
    })
  }, [flowNodes, getForNode, environment, graphDemo])

  const node = useMemo(() => {
    const fromFlow = nodesWithPulse.find((n) => n.id === nodeId)?.data
    if (fromFlow) {
      return fromFlow
    }
    return mockNodes.find((n) => n.id === nodeId) ?? null
  }, [nodesWithPulse, nodeId])

  const selectedPulse = !graphDemo && nodeId ? getForNode(nodeId) : null
  const selectedPulseEnriched = useMemo(() => {
    if (!selectedPulse) {
      return null
    }
    return enrichServicePulse(selectedPulse, selectedPulse.relatedIncidents ?? [], {
      expectedEnvironment: environment,
    })
  }, [selectedPulse, environment])

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
      }),
    )
  }, [navigate, stackName, nodeId, pulseTab, incidentId])

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
            }),
          )
          return
        }
        navigate(pulseStackPath(stackName, { nodeId, pulseTab: null, incidentId: null }))
        return
      }
      if (nodeId) {
        navigate(`/stack/${stackName}`)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [pulseOpen, incidentId, navigate, stackName, nodeId, pulseTab])

  const showBanner = error != null && error !== '' && error !== dismissedError

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
          tab={pulseTab || (incidentId ? 'incidents' : 'overview')}
          incidentId={incidentId}
          overview={overview}
          incidents={incidents}
          changes={changes}
          limitations={limitations}
          loading={pulseLoading}
          error={pulseError}
          staleSnapshot={pulseStaleSnapshot}
          hasSnapshot={pulseHasSnapshot}
          dataSources={pulseDataSources}
          graphNodeByServiceId={graphNodeByServiceId}
          graphDemo={graphDemo}
        />
      </div>
    </div>
  )
}
