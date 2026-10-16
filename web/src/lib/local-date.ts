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
/** Keep calendar fields consistent with the device offset used by Date setters.
 * Some browser scripts replace local getters / Intl with UTC while leaving
 * local constructors and the GMT offset in Date.toString() unchanged.
 */
export function getLocalDateTimeParts(date: Date): {
  year: number
  month: number
  day: number
  weekday: number
  hour: number
  minute: number
  second: number
} {
  const match = /GMT([+-])(\d{2})(\d{2})/.exec(date.toString())
  let offset = -date.getTimezoneOffset()
  if (match) {
    const minutes = Number(match[2]) * 60 + Number(match[3])
    offset = match[1] === '+' ? minutes : -minutes
  }
  const local = new Date(date.getTime() + offset * 60_000)
  return {
    year: local.getUTCFullYear(),
    month: local.getUTCMonth() + 1,
    day: local.getUTCDate(),
    weekday: local.getUTCDay(),
    hour: local.getUTCHours(),
    minute: local.getUTCMinutes(),
    second: local.getUTCSeconds(),
  }
}

export function formatLocalDateTime(
  date: Date,
  options: { seconds?: boolean; separator?: ' ' | 'T' } = {}
): string {
  if (!Number.isFinite(date.getTime())) return 'Invalid Date'
  const parts = getLocalDateTimeParts(date)
  const calendar = [parts.year, parts.month, parts.day]
    .map((value, index) => String(value).padStart(index === 0 ? 4 : 2, '0'))
    .join('-')
  const time = [parts.hour, parts.minute]
  if (options.seconds !== false) time.push(parts.second)
  return `${calendar}${options.separator ?? ' '}${time.map((value) => String(value).padStart(2, '0')).join(':')}`
}
