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
import { useNavigate } from '@tanstack/react-router'
import { Gift, Trophy, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'

import { formatUSD, getMyTier, tierLabel } from './api'

// TokenSheep tier card — contribution-triggered tier, dual wallet pool,
// daily-gift progress. Unlike 100b (spend + daily gates), everything here is
// driven by total_donated crossing TierThresholds. See economy-model.md §2/§10.
export function TierCard() {
  const { t } = useTranslation()
  const navigate = useNavigate()
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
  const dailyPct =
    data.gift_daily_limit > 0
      ? Math.min(
          100,
          Math.round((data.gift_used_today / data.gift_daily_limit) * 100)
        )
      : 0

  return (
    <TitledCard
      title={t('My Tier')}
      description={t('Your contribution tier and wallet pools.')}
      icon={<Trophy className='h-4 w-4' />}
      disableHoverEffect
      contentClassName='space-y-5'
    >
      {/* Current tier headline + contribution progress */}
        <div className='space-y-2'>
          <div className='flex items-center justify-between'>
            <span className='text-lg font-semibold'>
              {tierLabel(data.group)}
            </span>
            {hasNext ? (
              <Badge variant='outline' className='text-xs'>
                {t('Next')}: {tierLabel(data.next_tier)}
              </Badge>
            ) : (
              <Badge variant='outline' className='text-xs'>
                {t('Top tier')}
              </Badge>
            )}
          </div>
          {hasNext && (
            <>
              <Progress value={Math.round(data.next_progress * 100)} />
              <div className='text-muted-foreground flex justify-between text-xs'>
                <span>{formatUSD(data.total_donated)}</span>
                <span>
                  {t('To next tier')} {formatUSD(data.to_next_contribution)}
                </span>
                <span>{formatUSD(data.next_threshold)}</span>
              </div>
            </>
          )}
        </div>

        {/* Dual wallet pool — two clean tiles */}
        <div className='grid grid-cols-2 gap-3'>
          <div className='bg-muted/30 rounded-lg p-3'>
            <div className='text-muted-foreground mb-1 flex items-center gap-1.5 text-[11px] tracking-wide uppercase'>
              <WalletCards className='size-3.5' />
              {t('Paid balance')}
            </div>
            <div className='text-base font-semibold tabular-nums'>
              {formatUSD(data.quota_paid)}
            </div>
          </div>
          <div className='bg-muted/30 rounded-lg p-3'>
            <div className='text-muted-foreground mb-1 flex items-center gap-1.5 text-[11px] tracking-wide uppercase'>
              <Gift className='size-3.5' />
              {t('Gift balance')}
            </div>
            <div className='text-base font-semibold tabular-nums'>
              {formatUSD(data.quota_gift)}
              <span className='text-muted-foreground text-xs font-normal'>
                {' / '}
                {formatUSD(data.gift_pool_cap)}
              </span>
            </div>
          </div>
        </div>

        {/* Daily check-in usage — only tiers with a daily gift */}
        {data.daily_gift > 0 && (
          <div className='space-y-1.5'>
            <div className='text-muted-foreground flex justify-between text-xs'>
              <span>
                {t('Daily gift')} {formatUSD(data.daily_gift)}
              </span>
              <span>
                {t('Used today')} {formatUSD(data.gift_used_today)} /{' '}
                {formatUSD(data.gift_daily_limit)}
              </span>
            </div>
            <Progress value={dailyPct} />
          </div>
        )}

        {/* Contribute-to-upgrade CTA — centered grey button */}
        <Button
          variant='secondary'
          className='w-full'
          onClick={() => navigate({ to: '/wallet' })}
        >
          {t('Contribute to upgrade')}
        </Button>
    </TitledCard>
  )
}
