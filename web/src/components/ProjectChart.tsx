import { ResponsiveBar, type BarDatum } from '@nivo/bar'

import type { ProjectRow } from '../api'
import { formatHours, formatShare } from '../format'
import { nivoTheme, type Palette } from '../theme'
import { ChartCard } from './ChartCard'
import { labelMargin, truncate } from './charts'
import { makeDataBar } from './DataBar'
import { Tooltip } from './Tooltip'

interface Datum extends BarDatum {
  project: string
  Billable: number
  'Non-billable': number
}

const keys = ['Billable', 'Non-billable'] as const

// The outermost non-empty segment of each stack carries the rounded end.
const Bar = makeDataBar<Datum>('horizontal', (bar) => {
  const d = bar.data.data
  const last = d['Non-billable'] > 0 ? 'Non-billable' : 'Billable'
  return bar.data.id === last ? 'right' : null
})

export function ProjectChart({ rows, palette }: { rows: ProjectRow[]; palette: Palette }) {
  // Nivo draws horizontal bars bottom-up; reverse so the largest is on top.
  const data: Datum[] = rows
    .map((r) => ({ project: r.project, Billable: r.billable_hours, 'Non-billable': Math.max(r.hours - r.billable_hours, 0) }))
    .reverse()
  const color = { Billable: palette.billable, 'Non-billable': palette.nonBillable }

  return (
    <ChartCard
      title="Hours by project"
      subtitle="Billable and non-billable time, largest project first"
      legend={[
        { label: 'Billable', color: palette.billable },
        { label: 'Non-billable', color: palette.nonBillable },
      ]}
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
          keys={[...keys]}
          indexBy="project"
          layout="horizontal"
          margin={{ top: 8, right: 24, bottom: 40, left: labelMargin(rows.map((r) => r.project)) }}
          padding={0.3}
          innerPadding={2}
          colors={({ id }) => color[id as keyof typeof color]}
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
            <Tooltip value={formatHours(value)} series={String(id)} category={String(indexValue)} color={color[id as keyof typeof color]} />
          )}
        />
      }
    />
  )
}
