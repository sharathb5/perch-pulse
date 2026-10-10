import { RefreshCw } from 'lucide-react'

const envOptions = [
  { value: 'production', label: 'production' },
  { value: 'staging', label: 'staging' },
  { value: 'dev', label: 'dev' },
]

/** @param {{ stackName: string, environment: string, onEnvironmentChange: (v: string) => void, onRefresh?: () => void }} props */
export function Navbar({ stackName, environment, onEnvironmentChange, onRefresh }) {
  return (
    <header
      data-testid="perch-navbar"
      className="flex h-12 shrink-0 items-center justify-between border-b border-gray-200 bg-white px-4"
    >
      <div className="text-sm font-bold text-black">perch</div>

      <div
        data-testid="stack-name"
        className="rounded-full border border-gray-200 bg-gray-100 px-3 py-1 text-sm text-black"
      >
        {stackName}
      </div>

      <div className="flex items-center gap-2">
        <label htmlFor="env-select" className="sr-only">
          Environment
        </label>
        <select
          id="env-select"
          value={environment}
          onChange={(e) => onEnvironmentChange(e.target.value)}
          className="rounded-md border border-gray-200 bg-white px-2 py-1 text-xs text-gray-700 outline-none"
        >
          {envOptions.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
        <button
          type="button"
          onClick={() => onRefresh?.()}
          className="rounded-md p-1.5 text-gray-500 hover:bg-gray-100 hover:text-gray-900"
          aria-label="Refresh"
        >
          <RefreshCw className="h-4 w-4" strokeWidth={2} />
        </button>
      </div>
    </header>
  )
}
