import { useId, type ReactNode } from 'react'

export interface Column<R> {
  header: string
  cell: (row: R) => ReactNode
  numeric?: boolean
}

export interface LegendItem {
  label: string
  color: string
}

/**
 * A titled chart with its legend and an always-available table view, so no
 * value depends on hover or on telling colours apart.
 */
export function ChartCard<R>({
  title,
  subtitle,
  legend,
  chart,
  rows,
  columns,
  rowKey,
  height,
  empty,
  isEmpty = rows.length === 0,
}: {
  title: string
  subtitle: string
  legend: LegendItem[]
  chart: ReactNode
  rows: R[]
  columns: Column<R>[]
  rowKey: (row: R) => string
  height: number
  empty: string
  isEmpty?: boolean // defaults to having no rows
}) {
  const titleId = useId()
  return (
    <section className="card" aria-labelledby={titleId}>
      <header className="card-header">
        <div>
          <h2 id={titleId}>{title}</h2>
          <p className="subtitle">{subtitle}</p>
        </div>
        {legend.length > 1 && (
          <ul className="legend" aria-label="Legend">
            {legend.map((l) => (
              <li key={l.label}>
                <span className="swatch" style={{ background: l.color }} aria-hidden="true" />
                {l.label}
              </li>
            ))}
          </ul>
        )}
      </header>
      {isEmpty ? (
        <p className="empty">{empty}</p>
      ) : (
        <>
          <div className="chart" style={{ height }}>
            {chart}
          </div>
          <details className="table-view">
            <summary>View as table</summary>
            <table>
              <thead>
                <tr>
                  {columns.map((c) => (
                    <th key={c.header} scope="col" className={c.numeric ? 'num' : undefined}>
                      {c.header}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={rowKey(r)}>
                    {columns.map((c) => (
                      <td key={c.header} className={c.numeric ? 'num' : undefined}>
                        {c.cell(r)}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </details>
        </>
      )}
    </section>
  )
}
