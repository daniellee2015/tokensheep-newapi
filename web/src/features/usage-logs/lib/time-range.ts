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
import dayjs from '@/lib/dayjs'

export const LOG_TIME_PRESETS = ['today', '7d', 'week', '30d', 'month'] as const
export type LogTimePreset = (typeof LOG_TIME_PRESETS)[number]

export function getPresetTimeRange(preset: LogTimePreset): {
  start: Date
  end: Date
} {
  const now = dayjs()
  let start = now.startOf('day')
  let end = now.endOf('day')
  switch (preset) {
    case '7d':
      start = now.subtract(6, 'day').startOf('day')
      break
    case '30d':
      start = now.subtract(29, 'day').startOf('day')
      break
    case 'week':
      start = now.startOf('week')
      end = now.endOf('week')
      break
    case 'month':
      start = now.startOf('month')
      end = now.endOf('month')
      break
  }
  return { start: start.toDate(), end: end.toDate() }
}

// Relative selections are resolved when used; explicit historical URLs stay fixed.
export function resolveLogTimeRange(search: {
  timeRange?: unknown
  startTime?: unknown
  endTime?: unknown
}): { startTime?: Date; endTime?: Date; timeRange?: LogTimePreset } {
  const preset = LOG_TIME_PRESETS.find((value) => value === search.timeRange)
  if (preset || (search.startTime == null && search.endTime == null)) {
    const timeRange = preset ?? 'today'
    const { start, end } = getPresetTimeRange(timeRange)
    return { startTime: start, endTime: end, timeRange }
  }
  return {
    startTime:
      typeof search.startTime === 'number'
        ? new Date(search.startTime)
        : undefined,
    endTime:
      typeof search.endTime === 'number' ? new Date(search.endTime) : undefined,
  }
}
