import type { BarDatum, BarItemProps } from '@nivo/bar'
import { useTooltip } from '@nivo/tooltip'
import { createElement, type MouseEvent } from 'react'

/** Which side of a bar holds its value end; null for an inner stacked segment. */
export type DataEnd = 'top' | 'bottom' | 'left' | 'right' | null

const MAX_THICKNESS = 24 // never fill the whole band; leave air
const RADIUS = 4 // rounded data end, square at the baseline
const MIN_HIT = 24 // hit area is bigger than thin marks

/**
 * SVG path for a rectangle with only the data-end corners rounded. The
 * radius shrinks for small segments so the shape never self-intersects.
 */
export function barPath(x: number, y: number, w: number, h: number, end: DataEnd): string {
  if (w <= 0 || h <= 0) return ''
  const r = end === null ? 0 : Math.min(RADIUS, w / 2, h / 2)
  const [tl, tr, br, bl] =
    end === 'top' ? [r, r, 0, 0] : end === 'bottom' ? [0, 0, r, r] : end === 'right' ? [0, r, r, 0] : end === 'left' ? [r, 0, 0, r] : [0, 0, 0, 0]
  return [
    `M${x + tl},${y}`,
    `H${x + w - tr}`,
    tr ? `A${tr},${tr} 0 0 1 ${x + w},${y + tr}` : '',
    `V${y + h - br}`,
    br ? `A${br},${br} 0 0 1 ${x + w - br},${y + h}` : '',
    `H${x + bl}`,
    bl ? `A${bl},${bl} 0 0 1 ${x},${y + h - bl}` : '',
    `V${y + tl}`,
    tl ? `A${tl},${tl} 0 0 1 ${x + tl},${y}` : '',
    'Z',
  ].join('')
}

/**
 * makeDataBar returns a Nivo bar component that draws thin bars (capped at
 * 24px) with a rounded data end, keeps Nivo's tooltip on hover and keyboard
 * focus, and gives every bar a hit area at least 24px across.
 */
export function makeDataBar<D extends BarDatum>(
  layout: 'vertical' | 'horizontal',
  dataEnd: (bar: BarItemProps<D>['bar']) => DataEnd,
) {
  function DataBar(props: BarItemProps<D>) {
    const { bar, tooltip, isFocusable, ariaLabel } = props
    const { showTooltipFromEvent, showTooltipAt, hideTooltip } = useTooltip()
    const content = createElement(tooltip, { ...bar, ...bar.data })
    // A zero segment (an all-billable day's non-billable part) has nothing to
    // show: no mark, no hit area, no tab stop to stumble over.
    if (bar.data.value === 0) return null

    let { x, y, width: w, height: h } = bar
    if (layout === 'vertical' && w > MAX_THICKNESS) {
      x += (w - MAX_THICKNESS) / 2
      w = MAX_THICKNESS
    } else if (layout === 'horizontal' && h > MAX_THICKNESS) {
      y += (h - MAX_THICKNESS) / 2
      h = MAX_THICKNESS
    }
    const hit =
      layout === 'vertical'
        ? { x: bar.x, y: Math.min(bar.y, bar.y + bar.height - MIN_HIT), width: bar.width, height: Math.max(bar.height, MIN_HIT) }
        : { x: bar.x, y: bar.y, width: Math.max(bar.width, MIN_HIT), height: bar.height }

    const onMove = (e: MouseEvent<SVGElement>) => {
      showTooltipFromEvent(content, e)
    }
    return (
      <g
        className="data-bar"
        tabIndex={isFocusable ? 0 : undefined}
        role={isFocusable ? 'img' : undefined}
        aria-label={ariaLabel ? ariaLabel(bar.data) : undefined}
        onMouseEnter={onMove}
        onMouseMove={onMove}
        onMouseLeave={hideTooltip}
        onFocus={() => {
          showTooltipAt(content, [bar.absX + bar.width / 2, bar.absY])
        }}
        onBlur={hideTooltip}
        data-testid={`bar.${bar.data.id}.${bar.data.indexValue}`}
      >
        <rect {...hit} fill="transparent" />
        <path d={barPath(x, y, w, h, dataEnd(bar))} fill={bar.color} />
      </g>
    )
  }
  return DataBar
}
