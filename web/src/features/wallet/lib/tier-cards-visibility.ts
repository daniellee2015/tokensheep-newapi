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
// R17: visibility gate for TokensheepTierCards in the wallet "Add Funds"
// card. Pure predicate so the R16-3 fix can be unit-tested without a full
// component test (which needs QueryClient + i18n + user fetch + topup
// fetch mocks — too much scaffolding for a boolean gate). Encodes four
// holes that verification flagged:
//
//   1. Cold-cache flash: myTier is `undefined` while /api/user/self/tier
//      is in flight. topupInfo (fetched via a parallel useEffect) can
//      resolve first, so if we only checked `myTier?.commercial === true`
//      we'd render 3 tier cards to a wholesale user for the duration of
//      the first fetch (~staleTime window on direct /wallet navigation).
//      Fix: treat undefined myTier as "unknown — hide until proven safe".
//   2. Rollback footgun: a future backend rollback past R16-3 would omit
//      the `commercial` field. `undefined === true` is false, so the
//      cards would come back for wholesale users. Fix: cross-check the
//      user's group name against a hardcoded fallback list of commercial
//      groups (retail / wholesale / wholesale-plus per
//      setting/tokensheep_setting/economy.go defaults). Two-layer
//      backend defence still prevents actual tier bumps, so this only
//      fixes the UI-lie problem.
//   3. getMyTier null-return: features/tier/api.ts returns null on
//      non-success (auth blip, 5xx). null?.commercial === true is false
//      → cards showed. Fix: null is treated the same as undefined
//      (unknown → hide) rather than as "confirmed non-commercial".
//   4. Fetching without user context: `user?.group` is undefined until
//      /api/user/self resolves. That's a separate query, so we can't
//      rely on it alone — but when both signals are missing, we hide.

/**
 * Known commercial group names, mirrored from
 * `setting/tokensheep_setting/economy.go` default seed of
 * `EconomySettingV2.CommercialGroups`. The admin panel can add more at
 * runtime; the backend `MyTierView.commercial` field is the source of
 * truth. This constant is a *defensive fallback* used only when the
 * backend doesn't send the field (rollback scenario) — matching the
 * default seed is fine because a rollback also means the runtime
 * commercial_groups admin config is unavailable to the frontend.
 */
export const KNOWN_COMMERCIAL_GROUPS: readonly string[] = Object.freeze([
  'retail',
  'wholesale',
  'wholesale-plus',
])

export interface TierCardsVisibilityInput {
  /**
   * Result of the ['my-tier'] useQuery. `undefined` means the query is
   * still loading or hasn't been dispatched. `null` means getMyTier
   * fetched but the response was non-success (backend error / auth
   * expiry / 5xx) — treated the same as loading for visibility.
   */
  myTier: { group?: string; commercial?: boolean } | null | undefined
  /**
   * User's group from /api/user/self. Used as a defensive check when
   * `myTier` is available but its `commercial` field is missing
   * (rollback scenario). `undefined` when the user fetch is in flight.
   */
  userGroup: string | undefined
  /**
   * Live topup config. `undefined` while /api/user/topup/info is in
   * flight — cards can't render without tier options so we hide.
   */
  topupInfo:
    | {
        enable_tier_cards_in_recharge?: boolean
        tier_cards?: unknown
      }
    | null
    | undefined
}

/**
 * Whether the TokensheepTierCards row should render. Any one of the
 * below hides the row:
 *  - The operator has explicitly disabled the section
 *    (`enable_tier_cards_in_recharge === false`).
 *  - The tier_cards array is missing or empty.
 *  - The current user is commercial (backend flag), OR their group
 *    matches a known commercial group name (rollback defence).
 *  - The tier query hasn't resolved yet (undefined) or returned null
 *    (getMyTier's non-success path). Prevents the cold-cache flash on
 *    fresh /wallet navigation for wholesale users.
 */
export function shouldShowTierCards(input: TierCardsVisibilityInput): boolean {
  const { myTier, userGroup, topupInfo } = input

  if (!topupInfo) return false
  if (topupInfo.enable_tier_cards_in_recharge === false) return false
  const tierCards = topupInfo.tier_cards
  if (!Array.isArray(tierCards) || tierCards.length === 0) return false

  // Hole #1 + #3: unknown tier state ⇒ hide, don't guess. Includes
  // both `undefined` (still loading) and `null` (getMyTier error /
  // auth blip). Renders a soft-hide on cold cache — the row will
  // appear one tick later for non-commercial users, no flash for
  // commercial ones.
  if (myTier === undefined || myTier === null) return false

  if (myTier.commercial === true) return false

  // Hole #2: rollback defence. If the backend response omits the
  // `commercial` field (older backend post-rollback), fall back to
  // matching the user's group against the known commercial group
  // list. We check both myTier.group (server truth from
  // /api/user/self/tier) and the caller-supplied userGroup (from
  // /api/user/self) — either being a commercial group is enough.
  if (myTier.commercial === undefined) {
    const groupCandidates = [myTier.group, userGroup].filter(
      (g): g is string => typeof g === 'string' && g.length > 0
    )
    if (groupCandidates.some((g) => KNOWN_COMMERCIAL_GROUPS.includes(g))) {
      return false
    }
  }

  return true
}
