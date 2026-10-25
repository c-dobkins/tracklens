import { ApiError, endpointURL, fetchDashboard } from './api'

const filters = { days: 14, project: '  portal ', tz: 'Europe/Tallinn' }

describe('endpointURL', () => {
  it('sends only the parameters each endpoint accepts', () => {
    expect(endpointURL('/v1/summary', filters)).toBe('/v1/summary?days=14&project=portal')
    expect(endpointURL('/v1/compare', filters)).toBe('/v1/compare?days=14&project=portal')
    expect(endpointURL('/v1/days', filters)).toBe('/v1/days?days=14&project=portal&tz=Europe%2FTallinn')
  })

  it('omits an empty project', () => {
    expect(endpointURL('/v1/summary', { ...filters, project: ' ' })).toBe('/v1/summary?days=14')
  })
})

describe('fetchDashboard', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('surfaces the server error message', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(Response.json({ error: 'days must be a whole number from 1 to 366' }, { status: 400 }))),
    )
    const err = await fetchDashboard(filters, new AbortController().signal).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(400)
    expect((err as ApiError).message).toMatch(/whole number/)
  })

  it('falls back to the status line for non-JSON errors', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response('oops', { status: 502, statusText: 'Bad Gateway' }))))
    await expect(fetchDashboard(filters, new AbortController().signal)).rejects.toThrow('502 Bad Gateway')
  })
})
