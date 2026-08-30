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
// v4 R10: Billing-preference selector. The four preferences live in the
// backend (service/billing_session.go NewBillingSession dispatch on
// `subscription_first` / `wallet_first` / `subscription_only` /
// `wallet_only`, see docs/spec/economy-model-v4.md §3.1) and were
// previously only surfaced inside SubscriptionPlansCard. R9 hid that
// card in the TokenSheep flow (tier cards *are* the subscription), which
// stranded the preference toggle. This standalone card gives the user a
// dedicated home for the choice, sitting next to the tier ladder in the
// wallet page.
//
// Wire semantics (v4 §3.1):
//   wallet_only        — 只 quota_paid (gift 池嵌在 wallet 内, 仍会先扣)
//   subscription_only  — 只走活跃订阅池, 空即 429
//   subscription_first — 先订阅, 用完 fallback wallet (if plan allows overflow)
//   wallet_first       — 先钱包, 用完 fallback subscription
//
// Persist via PUT /api/subscription/self/preference; read via
// GET /api/subscription/self (nested billing_preference field).
import { Wallet as WalletIcon } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { TitledCard } from '@/components/ui/titled-card'
import {
  getSelfSubscriptionFull,
  updateBillingPreference,
} from '@/features/subscriptions/api'

// Set of valid backend enum values. Mirrors common.NormalizeBillingPreference
// in the tokensheep-newapi repo.
const PREFERENCES = [
  'subscription_first',
  'wallet_first',
  'subscription_only',
  'wallet_only',
] as const

type Preference = (typeof PREFERENCES)[number]

function isPreference(value: string): value is Preference {
  return (PREFERENCES as readonly string[]).includes(value)
}

// Human labels for each preference. Kept short so they fit the dropdown
// row; the description underneath the select surfaces the semantic detail.
function labelFor(pref: Preference, t: (k: string) => string): string {
  switch (pref) {
    case 'subscription_first':
      return t('wallet.billingPreference.subscriptionFirst')
    case 'wallet_first':
      return t('wallet.billingPreference.walletFirst')
    case 'subscription_only':
      return t('wallet.billingPreference.subscriptionOnly')
    case 'wallet_only':
      return t('wallet.billingPreference.walletOnly')
  }
}

function descriptionFor(pref: Preference, t: (k: string) => string): string {
  switch (pref) {
    case 'subscription_first':
      return t('wallet.billingPreference.subscriptionFirst.desc')
    case 'wallet_first':
      return t('wallet.billingPreference.walletFirst.desc')
    case 'subscription_only':
      return t('wallet.billingPreference.subscriptionOnly.desc')
    case 'wallet_only':
      return t('wallet.billingPreference.walletOnly.desc')
  }
}

interface BillingPreferenceCardProps {
  // R13→R14: commercial users (retail / wholesale / wholesale-plus)
  // don't participate in the subscription pool — their tier is contract
  // negotiated and there is no subscription-vs-wallet axis to choose on.
  // But they still have a paid wallet plus (in principle) a gift pool,
  // so wallet_first vs wallet_only stays meaningful (wallet_only would
  // let them opt out of any future gift/subscription plumbing without
  // having to touch settings again). The card therefore stays visible
  // for commercial users but the subscription_* options get filtered
  // out of the dropdown, and any subscription_* value the backend
  // reports back gets coerced up to wallet_first at load time.
  isCommercial?: boolean
}

export function BillingPreferenceCard({
  isCommercial = false,
}: BillingPreferenceCardProps = {}) {
  const { t } = useTranslation()
  const [preference, setPreference] = useState<Preference>('subscription_first')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      try {
        const res = await getSelfSubscriptionFull()
        if (cancelled) return
        const raw = res?.data?.billing_preference
        if (raw && isPreference(raw)) {
          setPreference(raw)
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void load()
    return () => {
      cancelled = true
    }
  }, [])

  const handleChange = async (next: string | null) => {
    if (!next || !isPreference(next)) return
    const previous = preference
    setPreference(next)
    setSaving(true)
    try {
      const res = await updateBillingPreference(next)
      if (!res.success) {
        toast.error(res.message || t('Update failed'))
        setPreference(previous)
        return
      }
      const normalized = res.data?.billing_preference
      if (normalized && isPreference(normalized)) {
        setPreference(normalized)
      }
      toast.success(t('Updated successfully'))
    } catch {
      toast.error(t('Request failed'))
      setPreference(previous)
    } finally {
      setSaving(false)
    }
  }

  // R16-1 (correcting R14): commercial users see all four options in the
  // dropdown, but subscription_* are rendered *disabled* with an inline
  // "(订阅池不可用)" tag instead of filtered out. Reason: user wanted
  // disable, not filter (image #29 follow-up). Filtering hides that the
  // options exist and reads as broken UI; disabling shows why they
  // aren't selectable.
  //
  // v4 §3.1 semantics for a commercial account:
  //   subscription_first → tryWallet fallback (equivalent to wallet_first
  //                       because commercial users never have an active
  //                       subscription)
  //   subscription_only  → hard 429 every request — actively broken
  //   wallet_first / wallet_only → normal wallet debit
  //
  // Trigger label coerces subscription_* → wallet_first so the collapsed
  // pill doesn't lie about what will happen when the user actually calls
  // an endpoint; the persisted preference value stays as-is until they
  // explicitly change it.
  const isSubscriptionOption = (p: Preference) =>
    p === 'subscription_first' || p === 'subscription_only'

  const displayValue: Preference =
    isCommercial && isSubscriptionOption(preference)
      ? 'wallet_first'
      : preference

  return (
    <TitledCard
      title={t('wallet.billingPreference.title')}
      description={t('wallet.billingPreference.subtitle')}
      icon={<WalletIcon className='h-4 w-4' />}
      disableHoverEffect
    >
      <div className='space-y-3'>
        <Select
          value={displayValue}
          onValueChange={handleChange}
          disabled={loading || saving}
        >
          <SelectTrigger className='w-full sm:w-72'>
            {/* Explicit label rather than the default <SelectValue />
                fallback. Without this Base UI Select renders the raw
                enum value ("subscription_first") in the trigger — the
                SelectItem labels only apply inside the dropdown, not to
                the trigger's collapsed state. See image #27 bug. */}
            <SelectValue>{labelFor(displayValue, t)}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {PREFERENCES.map((p) => {
                const disabled = isCommercial && isSubscriptionOption(p)
                return (
                  <SelectItem key={p} value={p} disabled={disabled}>
                    <span>{labelFor(p, t)}</span>
                    {disabled && (
                      <span className='text-muted-foreground ml-2 text-xs'>
                        {t('wallet.billingPreference.notAvailable')}
                      </span>
                    )}
                  </SelectItem>
                )
              })}
            </SelectGroup>
          </SelectContent>
        </Select>
        <p className='text-muted-foreground text-xs leading-relaxed'>
          {descriptionFor(displayValue, t)}
        </p>
      </div>
    </TitledCard>
  )
}
