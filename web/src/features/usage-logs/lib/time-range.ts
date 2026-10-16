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
import { getLocalDateTimeParts } from '@/lib/local-date'

export const LOG_TIME_PRESETS = ['today', '7d', 'week', '30d', 'month'] as const
export type LogTimePreset = (typeof LOG_TIME_PRESETS)[number]

export function getPresetTimeRange(preset: LogTimePreset): {
  start: Date
  end: Date
} {
  const now = getLocalDateTimeParts(new Date())
  const month = now.month - 1
  let start = new Date(now.year, month, now.day)
  let nextDay = new Date(now.year, month, now.day + 1)
  switch (preset) {
    case '7d':
      start = new Date(now.year, month, now.day - 6)
      break
    case '30d':
      start = new Date(now.year, month, now.day - 29)
      break
    case 'week':
      start = new Date(now.year, month, now.day - now.weekday)
      nextDay = new Date(now.year, month, now.day - now.weekday + 7)
      break
    case 'month':
      start = new Date(now.year, month, 1)
      nextDay = new Date(now.year, month + 1, 1)
      break
  }
  return { start, end: new Date(nextDay.getTime() - 1) }
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
