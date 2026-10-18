import { useState } from 'react'

import type { Filters } from './api'
import { CompareChart } from './components/CompareChart'
import { DayChart } from './components/DayChart'
import { FilterBar } from './components/FilterBar'
import { Findings } from './components/Findings'
import { ProjectChart } from './components/ProjectChart'
import { StatTiles } from './components/StatTiles'
import { usePalette } from './theme'
import { useDashboard } from './useDashboard'

const defaultFilters: Filters = {
  days: 7,
  project: '',
  tz: Intl.DateTimeFormat().resolvedOptions().timeZone,
}

export function App() {
  const [filters, setFilters] = useState(defaultFilters)
  const { data, loading, error, reload } = useDashboard(filters)
  const palette = usePalette()

  return (
    <div className="app">
      <header className="top">
        <h1>tracklens</h1>
        <p className="subtitle">Toggl Track time, billable share and data-quality checks</p>
      </header>

      <FilterBar value={filters} onChange={setFilters} onRefresh={reload} loading={loading} />

      {error && (
        <div className="error" role="alert">
          <strong>Could not load the dashboard.</strong> {error}{' '}
          <button type="button" onClick={reload}>
            Try again
          </button>
        </div>
      )}

      {data === null ? (
        !error && <p className="empty">Loading…</p>
      ) : (
        // Refetch keeps the previous render, dimmed, instead of flashing.
        <main className={loading ? 'content stale' : 'content'} aria-busy={loading}>
          <StatTiles summary={data.summary} comparison={data.comparison} days={filters.days} />
          <div className="grid">
            <ProjectChart rows={data.summary.rows} palette={palette} />
            <CompareChart rows={data.comparison.rows} days={filters.days} palette={palette} />
          </div>
          <DayChart rows={data.days.days} tz={filters.tz} palette={palette} />
          <Findings findings={data.summary.findings} />
        </main>
      )}
    </div>
  )
}
