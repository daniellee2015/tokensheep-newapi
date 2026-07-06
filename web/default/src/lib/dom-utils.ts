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
// Favicon is now managed statically in index.html with two <link> tags
// selected by `prefers-color-scheme`, so header/logo config no longer
// hijacks the browser tab icon. Keeping the export as a no-op so all
// upstream call sites remain valid without touching them.
// The parameter is retained purely for the existing call signature.
// eslint-disable-next-line @typescript-eslint/no-unused-vars
export function applyFaviconToDom(_url: string) {
  return
}
