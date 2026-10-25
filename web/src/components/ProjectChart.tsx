import { ResponsiveBar } from '@nivo/bar'

import type { ProjectRow } from '../api'
import { formatHours, formatShare } from '../format'
import { nivoTheme, type Palette } from '../theme'
import { ChartCard } from './ChartCard'
import { labelMargin, stackColors, stackKeys, stackedEnd, toStack, truncate, type StackDatum } from './charts'
import { makeDataBar } from './DataBar'
import { Tooltip } from './Tooltip'

interface Datum extends StackDatum {
  project: string
}

const Bar = makeDataBar<Datum>('horizontal', stackedEnd('right'))

export function ProjectChart({ rows, palette }: { rows: ProjectRow[]; palette: Palette }) {
  // Nivo draws horizontal bars bottom-up; reverse so the largest is on top.
  const data: Datum[] = rows.map((r) => ({ project: r.project, ...toStack(r.hours, r.billable_hours) })).reverse()
  const { colorOf, legend } = stackColors(palette)

  return (
    <ChartCard
      title="Hours by project"
      subtitle="Billable and non-billable time, largest project first"
      legend={legend}
      height={Math.max(160, rows.length * 40 + 56)}
      empty="No time tracked in this period."
      rows={rows}
      rowKey={(r) => r.project}
      columns={[
        { header: 'Project', cell: (r) => r.project },
        { header: 'Hours', cell: (r) => formatHours(r.hours), numeric: true },
        { header: 'Billable', cell: (r) => formatHours(r.billable_hours), numeric: true },
        { header: 'Billable share', cell: (r) => formatShare(r.billable_hours, r.hours), numeric: true },
      ]}
      chart={
        <ResponsiveBar<Datum>
          data={data}
          keys={[...stackKeys]}
          indexBy="project"
          layout="horizontal"
          margin={{ top: 8, right: 24, bottom: 40, left: labelMargin(rows.map((r) => r.project)) }}
          padding={0.3}
          innerPadding={2}
          colors={({ id }) => colorOf(id)}
          theme={nivoTheme(palette)}
          enableGridX
          enableGridY={false}
          enableLabel={false}
          axisLeft={{ tickSize: 0, tickPadding: 8, format: (v: string) => truncate(v) }}
          axisBottom={{ tickSize: 0, tickPadding: 8, tickValues: 5, legend: 'Hours', legendPosition: 'end', legendOffset: 32 }}
          barComponent={Bar}
          animate={false}
          isFocusable
          role="img"
          ariaLabel="Hours by project, billable and non-billable"
          barAriaLabel={(d) => `${d.indexValue}: ${d.id} ${formatHours(d.value ?? 0)}`}
          tooltip={({ id, value, indexValue }) => (
            <Tooltip value={formatHours(value)} series={String(id)} category={String(indexValue)} color={colorOf(id)} />
          )}
        />
      }
    />
  )
}
