import { useCallback, useEffect, useState } from 'react'

import { fetchDashboard, type Dashboard, type Filters } from './api'

export interface DashboardState {
  data: Dashboard | null // the last successful load, kept while refetching
  loading: boolean
  error: string | null
  reload: () => void
}

interface Result {
  key: string // which request produced this result
  data: Dashboard | null
  error: string | null
}

/**
 * Loads every view for filters. A newer request cancels an older one, and
 * loading is derived (the wanted key differs from the loaded one) rather than
 * stored, so the effect only ever sets state from the request callbacks.
 */
export function useDashboard(filters: Filters): DashboardState {
  const [generation, setGeneration] = useState(0)
  const [result, setResult] = useState<Result>({ key: '', data: null, error: null })
  const key = JSON.stringify([filters.days, filters.project, filters.tz, generation])

  useEffect(() => {
    const ctrl = new AbortController()
    fetchDashboard(filters, ctrl.signal)
      .then((data) => {
        setResult({ key, data, error: null })
      })
      .catch((err: unknown) => {
        if (ctrl.signal.aborted) return
        const error = err instanceof Error ? err.message : String(err)
        setResult((prev) => ({ key, data: prev.data, error }))
      })
    return () => {
      ctrl.abort()
    }
  }, [filters, key])

  const reload = useCallback(() => {
    setGeneration((g) => g + 1)
  }, [])
  return { data: result.data, loading: result.key !== key, error: result.error, reload }
}
