import type { Comparison, Summary } from '../api'
import { formatHours, formatShare, formatSignedHours } from '../format'
import { StatusIcon } from './StatusIcon'

/** Headline numbers. Total hours is the one hero figure for the view. */
export function StatTiles({ summary, comparison, days }: { summary: Summary; comparison: Comparison; days: number }) {
  const previous = comparison.rows.reduce((sum, r) => sum + r.previous_hours, 0)
  const current = comparison.rows.reduce((sum, r) => sum + r.current_hours, 0)
  const checks = summary.findings.length

  return (
    <div className="tiles">
      <div className="tile hero">
        <span className="tile-label">Total time</span>
        <span className="tile-value" data-testid="total-hours">
          {formatHours(summary.total_hours)}
        </span>
        <span className="tile-delta">
          {formatSignedHours(current - previous)} vs the previous {days} days
        </span>
      </div>
      <div className="tile">
        <span className="tile-label">Billable time</span>
        <span className="tile-value">{formatHours(summary.billable_hours)}</span>
      </div>
      <div className="tile">
        <span className="tile-label">Billable share</span>
        <span className="tile-value">{formatShare(summary.billable_hours, summary.total_hours)}</span>
      </div>
      <div className="tile">
        <span className="tile-label">Data checks</span>
        <span className="tile-value tile-status">
          <StatusIcon status={checks === 0 ? 'good' : 'warning'} />
          {checks === 0 ? 'All clear' : `${checks} to review`}
        </span>
      </div>
    </div>
  )
}
