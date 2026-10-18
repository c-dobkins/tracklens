import type { PartialTheme } from '@nivo/theming'
import { useSyncExternalStore } from 'react'

// Colours come from the dataviz reference palette and were validated with its
// palette checker in both modes (CVD separation, normal-vision floor, contrast):
//   billable/non-billable: aqua + violet; increase/decrease: blue + red.
// Light-mode aqua sits below 3:1 against the surface, so every chart also has
// a legend and a table view.
export interface Palette {
  surface: string
  ink: string
  inkSecondary: string
  muted: string
  grid: string
  axis: string
  billable: string
  nonBillable: string
  increase: string
  decrease: string
}

export const palettes: Record<'light' | 'dark', Palette> = {
  light: {
    surface: '#fcfcfb',
    ink: '#0b0b0b',
    inkSecondary: '#52514e',
    muted: '#898781',
    grid: '#e1e0d9',
    axis: '#c3c2b7',
    billable: '#1baf7a',
    nonBillable: '#6250d6',
    increase: '#2a78d6',
    decrease: '#e34948',
  },
  dark: {
    surface: '#1a1a19',
    ink: '#f0efec',
    inkSecondary: '#c3c2b7',
    muted: '#898781',
    grid: '#2c2c2a',
    axis: '#383835',
    billable: '#199e70',
    nonBillable: '#9085e9',
    increase: '#3987e5',
    decrease: '#e66767',
  },
}

const query = '(prefers-color-scheme: dark)'

function subscribe(onChange: () => void): () => void {
  const mql = window.matchMedia(query)
  mql.addEventListener('change', onChange)
  return () => {
    mql.removeEventListener('change', onChange)
  }
}

/** The palette for the viewer's colour scheme; follows OS changes live. */
export function usePalette(): Palette {
  const dark = useSyncExternalStore(
    subscribe,
    () => window.matchMedia(query).matches,
    () => false,
  )
  return dark ? palettes.dark : palettes.light
}

/** Nivo chrome: recessive hairline grid and axes, text in ink tokens. */
export function nivoTheme(p: Palette): PartialTheme {
  const text = { fill: p.muted, fontSize: 12, fontFamily: 'inherit' }
  return {
    background: 'transparent',
    text,
    axis: {
      domain: { line: { stroke: p.axis, strokeWidth: 1 } },
      ticks: { line: { stroke: p.axis, strokeWidth: 1 }, text },
      legend: { text: { ...text, fill: p.inkSecondary } },
    },
    grid: { line: { stroke: p.grid, strokeWidth: 1 } },
  }
}
