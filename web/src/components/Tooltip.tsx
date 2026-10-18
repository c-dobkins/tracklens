/** Tooltip body: the value leads, the series and category follow, keyed by a short line of the series colour. */
export function Tooltip({ value, series, category, color }: { value: string; series: string; category: string; color: string }) {
  return (
    <div className="tooltip" role="status">
      <strong className="tooltip-value">{value}</strong>
      <span className="tooltip-label">
        <span className="line-key" style={{ background: color }} aria-hidden="true" />
        {series} · {category}
      </span>
    </div>
  )
}
