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
