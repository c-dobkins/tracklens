import { ResponsiveBar } from '@nivo/bar'

import type { DayRow } from '../api'
import { formatDay, formatHours } from '../format'
import { nivoTheme, type Palette } from '../theme'
import { ChartCard } from './ChartCard'
import { sparseTicks, stackColors, stackKeys, stackedEnd, toStack, type StackDatum } from './charts'
import { makeDataBar } from './DataBar'
import { Tooltip } from './Tooltip'

interface Datum extends StackDatum {
  day: string
}

const Bar = makeDataBar<Datum>('vertical', stackedEnd('top'))

export function DayChart({ rows, tz, palette }: { rows: DayRow[]; tz: string; palette: Palette }) {
  const data: Datum[] = rows.map((r) => ({ day: r.day, ...toStack(r.hours, r.billable_hours) }))
  const { colorOf, legend } = stackColors(palette)

  return (
    <ChartCard
      title="Hours per day"
      subtitle={`Each day in ${tz}; entries crossing midnight are split`}
      legend={legend}
      height={280}
      empty="No time tracked in this period."
      // The API returns every day of the period, so emptiness is all zeros.
      isEmpty={rows.every((r) => r.hours === 0)}
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
          keys={[...stackKeys]}
          indexBy="day"
          margin={{ top: 12, right: 16, bottom: 40, left: 48 }}
          padding={0.25}
          innerPadding={2}
          colors={({ id }) => colorOf(id)}
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
            <Tooltip value={formatHours(value)} series={String(id)} category={formatDay(String(indexValue))} color={colorOf(id)} />
          )}
        />
      }
    />
  )
}
