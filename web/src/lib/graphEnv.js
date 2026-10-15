/**
 * Graph chrome query keys that must survive in-app navigations (node select,
 * Pulse open/close, Escape). Without these, the navbar env resets to the
 * historical default (`production`) and demo framing (`focus`) is lost.
 */
export const GRAPH_CHROME_QUERY_KEYS = ['env', 'focus']

/**
 * Resolve the Perch graph environment for the SPA navbar.
 * Viz opens `/?env=<defaultEnv>`; without a query param we keep the historical
 * default (`production`) so existing fixtures/e2e stay stable.
 *
 * @param {URLSearchParams | { get: (k: string) => string | null }} searchParams
 * @param {string} [fallback='production']
 * @returns {string}
 */
export function envFromSearchParams(searchParams, fallback = 'production') {
  const raw = searchParams?.get?.('env')
  if (typeof raw === 'string' && raw.trim() !== '') {
    return raw.trim()
  }
  return fallback
}

/**
 * Copy durable graph chrome params into a URLSearchParams builder.
 * Explicit overrides win; pass `null`/'' to omit a key even if present on source.
 *
 * @param {URLSearchParams} target
 * @param {URLSearchParams | { get: (k: string) => string | null } | null | undefined} source
 * @param {{ env?: string | null, focus?: string | null }} [overrides]
 * @returns {URLSearchParams}
 */
export function appendGraphChromeQuery(target, source, overrides = {}) {
  for (const key of GRAPH_CHROME_QUERY_KEYS) {
    // undefined = not overridden (copy from source); null/'' = omit.
    if (Object.prototype.hasOwnProperty.call(overrides, key) && overrides[key] !== undefined) {
      const override = overrides[key]
      if (override != null && String(override).trim() !== '') {
        target.set(key, String(override).trim())
      }
      continue
    }
    const raw = source?.get?.(key)
    if (typeof raw === 'string' && raw.trim() !== '') {
      target.set(key, raw.trim())
    }
  }
  return target
}
