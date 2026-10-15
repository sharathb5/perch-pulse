import { describe, expect, it } from 'vitest'
import { PAYMENT_PATH_NODE_IDS, paymentPathNodes, wantsPaymentPathFocus } from './demoFocus.js'

describe('demoFocus', () => {
  it('lists frontend → checkout → payment', () => {
    expect(PAYMENT_PATH_NODE_IDS).toEqual(['frontend', 'checkout', 'payment'])
  })

  it('filters to the payment path without inventing nodes', () => {
    const nodes = [
      { id: 'frontend' },
      { id: 'cart' },
      { id: 'checkout' },
      { id: 'payment' },
      { id: 'shipping' },
    ]
    expect(paymentPathNodes(nodes).map((n) => n.id)).toEqual(['frontend', 'checkout', 'payment'])
  })

  it('returns empty when path nodes are absent', () => {
    expect(paymentPathNodes([{ id: 'github' }, { id: 'vercel' }])).toEqual([])
  })

  it('detects focus=payment-path query', () => {
    expect(wantsPaymentPathFocus(new URLSearchParams('focus=payment-path'))).toBe(true)
    expect(wantsPaymentPathFocus(new URLSearchParams('focus=payment'))).toBe(true)
    expect(wantsPaymentPathFocus(new URLSearchParams('env=local'))).toBe(false)
  })
})
