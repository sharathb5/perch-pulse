import { Handle, Position } from '@xyflow/react'
import { useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { buildErrorPrompt, isErroredNode, openInAIWithFallback } from '../lib/aiHandoff.js'
import { ProviderBadge } from './ProviderBadge.jsx'
import { StatusPill } from './StatusPill.jsx'

function providerLabel(provider) {
  const map = {
    vercel: 'Vercel',
    supabase: 'Supabase',
    github: 'GitHub',
    render: 'Render',
    custom: 'Custom',
  }
  return map[provider] ?? provider
}

function metaValueClass(semantic) {
  if (semantic === 'success') return 'text-green-600'
  if (semantic === 'warning') return 'text-amber-500'
  if (semantic === 'danger') return 'text-red-500'
  return 'font-medium text-gray-900'
}

export function ServiceCard({ data, selected }) {
  const { stackName } = useParams()
  const navigate = useNavigate()
  const id = data.id

  const go = () => {
    navigate(`/stack/${stackName}/${id}`)
  }

  const ring = selected ? 'border-gray-400 ring-2 ring-gray-200' : 'border-gray-200'
  const showAIActions = isErroredNode(data)
  const [openInMenuOpen, setOpenInMenuOpen] = useState(false)

  const openAI = async (tool, event) => {
    event.stopPropagation()
    const prompt = buildErrorPrompt({ node: data, environment: data?.environment })
    try {
      await openInAIWithFallback({ tool, prompt })
    } catch {
      // silent; DetailPanel has richer fallback messaging
    }
    setOpenInMenuOpen(false)
  }

  return (
    <div data-testid={`service-node-${id}`} className="relative w-[210px] max-w-[210px]">
      <Handle type="target" position={Position.Left} className="h-2 w-2 border-0 bg-transparent opacity-0" />
      <Handle type="source" position={Position.Right} className="h-2 w-2 border-0 bg-transparent opacity-0" />

      <div className={`max-w-[210px] overflow-hidden rounded-xl border bg-white ${ring}`}>
        <button
          type="button"
          onClick={go}
          className="w-full max-w-full border-b border-gray-200 bg-gray-50 px-2.5 py-2 text-left hover:bg-gray-50"
        >
          <div className="flex min-w-0 items-center justify-between gap-2">
            <div className="flex min-w-0 flex-1 items-center gap-2">
              <ProviderBadge provider={data.provider} />
              <span className="min-w-0 truncate text-xs font-medium text-gray-900">{providerLabel(data.provider)}</span>
            </div>
            <div className="shrink-0">
              <StatusPill status={data.status} />
            </div>
          </div>
        </button>

        <button type="button" onClick={go} className="w-full max-w-full px-2.5 pb-2 pt-2 text-left">
          <div className="min-w-0 truncate text-[16px] font-medium leading-tight text-black" title={data.label}>
            {data.label}
          </div>
          <div className="mt-0.5 min-w-0 truncate font-mono text-[11px] text-gray-400" title={data.url}>
            {data.url}
          </div>
        </button>

        <button type="button" onClick={go} className="w-full max-w-full border-t border-gray-200 px-2.5 py-2 text-left">
          <div className="flex min-w-0 flex-col gap-1">
            {data.meta?.map((row) => (
              <div key={row.key} className="flex min-w-0 items-baseline justify-between gap-2 font-mono text-xs">
                <span className="max-w-[45%] shrink-0 truncate text-gray-500" title={row.key}>
                  {row.key}
                </span>
                <span
                  className={`min-w-0 max-w-[55%] truncate text-right ${metaValueClass(row.semantic)}`}
                  title={row.value}
                >
                  {row.value}
                </span>
              </div>
            ))}
          </div>
        </button>

        <div className="flex min-w-0 border-t border-gray-200">
          {data.tabs?.map((tab) => (
            <button
              key={tab}
              type="button"
              onClick={(e) => {
                e.stopPropagation()
                go()
              }}
              className="min-w-0 flex-1 truncate border-r border-gray-200 px-1.5 py-2 text-xs text-gray-500 last:border-r-0 hover:bg-gray-50 hover:text-gray-900"
              title={tab}
            >
              {tab}
            </button>
          ))}
        </div>
        {showAIActions && (
          <div className="relative border-t border-gray-200 p-1.5">
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation()
                setOpenInMenuOpen((v) => !v)
              }}
              className="w-full rounded border border-gray-200 px-1.5 py-1.5 text-[11px] text-gray-700 hover:bg-gray-50 hover:text-gray-900"
              title="Open in"
            >
              Open in
            </button>
            {openInMenuOpen && (
              <div className="absolute left-1.5 right-1.5 z-20 mt-1 rounded-md border border-gray-200 bg-white p-1 shadow-lg">
                <button
                  type="button"
                  onClick={(e) => void openAI('codex', e)}
                  className="block w-full rounded px-2 py-1.5 text-left text-[11px] text-gray-800 hover:bg-gray-50"
                >
                  Codex
                </button>
                <button
                  type="button"
                  onClick={(e) => void openAI('cursor', e)}
                  className="block w-full rounded px-2 py-1.5 text-left text-[11px] text-gray-800 hover:bg-gray-50"
                >
                  Cursor
                </button>
                <button
                  type="button"
                  onClick={(e) => void openAI('claude', e)}
                  className="block w-full rounded px-2 py-1.5 text-left text-[11px] text-gray-800 hover:bg-gray-50"
                >
                  Claude Code
                </button>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
