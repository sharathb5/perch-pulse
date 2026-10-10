const styles = {
  healthy: { dot: 'bg-green-500', label: 'healthy' },
  degraded: { dot: 'bg-amber-400', label: 'degraded' },
  down: { dot: 'bg-red-500', label: 'down' },
  source: { dot: 'bg-gray-400', label: 'source' },
  unknown: { dot: 'bg-slate-300', label: 'unknown' },
}

export function StatusPill({ status }) {
  const s = styles[status] ?? styles.unknown
  return (
    <span
      data-testid="status-pill"
      data-status={s.label}
      className="inline-flex items-center gap-1.5 text-xs text-gray-600"
    >
      <span className={`h-2 w-2 rounded-full ${s.dot}`} />
      <span>{s.label}</span>
    </span>
  )
}
