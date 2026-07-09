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
import { api } from '@/lib/api'

// MyTierView mirrors controller/tier_self.go myTierView. All monetary
// values are station dollars.
export type MyTierView = {
  group: string
  rpm: number
  session_limit: number
  quota_paid: number
  quota_gift: number
  gift_pool_cap: number
  gift_used_today: number
  gift_daily_limit: number
  daily_gift: number
  total_donated: number
  next_tier: string
  next_threshold: number
  next_progress: number
  to_next_contribution: number
}

type MyTierResponse = {
  success: boolean
  message?: string
  data?: MyTierView
}

export async function getMyTier(): Promise<MyTierView | null> {
  const res = await api.get<MyTierResponse>('/api/user/self/tier')
  return res.data?.success ? (res.data.data ?? null) : null
}

// Known tier group keys, in ladder order. `standard` is the paid non-tier
// group (normal ratio, doesn't climb the contribution ladder). The i18n keys
// tier.name.<key> hold the localized display name for each.
export const TIER_KEYS = [
  'free',
  'supporter',
  'fan',
  'bestie',
  'vip',
  'standard',
] as const

// tierDisplayName resolves a group key to its localized display name via the
// i18n key `tier.name.<key>`. Single source of truth for how a tier/group is
// shown to users across the app (header, dropdown, profile, wallet, overview,
// landing). Unknown/legacy keys (e.g. "default") fall back to the raw key so
// nothing silently disappears.
export function tierDisplayName(
  group: string | null | undefined,
  t: (key: string) => string
): string {
  const key = group?.trim()
  if (!key) return ''
  const i18nKey = `tier.name.${key}`
  const translated = t(i18nKey)
  // i18next returns the key itself when the translation is missing — in that
  // case fall back to the raw group name.
  return translated === i18nKey ? key : translated
}

// tierLabel is the non-i18n fallback (capitalize) kept for contexts without a
// translator. Prefer tierDisplayName wherever a `t` is available.
export function tierLabel(group: string): string {
  if (!group) return ''
  return group.charAt(0).toUpperCase() + group.slice(1)
}

export function formatUSD(n: number): string {
  if (!Number.isFinite(n)) return '$0'
  if (n >= 1000) return `$${n.toFixed(0)}`
  if (n === Math.floor(n)) return `$${n}`
  return `$${n.toFixed(2)}`
}
