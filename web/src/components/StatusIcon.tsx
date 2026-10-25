export type Status = 'good' | 'warning' | 'serious'

const paths: Record<Status, string> = {
  good: 'M3.5 8.5l3 3 6-7', // check
  warning: 'M8 4v5M8 11.5v.5', // exclamation
  serious: 'M5 5l6 6M11 5l-6 6', // cross
}

/** Status is never colour alone: every icon sits beside a text label. */
export function StatusIcon({ status }: { status: Status }) {
  return (
    <svg className={`status-icon status-${status}`} viewBox="0 0 16 16" width="16" height="16" aria-hidden="true">
      <circle cx="8" cy="8" r="8" />
      <path d={paths[status]} />
    </svg>
  )
}
