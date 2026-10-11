import { createContext } from 'react'

/** @type {import('react').Context<{ participated: Set<string>, errored: Set<string> }>} */
export const RunPulseHighlightContext = createContext({
  participated: new Set(),
  errored: new Set(),
})
