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
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowRight, Gift, Trophy, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'

import { formatUSD, getMyTier, tierLabel } from './api'

// TokenSheep tier card — contribution-triggered tier, dual wallet pool,
// daily-gift progress. Unlike 100b (spend + daily gates), everything here is
// driven by total_donated crossing TierThresholds. See economy-model.md §2/§10.
export function TierCard() {
  const { t } = useTranslation()
  const { data, isLoading } = useQuery({
    queryKey: ['my-tier'],
    queryFn: getMyTier,
    refetchInterval: 60_000,
    refetchOnWindowFocus: true,
  })

  if (isLoading || !data) {
    return (
      <Card>
        <CardHeader>
          <Skeleton className='h-6 w-24' />
        </CardHeader>
        <CardContent className='space-y-3'>
          <Skeleton className='h-8 w-32' />
          <Skeleton className='h-3 w-full' />
          <Skeleton className='h-16 w-full' />
        </CardContent>
      </Card>
    )
  }

  const hasNext = data.next_tier !== ''
  const giftCapPct =
    data.gift_pool_cap > 0
      ? Math.min(100, Math.round((data.quota_gift / data.gift_pool_cap) * 100))
      : 0
  const dailyPct =
    data.gift_daily_limit > 0
      ? Math.min(
          100,
          Math.round((data.gift_used_today / data.gift_daily_limit) * 100)
        )
      : 0

  return (
    <Card>
      <CardHeader>
        <div className='flex items-center justify-between'>
          <CardTitle className='flex items-center gap-2 text-base'>
            <Trophy className='size-4 text-amber-500' />
            {t('My Tier')}
          </CardTitle>
          <Badge variant='outline' className='text-xs'>
            {tierLabel(data.group)}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className='space-y-4'>
        {/* Current tier + contribution progress toward next */}
        <div className='flex items-baseline gap-3'>
          <span className='text-2xl font-bold'>{tierLabel(data.group)}</span>
          {hasNext && (
            <span className='text-muted-foreground text-xs'>
              {t('Next')}: {tierLabel(data.next_tier)}
            </span>
          )}
        </div>

        {hasNext ? (
          <div className='space-y-1.5'>
            <div className='text-muted-foreground flex justify-between text-xs'>
              <span>
                {t('Contributed')} {formatUSD(data.total_donated)}
              </span>
              <span>
                {t('Next')} {formatUSD(data.next_threshold)}
              </span>
            </div>
            <Progress value={Math.round(data.next_progress * 100)} />
            <div className='text-muted-foreground text-right text-xs'>
              {t('To next tier')}: {formatUSD(data.to_next_contribution)}
            </div>
          </div>
        ) : (
          <div className='text-muted-foreground text-xs'>
            {t('You are at the top tier. Thank you for your support! 🐑')}
          </div>
        )}

        {/* Dual wallet pool */}
        <div className='grid grid-cols-2 gap-3 border-t pt-3'>
          <div className='flex items-center gap-2'>
            <WalletCards className='text-muted-foreground size-4 shrink-0' />
            <div className='min-w-0'>
              <div className='text-muted-foreground text-[11px] tracking-wide uppercase'>
                {t('Paid balance')}
              </div>
              <div className='font-semibold tabular-nums'>
                {formatUSD(data.quota_paid)}
              </div>
            </div>
          </div>
          <div className='flex items-center gap-2'>
            <Gift className='text-muted-foreground size-4 shrink-0' />
            <div className='min-w-0'>
              <div className='text-muted-foreground text-[11px] tracking-wide uppercase'>
                {t('Gift balance')}
              </div>
              <div className='font-semibold tabular-nums'>
                {formatUSD(data.quota_gift)}
                <span className='text-muted-foreground text-xs font-normal'>
                  {' / '}
                  {formatUSD(data.gift_pool_cap)}
                </span>
              </div>
            </div>
          </div>
        </div>

        {/* Gift pool cap progress */}
        <div className='space-y-1'>
          <div className='text-muted-foreground flex justify-between text-xs'>
            <span>{t('Gift pool')}</span>
            <span>
              {formatUSD(data.quota_gift)} / {formatUSD(data.gift_pool_cap)}
            </span>
          </div>
          <Progress value={giftCapPct} />
        </div>

        {/* Daily gift usage (only when this tier gets a daily gift) */}
        {data.daily_gift > 0 && (
          <div className='space-y-1'>
            <div className='text-muted-foreground flex justify-between text-xs'>
              <span>{t("Today's gift usage")}</span>
              <span>
                {formatUSD(data.gift_used_today)} /{' '}
                {formatUSD(data.gift_daily_limit)}
              </span>
            </div>
            <Progress value={dailyPct} />
            <div className='text-muted-foreground text-xs'>
              {t('Daily check-in reward')}: {formatUSD(data.daily_gift)}
            </div>
          </div>
        )}

        <Link
          to='/wallet'
          className='text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-xs'
        >
          {t('Contribute to upgrade')}
          <ArrowRight className='size-3' />
        </Link>
      </CardContent>
    </Card>
  )
}
