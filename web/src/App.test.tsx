import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

import type { Comparison, Days, Summary } from './api'
import { App } from './App'

const summary: Summary = {
  rows: [
    { project: 'Client portal', hours: 3.25, billable_hours: 3.25 },
    { project: '(no project)', hours: 0.5, billable_hours: 0 },
  ],
  total_hours: 3.75,
  billable_hours: 3.25,
  findings: [
    { kind: 'running', entry_ids: [105], message: 'entry 105 is still running (1h0m0s so far)' },
    { kind: 'overlap', entry_ids: [101, 102], message: 'entries 101 and 102 overlap by 15m0s' },
  ],
}
const days: Days = { days: [{ day: '2026-10-15', hours: 3.75, billable_hours: 3.25 }] }
const comparison: Comparison = {
  rows: [{ project: 'Client portal', current_hours: 3.25, previous_hours: 2, change_hours: 1.25, change_percent: 62.5 }],
}

const urlOf = (input: RequestInfo | URL): string => (input instanceof Request ? input.url : input.toString())

function mockAPI(overrides: Partial<Record<string, () => Response>> = {}) {
  const fetchMock = vi.fn((input: RequestInfo | URL) => {
    const url = urlOf(input)
    const path = url.split('?')[0] ?? ''
    const custom = overrides[path]
    if (custom) return Promise.resolve(custom())
    const body = path === '/v1/summary' ? summary : path === '/v1/days' ? days : comparison
    return Promise.resolve(Response.json(body))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('App', () => {
  it('shows the headline numbers, tables and findings', async () => {
    mockAPI()
    render(<App />)

    expect(await screen.findByTestId('total-hours')).toHaveTextContent('3.75 h')
    expect(screen.getByText('+1.25 h vs the previous 7 days')).toBeInTheDocument()
    expect(screen.getByText('87%')).toBeInTheDocument() // 3.25 of 3.75

    // Every chart has a table twin, so values never depend on colour or hover.
    const projects = screen.getByRole('region', { name: 'Hours by project' })
    expect(within(projects).getByRole('table')).toHaveTextContent('Client portal3.25 h3.25 h100%')
    const change = screen.getByRole('region', { name: 'Change by project' })
    expect(within(change).getByRole('table')).toHaveTextContent('+63%')

    // Findings are sorted most serious first and labelled, not colour-coded alone.
    const items = within(screen.getByRole('region', { name: 'Data checks' })).getAllByRole('listitem')
    expect(items[0]).toHaveTextContent('Overlap')
    expect(items[1]).toHaveTextContent('Running timer')
  })

  it('refetches every view when the period changes', async () => {
    const fetchMock = mockAPI()
    render(<App />)
    await screen.findByTestId('total-hours')

    await userEvent.click(screen.getByLabelText('Last 30 days'))
    await waitFor(() => {
      const urls = fetchMock.mock.calls.map(([u]) => urlOf(u))
      expect(urls).toContain('/v1/summary?days=30')
      expect(urls).toContain('/v1/compare?days=30')
      expect(urls.some((u) => u.startsWith('/v1/days?days=30&tz='))).toBe(true)
    })
  })

  it('debounces the project filter', async () => {
    const fetchMock = mockAPI()
    render(<App />)
    await screen.findByTestId('total-hours')
    const before = fetchMock.mock.calls.length

    await userEvent.type(screen.getByRole('searchbox', { name: 'Project' }), 'portal')
    await waitFor(() => {
      expect(fetchMock.mock.calls.map(([u]) => urlOf(u))).toContain('/v1/summary?days=7&project=portal')
    })
    // One refetch of three views for the whole word, not one per keystroke.
    expect(fetchMock.mock.calls.length - before).toBe(3)
  })

  it('shows the server error and recovers on retry', async () => {
    let fail = true
    mockAPI({
      '/v1/summary': () => (fail ? Response.json({ error: 'data source unavailable' }, { status: 503 }) : Response.json(summary)),
    })
    render(<App />)

    expect(await screen.findByRole('alert')).toHaveTextContent('data source unavailable')
    fail = false
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByTestId('total-hours')).toHaveTextContent('3.75 h')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('says so when a period has no time', async () => {
    mockAPI({
      '/v1/summary': () => Response.json({ rows: [], total_hours: 0, billable_hours: 0, findings: [] }),
      '/v1/days': () => Response.json({ days: [] }),
      '/v1/compare': () => Response.json({ rows: [] }),
    })
    render(<App />)
    expect(await screen.findAllByText('No time tracked in this period.')).toHaveLength(2)
    expect(screen.getByText('No problems found in this period.')).toBeInTheDocument()
    expect(screen.getByText('–')).toBeInTheDocument() // billable share of zero
  })
})
