import { useCallback, useEffect, useMemo, useRef } from 'react'
import {
  Background,
  Controls,
  ControlButton,
  MarkerType,
  MiniMap,
  ReactFlow,
  useEdgesState,
  useNodesState,
  useReactFlow,
} from '@xyflow/react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { paymentPathNodes, wantsPaymentPathFocus } from '../lib/demoFocus.js'
import { decodeIncidentId, decodeRunId, normalizePulseTab, pulseStackPath } from '../lib/pulse.js'
import { ServiceCard } from '../components/ServiceCard.jsx'
import { mockEdges, mockNodes } from '../data/mock.js'
import { getLayoutedElements, NODE_HEIGHT, NODE_WIDTH, separateOverlappingNodes } from './layout.js'

const nodeTypes = { serviceCard: ServiceCard }

const defaultEdgeOptions = {
  animated: true,
  style: { stroke: '#94a3b8' },
  markerEnd: {
    type: MarkerType.ArrowClosed,
    color: '#94a3b8',
  },
}

function buildFlowElements() {
  const rawNodes = mockNodes.map((n) => ({
    id: n.id,
    type: 'serviceCard',
    data: { ...n },
    position: { x: 0, y: 0 },
  }))
  const rawEdges = mockEdges.map((e) => ({
    id: e.id,
    source: e.source,
    target: e.target,
    animated: e.animated,
  }))
  return getLayoutedElements(rawNodes, rawEdges)
}

/** Re-fit when the graph node set changes (e.g. live data replaces mock). */
function FitViewOnReady({ nodeCount, preferPaymentPath }) {
  const { fitView, getNodes } = useReactFlow()
  useEffect(() => {
    const id = requestAnimationFrame(() => {
      const path = preferPaymentPath ? paymentPathNodes(getNodes()) : []
      if (path.length >= 2) {
        fitView({ nodes: path, padding: 0.45, maxZoom: 1.05, duration: 300 })
      } else {
        fitView({ padding: 0.2 })
      }
    })
    return () => cancelAnimationFrame(id)
  }, [fitView, getNodes, nodeCount, preferPaymentPath])
  return null
}

/** Demo framing: zoom to static frontend → checkout → payment path (full graph preserved). */
function PaymentPathControls() {
  const { fitView, getNodes } = useReactFlow()

  const focusPaymentPath = useCallback(() => {
    const path = paymentPathNodes(getNodes())
    if (path.length < 2) return
    fitView({ nodes: path, padding: 0.45, maxZoom: 1.05, duration: 400 })
  }, [fitView, getNodes])

  const showAll = useCallback(() => {
    fitView({ padding: 0.2, duration: 400 })
  }, [fitView])

  return (
    <Controls className="!bottom-3 !left-3 rounded-md border border-gray-200 bg-white shadow-sm">
      <ControlButton
        type="button"
        data-testid="focus-payment-path"
        onClick={focusPaymentPath}
        title="Focus frontend → checkout → payment"
        aria-label="Focus frontend checkout payment path"
      >
        <span className="text-[10px] font-semibold leading-none tracking-tight">pay</span>
      </ControlButton>
      <ControlButton
        type="button"
        data-testid="fit-all-graph"
        onClick={showAll}
        title="Fit entire graph"
        aria-label="Fit entire graph"
      >
        <span className="text-[10px] font-semibold leading-none tracking-tight">all</span>
      </ControlButton>
    </Controls>
  )
}

/** @param {{ selectedNodeId?: string, nodes?: import('@xyflow/react').Node[], edges?: import('@xyflow/react').Edge[], layoutResetKey?: string }} props */
export function PerchGraph({ selectedNodeId, nodes: nodesProp, edges: edgesProp, layoutResetKey = 'default' }) {
  const { stackName } = useParams()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const preferPaymentPath = wantsPaymentPathFocus(searchParams)
  const defaultFlow = useMemo(() => buildFlowElements(), [])
  const baseNodes = nodesProp ?? defaultFlow.nodes
  const baseEdges = edgesProp ?? defaultFlow.edges

  /** Dragged positions survive parent re-layout (e.g. /api poll). Cleared when layoutResetKey changes. */
  const userPositionsRef = useRef({})
  const lastLayoutKeyRef = useRef(layoutResetKey)

  const [nodes, setNodes, onNodesChange] = useNodesState(baseNodes)
  const [edges, setEdges, onEdgesChange] = useEdgesState(baseEdges)

  useEffect(() => {
    if (lastLayoutKeyRef.current !== layoutResetKey) {
      userPositionsRef.current = {}
      lastLayoutKeyRef.current = layoutResetKey
    }
  }, [layoutResetKey])

  useEffect(() => {
    const src = nodesProp ?? defaultFlow.nodes
    setNodes(
      src.map((n) => ({
        ...n,
        data: {
          ...n.data,
          environment: layoutResetKey,
        },
        position: userPositionsRef.current[n.id] ?? n.position,
        selected: n.id === selectedNodeId,
      })),
    )
    setEdges(edgesProp ?? defaultFlow.edges)
  }, [nodesProp, edgesProp, defaultFlow.nodes, defaultFlow.edges, selectedNodeId, layoutResetKey, setNodes, setEdges])

  const onNodeDragStop = useCallback(() => {
    setNodes((nds) => {
      const sep = separateOverlappingNodes(nds, NODE_WIDTH, NODE_HEIGHT)
      for (const n of sep) {
        userPositionsRef.current[n.id] = { ...n.position }
      }
      return sep.map((n) => ({
        ...n,
        selected: n.id === selectedNodeId,
      }))
    })
  }, [selectedNodeId, setNodes])

  const onNodeClick = useCallback(
    (_, node) => {
      // Preserve Pulse sidebar URL state when selecting a graph node (ADR-017).
      navigate(
        pulseStackPath(stackName, {
          nodeId: node.id,
          pulseTab: normalizePulseTab(searchParams.get('pulse')),
          incidentId: decodeIncidentId(searchParams.get('incident')) || null,
          runId: decodeRunId(searchParams.get('run')) || null,
        }),
      )
    },
    [navigate, stackName, searchParams],
  )

  return (
    <div data-testid="perch-graph" className="h-full w-full bg-white">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeDragStop={onNodeDragStop}
        nodeTypes={nodeTypes}
        onNodeClick={onNodeClick}
        snapToGrid
        snapGrid={[12, 12]}
        defaultEdgeOptions={defaultEdgeOptions}
        proOptions={{ hideAttribution: true }}
        minZoom={0.2}
        maxZoom={1.5}
      >
        <Background variant="dots" color="#e5e7eb" />
        <MiniMap className="!bottom-3 !right-3 !h-20 !w-28 rounded-md border border-gray-200 bg-white" />
        <PaymentPathControls />
        <FitViewOnReady nodeCount={nodes.length} preferPaymentPath={preferPaymentPath} />
      </ReactFlow>
    </div>
  )
}
