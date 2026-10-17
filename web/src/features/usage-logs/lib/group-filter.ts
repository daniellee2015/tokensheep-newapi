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
export interface LogGroupOption {
  value: string
  label: string
  aliases: string[]
}

export function normalizeGroupQuery(value: string): string {
  return value.trim().normalize('NFKC').toLowerCase().replaceAll(/\s+/g, ' ')
}

// Exact raw keys retain priority; translated names and partial queries may
// intentionally match several groups. Unknown keys remain searchable in history.
export function resolveLogGroupKeys(
  query: string,
  options: LogGroupOption[]
): string[] {
  const result = new Set<string>()
  for (const input of query
    .split(/[,，、\n]/)
    .map((value) => value.trim())
    .filter(Boolean)) {
    if (options.some((option) => option.value === input)) {
      result.add(input)
      continue
    }
    const normalized = normalizeGroupQuery(input)
    const exact = options.filter((option) =>
      option.aliases.includes(normalized)
    )
    const matches =
      exact.length > 0
        ? exact
        : options.filter((option) =>
            option.aliases.some((alias) => alias.includes(normalized))
          )
    if (matches.length === 0) result.add(input)
    for (const option of matches) result.add(option.value)
  }
  return [...result]
}
