import { describe, expect, it } from 'vitest'
import { appendGraphChromeQuery, envFromSearchParams } from './graphEnv.js'

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

describe('appendGraphChromeQuery', () => {
  it('copies env and focus from searchParams', () => {
    const qs = new URLSearchParams()
    appendGraphChromeQuery(qs, new URLSearchParams('env=local&focus=payment-path&pulse=overview'))
    expect(qs.get('env')).toBe('local')
    expect(qs.get('focus')).toBe('payment-path')
    expect(qs.get('pulse')).toBeNull()
  })

  it('lets explicit overrides win', () => {
    const qs = new URLSearchParams()
    appendGraphChromeQuery(qs, new URLSearchParams('env=local'), { env: 'dev', focus: null })
    expect(qs.get('env')).toBe('dev')
    expect(qs.get('focus')).toBeNull()
  })
})
