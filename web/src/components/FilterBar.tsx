import { useEffect, useState } from 'react'

import type { Filters } from '../api'

const presets = [7, 14, 30, 90] as const
const zones = typeof Intl.supportedValuesOf === 'function' ? Intl.supportedValuesOf('timeZone') : ['UTC']

/** One row of filters scoping every chart below it. */
export function FilterBar({ value, onChange, onRefresh, loading }: {
  value: Filters
  onChange: (f: Filters) => void
  onRefresh: () => void
  loading: boolean
}) {
  // Typing in the project box waits for a pause before refetching.
  const [project, setProject] = useState(value.project)
  useEffect(() => {
    if (project === value.project) return
    const t = setTimeout(() => {
      onChange({ ...value, project })
    }, 300)
    return () => {
      clearTimeout(t)
    }
  }, [project, value, onChange])

  return (
    <form
      className="filters"
      aria-label="Filters"
      onSubmit={(e) => {
        e.preventDefault()
        onChange({ ...value, project })
      }}
    >
      <fieldset className="segmented">
        <legend className="visually-hidden">Period</legend>
        {presets.map((d) => (
          <label key={d}>
            <input
              type="radio"
              name="days"
              value={d}
              checked={value.days === d}
              onChange={() => {
                onChange({ ...value, days: d })
              }}
            />
            <span>
              <span className="visually-hidden">Last </span>
              {d} days
            </span>
          </label>
        ))}
      </fieldset>
      <label className="field">
        <span>Project</span>
        <input
          type="search"
          placeholder="Any project"
          value={project}
          onChange={(e) => {
            setProject(e.target.value)
          }}
        />
      </label>
      <label className="field">
        <span>Time zone</span>
        <select
          value={value.tz}
          onChange={(e) => {
            onChange({ ...value, tz: e.target.value })
          }}
        >
          {(zones.includes(value.tz) ? zones : [value.tz, ...zones]).map((z) => (
            <option key={z} value={z}>
              {z}
            </option>
          ))}
        </select>
      </label>
      <button type="button" className="refresh" onClick={onRefresh} disabled={loading}>
        {loading ? 'Loading…' : 'Refresh'}
      </button>
    </form>
  )
}
