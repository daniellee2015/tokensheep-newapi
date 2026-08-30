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
// R17-A test coverage for R16-3 tier-cards gate.
//
// The actual commercial-hide decision for TokensheepTierCards lives in
// features/wallet/index.tsx as `tierCardsVisible = shouldShowTierCards(...)`
// — the component itself doesn't know about commercial users. The gate
// logic is covered by features/wallet/lib/tier-cards-visibility.test.ts
// (four holes: cold-cache flash, rollback footgun, null-return from
// getMyTier, missing user context).
//
// This file complements that test by pinning the *component-side* fallback:
// even if the gate slipped and passed us an empty tier list, the component
// still returns null and doesn't render a card frame. Together the two
// files bracket the R16-3 fix — the parent gate hides for commercial
// users, and the component hides for empty inputs.
import { render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { TokensheepTierCards } = await import('../tokensheep-tier-cards')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        'wallet.tierCards.title': 'Contribute for a tier',
        'wallet.tierCards.subtitle': 'Pick an amount',
        'wallet.tierCards.hint': 'WeChat only',
        'wallet.tierCards.contribute': 'Contribute',
        'wallet.tierCards.processing': 'Processing',
        'wallet.tierCards.topUp': 'Top up',
        'wallet.tierCards.reached': 'Reached',
        'wallet.tierCards.popular': 'Popular',
        'wallet.tierCards.concurrency': 'concurrency',
        'wallet.tierCards.dailyGift': 'daily gift',
      },
    },
  },
})

function wrap(node: React.ReactNode) {
  return render(<I18nextProvider i18n={i18n}>{node}</I18nextProvider>)
}

describe('TokensheepTierCards R16-3 component gate', () => {
  test('renders nothing when tiers is an empty array', () => {
    // The parent gate (features/wallet/index.tsx tierCardsVisible +
    // shouldShowTierCards) hides the whole block for commercial users
    // before this component is even mounted. But if a future refactor
    // routed an empty list through instead, the card frame + heading
    // must not render — that would falsely advertise a contribution
    // ladder to a wholesale account. This assertion nails the empty
    // -> null contract inside the component.
    const { container } = wrap(
      <TokensheepTierCards tiers={[]} onSelect={vi.fn()} />
    )
    expect(container).toBeEmptyDOMElement()
    expect(screen.queryByText('Contribute for a tier')).toBe(null)
  })

  test('renders nothing when tiers is undefined', () => {
    // Same fail-closed behaviour when the topupInfo response is malformed
    // and hands undefined instead of []. The component's guard is
    // `!tiers || tiers.length === 0`, so both branches matter.
    const { container } = wrap(
      // deliberately violate the prop type to simulate a runtime hole
      // where the backend omits tier_cards entirely.
      <TokensheepTierCards
        tiers={undefined as unknown as never[]}
        onSelect={vi.fn()}
      />
    )
    expect(container).toBeEmptyDOMElement()
  })

  test('renders the card when tiers has entries (positive control)', () => {
    // Sanity: confirm the null return is a real gate and not the
    // component being generally broken. With entries the heading is
    // visible and each tier renders one button.
    wrap(
      <TokensheepTierCards
        tiers={[
          { tier: 'supporter', amount: 10 },
          { tier: 'sponsor', amount: 50 },
        ]}
        currentTier='free'
        onSelect={vi.fn()}
      />
    )
    expect(screen.getByText('Contribute for a tier')).toBeInTheDocument()
    // Two Contribute action pseudo-buttons — one per tier row.
    const actionButtons = screen
      .getAllByRole('button')
      .filter((el) => el.textContent?.includes('$'))
    expect(actionButtons).toHaveLength(2)
  })
})
