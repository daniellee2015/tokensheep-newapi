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
/** One rewrite applied to an upstream error message before a caller sees it. */
export interface ErrorMaskRule {
  pattern: string
  replace: string
  is_regex?: boolean
  ignore_case?: boolean
}

/** A rule plus the row identity the editor table needs; `id` is not persisted. */
export interface ErrorMaskRuleRow extends ErrorMaskRule {
  id: number
}

export interface ErrorMaskSettings {
  'error_mask_setting.enabled': boolean
  'error_mask_setting.fallback_message': string
  'error_mask_setting.rules': string
}
