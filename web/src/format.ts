const hoursFmt = new Intl.NumberFormat(undefined, { minimumFractionDigits: 1, maximumFractionDigits: 2 })
const pctFmt = new Intl.NumberFormat(undefined, { style: 'percent', maximumFractionDigits: 0 })
const signedFmt = new Intl.NumberFormat(undefined, {
  signDisplay: 'exceptZero',
  minimumFractionDigits: 1,
  maximumFractionDigits: 2,
})

/** 5.75 -> "5.75 h" */
export const formatHours = (h: number): string => `${hoursFmt.format(h)} h`

/** +1.25 -> "+1.25 h"; 0 -> "0.0 h" */
export const formatSignedHours = (h: number): string => `${signedFmt.format(h)} h`

/** part/whole as a percentage, or an en dash when whole is zero. */
export const formatShare = (part: number, whole: number): string => (whole > 0 ? pctFmt.format(part / whole) : '–')

/** 62.5 -> "+63%", null -> "new" */
export function formatChange(percent: number | null): string {
  if (percent === null) return 'new'
  const rounded = Math.sign(percent) * Math.round(Math.abs(percent)) // half away from zero, like the Go side
  return `${rounded > 0 ? '+' : ''}${rounded}%`
}

/** "2026-10-15" -> "Thu 15 Oct" without time-zone shifts. */
export function formatDay(day: string): string {
  const [y, m, d] = day.split('-').map(Number)
  const date = new Date(Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1))
  return date.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short', timeZone: 'UTC' })
}
