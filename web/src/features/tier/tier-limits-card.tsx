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
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import { getMyLimitsUsage, getMyTier, type MyLimitsUsage } from './api'

// TokenSheep API Limits card — shows the current tier's RPM + concurrency
// (session) ceilings plus, below the concurrency ceiling, a live counter
// so the user can see "3 / 100 in use" and understand whether they're
// saturating the ceiling. RPM comes from the native per-group rate-limit
// map (static); session limit from tokensheep_setting.SessionLimit; live
// concurrency from /api/user/self/limits/usage (a Redis-backed counter).
//
// Why no live RPM: computing an accurate per-user RPM would require
// scanning logs on every card refresh. Q1 users flagged this as too
// expensive for the value, so this card only shows the static ceiling
// with a subtitle explaining the window duration when it's >1 minute.
// If we ever need it we can add another endpoint and extend the card.
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
  // Live usage runs on a much shorter refetch cadence (15s) than the tier
  // snapshot (60s) because the counter is what the user actually watches
  // when they think a request is being throttled. staleTime lets React
  // Query re-use the cache when several cards mount simultaneously (e.g.
  // R21-C Dashboard hint + Profile card) without triggering a duplicate
  // GET on top of the 15s poll.
  const { data: usage } = useQuery({
    queryKey: ['limits-usage'],
    queryFn: getMyLimitsUsage,
    refetchInterval: 15_000,
    refetchOnWindowFocus: true,
    staleTime: 5_000,
    // The backend degrades to source="unavailable" on Redis blips instead
    // of 5xx'ing, so a real error here means something bigger. One retry
    // is enough to distinguish a flake from a genuine outage without
    // hammering the endpoint every 15s.
    retry: 1,
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

  // When the endpoint reports a window >1 minute, spell it out so users
  // don't think "RPM" means the literal last 60 seconds. Falls back to
  // the original static subtitle otherwise (window=1 is the common case).
  const rpmWindowMinutes = usage?.rpm_window_minutes ?? 1
  const rpmSubtitle =
    rpmWindowMinutes > 1
      ? t('limits.usage.rpmWindow', { minutes: rpmWindowMinutes })
      : t('Max requests per minute')

  const items = [
    {
      icon: Zap,
      label: t('Requests / min'),
      sub: rpmSubtitle,
      value: fmt(data.rpm, t),
      usageLine: null as React.ReactNode | null,
    },
    {
      icon: Gauge,
      label: t('Concurrent sessions'),
      sub: t('Requests running at the same time'),
      value: fmt(data.session_limit, t),
      usageLine: renderConcurrencyUsage(usage, data.session_limit, t),
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
            className='bg-muted/30 flex flex-col gap-1 rounded-md p-3'
          >
            <div className='flex items-center gap-3'>
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
            {it.usageLine}
          </div>
        )
      })}
    </TitledCard>
  )
}

// renderConcurrencyUsage renders the "3 / 100 in use" subtitle below the
// ceiling row, with three visible states:
//
//   loading       usage is undefined (initial fetch, no cache) → em dash
//   unavailable   source === "unavailable" → italic gray "Live usage
//                 unavailable" with a tooltip explaining the ceiling is
//                 still enforced
//   normal        source is live/idle → "current X / Y in use"
//
// The unavailable state stays present (rather than hiding) so the user
// can tell the difference between "0 in-flight" and "we don't know". The
// pl-11 keeps the subtitle aligned with the label text above (icon 32px
// + gap 12px ≈ 44px = pl-11 in Tailwind 4px scale).
function renderConcurrencyUsage(
  usage: MyLimitsUsage | null | undefined,
  ceiling: number,
  t: (k: string, opts?: Record<string, unknown>) => string
): React.ReactNode {
  // Ceiling 0 in the tier snapshot means "unlimited" — the card renders
  // "Unlimited" as the value. Preserve that in the usage subtitle so the
  // user doesn't see "3 / 0 in use"; use the localized Unlimited label
  // as the denominator instead.
  const limitLabel = ceiling > 0 ? String(ceiling) : t('Unlimited')

  if (usage === undefined || usage === null) {
    // Query hasn't resolved yet, or returned null (network/JSON error).
    // Show a placeholder in the same slot so the card height doesn't
    // jump when data lands.
    return (
      <div
        className='text-muted-foreground pl-11 text-xs'
        data-testid='concurrency-usage'
      >
        —
      </div>
    )
  }

  if (usage.concurrency_source === 'unavailable') {
    return (
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger render={<div />}>
            <div
              className='text-muted-foreground/70 pl-11 text-xs italic'
              data-testid='concurrency-usage'
            >
              {t('limits.usage.unavailable')}
            </div>
          </TooltipTrigger>
          <TooltipContent>
            {t(
              'Live concurrency counter is temporarily unavailable. The ceiling above is still enforced.'
            )}
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
    )
  }

  return (
    <div
      className='text-muted-foreground pl-11 text-xs'
      data-testid='concurrency-usage'
    >
      {t('limits.usage.currentlyActive', {
        used: usage.concurrency_used,
        limit: limitLabel,
      })}
    </div>
  )
}
