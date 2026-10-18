import type { Finding, FindingKind } from '../api'
import { StatusIcon, type Status } from './StatusIcon'

const kinds: Record<FindingKind, { label: string; status: Status }> = {
  overlap: { label: 'Overlap', status: 'serious' },
  no_project: { label: 'No project', status: 'warning' },
  running: { label: 'Running timer', status: 'warning' },
}

/** Data-quality findings for the period, most serious first. */
export function Findings({ findings }: { findings: Finding[] }) {
  const order: FindingKind[] = ['overlap', 'no_project', 'running']
  const sorted = [...findings].sort((a, b) => order.indexOf(a.kind) - order.indexOf(b.kind))
  return (
    <section className="card" aria-labelledby="findings-title">
      <header className="card-header">
        <div>
          <h2 id="findings-title">Data checks</h2>
          <p className="subtitle">Entries worth fixing in Toggl before invoicing</p>
        </div>
      </header>
      {sorted.length === 0 ? (
        <p className="empty">
          <StatusIcon status="good" /> No problems found in this period.
        </p>
      ) : (
        <ul className="findings">
          {sorted.map((f) => (
            <li key={`${f.kind}-${f.entry_ids.join('-')}`}>
              <span className={`badge badge-${kinds[f.kind].status}`}>
                <StatusIcon status={kinds[f.kind].status} />
                {kinds[f.kind].label}
              </span>
              <span>{f.message}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
