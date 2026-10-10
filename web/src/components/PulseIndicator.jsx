import { PULSE_KIND, pulseKindLabel, pulseKindStyles } from '../lib/pulse.js'

/**
 * Small Pulse intelligence badge for ServiceCard. Does not replace StatusPill.
 * @param {{ pulse: object | null | undefined }} props
 */
export function PulseIndicator({ pulse }) {
  if (!pulse || pulse.kind === PULSE_KIND.NONE || !pulse.kind) {
    return null
  }
  const styles = pulseKindStyles(pulse.kind)
  const label = pulseKindLabel(pulse.kind)
  const title = [label, pulse.hint].filter(Boolean).join(' — ')

  return (
    <span
      data-testid="pulse-indicator"
      data-pulse-kind={pulse.kind}
      title={title}
      className={`mt-1 inline-flex max-w-full items-center gap-1 truncate text-[10px] ${styles.text}`}
      aria-label={title}
    >
      <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${styles.dot}`} aria-hidden="true" />
      <span className="truncate">{label}</span>
    </span>
  )
}
