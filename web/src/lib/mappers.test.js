import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import {
  buildMeta,
  deriveStatus,
  mapGraphToEdges,
  mapGraphToNodes,
  tabsForProvider,
} from './mappers.js'

const fixturesDir = join(dirname(fileURLToPath(import.meta.url)), '../../fixtures')

function load(name) {
  return JSON.parse(readFileSync(join(fixturesDir, name), 'utf8'))
}

const graphOk = load('graph.ok.json')
const statusOk = load('status.ok.json')
const graphEmpty = load('graph.empty.json')
const statusEmpty = load('status.empty.json')
const statusPartial = load('status.stale-unknown.json')

describe('mapGraphToNodes — identity and health', () => {
  it('maps real graph+status fixtures with stable node ids', () => {
    const nodes = mapGraphToNodes(graphOk, statusOk)
    expect(nodes.map((n) => n.id)).toEqual(['github', 'web', 'api', 'db'])
    expect(nodes.find((n) => n.id === 'web').label).toBe('web')
    expect(nodes.find((n) => n.id === 'web').provider).toBe('vercel')
  })

  it('derives health without inventing healthy for missing rows', () => {
    const nodes = mapGraphToNodes(graphOk, statusPartial)
    expect(nodes.find((n) => n.id === 'web').status).toBe('healthy')
    expect(nodes.find((n) => n.id === 'api').status).toBe('unknown')
    expect(nodes.find((n) => n.id === 'db').status).toBe('unknown')
    expect(nodes.find((n) => n.id === 'github').status).toBe('source')
  })

  it('marks down and degraded from status fields', () => {
    const nodes = mapGraphToNodes(graphOk, statusOk)
    expect(nodes.find((n) => n.id === 'db').status).toBe('down')
    expect(nodes.find((n) => n.id === 'api').status).toBe('degraded')
    expect(nodes.find((n) => n.id === 'web').status).toBe('healthy')
  })

  it('handles empty graph without throwing or inventing nodes', () => {
    expect(mapGraphToNodes(graphEmpty, statusEmpty)).toEqual([])
  })
})

describe('mapGraphToEdges — associations', () => {
  it('preserves from→to as source→target with deterministic ids', () => {
    const edges = mapGraphToEdges(graphOk)
    expect(edges).toHaveLength(4)
    expect(edges.map((e) => [e.source, e.target])).toEqual([
      ['github', 'web'],
      ['github', 'api'],
      ['web', 'db'],
      ['api', 'db'],
    ])
    expect(edges.every((e) => e.animated === true)).toBe(true)
    expect(new Set(edges.map((e) => e.id)).size).toBe(edges.length)
  })

  it('returns empty edges for empty graph', () => {
    expect(mapGraphToEdges(graphEmpty)).toEqual([])
  })
})

describe('deriveStatus / buildMeta / tabs', () => {
  it('never treats missing status as healthy', () => {
    expect(deriveStatus({ name: 'x', provider: 'vercel' }, undefined)).toBe('unknown')
  })

  it('includes recent_errors in meta when present', () => {
    const meta = buildMeta(
      { name: 'api', provider: 'render' },
      { healthy: true, error_rate: 0.05, recent_errors: ['upstream timeout'] },
    )
    expect(meta.find((m) => m.key === 'recent_errors')?.value).toContain('upstream timeout')
  })

  it('returns provider tab sets without inventing pulse tabs', () => {
    expect(tabsForProvider('vercel')).toEqual(['logs', 'env', 'deploy'])
    expect(tabsForProvider('custom')).toEqual(['command', 'output'])
  })
})

describe('mock data honesty (documented current behavior)', () => {
  it('fixture stack ids do not match demo mock ids for live mapping', async () => {
    const { mockNodes } = await import('../data/mock.js')
    const liveIds = mapGraphToNodes(graphOk, statusOk).map((n) => n.id)
    const mockIds = mockNodes.map((n) => n.id)
    // Live fixture uses web/api/db; mock uses vercel/render/supabase — must stay distinguishable.
    expect(liveIds).not.toEqual(mockIds)
    expect(mockIds).toContain('vercel')
    expect(liveIds).toContain('web')
  })
})
