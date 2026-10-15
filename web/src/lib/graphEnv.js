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
