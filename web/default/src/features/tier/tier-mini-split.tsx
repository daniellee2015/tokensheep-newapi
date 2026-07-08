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
import { ArrowRight, Gauge, Trophy, Zap } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Progress } from '@/components/ui/progress'

import { formatUSD, getMyTier, tierLabel } from './api'

// Overview first-screen: the old mini strip split into two side-by-side mini
// cards — TierMiniPanel (left, 6 cols) + ApiLimitsMiniPanel (right, 4 cols).
// Same compact visual language as the original TierMiniCard, just separated.

const fmtLimit = (n: number, unlimited: string) =>
  n <= 0 ? unlimited : String(n)

// Left panel (6 cols): current tier + contribution progress. Mini height.
export function TierMiniPanel() {
  const { t } = useTranslation()
  const { data, isLoading } = useQuery({
    queryKey: ['my-tier'],
    queryFn: getMyTier,
    refetchInterval: 60_000,
    refetchOnWindowFocus: true,
  })

  if (isLoading || !data) return null

  const hasNext = data.next_tier !== ''

  return (
    <div className='bg-card h-full overflow-hidden rounded-2xl border shadow-xs'>
      <div className='flex h-full flex-wrap items-center gap-4 px-4 py-3 sm:px-5'>
        <div className='flex min-w-0 items-center gap-3'>
          <span className='flex size-9 shrink-0 items-center justify-center rounded-xl bg-amber-500/10'>
            <Trophy className='size-4 text-amber-500' aria-hidden='true' />
          </span>
          {/* Current tier + next tier on one row */}
          <div className='flex flex-wrap items-center gap-2'>
            <span className='text-sm font-semibold tracking-tight'>
              {t('My Tier')}
            </span>
            <Badge variant='outline' className='text-xs'>
              {tierLabel(data.group)}
            </Badge>
            {hasNext && (
              <span className='text-muted-foreground inline-flex items-center gap-1 text-xs'>
                <ArrowRight className='size-3' aria-hidden='true' />
                {tierLabel(data.next_tier)}
              </span>
            )}
          </div>
        </div>

        {hasNext && (
          <div className='flex min-w-[180px] flex-1 flex-col gap-1.5'>
            <div className='text-muted-foreground flex justify-between text-xs'>
              <span>
                {t('Contributed')} {formatUSD(data.total_donated)}
              </span>
              <span>
                {t('To next tier')}: {formatUSD(data.to_next_contribution)}
              </span>
            </div>
            <Progress value={Math.round(data.next_progress * 100)} />
          </div>
        )}

        <Link
          to='/profile'
          className='text-muted-foreground hover:text-foreground ml-auto inline-flex shrink-0 items-center gap-1 text-xs'
        >
          {t('View details')}
          <ArrowRight className='size-3' aria-hidden='true' />
        </Link>
      </div>
    </div>
  )
}

// Right panel (4 cols): API limits — RPM + concurrency. Mini height.
export function ApiLimitsMiniPanel() {
  const { t } = useTranslation()
  const { data, isLoading } = useQuery({
    queryKey: ['my-tier'],
    queryFn: getMyTier,
    refetchInterval: 60_000,
    refetchOnWindowFocus: true,
  })

  if (isLoading || !data) return null

  return (
    <div className='bg-card h-full overflow-hidden rounded-2xl border shadow-xs'>
      <div className='flex h-full items-center gap-3 px-4 py-3 sm:px-5'>
        {/* Left: icon + title + subtitle */}
        <span className='flex size-9 shrink-0 items-center justify-center rounded-xl bg-sky-500/10'>
          <Zap className='size-4 text-sky-500' aria-hidden='true' />
        </span>
        <div className='min-w-0'>
          <div className='text-sm font-semibold tracking-tight'>
            {t('API Limits')}
          </div>
          <div className='text-muted-foreground line-clamp-1 text-xs'>
            {t('Requests / min')} · {t('Concurrent sessions')}
          </div>
        </div>

        {/* Right: two metric groups — icon + label on top, number below */}
        <div className='ml-auto flex shrink-0 items-center gap-4'>
          <div className='flex items-center gap-1.5'>
            <Zap className='text-muted-foreground size-3.5' aria-hidden='true' />
            <div className='leading-tight'>
              <div className='text-muted-foreground text-[10px] tracking-wide uppercase'>
                RPM
              </div>
              <div className='text-sm font-bold tabular-nums'>
                {fmtLimit(data.rpm, t('Unlimited'))}
              </div>
            </div>
          </div>
          <div className='flex items-center gap-1.5'>
            <Gauge
              className='text-muted-foreground size-3.5'
              aria-hidden='true'
            />
            <div className='leading-tight'>
              <div className='text-muted-foreground text-[10px] tracking-wide uppercase'>
                {t('Sessions')}
              </div>
              <div className='text-sm font-bold tabular-nums'>
                {fmtLimit(data.session_limit, t('Unlimited'))}
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
