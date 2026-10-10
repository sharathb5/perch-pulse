import { Loader2, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useNodeLogs } from '../hooks/useNodeLogs.js'
import { buildErrorPrompt, isErroredNode, openInAIWithFallback } from '../lib/aiHandoff.js'
import { credentialKeyForNode } from '../lib/mappers.js'
import { DeployRow } from './DeployRow.jsx'
import { PulseSection } from './PulseSection.jsx'

function providerTitle(provider) {
  const map = {
    vercel: 'Vercel',
    supabase: 'Supabase',
    github: 'GitHub',
    render: 'Render',
    custom: 'Custom',
  }
  return map[provider] ?? provider
}

function metaLookup(meta, key) {
  return meta?.find((m) => m.key === key)?.value ?? '—'
}

/** @param {{ key: string, value: string }[] | undefined} meta */
function metaValue(meta, key) {
  const v = meta?.find((m) => m.key === key)?.value
  if (v == null) {
    return ''
  }
  return String(v)
}

/**
 * @param {{ meta?: { key: string, value: string }[], recentErrors?: string[] } | null} node
 * @param {string} environment
 */
function buildDeploymentRows(node, environment) {
  if (!node) {
    return []
  }
  const sha = metaValue(node.meta, 'last_deploy.sha').trim()
  const ago = metaValue(node.meta, 'last_deploy.ago').trim()
  const hasLastDeploy = sha !== '' || ago !== ''

  const rows = []
  if (hasLastDeploy) {
    rows.push({
      id: sha !== '' ? sha.slice(0, 7) : 'deploy',
      env: environment,
      current: true,
      branch: '—',
      commit: sha !== '' ? sha : '—',
      date: ago !== '' ? ago : '—',
      duration: '—',
    })
  }

  const errs = Array.isArray(node.recentErrors) ? node.recentErrors : []
  errs.forEach((msg, i) => {
    const t = String(msg ?? '').trim()
    if (t === '') {
      return
    }
    rows.push({
      id: `recent-${i}-${t.slice(0, 12)}`,
      env: 'Recent activity',
      current: false,
      branch: '—',
      commit: t,
      date: '—',
      duration: '—',
    })
  })

  return rows
}

const statusLabel = {
  healthy: 'healthy',
  degraded: 'degraded',
  down: 'down',
  source: 'source',
  unknown: 'unknown',
}

/** @param {{ text: string, copied: boolean, onCopy: () => void }} props */
function CopyBlock({ label, text, copied, onCopy }) {
  const trimmed = text.trim()
  if (trimmed === '') {
    return null
  }
  return (
    <div className="mt-3 first:mt-0">
      <div className="text-[11px] font-medium text-gray-600">{label}</div>
      <pre className="mt-1 max-h-[28vh] overflow-auto whitespace-pre-wrap break-all rounded-md border border-gray-200 bg-gray-50 p-2 font-mono text-[11px] leading-relaxed text-gray-900">
        {trimmed}
      </pre>
      <button
        type="button"
        onClick={onCopy}
        className="mt-1.5 rounded-md border border-gray-300 bg-white px-2.5 py-1 text-xs font-medium text-gray-800 hover:bg-gray-50"
      >
        {copied ? 'Copied' : 'Copy'}
      </button>
    </div>
  )
}

const urlInLine = /(https?:\/\/\S+)/

/** @param {{ line: string }} props */
function SetupHintLine({ line }) {
  const match = line.match(urlInLine)
  if (!match) {
    return <p>{line}</p>
  }
  const url = match[1]
  const idx = line.indexOf(url)
  return (
    <p>
      {line.slice(0, idx)}
      <a
        href={url}
        target="_blank"
        rel="noopener noreferrer"
        className="text-blue-600 underline hover:text-blue-800"
      >
        {url}
      </a>
      {line.slice(idx + url.length)}
    </p>
  )
}

/** @param {{ hint: string }} props */
function SetupHint({ hint }) {
  const lines = String(hint ?? '')
    .split('\n')
    .filter((l) => l.length > 0)
  return (
    <div className="space-y-2 text-sm text-gray-800">
      {lines.map((line, i) => (
        <SetupHintLine key={`${i}-${line.slice(0, 24)}`} line={line} />
      ))}
    </div>
  )
}

/**
 * @param {object | undefined} node
 * @param {string} setupHint
 * @returns {string}
 */
function dashboardURLForLogsSetup(node, setupHint) {
  const fromNode = String(node?.credentialsDashboardUrl ?? '').trim()
  if (fromNode !== '') {
    return fromNode
  }
  const match = String(setupHint ?? '').match(/https?:\/\/\S+/)
  return match ? match[0] : ''
}

/** @param {{ node: object, setupHint: string, strategiesTried?: { name?: string, result?: string }[], onRefresh: () => void | Promise<void>, refreshing: boolean }} props */
function LogsSetupConnect({ node, setupHint, strategiesTried, onRefresh, refreshing }) {
  const [tokenInput, setTokenInput] = useState('')
  const [connecting, setConnecting] = useState(false)
  const [connectError, setConnectError] = useState(null)

  const providerCredentialKey = credentialKeyForNode(node)
  const dashboardUrl = dashboardURLForLogsSetup(node, setupHint)
  const providerLabel = providerTitle(node?.provider ?? '')
  const tokenExpired = Array.isArray(strategiesTried)
    ? strategiesTried.some((s) => String(s?.result ?? '') === 'token_expired')
    : false

  async function handleConnect() {
    if (!providerCredentialKey || !tokenInput.trim()) {
      return
    }
    setConnecting(true)
    setConnectError(null)
    try {
      const res = await fetch('/api/credentials', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          key: providerCredentialKey,
          token: tokenInput.trim(),
        }),
      })
      const text = await res.text()
      if (!res.ok) {
        let msg = text
        try {
          const data = text !== '' ? JSON.parse(text) : null
          if (data?.error) {
            msg = String(data.error)
          }
        } catch {
          // keep raw text
        }
        throw new Error(msg || res.statusText || 'request failed')
      }
      setTokenInput('')
      await onRefresh()
    } catch (e) {
      setConnectError(e instanceof Error ? e.message : String(e))
    } finally {
      setConnecting(false)
    }
  }

  const promptOnly =
    dashboardUrl === '' &&
    providerCredentialKey === '' &&
    String(setupHint ?? '').trim() !== ''

  return (
    <div className="space-y-3">
      {dashboardUrl !== '' && (
        <p className="text-sm text-gray-800">
          Get token from here:{' '}
          <a
            href={dashboardUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="text-blue-600 underline hover:text-blue-800"
          >
            Open {providerLabel} dashboard →
          </a>
        </p>
      )}
      {promptOnly && <SetupHint hint={setupHint} />}
      {providerCredentialKey !== '' && (
        <div className="mt-3 space-y-2">
          <input
            type="password"
            placeholder="Paste API token here"
            value={tokenInput}
            onChange={(e) => setTokenInput(e.target.value)}
            autoComplete="off"
            className="w-full rounded-md border border-gray-300 bg-white px-3 py-2 font-mono text-sm text-gray-900 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
          />
          <button
            type="button"
            onClick={() => void handleConnect()}
            disabled={!tokenInput.trim() || connecting || refreshing}
            className="rounded-md bg-gray-900 px-3 py-1.5 text-xs font-medium text-white hover:bg-gray-800 disabled:cursor-not-allowed disabled:opacity-60"
          >
            {connecting ? 'Connecting...' : 'Connect'}
          </button>
        </div>
      )}
      {tokenExpired && (
        <p className="text-sm text-red-600">
          Your token was saved, but the provider API rejected it (unauthorized). Create a new token and try again.
        </p>
      )}
      {connectError && <p className="text-sm text-red-600">{connectError}</p>}
      <button
        type="button"
        onClick={() => void onRefresh()}
        disabled={refreshing || connecting}
        className="rounded-md border border-gray-300 bg-white px-2.5 py-1 text-xs font-medium text-gray-800 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-60"
      >
        Retry
      </button>
    </div>
  )
}

/** @param {{ logs: object, node: object, onRefresh: () => void | Promise<void>, refreshing: boolean }} props */
function ProviderLogsContent({ logs, node, onRefresh, refreshing }) {
  const source = String(logs?.source ?? '')
  const stdoutLines = Array.isArray(logs?.stdout_lines)
    ? logs.stdout_lines.map((s) => String(s ?? ''))
    : []

  if (source === 'none') {
    return (
      <LogsSetupConnect
        node={node}
        setupHint={String(logs?.setup_hint ?? '')}
        strategiesTried={logs?.strategies_tried}
        onRefresh={onRefresh}
        refreshing={refreshing}
      />
    )
  }

  return (
    <div className="space-y-2">
      <pre className="max-h-[400px] overflow-auto rounded-md bg-gray-900 p-2 font-mono text-xs leading-relaxed text-gray-100">
        {stdoutLines.length === 0 ? (
          <div className="text-gray-400">No log lines returned.</div>
        ) : (
          stdoutLines.map((line, i) => <div key={`${i}-${line.slice(0, 16)}`}>{line}</div>)
        )}
        {logs?.truncated && <div className="mt-1 text-[11px] text-gray-500">[output truncated]</div>}
        {logs?.timed_out && <div className="mt-1 text-[11px] text-gray-500">[command timed out]</div>}
        {source !== '' && (
          <div className="mt-2 border-t border-gray-700 pt-2 text-[11px] text-gray-500">source: {source}</div>
        )}
      </pre>
      <button
        type="button"
        onClick={onRefresh}
        disabled={refreshing}
        className="rounded-md border border-gray-300 bg-white px-2.5 py-1 text-xs font-medium text-gray-800 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-60"
      >
        Refresh
      </button>
    </div>
  )
}

/** @param {{ node: object, environment: string, logs: object | null, loading: boolean, error: string | null, fetchLogs: () => void }} props */
function ProviderLogsTab({ node, environment, logs, loading, error, fetchLogs }) {
  const logsResultForAI =
    logs != null
      ? {
          stdoutLines: Array.isArray(logs.stdout_lines)
            ? logs.stdout_lines.map((s) => String(s ?? ''))
            : [],
          stderrLines: Array.isArray(logs.stderr_lines)
            ? logs.stderr_lines.map((s) => String(s ?? ''))
            : [],
        }
      : null
  const showAIActions = isErroredNode(node)

  if (loading) {
    return (
      <div className="flex items-center gap-2 text-sm text-gray-600">
        <Loader2 className="h-4 w-4 animate-spin text-gray-500" strokeWidth={2} />
        Fetching logs...
      </div>
    )
  }

  if (error) {
    return (
      <div className="space-y-2">
        <p className="text-sm text-red-600">Failed to fetch logs: {error}</p>
        <button
          type="button"
          onClick={() => void fetchLogs()}
          className="rounded-md border border-gray-300 bg-white px-2.5 py-1 text-xs font-medium text-gray-800 hover:bg-gray-50"
        >
          Retry
        </button>
      </div>
    )
  }

  if (logs == null) {
    return <p className="text-sm text-gray-500">Open this tab to load logs.</p>
  }

  return (
    <div className="space-y-3">
      <ProviderLogsContent
        logs={logs}
        node={node}
        onRefresh={() => fetchLogs()}
        refreshing={loading}
      />
      {showAIActions && (
        <ProviderLogsAIActions node={node} environment={environment} logsResult={logsResultForAI} />
      )}
    </div>
  )
}

/** @param {{ node: object, environment: string, logsResult: object | null }} props */
function ProviderLogsAIActions({ node, environment, logsResult }) {
  const [copyWhich, setCopyWhich] = useState(null)
  const [handoffStatus, setHandoffStatus] = useState('')
  const [openInMenuOpen, setOpenInMenuOpen] = useState(false)

  const copyText = async (which, text) => {
    try {
      await navigator.clipboard.writeText(text)
      setCopyWhich(which)
      window.setTimeout(() => setCopyWhich(null), 2000)
    } catch {
      setCopyWhich(null)
    }
  }

  const openAI = async (tool) => {
    const prompt = buildErrorPrompt({ node, environment, logsResult })
    try {
      const result = await openInAIWithFallback({ tool, prompt })
      if (result.mode === 'deep_link') {
        setHandoffStatus(`Opened ${tool}`)
      } else {
        setHandoffStatus(`Prompt copied for ${tool}`)
      }
    } catch {
      setHandoffStatus(`Could not open ${tool}; copy failed`)
    }
    setOpenInMenuOpen(false)
    window.setTimeout(() => setHandoffStatus(''), 2500)
  }

  return (
    <div className="border-t border-gray-100 pt-2">
      <div className="flex flex-wrap gap-2">
        <button
          type="button"
          onClick={() => {
            const prompt = buildErrorPrompt({ node, environment, logsResult })
            void copyText('prompt', prompt)
          }}
          className="rounded-md border border-gray-300 bg-white px-2.5 py-1 text-xs font-medium text-gray-800 hover:bg-gray-50"
        >
          {copyWhich === 'prompt' ? 'Prompt copied' : 'Copy prompt'}
        </button>
        <div className="relative inline-block">
          <button
            type="button"
            onClick={() => setOpenInMenuOpen((v) => !v)}
            className="rounded-md border border-gray-300 bg-white px-2.5 py-1 text-xs font-medium text-gray-800 hover:bg-gray-50"
          >
            Open in
          </button>
          {openInMenuOpen && (
            <div className="absolute z-20 mt-1 min-w-[180px] rounded-md border border-gray-200 bg-white p-1 shadow-lg">
              {['codex', 'cursor', 'claude'].map((tool) => (
                <button
                  key={tool}
                  type="button"
                  onClick={() => void openAI(tool)}
                  className="block w-full rounded px-2 py-1.5 text-left text-xs capitalize text-gray-800 hover:bg-gray-50"
                >
                  {tool === 'claude' ? 'Claude Code' : tool}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>
      {handoffStatus !== '' && <p className="mt-2 text-xs text-gray-500">{handoffStatus}</p>}
    </div>
  )
}

/**
 * @param {{
 *   node: object | null,
 *   environment: string,
 *   pulse?: object | null,
 *   pulseDataSources?: object | null,
 *   pulseError?: string | null,
 *   pulseStaleSnapshot?: boolean,
 *   pulseLastSuccessAt?: string | null,
 *   graphDemo?: boolean,
 * }} props
 */
export function DetailPanel({
  node,
  environment,
  pulse = null,
  pulseDataSources = null,
  pulseError = null,
  pulseStaleSnapshot = false,
  pulseLastSuccessAt = null,
  graphDemo = false,
}) {
  const { stackName, nodeId } = useParams()
  const navigate = useNavigate()
  const [tab, setTab] = useState('deployments')
  const [copyWhich, setCopyWhich] = useState(null)
  const logsTabFetched = useRef(false)
  const isCustom = (node?.provider ?? '').toLowerCase() === 'custom'
  const { logs, loading: logsLoading, error: logsError, fetchLogs } = useNodeLogs(
    isCustom ? undefined : node?.id,
    environment,
  )

  const close = () => {
    navigate(`/stack/${stackName}`)
  }

  useEffect(() => {
    const onKey = (e) => {
      if (e.key === 'Escape') navigate(`/stack/${stackName}`)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [navigate, stackName])

  useEffect(() => {
    // Reset ephemeral copy feedback when the selected node or tab changes.
    // eslint-disable-next-line react-hooks/set-state-in-effect -- intentional UI reset on navigation
    setCopyWhich(null)
    logsTabFetched.current = false
  }, [node?.id, tab])

  useEffect(() => {
    logsTabFetched.current = false
  }, [node?.id, environment])

  const deployments = node ? buildDeploymentRows(node, environment) : []
  const logsCmd = node?.logs != null ? String(node.logs).trim() : ''
  const statusCmd = node?.statusCommand != null ? String(node.statusCommand).trim() : ''
  const hasLogsCommand = logsCmd !== ''
  const hasStatusCommand = statusCmd !== ''

  const region = node ? metaLookup(node.meta, 'region') : '—'
  const branch = node ? metaLookup(node.meta, 'branch') : '—'
  const st = node ? statusLabel[node.status] ?? node.status : '—'
  const project = node ? metaLookup(node.meta, 'project') : '—'
  const service = node ? metaLookup(node.meta, 'service') : '—'
  const errRate = node ? metaValue(node.meta, 'error_rate').trim() : ''
  const dailyCost = node ? metaValue(node.meta, 'daily_cost_usd').trim() : ''

  const copyText = async (which, text) => {
    try {
      await navigator.clipboard.writeText(text)
      setCopyWhich(which)
      window.setTimeout(() => setCopyWhich(null), 2000)
    } catch {
      setCopyWhich(null)
    }
  }

  const selectTab = (id) => {
    setTab(id)
    if (id === 'logs' && node && !isCustom && !logsTabFetched.current) {
      logsTabFetched.current = true
      void fetchLogs()
    }
  }

  const recentList = node && Array.isArray(node.recentErrors) ? node.recentErrors.filter((s) => String(s).trim() !== '') : []

  return (
    <aside
      data-testid="detail-panel"
      className="flex h-full w-[300px] shrink-0 flex-col border-l border-gray-200 bg-white"
    >
      <div className="flex items-start justify-between gap-2 border-b border-gray-200 px-3 py-3">
        <div className="min-w-0">
          <div className="text-[13px] font-medium leading-tight text-black">
            {node ? node.label : 'Not found'}
          </div>
          <div className="mt-0.5 text-[11px] text-gray-400">
            {node ? (
              <>
                {providerTitle(node.provider)} · {environment}
              </>
            ) : (
              <span className="font-mono text-gray-500">{nodeId}</span>
            )}
          </div>
        </div>
        <button
          type="button"
          onClick={close}
          className="rounded-md p-1 text-gray-500 hover:bg-gray-100 hover:text-gray-900"
          aria-label="Close panel"
        >
          <X className="h-4 w-4" strokeWidth={2} />
        </button>
      </div>

      {node && (
        <div className="grid grid-cols-3 gap-2 border-b border-gray-200 px-3 py-2">
          <div>
            <div className="text-[10px] uppercase tracking-wide text-gray-400">region</div>
            <div className="truncate text-xs font-medium text-gray-900">{region}</div>
          </div>
          <div>
            <div className="text-[10px] uppercase tracking-wide text-gray-400">status</div>
            <div className="truncate text-xs font-medium text-gray-900">{st}</div>
          </div>
          <div>
            <div className="text-[10px] uppercase tracking-wide text-gray-400">branch</div>
            <div className="truncate text-xs font-medium text-gray-900">{branch}</div>
          </div>
        </div>
      )}

      {node && (project !== '—' || service !== '—' || errRate !== '' || dailyCost !== '' || recentList.length > 0) && (
        <div className="space-y-1.5 border-b border-gray-200 px-3 py-2 text-[11px] text-gray-600">
          {(project !== '—' || service !== '—') && (
            <div className="flex flex-wrap gap-x-2 gap-y-0.5">
              {project !== '—' && (
                <span>
                  <span className="text-gray-400">project</span> {project}
                </span>
              )}
              {service !== '—' && (
                <span>
                  <span className="text-gray-400">service</span> {service}
                </span>
              )}
            </div>
          )}
          {(errRate !== '' || dailyCost !== '') && (
            <div className="flex flex-wrap gap-x-2">
              {errRate !== '' && (
                <span>
                  <span className="text-gray-400">error rate</span> {errRate}
                </span>
              )}
              {dailyCost !== '' && (
                <span>
                  <span className="text-gray-400">daily $</span> {dailyCost}
                </span>
              )}
            </div>
          )}
          {recentList.length > 0 && (
            <div>
              <div className="mb-0.5 text-[10px] uppercase tracking-wide text-gray-400">recent errors</div>
              <ul className="list-inside list-disc space-y-0.5 text-gray-800">
                {recentList.slice(0, 8).map((line, i) => (
                  <li key={`${i}-${String(line).slice(0, 24)}`} className="break-words">
                    {String(line)}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}

      {!node && (
        <div className="border-b border-gray-200 px-3 py-3 text-sm text-gray-500">
          No node exists for this id in the stack graph.
        </div>
      )}

      {node && (
        <PulseSection
          pulse={pulse}
          dataSources={pulseDataSources}
          pulseError={pulseError}
          staleSnapshot={pulseStaleSnapshot}
          lastSuccessAt={pulseLastSuccessAt}
          graphDemo={graphDemo}
        />
      )}

      {node && (
        <>
          <div className="flex border-b border-gray-200 text-xs text-gray-500">
            {[
              { id: 'deployments', label: 'deployments' },
              { id: 'logs', label: 'logs' },
            ].map((t) => (
              <button
                key={t.id}
                type="button"
                onClick={() => selectTab(t.id)}
                className={`flex-1 border-r border-gray-200 px-2 py-2 capitalize last:border-r-0 hover:bg-gray-50 hover:text-gray-900 ${
                  tab === t.id ? 'bg-gray-50 text-gray-900' : ''
                }`}
              >
                {t.label}
              </button>
            ))}
          </div>

          <div className="min-h-0 flex-1 overflow-y-auto">
            {tab === 'deployments' && (
              <div>
                {deployments.length === 0 ? (
                  <div className="space-y-2 p-3 text-sm text-gray-600">
                    <p className="font-medium text-gray-800">No deployment data</p>
                    {isCustom ? (
                      <p>
                        Custom nodes only expose health from the <span className="font-mono text-xs">status:</span> shell
                        exit code right now. Last deploy and recent errors are not filled for this provider yet.
                      </p>
                    ) : (
                      <p>
                        Deployable providers (Vercel, Render, …) still return a placeholder from{' '}
                        <span className="font-mono text-xs">perch status</span> in this build, so{' '}
                        <span className="font-mono text-xs">last_deploy</span> and errors do not appear here.
                      </p>
                    )}
                    <p className="text-xs text-gray-500">
                      In <span className="font-mono">examples/scenarios/full-stack</span>, switch the header environment
                      to <span className="font-semibold text-gray-700">dev</span> for demo{' '}
                      <span className="font-mono text-[11px]">custom</span> nodes. Production{' '}
                      <span className="font-mono text-[11px]">web</span> is Vercel YAML only (no shell hooks).
                    </p>
                  </div>
                ) : (
                  deployments.map((d, i) => <DeployRow key={`${node.id}-row-${i}`} deployment={d} />)
                )}
              </div>
            )}

            {tab === 'logs' && (
              <div className="p-3 text-sm text-gray-700">
                {isCustom ? (
                  <>
                    {hasStatusCommand && (
                      <CopyBlock
                        label="Health check (status:)"
                        text={statusCmd}
                        copied={copyWhich === 'status'}
                        onCopy={() => void copyText('status', statusCmd)}
                      />
                    )}
                    {hasLogsCommand ? (
                      <CopyBlock
                        label="Tail logs (logs:)"
                        text={logsCmd}
                        copied={copyWhich === 'logs'}
                        onCopy={() => void copyText('logs', logsCmd)}
                      />
                    ) : (
                      <p className="text-sm text-gray-600">No log command configured for this node.</p>
                    )}
                  </>
                ) : (
                  <ProviderLogsTab
                    node={node}
                    environment={environment}
                    logs={logs}
                    loading={logsLoading}
                    error={logsError}
                    fetchLogs={fetchLogs}
                  />
                )}
              </div>
            )}
          </div>
        </>
      )}
    </aside>
  )
}
