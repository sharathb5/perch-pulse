import { ReactFlowProvider } from '@xyflow/react'
import { X } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useParams } from 'react-router-dom'
import { DetailPanel } from '../components/DetailPanel.jsx'
import { Navbar } from '../components/Navbar.jsx'
import { mockNodes } from '../data/mock.js'
import { PerchGraph } from '../graph/PerchGraph.jsx'
import { usePerchData } from '../hooks/usePerchData.js'

export function StackView() {
  const { stackName, nodeId } = useParams()
  const [environment, setEnvironment] = useState('production')
  /** When set, banner stays hidden until `error` changes to a different message. */
  const [dismissedError, setDismissedError] = useState(null)

  const { nodes: flowNodes, edges: flowEdges, appName, error, refetch } = usePerchData(environment)
  const stackTitle = (appName && appName.trim() !== '' ? appName : stackName) ?? ''

  const node = useMemo(() => {
    const fromFlow = flowNodes.find((n) => n.id === nodeId)?.data
    if (fromFlow) {
      return fromFlow
    }
    return mockNodes.find((n) => n.id === nodeId) ?? null
  }, [flowNodes, nodeId])

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
      />

      <div className="flex min-h-0 w-full flex-1">
        <ReactFlowProvider>
          <div className="min-h-0 min-w-0 flex-1">
            <PerchGraph
              selectedNodeId={nodeId}
              nodes={flowNodes}
              edges={flowEdges}
              layoutResetKey={environment}
            />
          </div>
        </ReactFlowProvider>

        {nodeId != null && nodeId !== '' && (
          <DetailPanel node={node} environment={environment} />
        )}
      </div>
    </div>
  )
}
