import { useContext } from 'react'
import { RunPulseHighlightContext } from './runPulseHighlightContext.js'

/**
 * @param {string} nodeId
 * @returns {'observed' | 'error' | null}
 */
export function useRunPulseHighlight(nodeId) {
  const { participated, errored } = useContext(RunPulseHighlightContext)
  if (!nodeId || !participated?.has(nodeId)) {
    return null
  }
  return errored?.has(nodeId) ? 'error' : 'observed'
}
