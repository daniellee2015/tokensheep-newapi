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

export function BillingPreferenceCard() {
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

  return (
    <TitledCard
      title={t('wallet.billingPreference.title')}
      description={t('wallet.billingPreference.subtitle')}
      icon={<WalletIcon className='h-4 w-4' />}
      disableHoverEffect
    >
      <div className='space-y-3'>
        <Select
          value={preference}
          onValueChange={handleChange}
          disabled={loading || saving}
        >
          <SelectTrigger className='w-full sm:w-72'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {PREFERENCES.map((p) => (
                <SelectItem key={p} value={p}>
                  {labelFor(p, t)}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
        <p className='text-muted-foreground text-xs leading-relaxed'>
          {descriptionFor(preference, t)}
        </p>
      </div>
    </TitledCard>
  )
}
