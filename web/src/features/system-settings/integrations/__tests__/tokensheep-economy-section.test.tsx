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
// R17-A test coverage for R16-2 (commercial_groups + disabled_tiers admin
// UI). The v4 backend has had these two map[string]bool options since R7
// (setting/tokensheep_setting/economy.go), but until R16-2 the only way to
// flip them was raw PUT /api/option/. This section adds two checkbox
// columns; these tests pin the load-side unioning + save-side sparse
// payload shape so a future refactor can't quietly regress either.
//
// Sentinel-precision round-trip check: the commit message for R16-2
// mentions commercial groups "carry sentinel thresholds". In practice
// the seed config defines commercial_groups without any accompanying
// TierThresholds entry, so the load path exposes them with an empty
// threshold field and saves them back as 0 quota — no floating-point
// exposure. The typical dollar values used elsewhere (10, 50, 100)
// round-trip losslessly through the *500_000 conversion, so no separate
// precision test is warranted. If verification later surfaces a real
// bug (e.g. an operator entered a value near Number.MAX_SAFE_INTEGER),
// add a case here that saves then reloads and asserts equality.
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test } from 'vitest'

const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { api } = await import('@/lib/api')
const { TokensheepEconomySection } = await import(
  '../tokensheep-economy-section'
)

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: { en: { translation: {} } },
})

type ApiMethod = (url: string, data?: unknown) => Promise<{ data: unknown }>
type MockableApi = { put: ApiMethod; get: ApiMethod }
const apiClient = api as unknown as MockableApi
const originalPut = apiClient.put
const originalGet = apiClient.get

type PutCall = { url: string; body: Record<string, string> }

function installApiFixture(): { calls: PutCall[] } {
  const calls: PutCall[] = []
  apiClient.put = async (url, data) => {
    // The section batches one PUT per key; each body is { key, value }.
    calls.push({ url, body: data as Record<string, string> })
    return { data: { success: true, message: '' } }
  }
  return { calls }
}

afterEach(() => {
  apiClient.put = originalPut
  apiClient.get = originalGet
})

function renderSection(overrides: Partial<Parameters<
  typeof TokensheepEconomySection
>[0]['defaultValues']> = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  })
  const defaultValues = {
    TierThresholds: { supporter: 5_000_000, fan: 25_000_000 }, // $10, $50
    CheckinAwardByGroup: { supporter: 250_000, fan: 500_000 },
    SessionLimits: { free: 1, supporter: 3, fan: 5, retail: 30 },
    GiftPoolCap: 25_000_000,
    GiftPoolInactiveDays: 30,
    DowngradeInactiveDays: 30,
    CommercialGroups: { retail: true } as Record<string, boolean>,
    DisabledTiers: {} as Record<string, boolean>,
    SystemConcurrency: 260,
    ...overrides,
  }
  render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <TokensheepEconomySection defaultValues={defaultValues} />
      </I18nextProvider>
    </QueryClientProvider>
  )
  return { queryClient }
}

function findRowByTierName(tierName: string): HTMLTableRowElement {
  const inputs = [
    ...document.querySelectorAll<HTMLInputElement>(
      'input[placeholder="supporter"]'
    ),
  ]
  const target = inputs.find((el) => el.value === tierName)
  if (!target) throw new Error(`No row with tier name "${tierName}"`)
  const row = target.closest('tr')
  if (!row) throw new Error(`No <tr> for tier "${tierName}"`)
  return row as HTMLTableRowElement
}

function getCheckboxByLabel(
  row: HTMLTableRowElement,
  ariaLabel: 'Disabled' | 'Commercial'
): HTMLInputElement {
  const el = row.querySelector<HTMLInputElement>(
    `input[type="checkbox"][aria-label="${ariaLabel}"]`
  )
  if (!el) throw new Error(`No ${ariaLabel} checkbox on row`)
  return el
}

async function clickSaveAndWait(calls: PutCall[]): Promise<void> {
  const saveButton = screen
    .getAllByRole('button')
    .find((b) => b.textContent?.trim() === 'Save')
  if (!saveButton) throw new Error('No Save button found')
  await waitFor(() => expect(saveButton).not.toBeDisabled())
  fireEvent.click(saveButton)
  // Section writes 9 keys sequentially, awaiting each in turn.
  await waitFor(() => expect(calls.length).toBeGreaterThanOrEqual(9), {
    timeout: 3000,
  })
}

function findCallByKey(calls: PutCall[], key: string): PutCall {
  const call = calls.find((c) => c.body.key === key)
  if (!call) {
    throw new Error(
      `No PUT call for key "${key}". Sent keys: ${calls.map((c) => c.body.key).join(', ')}`
    )
  }
  return call
}

describe('TokensheepEconomySection R16-2 commercial_groups + disabled_tiers', () => {
  beforeEach(() => {
    // Nothing yet; installApiFixture per test to keep the calls array
    // owned by each case.
  })

  test('load-side unions commercial-only tiers into the row set', () => {
    // R16-2 core: retail is in CommercialGroups but has no
    // TierThresholds entry. Before R16-2, buildInitialRows only walked
    // TierThresholds ∪ CheckinAwardByGroup ∪ SessionLimits so retail
    // was invisible in the admin table — you had to add a threshold to
    // see it, which was the exact footgun R16-2 fixes.
    renderSection()

    // "supporter", "fan", "retail" all present; "free" is filtered out
    // (NON_EDITABLE_TIERS).
    expect(() => findRowByTierName('supporter')).not.toThrow()
    expect(() => findRowByTierName('fan')).not.toThrow()
    expect(() => findRowByTierName('retail')).not.toThrow()

    const retailRow = findRowByTierName('retail')
    // Commercial box is pre-ticked from defaultValues.CommercialGroups.
    expect(getCheckboxByLabel(retailRow, 'Commercial').checked).toBe(true)
    expect(getCheckboxByLabel(retailRow, 'Disabled').checked).toBe(false)
  })

  test('save writes commercial_groups + disabled_tiers with only ticked entries', async () => {
    // The backend treats an absent key as false, so the section rebuilds
    // both maps from scratch each save and only writes true entries.
    // Regressing to "always emit every tier as false" would balloon the
    // payload and — because the maps are compared for equality in the
    // watcher — trigger no-op broadcasts on every save.
    const { calls } = installApiFixture()
    renderSection()

    // Tick "Disabled" on the fan row, keep retail commercial ticked.
    const fanRow = findRowByTierName('fan')
    fireEvent.click(getCheckboxByLabel(fanRow, 'Disabled'))
    expect(getCheckboxByLabel(fanRow, 'Disabled').checked).toBe(true)

    await clickSaveAndWait(calls)

    const commercialCall = findCallByKey(
      calls,
      'tokensheep_economy.commercial_groups'
    )
    const disabledCall = findCallByKey(
      calls,
      'tokensheep_economy.disabled_tiers'
    )

    // Sparse maps: only ticked keys appear. supporter never ticked,
    // retail always ticked, fan just ticked its Disabled box.
    expect(JSON.parse(commercialCall.body.value)).toEqual({ retail: true })
    expect(JSON.parse(disabledCall.body.value)).toEqual({ fan: true })
  })

  test('save clears a previously-ticked commercial group when the box is unticked', async () => {
    // The other half of the sparse-map contract. If the operator
    // untickets a reseller (say, retail is being decommissioned), the
    // saved payload must be `{}`, not `{ retail: false }` — the backend
    // watcher only writes what it sees, and an entry with false there
    // would count as "still commercial" in strict-equality tools.
    const { calls } = installApiFixture()
    renderSection()

    const retailRow = findRowByTierName('retail')
    const commercialBox = getCheckboxByLabel(retailRow, 'Commercial')
    expect(commercialBox.checked).toBe(true)
    fireEvent.click(commercialBox)
    expect(commercialBox.checked).toBe(false)

    await clickSaveAndWait(calls)

    const commercialCall = findCallByKey(
      calls,
      'tokensheep_economy.commercial_groups'
    )
    expect(JSON.parse(commercialCall.body.value)).toEqual({})
  })

  test('save also emits system_concurrency and the other legacy keys', async () => {
    // Regression guard: R16-4 added system_concurrency alongside the
    // R16-2 additions. This test locks in the full 9-key payload shape
    // so a future refactor that (say) forgets to include
    // system_concurrency in the updates array trips one assertion here.
    const { calls } = installApiFixture()
    renderSection()

    // Trigger dirty state by ticking any box.
    const fanRow = findRowByTierName('fan')
    fireEvent.click(getCheckboxByLabel(fanRow, 'Disabled'))

    await clickSaveAndWait(calls)

    const emittedKeys = calls.map((c) => c.body.key).sort()
    expect(emittedKeys).toEqual(
      [
        'tokensheep_economy.checkin_award_by_group',
        'tokensheep_economy.commercial_groups',
        'tokensheep_economy.disabled_tiers',
        'tokensheep_economy.downgrade_inactive_days',
        'tokensheep_economy.gift_pool_cap',
        'tokensheep_economy.gift_pool_inactive_days',
        'tokensheep_economy.session_limits',
        'tokensheep_economy.system_concurrency',
        'tokensheep_economy.tier_thresholds',
      ].sort()
    )

    const systemConcurrencyCall = findCallByKey(
      calls,
      'tokensheep_economy.system_concurrency'
    )
    expect(systemConcurrencyCall.body.value).toBe('260')
  })
})
