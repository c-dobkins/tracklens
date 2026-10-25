// Typed client for the tracklens HTTP API (internal/server).

export interface ProjectRow {
  project: string
  hours: number
  billable_hours: number
}

export type FindingKind = 'running' | 'no_project' | 'overlap'

export interface Finding {
  kind: FindingKind
  entry_ids: number[]
  message: string
}

export interface Summary {
  rows: ProjectRow[]
  total_hours: number
  billable_hours: number
  findings: Finding[]
}

export interface DayRow {
  day: string // YYYY-MM-DD
  hours: number
  billable_hours: number
}

export interface Days {
  days: DayRow[]
}

export interface CompareRow {
  project: string
  current_hours: number
  previous_hours: number
  change_hours: number
  change_percent: number | null // null when there was no previous time
}

export interface Comparison {
  rows: CompareRow[]
}

export interface Filters {
  days: number
  project: string
  tz: string
}

export interface Dashboard {
  summary: Summary
  days: Days
  comparison: Comparison
}

/** ApiError carries the server's error message and HTTP status. */
export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

/** Builds an endpoint URL with only the parameters that endpoint accepts. */
export function endpointURL(path: '/v1/summary' | '/v1/days' | '/v1/compare', f: Filters): string {
  const q = new URLSearchParams({ days: String(f.days) })
  if (f.project.trim() !== '') q.set('project', f.project.trim())
  if (path === '/v1/days') q.set('tz', f.tz)
  return `${path}?${q.toString()}`
}

async function getJSON<T>(url: string, signal: AbortSignal): Promise<T> {
  const res = await fetch(url, { signal, headers: { Accept: 'application/json' } })
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`
    try {
      const body = (await res.json()) as { error?: string }
      if (body.error) message = body.error
    } catch {
      // not JSON; keep the status line
    }
    throw new ApiError(message, res.status)
  }
  return (await res.json()) as T
}

/** Fetches every view of the dashboard for one set of filters, in parallel. */
export async function fetchDashboard(f: Filters, signal: AbortSignal): Promise<Dashboard> {
  const [summary, days, comparison] = await Promise.all([
    getJSON<Summary>(endpointURL('/v1/summary', f), signal),
    getJSON<Days>(endpointURL('/v1/days', f), signal),
    getJSON<Comparison>(endpointURL('/v1/compare', f), signal),
  ])
  return { summary, days, comparison }
}
