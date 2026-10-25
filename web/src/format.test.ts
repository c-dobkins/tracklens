import { formatChange, formatDay, formatHours, formatShare, formatSignedHours } from './format'

describe('format', () => {
  it('formats hours with one or two decimals', () => {
    expect(formatHours(5.75)).toBe('5.75 h')
    expect(formatHours(1)).toBe('1.0 h')
  })

  it('signs changes', () => {
    expect(formatSignedHours(1.25)).toBe('+1.25 h')
    expect(formatSignedHours(-2)).toBe('-2.0 h')
    expect(formatSignedHours(0)).toBe('0.0 h')
  })

  it('rounds percentages half away from zero, like the Go reports', () => {
    expect(formatChange(62.5)).toBe('+63%')
    expect(formatChange(-62.5)).toBe('-63%')
    expect(formatChange(0)).toBe('0%')
    expect(formatChange(null)).toBe('new')
  })

  it('handles shares of zero', () => {
    expect(formatShare(3, 4)).toBe('75%')
    expect(formatShare(0, 0)).toBe('–')
  })

  it('formats calendar days without shifting them by time zone', () => {
    expect(formatDay('2026-10-15')).toMatch(/15/)
    expect(formatDay('2026-10-15')).toMatch(/Oct/)
  })
})
