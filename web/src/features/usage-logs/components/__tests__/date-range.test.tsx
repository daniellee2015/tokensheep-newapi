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
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { buildSearchParams } from '../../lib/filter'
import { buildApiParams, getDefaultTimeRange } from '../../lib/utils'
import { CompactDateTimeRangePicker } from '../compact-date-time-range-picker'

function Picker() {
  const [range, setRange] = useState<{ start?: Date; end?: Date }>({})
  const params = buildApiParams({
    page: 1,
    pageSize: 20,
    isAdmin: false,
    searchParams: buildSearchParams(
      { startTime: range.start, endTime: range.end },
      'common'
    ),
  })
  return (
    <>
      <CompactDateTimeRangePicker {...range} onChange={setRange} />
      <output>
        {params.start_timestamp} / {params.end_timestamp}
      </output>
    </>
  )
}
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 16, 0, 30))
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.useRealTimers()
})
test.each([
  [
    'Today',
    '2026-10-16 00:00 ~ 2026-10-16 23:59',
    new Date(2026, 9, 16).getTime() / 1000,
  ],
  [
    '7 Days',
    '2026-10-10 00:00 ~ 2026-10-16 23:59',
    new Date(2026, 9, 10).getTime() / 1000,
  ],
  [
    'This week',
    '2026-10-11 00:00 ~ 2026-10-17 23:59',
    new Date(2026, 9, 11).getTime() / 1000,
  ],
  [
    'This month',
    '2026-10-01 00:00 ~ 2026-10-31 23:59',
    new Date(2026, 9, 1).getTime() / 1000,
  ],
  [
    '30 Days',
    '2026-09-17 00:00 ~ 2026-10-16 23:59',
    new Date(2026, 8, 17).getTime() / 1000,
  ],
])(
  '%s retains local calendar boundaries through the picker and API timestamp conversion',
  async (preset, label, start) => {
    const user = userEvent.setup()
    render(<Picker />)
    await user.click(screen.getByRole('button', { name: 'Date Range' }))
    await user.click(screen.getByRole('button', { name: preset }))
    expect(screen.getByRole('button', { name: label })).toBeVisible()
    let endDay = 16
    if (preset === 'This week') endDay = 17
    if (preset === 'This month') endDay = 31
    expect(screen.getByRole('status')).toHaveTextContent(
      `${start} / ${new Date(2026, 9, endDay, 23, 59, 59).getTime() / 1000}`
    )
  }
)

test('the default range includes later requests on the same local day', () => {
  const range = getDefaultTimeRange()
  expect(range.start).toEqual(new Date(2026, 9, 16, 0, 0, 0, 0))
  expect(range.end).toEqual(new Date(2026, 9, 16, 23, 59, 59, 999))
})

test('Today uses calendar boundaries on a daylight-saving transition date', async () => {
  vi.setSystemTime(new Date(2026, 2, 8, 12))
  const user = userEvent.setup()
  render(<Picker />)
  await user.click(screen.getByRole('button', { name: 'Date Range' }))
  await user.click(screen.getByRole('button', { name: 'Today' }))
  expect(
    screen.getByRole('button', { name: '2026-03-08 00:00 ~ 2026-03-08 23:59' })
  ).toBeVisible()
  expect(screen.getByRole('status')).toHaveTextContent(
    `${new Date(2026, 2, 8).getTime() / 1000} / ${new Date(2026, 2, 8, 23, 59, 59).getTime() / 1000}`
  )
})

test('Today label and date inputs stay local when browser hour getters report UTC', async () => {
  vi.spyOn(Date.prototype, 'getHours').mockImplementation(
    Date.prototype.getUTCHours
  )
  vi.spyOn(Date.prototype, 'getDate').mockImplementation(
    Date.prototype.getUTCDate
  )
  vi.spyOn(Date.prototype, 'getTimezoneOffset').mockReturnValue(0)
  const user = userEvent.setup()
  render(<Picker />)
  await user.click(screen.getByRole('button', { name: 'Date Range' }))
  await user.click(screen.getByRole('button', { name: 'Today' }))
  const trigger = screen.getByRole('button', {
    name: '2026-10-16 00:00 ~ 2026-10-16 23:59',
  })
  await user.click(trigger)
  expect(screen.getByDisplayValue('2026-10-16T00:00')).toBeVisible()
  expect(screen.getByDisplayValue('2026-10-16T23:59')).toBeVisible()
})
