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
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { formatTimestampToDate } from '@/lib/format'

import { getPresetTimeRange } from '../time-range'

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 16, 19, 40, 30))
  const nativeToString = Date.prototype.toString
  vi.spyOn(Date.prototype, 'toString').mockImplementation(
    function (this: Date) {
      const utcTime = [
        this.getUTCHours(),
        this.getUTCMinutes(),
        this.getUTCSeconds(),
      ]
        .map((part) => String(part).padStart(2, '0'))
        .join(':')
      return nativeToString.call(this).replace(/\d{2}:\d{2}:\d{2}/, utcTime)
    }
  )
  // The affected browser reports UTC getters but retains local constructor/setter
  // semantics and the device GMT offset in Date.toString().
  vi.spyOn(Date.prototype, 'getFullYear').mockImplementation(
    Date.prototype.getUTCFullYear
  )
  vi.spyOn(Date.prototype, 'getMonth').mockImplementation(
    Date.prototype.getUTCMonth
  )
  vi.spyOn(Date.prototype, 'getDate').mockImplementation(
    Date.prototype.getUTCDate
  )
  vi.spyOn(Date.prototype, 'getDay').mockImplementation(
    Date.prototype.getUTCDay
  )
  vi.spyOn(Date.prototype, 'getHours').mockImplementation(
    Date.prototype.getUTCHours
  )
  vi.spyOn(Date.prototype, 'getMinutes').mockImplementation(
    Date.prototype.getUTCMinutes
  )
  vi.spyOn(Date.prototype, 'getTimezoneOffset').mockReturnValue(0)
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
})

test('a current log keeps the device wall time when browser getters report UTC', () => {
  const timestamp = new Date(2026, 9, 16, 19, 40, 30).getTime()
  expect(formatTimestampToDate(timestamp / 1000)).toBe('2026-10-16 19:40:30')
  expect(formatTimestampToDate(timestamp, 'milliseconds')).toBe(
    '2026-10-16 19:40:30'
  )
})

test('Today after local midnight stays on the local date when browser getters report UTC', () => {
  vi.setSystemTime(new Date(2026, 9, 16, 0, 30))
  const range = getPresetTimeRange('today')
  expect(range.start.getTime()).toBe(new Date(2026, 9, 16).getTime())
  expect(range.end.getTime()).toBe(
    new Date(2026, 9, 16, 23, 59, 59, 999).getTime()
  )
  expect(formatTimestampToDate(range.start.getTime(), 'milliseconds')).toBe(
    '2026-10-16 00:00:00'
  )
  expect(formatTimestampToDate(range.end.getTime(), 'milliseconds')).toBe(
    '2026-10-16 23:59:59'
  )
})
