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
import type { LogOtherData } from '../types'

export type StreamTimingMetadata = Pick<
  LogOtherData,
  | 'frt_metric'
  | 'frt_event_ms'
  | 'first_text_ms'
  | 'first_thinking_ms'
  | 'first_tool_ms'
  | 'upstream_first_content_ms'
>

/** Zero is a real measurement; sentinels and malformed log data are not. */
export function timingSeconds(milliseconds: unknown): number | null {
  if (
    typeof milliseconds !== 'number' ||
    !Number.isFinite(milliseconds) ||
    milliseconds < 0
  ) {
    return null
  }
  return milliseconds / 1000
}
