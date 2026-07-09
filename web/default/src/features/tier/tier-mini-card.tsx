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

import { formatUSD, getMyTier, tierDisplayName } from './api'

// Compact tier + limits strip for the Overview first screen. Shows current
// tier, contribution progress, and RPM/concurrency inline. Links to /profile.
export function TierMiniCard() {
  const { t } = useTranslation()
  const { data, isLoading } = useQuery({
    queryKey: ['my-tier'],
    queryFn: getMyTier,
    refetchInterval: 60_000,
    refetchOnWindowFocus: true,
  })

  if (isLoading || !data) return null

  const hasNext = data.next_tier !== '' && data.next_tier !== data.group
  const fmtLimit = (n: number) => (n <= 0 ? t('Unlimited') : String(n))

  return (
    <div className='bg-card overflow-hidden rounded-2xl border shadow-xs'>
      <div className='flex flex-wrap items-center gap-4 px-4 py-3 sm:px-5'>
        <div className='flex min-w-0 items-center gap-3'>
          <span className='flex size-9 shrink-0 items-center justify-center rounded-xl bg-amber-500/10'>
            <Trophy className='size-4 text-amber-500' aria-hidden='true' />
          </span>
          <div className='min-w-0'>
            <div className='flex items-center gap-2'>
              <span className='text-sm font-semibold tracking-tight'>
                {t('My Tier')}
              </span>
              <Badge variant='outline' className='text-xs'>
                {tierDisplayName(data.group, t)}
              </Badge>
            </div>
            <div className='text-muted-foreground flex items-center gap-2 text-xs'>
              <span className='inline-flex items-center gap-1'>
                <Zap className='size-3' /> {fmtLimit(data.rpm)} RPM
              </span>
              <span className='inline-flex items-center gap-1'>
                <Gauge className='size-3' /> {fmtLimit(data.session_limit)}{' '}
                {t('concurrent')}
              </span>
            </div>
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
