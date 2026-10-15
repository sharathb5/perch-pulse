/** Node ids on the Astronomy Shop static dependency path used for demo framing. */
export const PAYMENT_PATH_NODE_IDS = Object.freeze(['frontend', 'checkout', 'payment'])

/**
 * Filter React Flow nodes to the frontend → checkout → payment path.
 * Preserves full-graph data; callers only use this for fitView framing.
 *
 * @param {Iterable<{ id?: string }>} nodes
 * @returns {{ id?: string }[]}
 */
export function paymentPathNodes(nodes) {
  const want = new Set(PAYMENT_PATH_NODE_IDS)
  const list = Array.isArray(nodes) ? nodes : [...(nodes || [])]
  return list.filter((n) => n && want.has(n.id))
}

/**
 * @param {URLSearchParams | { get: (k: string) => string | null }} searchParams
 * @returns {boolean}
 */
export function wantsPaymentPathFocus(searchParams) {
  const raw = searchParams?.get?.('focus')
  if (typeof raw !== 'string') return false
  const v = raw.trim().toLowerCase()
  return v === 'payment-path' || v === 'payment' || v === 'checkout-path'
}
