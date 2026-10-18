import { ResponsiveBar, type BarDatum } from '@nivo/bar'

import type { DayRow } from '../api'
import { formatDay, formatHours } from '../format'
import { nivoTheme, type Palette } from '../theme'
import { ChartCard } from './ChartCard'
import { sparseTicks } from './charts'
import { makeDataBar } from './DataBar'
import { Tooltip } from './Tooltip'

interface Datum extends BarDatum {
  day: string
  Billable: number
  'Non-billable': number
}

const Bar = makeDataBar<Datum>('vertical', (bar) => {
  const d = bar.data.data
  const last = d['Non-billable'] > 0 ? 'Non-billable' : 'Billable'
  return bar.data.id === last ? 'top' : null
})

export function DayChart({ rows, tz, palette }: { rows: DayRow[]; tz: string; palette: Palette }) {
  const data: Datum[] = rows.map((r) => ({
    day: r.day,
    Billable: r.billable_hours,
    'Non-billable': Math.max(r.hours - r.billable_hours, 0),
  }))
  const color = { Billable: palette.billable, 'Non-billable': palette.nonBillable }

  return (
    <ChartCard
      title="Hours per day"
      subtitle={`Each day in ${tz}; entries crossing midnight are split`}
      legend={[
        { label: 'Billable', color: palette.billable },
        { label: 'Non-billable', color: palette.nonBillable },
      ]}
      height={280}
      empty="No time tracked in this period."
      rows={rows}
      rowKey={(r) => r.day}
      columns={[
        { header: 'Day', cell: (r) => formatDay(r.day) },
        { header: 'Hours', cell: (r) => formatHours(r.hours), numeric: true },
        { header: 'Billable', cell: (r) => formatHours(r.billable_hours), numeric: true },
      ]}
      chart={
        <ResponsiveBar<Datum>
          data={data}
          keys={['Billable', 'Non-billable']}
          indexBy="day"
          margin={{ top: 12, right: 16, bottom: 40, left: 48 }}
          padding={0.25}
          innerPadding={2}
          colors={({ id }) => color[id as keyof typeof color]}
          theme={nivoTheme(palette)}
          enableLabel={false}
          axisLeft={{ tickSize: 0, tickPadding: 8, tickValues: 4, format: (v: number) => `${v} h` }}
          axisBottom={{
            tickSize: 0,
            tickPadding: 8,
            tickValues: sparseTicks(
              rows.map((r) => r.day),
              8,
            ),
            format: (v: string) => formatDay(v),
          }}
          barComponent={Bar}
          animate={false}
          isFocusable
          role="img"
          ariaLabel="Hours per day, billable and non-billable"
          barAriaLabel={(d) => `${formatDay(String(d.indexValue))}: ${d.id} ${formatHours(d.value ?? 0)}`}
          tooltip={({ id, value, indexValue }) => (
            <Tooltip value={formatHours(value)} series={String(id)} category={formatDay(String(indexValue))} color={color[id as keyof typeof color]} />
          )}
        />
      }
    />
  )
}
