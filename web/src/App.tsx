import { useCallback, useState } from 'react'

import type { Filters } from './api'
import { CompareChart } from './components/CompareChart'
import { DayChart } from './components/DayChart'
import { FilterBar } from './components/FilterBar'
import { Findings } from './components/Findings'
import { ProjectChart } from './components/ProjectChart'
import { StatTiles } from './components/StatTiles'
import { usePalette } from './theme'
import { filtersKey, useDashboard } from './useDashboard'

const defaultFilters: Filters = {
  days: 7,
  project: '',
  tz: Intl.DateTimeFormat().resolvedOptions().timeZone,
}

export function App() {
  const [filters, setFilters] = useState(defaultFilters)
  // Keep the same object while the values are unchanged, so nothing refetches.
  const changeFilters = useCallback((next: Filters) => {
    setFilters((prev) => (filtersKey(prev) === filtersKey(next) ? prev : next))
  }, [])
  const { loaded, loading, error, reload } = useDashboard(filters)
  const palette = usePalette()
  // Labels describe the data on screen, which may be from earlier filters
  // while a refetch runs or after one fails.
  const shown = loaded?.filters

  return (
    <div className="app">
      <header className="top">
        <h1>tracklens</h1>
        <p className="subtitle">Toggl Track time, billable share and data-quality checks</p>
      </header>

      <FilterBar value={filters} onChange={changeFilters} onRefresh={reload} loading={loading} />

      {error && (
        <div className="error" role="alert">
          <strong>Could not load the dashboard.</strong> {error}
          {shown && ` Showing the last result that loaded (last ${shown.days} days${shown.project ? `, projects matching “${shown.project}”` : ''}).`}{' '}
          <button type="button" onClick={reload}>
            Try again
          </button>
        </div>
      )}

      {loaded === null || shown === undefined ? (
        !error && <p className="empty">Loading…</p>
      ) : (
        // Refetch keeps the previous render, dimmed, instead of flashing.
        <main className={loading ? 'content stale' : 'content'} aria-busy={loading}>
          <StatTiles summary={loaded.dashboard.summary} comparison={loaded.dashboard.comparison} days={shown.days} />
          <div className="grid">
            <ProjectChart rows={loaded.dashboard.summary.rows} palette={palette} />
            <CompareChart rows={loaded.dashboard.comparison.rows} days={shown.days} palette={palette} />
          </div>
          <DayChart rows={loaded.dashboard.days.days} tz={shown.tz} palette={palette} />
          <Findings findings={loaded.dashboard.summary.findings} />
        </main>
      )}
    </div>
  )
}
