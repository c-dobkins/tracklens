import type { BarItemProps } from '@nivo/bar'

import type { Palette } from '../theme'
import type { DataEnd } from './DataBar'

/** Shortens long category names for axis ticks; the full name is in the tooltip and table. */
export function truncate(s: string, max = 22): string {
  return s.length > max ? `${s.slice(0, max - 1)}…` : s
}

/** Left margin wide enough for the longest (truncated) category label. */
export function labelMargin(names: string[]): number {
  const longest = Math.max(0, ...names.map((n) => truncate(n).length))
  return Math.min(200, 16 + longest * 7)
}

/** Every nth label so at most `max` ticks show on a crowded axis. */
export function sparseTicks<T>(values: T[], max: number): T[] {
  const step = Math.max(1, Math.ceil(values.length / max))
  return values.filter((_, i) => i % step === 0)
}

/** The two stacked series, billable first (nearest the baseline). */
export const stackKeys = ['Billable', 'Non-billable'] as const

export interface StackDatum {
  [key: string]: string | number
  Billable: number
  'Non-billable': number
}

/** Splits total hours into the two stacked series. */
export function toStack(hours: number, billable: number): Pick<StackDatum, (typeof stackKeys)[number]> {
  return { Billable: billable, 'Non-billable': Math.max(hours - billable, 0) }
}

/** Series colours and legend entries for a billable/non-billable stack. */
export function stackColors(p: Palette) {
  const color: Record<(typeof stackKeys)[number], string> = { Billable: p.billable, 'Non-billable': p.nonBillable }
  return {
    color,
    colorOf: (id: string | number) => color[id as (typeof stackKeys)[number]],
    legend: stackKeys.map((k) => ({ label: k, color: color[k] })),
  }
}

/** The outermost non-empty segment of a stack carries the rounded end. */
export function stackedEnd<D extends StackDatum>(side: Exclude<DataEnd, null>) {
  return (bar: BarItemProps<D>['bar']): DataEnd => {
    const last = bar.data.data['Non-billable'] > 0 ? 'Non-billable' : 'Billable'
    return bar.data.id === last ? side : null
  }
}
