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
// R17-A test coverage for R16-1 (disable, not filter) + R16-4 wallet-side
// UI. The card has been rewritten three times: R13 hid it for commercial
// users, R14 kept it but filtered subscription_* out of the list, R16-1
// switched to disabling those two options in place so the user sees why
// they aren't clickable. These tests pin the R16-1 semantics against
// regressing back to either R13 (hide entire card) or R14 (filter list).
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')

// Mock the subscriptions API before importing the component so useEffect
// doesn't fire real HTTP calls (jsdom would 404 them). Vitest's cleanMocks
// afterEach resets call state, and we swap the impl per test via
// vi.mocked() so a commercial user can hydrate with subscription_only.
vi.mock('@/features/subscriptions/api', () => ({
  getSelfSubscriptionFull: vi.fn(async () => ({
    success: true,
    data: { billing_preference: 'subscription_first' },
  })),
  updateBillingPreference: vi.fn(async (preference: string) => ({
    success: true,
    data: { billing_preference: preference },
  })),
}))

const subscriptionsApi = await import('@/features/subscriptions/api')
const { BillingPreferenceCard } = await import('../billing-preference-card')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'zh',
  resources: {
    zh: {
      translation: {
        'wallet.billingPreference.title': '扣费优先级',
        'wallet.billingPreference.subtitle': '决定订阅池 vs 钱包 扣费顺序',
        'wallet.billingPreference.subscriptionFirst': '优先订阅',
        'wallet.billingPreference.walletFirst': '优先钱包',
        'wallet.billingPreference.subscriptionOnly': '仅用订阅',
        'wallet.billingPreference.walletOnly': '仅用钱包',
        'wallet.billingPreference.subscriptionFirst.desc': '优先订阅描述',
        'wallet.billingPreference.walletFirst.desc': '优先钱包描述',
        'wallet.billingPreference.subscriptionOnly.desc': '仅订阅描述',
        'wallet.billingPreference.walletOnly.desc': '仅钱包描述',
        'wallet.billingPreference.notAvailable': '(订阅池不可用)',
      },
    },
  },
})

function renderCard(props: { isCommercial: boolean }) {
  return render(
    <I18nextProvider i18n={i18n}>
      <BillingPreferenceCard isCommercial={props.isCommercial} />
    </I18nextProvider>
  )
}

async function openDropdown() {
  const trigger = screen.getByRole('combobox')
  // Wait for the initial load useEffect to complete (disabled=true while
  // loading, then re-enabled). Otherwise Base UI ignores the click.
  await waitFor(() => expect(trigger).not.toBeDisabled())
  fireEvent.click(trigger)
  // Base UI Select renders the list into a portal; wait for options to
  // materialize before assertions.
  await waitFor(() => {
    const items = document.querySelectorAll('[data-slot="select-item"]')
    expect(items.length).toBeGreaterThan(0)
  })
  return trigger
}

function getSelectItems(): HTMLElement[] {
  return [
    ...document.querySelectorAll<HTMLElement>('[data-slot="select-item"]'),
  ]
}

function findItemByLabel(label: string): HTMLElement {
  const item = getSelectItems().find((el) => el.textContent?.includes(label))
  if (!item) {
    throw new Error(`Expected select-item containing "${label}"`)
  }
  return item
}

beforeEach(() => {
  // Reset to the default subscription_first backend response between
  // tests. Individual tests can override via vi.mocked().mockResolvedValueOnce.
  vi.mocked(subscriptionsApi.getSelfSubscriptionFull).mockResolvedValue({
    success: true,
    data: { billing_preference: 'subscription_first' },
  } as unknown as Awaited<
    ReturnType<typeof subscriptionsApi.getSelfSubscriptionFull>
  >)
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('BillingPreferenceCard - R16-1 disable not filter', () => {
  test('commercial user sees all four options with subscription_* disabled', async () => {
    renderCard({ isCommercial: true })
    await openDropdown()

    // R16-1 core invariant: four options total, not two. R14 previously
    // filtered subscription_* out of the list entirely — regressing to
    // that would drop the count to two and this assertion catches it.
    const items = getSelectItems()
    expect(items).toHaveLength(4)

    const labels = items.map((el) =>
      el.textContent?.replaceAll(/\s+/g, ' ').trim()
    )
    expect(labels).toEqual(
      expect.arrayContaining([
        expect.stringContaining('优先订阅'),
        expect.stringContaining('优先钱包'),
        expect.stringContaining('仅用订阅'),
        expect.stringContaining('仅用钱包'),
      ])
    )

    // Base UI Select uses `data-disabled` (attribute is present when
    // disabled, absent otherwise) — see @base-ui/react
    // SelectItemDataAttributes.
    const subscriptionFirst = findItemByLabel('优先订阅')
    const subscriptionOnly = findItemByLabel('仅用订阅')
    const walletFirst = findItemByLabel('优先钱包')
    const walletOnly = findItemByLabel('仅用钱包')

    expect(subscriptionFirst).toHaveAttribute('data-disabled')
    expect(subscriptionOnly).toHaveAttribute('data-disabled')
    expect(walletFirst).not.toHaveAttribute('data-disabled')
    expect(walletOnly).not.toHaveAttribute('data-disabled')

    // Inline hint tag "(订阅池不可用)" is only on the disabled rows so
    // the user knows *why* they can't select them. Absence of this tag
    // on the wallet rows keeps the dropdown quiet.
    expect(subscriptionFirst.textContent).toContain('(订阅池不可用)')
    expect(subscriptionOnly.textContent).toContain('(订阅池不可用)')
    expect(walletFirst.textContent).not.toContain('(订阅池不可用)')
    expect(walletOnly.textContent).not.toContain('(订阅池不可用)')
  })

  test('non-commercial user sees four options with none disabled', async () => {
    renderCard({ isCommercial: false })
    await openDropdown()

    const items = getSelectItems()
    expect(items).toHaveLength(4)

    // No item carries data-disabled, no "(订阅池不可用)" tag anywhere.
    for (const item of items) {
      expect(item).not.toHaveAttribute('data-disabled')
      expect(item.textContent).not.toContain('(订阅池不可用)')
    }
  })

  test('commercial user with subscription_only from backend shows 优先钱包 in trigger', async () => {
    // R14→R16-1 reason for the trigger coercion: if the backend hands
    // back a preference the user can no longer act on (subscription_*
    // for a commercial account), the collapsed pill would otherwise
    // read "仅用订阅" while the request goes through wallet fallback.
    // The displayValue guard in the component coerces subscription_* →
    // wallet_first purely for display so the pill matches reality.
    vi.mocked(subscriptionsApi.getSelfSubscriptionFull).mockResolvedValueOnce({
      success: true,
      data: { billing_preference: 'subscription_only' },
    } as unknown as Awaited<
      ReturnType<typeof subscriptionsApi.getSelfSubscriptionFull>
    >)

    renderCard({ isCommercial: true })

    const trigger = screen.getByRole('combobox')
    await waitFor(() => expect(trigger).not.toBeDisabled())
    // Wait for the loaded preference to propagate into the trigger label.
    await waitFor(() => {
      expect(trigger.textContent).toContain('优先钱包')
    })
    expect(trigger.textContent).not.toContain('仅用订阅')
  })

  test('non-commercial user with subscription_only from backend shows 仅用订阅 in trigger', async () => {
    // Complement to the previous test — the coercion is *only* for
    // commercial users. A normal user who explicitly picked
    // subscription_only should still see that label on the trigger.
    vi.mocked(subscriptionsApi.getSelfSubscriptionFull).mockResolvedValueOnce({
      success: true,
      data: { billing_preference: 'subscription_only' },
    } as unknown as Awaited<
      ReturnType<typeof subscriptionsApi.getSelfSubscriptionFull>
    >)

    renderCard({ isCommercial: false })
    const trigger = screen.getByRole('combobox')
    await waitFor(() => expect(trigger).not.toBeDisabled())
    await waitFor(() => {
      expect(trigger.textContent).toContain('仅用订阅')
    })
  })
})
