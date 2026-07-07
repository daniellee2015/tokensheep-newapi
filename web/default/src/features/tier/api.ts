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

// Tier display label. TokenSheep tier names are already human-ish
// (free/supporter/fan/bestie/vip); capitalize for display.
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
