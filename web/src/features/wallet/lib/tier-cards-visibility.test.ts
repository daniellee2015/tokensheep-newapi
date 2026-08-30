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
import { describe, expect, test } from 'vitest'

import {
  KNOWN_COMMERCIAL_GROUPS,
  shouldShowTierCards,
} from './tier-cards-visibility'

// Pins the four holes from R17 verification of R16-3
// (TokensheepTierCards visibility gate). Each `test` names the hole
// so a future regression grep matches the fix intent.

const TIER_CARDS_STUB = [
  { tier: 'supporter', amount: 5 },
  { tier: 'fan', amount: 20 },
  { tier: 'bestie', amount: 50 },
]

const HAPPY_TOPUP = {
  enable_tier_cards_in_recharge: true,
  tier_cards: TIER_CARDS_STUB,
}

describe('shouldShowTierCards — happy paths', () => {
  test('shows for free-tier user with loaded tier + topup + non-commercial', () => {
    expect(
      shouldShowTierCards({
        myTier: { group: 'free', commercial: false },
        userGroup: 'free',
        topupInfo: HAPPY_TOPUP,
      })
    ).toBe(true)
  })

  test('shows for supporter/standard non-commercial groups', () => {
    for (const g of ['supporter', 'fan', 'bestie', 'vip', 'standard']) {
      expect(
        shouldShowTierCards({
          myTier: { group: g, commercial: false },
          userGroup: g,
          topupInfo: HAPPY_TOPUP,
        })
      ).toBe(true)
    }
  })
})

describe('shouldShowTierCards — commercial users hidden (R16-3)', () => {
  test('hides when backend commercial=true', () => {
    expect(
      shouldShowTierCards({
        myTier: { group: 'wholesale', commercial: true },
        userGroup: 'wholesale',
        topupInfo: HAPPY_TOPUP,
      })
    ).toBe(false)
  })

  test.each(KNOWN_COMMERCIAL_GROUPS)(
    'hides for known commercial group %s even without commercial flag (rollback defence)',
    (group) => {
      // Hole #2: rollback footgun. Backend rolled back past R16-3 →
      // response omits `commercial`. Frontend must still hide.
      expect(
        shouldShowTierCards({
          myTier: { group, commercial: undefined },
          userGroup: group,
          topupInfo: HAPPY_TOPUP,
        })
      ).toBe(false)
    }
  )

  test('hides when only userGroup (self) is commercial and myTier.group is empty', () => {
    expect(
      shouldShowTierCards({
        myTier: { group: '', commercial: undefined },
        userGroup: 'wholesale-plus',
        topupInfo: HAPPY_TOPUP,
      })
    ).toBe(false)
  })

  test('hides when myTier.group is commercial and self is loading', () => {
    expect(
      shouldShowTierCards({
        myTier: { group: 'retail', commercial: undefined },
        userGroup: undefined,
        topupInfo: HAPPY_TOPUP,
      })
    ).toBe(false)
  })
})

describe('shouldShowTierCards — cold cache (R17 hole #1)', () => {
  test('hides while myTier is still loading (undefined)', () => {
    // Hole #1: user lands on /wallet directly, topupInfo resolves
    // first, myTier still in flight. If we returned true we'd flash
    // 3 tier cards for a wholesale user until the query lands.
    expect(
      shouldShowTierCards({
        myTier: undefined,
        userGroup: 'wholesale',
        topupInfo: HAPPY_TOPUP,
      })
    ).toBe(false)
  })

  test('hides while myTier is still loading even for free user (accepted flicker vs lie)', () => {
    // A free user momentarily doesn't see cards — accepted; the row
    // renders one tick later when the query resolves. Better than
    // the alternative (wholesale users seeing forbidden cards).
    expect(
      shouldShowTierCards({
        myTier: undefined,
        userGroup: 'free',
        topupInfo: HAPPY_TOPUP,
      })
    ).toBe(false)
  })
})

describe('shouldShowTierCards — null return from getMyTier (R17 hole #3)', () => {
  test('hides when getMyTier returned null (auth / 5xx)', () => {
    // features/tier/api.ts:53 returns null on non-success. Treating
    // null the same as undefined means a networking blip doesn't
    // suddenly reveal cards to a commercial user.
    expect(
      shouldShowTierCards({
        myTier: null,
        userGroup: 'wholesale',
        topupInfo: HAPPY_TOPUP,
      })
    ).toBe(false)
  })

  test('hides even for free user when getMyTier returned null', () => {
    // Conservative: we simply don't know. Better to hide for a tick
    // than to guess wrong on a commercial user.
    expect(
      shouldShowTierCards({
        myTier: null,
        userGroup: 'free',
        topupInfo: HAPPY_TOPUP,
      })
    ).toBe(false)
  })
})

describe('shouldShowTierCards — feature flag + empty list gates', () => {
  test('hides when topupInfo is undefined (still loading)', () => {
    expect(
      shouldShowTierCards({
        myTier: { group: 'free', commercial: false },
        userGroup: 'free',
        topupInfo: undefined,
      })
    ).toBe(false)
  })

  test('hides when operator disabled the section', () => {
    expect(
      shouldShowTierCards({
        myTier: { group: 'free', commercial: false },
        userGroup: 'free',
        topupInfo: {
          enable_tier_cards_in_recharge: false,
          tier_cards: TIER_CARDS_STUB,
        },
      })
    ).toBe(false)
  })

  test('shows when enable_tier_cards_in_recharge is undefined (default-on)', () => {
    // Matches pre-R16-3 behaviour: only `=== false` explicitly hides.
    expect(
      shouldShowTierCards({
        myTier: { group: 'free', commercial: false },
        userGroup: 'free',
        topupInfo: { tier_cards: TIER_CARDS_STUB },
      })
    ).toBe(true)
  })

  test('hides when tier_cards is empty', () => {
    expect(
      shouldShowTierCards({
        myTier: { group: 'free', commercial: false },
        userGroup: 'free',
        topupInfo: {
          enable_tier_cards_in_recharge: true,
          tier_cards: [],
        },
      })
    ).toBe(false)
  })

  test('hides when tier_cards is missing / non-array', () => {
    expect(
      shouldShowTierCards({
        myTier: { group: 'free', commercial: false },
        userGroup: 'free',
        topupInfo: { enable_tier_cards_in_recharge: true },
      })
    ).toBe(false)

    expect(
      shouldShowTierCards({
        myTier: { group: 'free', commercial: false },
        userGroup: 'free',
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        topupInfo: {
          enable_tier_cards_in_recharge: true,
          tier_cards: 'not-an-array' as unknown,
        },
      })
    ).toBe(false)
  })
})

describe('shouldShowTierCards — unknown group with missing commercial flag', () => {
  test('shows for unknown group when commercial is undefined (opt-in fail-open only for unrecognized groups)', () => {
    // If a future admin adds a new non-commercial group and the
    // backend has been rolled back past R16-3 (commercial field
    // missing), we can't know either way. Since the two-layer
    // backend defence still short-circuits total_donated for
    // *known* commercial groups, showing cards to an unknown group
    // is the least-bad option — otherwise the row disappears for
    // every non-listed group after a rollback.
    expect(
      shouldShowTierCards({
        myTier: { group: 'newlaunched', commercial: undefined },
        userGroup: 'newlaunched',
        topupInfo: HAPPY_TOPUP,
      })
    ).toBe(true)
  })
})
