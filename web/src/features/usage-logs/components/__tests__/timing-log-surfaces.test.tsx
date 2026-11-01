/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test } from 'vitest'

import { usageLogSchema, type UsageLog } from '../../data/schema'
import { useCommonLogsColumns } from '../columns/common-logs-columns'
import { UsageLogsMobileList } from '../usage-logs-mobile-card'
import { UsageLogsProvider } from '../usage-logs-provider'

function TimingLogSurface(props: { log: UsageLog; mobile: boolean }) {
  const columns = useCommonLogsColumns(false)
  const table = useReactTable({
    data: [props.log],
    columns,
    getCoreRowModel: getCoreRowModel(),
  })
  if (props.mobile) {
    return <UsageLogsMobileList table={table} logCategory='common' />
  }
  const cell = table
    .getRowModel()
    .rows[0].getAllCells()
    .find((item) => item.column.id === 'use_time')
  if (!cell) throw new Error('The log table has no timing column')
  return <>{flexRender(cell.column.columnDef.cell, cell.getContext())}</>
}

test.each([false, true])(
  'real desktop/mobile log surfaces pass timing provenance (mobile=%s)',
  async (mobile) => {
    const user = userEvent.setup()
    const log = usageLogSchema.parse({
      id: 1,
      user_id: 1,
      created_at: 1793500000,
      type: 2,
      content: '',
      model_name: 'test-model',
      is_stream: true,
      use_time: 16,
      completion_tokens: 96,
      other: JSON.stringify({
        frt: 4500,
        frt_metric: 'downstream_first_content',
        frt_event_ms: 300,
        first_thinking_ms: 4500,
        first_text_ms: 15000,
      }),
    })
    render(
      <UsageLogsProvider>
        <TimingLogSurface log={log} mobile={mobile} />
      </UsageLogsProvider>
    )
    expect(screen.getByText('First content')).toBeInTheDocument()
    expect(screen.queryByText('First token')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Timing details' }))
    expect(
      screen.getByRole('definition', { name: 'First text' })
    ).toHaveTextContent('15.0s')
    expect(
      screen.getByRole('definition', { name: 'First event' })
    ).toHaveTextContent('0.3s')
    if (mobile) {
      expect(screen.getByLabelText('End-to-end output rate')).toHaveTextContent(
        '6 t/s'
      )
    }
  }
)
