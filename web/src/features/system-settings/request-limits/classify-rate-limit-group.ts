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

// R17-C: classifyRateLimitGroup mirrors controller/group.go `groupKind()`.
// The RPM map (`ModelRequestRateLimitGroup`) is a flat `map[string][2]int`
// that mixes two semantically distinct namespaces:
//
//   * user tiers (free / supporter / fan / bestie / vip / ...) —
//     progression on the contribution ladder;
//   * commercial groups (retail / wholesale / promo / ...) —
//     admin-assigned reseller identities that are still user-scoped;
//   * channel groups (GPT-Pro / aws-q / claude-max / kirobus-api / ...) —
//     upstream capacity, throttled as a second wall past the per-user cap.
//
// v4 spec §八 R3-6 asked for these to be visually split in the RPM editor
// so an operator writing "kirobus-api: [60, 60]" (channel wall) doesn't
// accidentally review it next to "bestie: [0, 5000]" (user ladder rung)
// and mis-tune one thinking it's the other.
//
// Classification data comes from the same `/api/option/` payload used by
// tokensheep-economy-section.tsx so a newly added tier / commercial group
// is picked up automatically (no hard-coded whitelist).
export type RateLimitGroupKind = 'tier' | 'commercial' | 'channel'

// Free is always a user tier even though it carries no threshold row —
// it's the fallback identity, not a purchasable rung. Matches the special
// case in controller/group.go `groupKind()`.
const FREE_TIER = 'free'

export function classifyRateLimitGroup(
  name: string,
  tierNames: ReadonlySet<string>,
  commercialNames: ReadonlySet<string>
): RateLimitGroupKind {
  if (commercialNames.has(name)) return 'commercial'
  if (tierNames.has(name)) return 'tier'
  if (name === FREE_TIER) return 'tier'
  return 'channel'
}

// Parse a JSON object payload (as stored in the option map) into a `Set`
// of its keys. Tolerates empty/invalid values so a missing DB row falls
// back to an empty set, matching parseJsonObjectMap in billing/section-
// registry.tsx.
export function parseGroupNameSet(raw: string | undefined): Set<string> {
  if (!raw) return new Set()
  try {
    const parsed = JSON.parse(raw)
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return new Set(Object.keys(parsed))
    }
  } catch {
    // fall through to empty set
  }
  return new Set()
}
