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
import { Activity, BarChart3, Gift, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { IconBadge, type IconBadgeTone } from '@/components/ui/icon-badge'
import { Skeleton } from '@/components/ui/skeleton'
import { formatQuota } from '@/lib/format'

import type { UserWalletData } from '../types'

interface WalletStatsCardProps {
  user: UserWalletData | null
  loading?: boolean
}

// The gift pool has a fixed cap set by the backend (see
// setting/tokensheep_setting/economy.go: GiftPoolCap, default $50 = 5,000,000
// quota cents). Surfacing the cap alongside the balance turns the number into
// "$3.20 / $50" so users understand why check-ins get refused past the cap.
const GIFT_POOL_CAP_QUOTA = 5_000_000

export function WalletStatsCard(props: WalletStatsCardProps) {
  const { t } = useTranslation()
  if (props.loading) {
    return (
      // Four cells to match the paid/gift/usage/requests layout below.
      <div className='overflow-hidden rounded-lg border'>
        <div className='divide-border/60 grid grid-cols-2 divide-x md:grid-cols-4'>
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className='min-w-0 px-2.5 py-2.5 sm:px-5 sm:py-4'>
              <Skeleton className='h-3.5 w-full' />
              <Skeleton className='mt-2 h-6 w-full sm:h-7' />
              <Skeleton className='mt-1.5 hidden h-3.5 w-24 md:block' />
            </div>
          ))}
        </div>
      </div>
    )
  }

  const giftBalance = props.user?.quota_gift ?? 0
  const giftLabel =
    giftBalance > 0
      ? `${formatQuota(giftBalance)} / ${formatQuota(GIFT_POOL_CAP_QUOTA)}`
      : formatQuota(0)

  const stats: {
    label: string
    value: string
    description: string
    icon: typeof WalletCards
    tone: IconBadgeTone
  }[] = [
    {
      label: t('wallet.pool.paid.label'),
      value: formatQuota(props.user?.quota ?? 0),
      description: t('wallet.pool.paid.description'),
      icon: WalletCards,
      tone: 'success',
    },
    {
      label: t('wallet.pool.gift.label'),
      value: giftLabel,
      description: t('wallet.pool.gift.description'),
      icon: Gift,
      tone: 'chart-3',
    },
    {
      label: t('Total Usage'),
      value: formatQuota(props.user?.used_quota ?? 0),
      description: t('Total consumed quota'),
      icon: BarChart3,
      tone: 'info',
    },
    {
      label: t('API Requests'),
      value: (props.user?.request_count ?? 0).toLocaleString(),
      description: t('Total requests made'),
      icon: Activity,
      tone: 'chart-4',
    },
  ]

  // Four columns to fit the paid/gift/usage/requests set. IconBadge and the
  // typography are taken from upstream so the wallet page reads as one system
  // with the rest of the dashboard.
  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='divide-border/60 grid grid-cols-2 divide-x md:grid-cols-4'>
        {stats.map((item) => (
          <div
            key={item.label}
            className='min-w-0 px-2.5 py-2.5 sm:px-5 sm:py-4'
          >
            <div className='flex items-center gap-1.5 sm:gap-2.5'>
              <IconBadge tone={item.tone} size='stat'>
                <item.icon />
              </IconBadge>
              <div className='text-muted-foreground truncate text-[11px] font-medium tracking-wider uppercase sm:text-xs'>
                {item.label}
              </div>
            </div>

            <div className='text-foreground mt-1.5 font-mono text-sm font-bold tracking-tight break-all tabular-nums sm:mt-2.5 sm:text-2xl'>
              {item.value}
            </div>
            <div className='text-muted-foreground/60 mt-1 hidden text-xs md:block'>
              {item.description}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
