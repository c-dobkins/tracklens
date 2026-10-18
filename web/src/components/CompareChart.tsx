import { ResponsiveBar, type BarDatum } from '@nivo/bar'

import type { CompareRow } from '../api'
import { formatChange, formatHours, formatSignedHours } from '../format'
import { nivoTheme, type Palette } from '../theme'
import { ChartCard } from './ChartCard'
import { labelMargin, truncate } from './charts'
import { makeDataBar } from './DataBar'
import { Tooltip } from './Tooltip'

interface Datum extends BarDatum {
  project: string
  Change: number
}

// A diverging bar: the rounded end points away from zero.
const Bar = makeDataBar<Datum>('horizontal', (bar) => (bar.data.data.Change < 0 ? 'left' : 'right'))

export function CompareChart({ rows, days, palette }: { rows: CompareRow[]; days: number; palette: Palette }) {
  const data: Datum[] = rows.map((r) => ({ project: r.project, Change: r.change_hours })).reverse()
  const colorFor = (v: number) => (v < 0 ? palette.decrease : palette.increase)
  const extent = Math.max(1, ...rows.map((r) => Math.abs(r.change_hours)))

  return (
    <ChartCard
      title="Change by project"
      subtitle={`Last ${days} days compared with the ${days} days before, biggest change first`}
      legend={[
        { label: 'More time', color: palette.increase },
        { label: 'Less time', color: palette.decrease },
      ]}
      height={Math.max(160, rows.length * 40 + 56)}
      empty="No time in either period."
      rows={rows}
      rowKey={(r) => r.project}
      columns={[
        { header: 'Project', cell: (r) => r.project },
        { header: 'This period', cell: (r) => formatHours(r.current_hours), numeric: true },
        { header: 'Previous', cell: (r) => formatHours(r.previous_hours), numeric: true },
        { header: 'Change', cell: (r) => formatSignedHours(r.change_hours), numeric: true },
        { header: 'Change %', cell: (r) => formatChange(r.change_percent), numeric: true },
      ]}
      chart={
        <ResponsiveBar<Datum>
          data={data}
          keys={['Change']}
          indexBy="project"
          layout="horizontal"
          valueScale={{ type: 'linear', min: -extent, max: extent }}
          margin={{ top: 8, right: 24, bottom: 40, left: labelMargin(rows.map((r) => r.project)) }}
          padding={0.3}
          colors={({ data: d }) => colorFor(d.Change)}
          theme={nivoTheme(palette)}
          enableGridX
          enableGridY={false}
          enableLabel={false}
          markers={[{ axis: 'x', value: 0, lineStyle: { stroke: palette.axis, strokeWidth: 1 } }]}
          axisLeft={{ tickSize: 0, tickPadding: 8, format: (v: string) => truncate(v) }}
          axisBottom={{
            tickSize: 0,
            tickPadding: 8,
            tickValues: 5,
            format: (v: number) => formatSignedHours(v),
          }}
          barComponent={Bar}
          animate={false}
          isFocusable
          role="img"
          ariaLabel="Change in hours by project"
          barAriaLabel={(d) => `${d.indexValue}: ${formatSignedHours(d.value ?? 0)}`}
          tooltip={({ value, indexValue }) => {
            const row = rows.find((r) => r.project === indexValue)
            return (
              <Tooltip
                value={`${formatSignedHours(value)} (${formatChange(row?.change_percent ?? null)})`}
                series={row ? `${formatHours(row.previous_hours)} → ${formatHours(row.current_hours)}` : 'Change'}
                category={String(indexValue)}
                color={colorFor(value)}
              />
            )
          }}
        />
      }
    />
  )
}
