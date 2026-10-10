import { describe, expect, it } from 'vitest'
import { deriveStatus } from '../lib/mappers.js'

/**
 * Contract: unknown/missing must not be styled as healthy.
 * StatusPill maps status → label; deriveStatus is the source of truth for live data.
 */
describe('health indicator contract', () => {
  it('maps missing status rows to unknown (not healthy)', () => {
    expect(deriveStatus({ provider: 'vercel', name: 'web' }, undefined)).toBe('unknown')
  })

  it('keeps unhealthy probes as down', () => {
    expect(deriveStatus({ provider: 'vercel', name: 'web' }, { healthy: false })).toBe('down')
  })

  it('uses degraded for elevated error_rate while healthy flag is true', () => {
    expect(
      deriveStatus({ provider: 'render', name: 'api' }, { healthy: true, error_rate: 0.02 }),
    ).toBe('degraded')
  })
})
