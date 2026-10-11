import { RunPulseHighlightContext } from './runPulseHighlightContext.js'

/** @param {{ participated: Set<string>, errored: Set<string>, children: import('react').ReactNode }} props */
export function RunPulseHighlightProvider({ participated, errored, children }) {
  return (
    <RunPulseHighlightContext.Provider value={{ participated, errored }}>
      {children}
    </RunPulseHighlightContext.Provider>
  )
}
