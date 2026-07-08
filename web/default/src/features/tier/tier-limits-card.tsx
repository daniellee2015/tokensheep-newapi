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
import { Gauge, Zap } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'

import { getMyTier } from './api'

// TokenSheep API Limits card — shows the current tier's RPM + concurrency
// (session) ceilings. RPM comes from the native per-group rate-limit map;
// session limit from tokensheep_setting.SessionLimit. 0 = unlimited.
function fmt(n: number, t: (k: string) => string): string {
  return n <= 0 ? t('Unlimited') : String(n)
}

export function TierLimitsCard() {
  const { t } = useTranslation()
  const { data, isLoading } = useQuery({
    queryKey: ['my-tier'],
    queryFn: getMyTier,
    refetchInterval: 60_000,
    refetchOnWindowFocus: true,
  })

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <Skeleton className='h-6 w-32' />
        </CardHeader>
        <CardContent>
          <Skeleton className='h-24 w-full' />
        </CardContent>
      </Card>
    )
  }
  if (!data) return null

  const items = [
    {
      icon: Zap,
      label: t('Requests / min'),
      sub: t('Max requests per minute'),
      value: fmt(data.rpm, t),
    },
    {
      icon: Gauge,
      label: t('Concurrent sessions'),
      sub: t('Requests running at the same time'),
      value: fmt(data.session_limit, t),
    },
  ]

  return (
    <TitledCard
      title={t('API Limits')}
      description={t('Rate and concurrency ceilings for your tier.')}
      icon={<Zap className='h-4 w-4' />}
      disableHoverEffect
      contentClassName='space-y-3'
    >
      {items.map((it) => {
        const Icon = it.icon
        return (
          <div
            key={it.label}
            className='bg-muted/30 flex items-center gap-3 rounded-md p-3'
          >
            <span className='bg-background flex size-8 shrink-0 items-center justify-center rounded-full border'>
              <Icon className='text-muted-foreground size-4' />
            </span>
            <div className='min-w-0 flex-1'>
              <div className='text-sm font-medium'>{it.label}</div>
              <div className='text-muted-foreground line-clamp-1 text-xs'>
                {it.sub}
              </div>
            </div>
            <div className='text-foreground text-base font-semibold tabular-nums'>
              {it.value}
            </div>
          </div>
        )
      })}
    </TitledCard>
  )
}
