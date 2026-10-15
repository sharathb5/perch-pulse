import { describe, expect, it } from 'vitest'
import { envFromSearchParams } from './graphEnv.js'

describe('envFromSearchParams', () => {
  it('defaults to production when env is absent', () => {
    expect(envFromSearchParams(new URLSearchParams(''))).toBe('production')
  })

  it('reads ?env=local for Astronomy Shop / Live Pulse demos', () => {
    expect(envFromSearchParams(new URLSearchParams('env=local'))).toBe('local')
  })

  it('trims whitespace', () => {
    expect(envFromSearchParams(new URLSearchParams('env=%20staging%20'))).toBe('staging')
  })
})
