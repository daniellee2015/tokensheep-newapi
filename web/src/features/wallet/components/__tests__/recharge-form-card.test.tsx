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
// R20-A test coverage for the Add Funds card wording split.
//
// Background: R16-3 hid TokensheepTierCards for commercial users, but the
// Add Funds (recharge-form-card) card still rendered — and its default
// subtitle "Normal rate, no cap, does not count toward contribution tiers"
// is misleading for a commercial account (they never participated in the
// contribution ladder in the first place). R20-A adds an `isCommercial`
// prop, wires it from wallet/index.tsx, and swaps the subtitle to contract
// wording plus appends a hint suffix pointing to the group owner for
// tier changes.
//
// These tests pin the subtitle/hint branch so a future revert doesn't
// silently drop the commercial variants back to the default copy.
import { render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { RechargeFormCard } = await import('../recharge-form-card')

// Minimal topupInfo shape — we only need `enable_online_topup` truthy so
// `hasConfigurableTopup` is true and the header block (subtitle + hint)
// renders. No pay_methods needed for the copy-only assertions here.
const topupInfo = {
  enable_online_topup: true,
  enable_stripe_topup: false,
  min_topup: 1,
  pay_methods: [],
  discount: {},
} as unknown as Parameters<typeof RechargeFormCard>[0]['topupInfo']

function makeI18n(lng: 'zh' | 'en' | 'zhTW', resources: Record<string, string>) {
  const i18n = createInstance()
  return i18n.use(initReactI18next).init({
    lng,
    fallbackLng: 'en',
    resources: { [lng]: { translation: resources } },
  }).then(() => i18n)
}

function baseProps(overrides: Partial<Parameters<typeof RechargeFormCard>[0]> = {}) {
  return {
    topupInfo,
    presetAmounts: [],
    selectedPreset: null,
    onSelectPreset: vi.fn(),
    topupAmount: 0,
    onTopupAmountChange: vi.fn(),
    paymentAmount: 0,
    calculating: false,
    onPaymentMethodSelect: vi.fn(),
    paymentLoading: null,
    enableCustomTopup: true,
    enableCustomAmountInput: true,
    ...overrides,
  } satisfies Parameters<typeof RechargeFormCard>[0]
}

describe('RechargeFormCard R20-A commercial wording', () => {
  test('non-commercial: subtitle is the contribution-tier disclaimer, no commercial suffix in hint', async () => {
    // Default rendering — the wording that has been in production for
    // regular users. Guards against a regression that flips the branch
    // and shows contract copy to everyone.
    const i18n = await makeI18n('zh', {
      'Add Funds': '增加资金',
      'Choose an amount and payment method': '选择金额和支付方式',
      'Order History': '订单历史',
      'wallet.normalTopup.title': '标准充值',
      'wallet.normalTopup.subtitle': '正常倍率、不限额，不参与贡献等级。',
      'wallet.normalTopup.subtitleCommercial':
        '商业档合约充值 · 按合约倍率计费 · 不影响贡献等级',
      'wallet.normalTopup.hint': '标准充值收取 5% 手续费。',
      'wallet.normalTopup.hintCommercialSuffix':
        ' 商业档档位调整请联系群主, 此处仅充值钱包余额。',
    })

    render(
      <I18nextProvider i18n={i18n}>
        <RechargeFormCard {...baseProps({ isCommercial: false })} />
      </I18nextProvider>
    )

    expect(screen.getByText('正常倍率、不限额，不参与贡献等级。')).toBeInTheDocument()
    expect(
      screen.queryByText(/商业档合约充值/)
    ).not.toBeInTheDocument()
    // The hint suffix must not leak into the default render, otherwise
    // regular users would see "contact the group owner for tier
    // adjustments" — a bug that would confuse the contribution-ladder
    // flow.
    expect(
      screen.queryByText(/商业档档位调整请联系群主/)
    ).not.toBeInTheDocument()
  })

  test('commercial (zh): subtitle switches to contract wording and hint suffix appears', async () => {
    // The core R20-A branch: for a commercial user the subtitle now
    // reads "商业档合约充值 · 按合约倍率计费 · 不影响贡献等级" and the
    // hint gets an extra sentence pointing back to the group owner.
    const i18n = await makeI18n('zh', {
      'Add Funds': '增加资金',
      'Choose an amount and payment method': '选择金额和支付方式',
      'Order History': '订单历史',
      'wallet.normalTopup.title': '标准充值',
      'wallet.normalTopup.subtitle': '正常倍率、不限额，不参与贡献等级。',
      'wallet.normalTopup.subtitleCommercial':
        '商业档合约充值 · 按合约倍率计费 · 不影响贡献等级',
      'wallet.normalTopup.hint': '标准充值收取 5% 手续费。',
      'wallet.normalTopup.hintCommercialSuffix':
        ' 商业档档位调整请联系群主, 此处仅充值钱包余额。',
    })

    render(
      <I18nextProvider i18n={i18n}>
        <RechargeFormCard {...baseProps({ isCommercial: true })} />
      </I18nextProvider>
    )

    expect(
      screen.getByText('商业档合约充值 · 按合约倍率计费 · 不影响贡献等级')
    ).toBeInTheDocument()
    expect(
      screen.queryByText('正常倍率、不限额，不参与贡献等级。')
    ).not.toBeInTheDocument()
    // Hint suffix rides on the same <AlertDescription> as the base
    // hint — react renders them as adjacent text nodes, so match by a
    // substring rather than exact string equality.
    expect(
      screen.getByText(/商业档档位调整请联系群主, 此处仅充值钱包余额。/)
    ).toBeInTheDocument()
  })

  test('commercial (en): the English commercial variant renders and the fee hint is preserved', async () => {
    // Locks the English wording so the fallback-to-en path (which the
    // real app uses when zhTW is missing a key) still speaks contract
    // language instead of "does not count toward contribution tiers".
    const i18n = await makeI18n('en', {
      'Add Funds': 'Add Funds',
      'Choose an amount and payment method': 'Choose an amount and payment method',
      'Order History': 'Order History',
      'wallet.normalTopup.title': 'Standard top-up',
      'wallet.normalTopup.subtitle':
        'Normal rate, no cap, does not count toward contribution tiers.',
      'wallet.normalTopup.subtitleCommercial':
        'Commercial contract top-up · Charged at contract rate · Does not affect contribution tiers',
      'wallet.normalTopup.hint': 'Standard top-ups carry a 5% processing fee.',
      'wallet.normalTopup.hintCommercialSuffix':
        ' For commercial tier adjustments, contact the group owner. This card only tops up wallet balance.',
    })

    render(
      <I18nextProvider i18n={i18n}>
        <RechargeFormCard {...baseProps({ isCommercial: true })} />
      </I18nextProvider>
    )

    expect(
      screen.getByText(
        'Commercial contract top-up · Charged at contract rate · Does not affect contribution tiers'
      )
    ).toBeInTheDocument()
    expect(
      screen.queryByText(
        'Normal rate, no cap, does not count toward contribution tiers.'
      )
    ).not.toBeInTheDocument()
    expect(
      screen.getByText(
        /For commercial tier adjustments, contact the group owner\. This card only tops up wallet balance\./
      )
    ).toBeInTheDocument()
  })
})
