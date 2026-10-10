import { useCallback, useEffect, useMemo, useRef } from 'react'
import {
  Background,
  Controls,
  MarkerType,
  MiniMap,
  ReactFlow,
  useEdgesState,
  useNodesState,
  useReactFlow,
} from '@xyflow/react'
import { useNavigate, useParams } from 'react-router-dom'
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
function FitViewOnReady({ nodeCount }) {
  const { fitView } = useReactFlow()
  useEffect(() => {
    const id = requestAnimationFrame(() => {
      fitView({ padding: 0.2 })
    })
    return () => cancelAnimationFrame(id)
  }, [fitView, nodeCount])
  return null
}

/** @param {{ selectedNodeId?: string, nodes?: import('@xyflow/react').Node[], edges?: import('@xyflow/react').Edge[], layoutResetKey?: string }} props */
export function PerchGraph({ selectedNodeId, nodes: nodesProp, edges: edgesProp, layoutResetKey = 'default' }) {
  const { stackName } = useParams()
  const navigate = useNavigate()
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
      navigate(`/stack/${stackName}/${node.id}`)
    },
    [navigate, stackName],
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
        <Controls className="!bottom-3 !left-3 rounded-md border border-gray-200 bg-white shadow-sm" />
        <FitViewOnReady nodeCount={nodes.length} />
      </ReactFlow>
    </div>
  )
}
