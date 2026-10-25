import { useCallback, useEffect, useState } from 'react'

import { fetchDashboard, type Dashboard, type Filters } from './api'

/** A successful load and the filters that produced it. */
export interface Loaded {
  dashboard: Dashboard
  filters: Filters
}

export interface DashboardState {
  loaded: Loaded | null // the last successful load, kept while refetching or after an error
  loading: boolean
  error: string | null
  reload: () => void
}

interface Result {
  key: string // which request produced this result
  loaded: Loaded | null
  error: string | null
}

/** A stable identity for filter values. */
export const filtersKey = (f: Filters): string => JSON.stringify([f.days, f.project, f.tz])

/**
 * Loads every view for filters. A newer request cancels an older one, and
 * loading is derived (the wanted key differs from the loaded one) rather than
 * stored, so the effect only ever sets state from the request callbacks.
 * Callers keep filters referentially stable while their values are unchanged.
 */
export function useDashboard(filters: Filters): DashboardState {
  const [generation, setGeneration] = useState(0)
  const [result, setResult] = useState<Result>({ key: '', loaded: null, error: null })
  const key = `${filtersKey(filters)}#${generation}`

  useEffect(() => {
    const ctrl = new AbortController()
    fetchDashboard(filters, ctrl.signal)
      .then((dashboard) => {
        setResult({ key, loaded: { dashboard, filters }, error: null })
      })
      .catch((err: unknown) => {
        if (ctrl.signal.aborted) return
        const error = err instanceof Error ? err.message : String(err)
        setResult((prev) => ({ key, loaded: prev.loaded, error }))
      })
    return () => {
      ctrl.abort()
    }
  }, [filters, key])

  const reload = useCallback(() => {
    setGeneration((g) => g + 1)
  }, [])
  return { loaded: result.loaded, loading: result.key !== key, error: result.error, reload }
}
