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
// TokenSheep tier upgrade cards — three quick-pick amounts backed by a single
// Pancake product (see docs/spec/economy-model.md §7.2). One click sends the
// user to Pancake with the corresponding amount; cumulative donation total
// determines the tier they land in (§2.2). Rendered on the dedicated
// contribution page only when the operator has enabled the Pancake channel.
import { Check, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

interface TierOption {
  /** Tier key matching the backend user_group name */
  tier: 'supporter' | 'fan' | 'bestie'
  /** In-station amount (station unit = $1, see WaffoPancakeUnitPrice=1.0) */
  amount: number
  /** Whether to render the featured/Popular ribbon */
  featured?: boolean
}

const TIER_OPTIONS: TierOption[] = [
  { tier: 'supporter', amount: 10 },
  { tier: 'fan', amount: 50, featured: true },
  { tier: 'bestie', amount: 100 },
]
const TIER_SKELETON_KEYS = ['supporter', 'fan', 'bestie']

interface TokensheepTierCardsProps {
  /** Backend flag: Pancake channel is provisioned and enabled */
  enabled?: boolean
  /** Current user tier (server truth); used to hide already-reached options */
  currentTier?: string
  /** Fires when the user picks a card — parent triggers Pancake checkout */
  onSelect: (amount: number, tier: TierOption['tier']) => void
  /** Which tier's card is currently mid-request; disables the whole row */
  loadingTier?: TierOption['tier'] | null
  /** Skeleton while topup info loads */
  loading?: boolean
}

const TIER_ORDER: Record<string, number> = {
  free: 0,
  supporter: 1,
  fan: 2,
  bestie: 3,
  vip: 4,
}

export function TokensheepTierCards({
  enabled,
  currentTier,
  onSelect,
  loadingTier,
  loading,
}: TokensheepTierCardsProps) {
  const { t } = useTranslation()

  if (loading) {
    return (
      <div className='grid grid-cols-1 gap-3 sm:grid-cols-3'>
        {TIER_SKELETON_KEYS.map((key) => (
          <Skeleton key={key} className='h-40 rounded-2xl' />
        ))}
      </div>
    )
  }

  if (!enabled) return null

  const currentRank = TIER_ORDER[currentTier ?? 'free'] ?? 0

  return (
    <Card data-card-hover='false' className='overflow-hidden'>
      <CardContent className='space-y-4 p-4 sm:p-6'>
        <div className='space-y-1'>
          <h3 className='text-foreground flex items-center gap-2 text-base font-semibold tracking-tight sm:text-lg'>
            <Sparkles className='text-primary size-4' />
            {t('wallet.tierCards.title')}
          </h3>
          <p className='text-muted-foreground text-xs sm:text-sm'>
            {t('wallet.tierCards.subtitle')}
          </p>
        </div>

        <div className='grid grid-cols-1 gap-3 sm:grid-cols-3'>
          {TIER_OPTIONS.map((option) => {
            const reached = TIER_ORDER[option.tier] <= currentRank
            const isLoading = loadingTier === option.tier
            return (
              <TierCard
                key={option.tier}
                option={option}
                reached={reached}
                loading={isLoading}
                disabled={!!loadingTier && !isLoading}
                onSelect={() => onSelect(option.amount, option.tier)}
              />
            )
          })}
        </div>
      </CardContent>
    </Card>
  )
}

interface TierCardProps {
  option: TierOption
  reached: boolean
  loading: boolean
  disabled: boolean
  onSelect: () => void
}

function TierCard({
  option,
  reached,
  loading,
  disabled,
  onSelect,
}: TierCardProps) {
  const { t } = useTranslation()
  let actionLabel = t('wallet.tierCards.contribute')
  if (loading) {
    actionLabel = t('wallet.tierCards.processing')
  } else if (reached) {
    actionLabel = t('wallet.tierCards.topUp')
  }

  return (
    <button
      type='button'
      onClick={onSelect}
      disabled={loading || disabled}
      className={cn(
        'group relative flex flex-col gap-3 rounded-2xl border p-4 text-left transition-all',
        'hover:-translate-y-0.5 hover:shadow-lg disabled:cursor-not-allowed disabled:opacity-60 disabled:hover:translate-y-0 disabled:hover:shadow-none',
        option.featured
          ? 'border-fuchsia-500/40 bg-card shadow-md shadow-fuchsia-500/[0.08]'
          : 'border-border/60 bg-card'
      )}
    >
      {option.featured && (
        <span className='absolute -top-2 right-3 rounded-full bg-fuchsia-500 px-2 py-0.5 text-[10px] font-semibold tracking-wider text-white uppercase'>
          {t('wallet.tierCards.popular')}
        </span>
      )}

      <div className='flex items-center justify-between'>
        <span className='text-muted-foreground text-[11px] font-medium tracking-wider uppercase'>
          {t(`wallet.tierCards.${option.tier}.badge`)}
        </span>
        {reached && (
          <span className='inline-flex items-center gap-1 rounded-full bg-emerald-500/10 px-2 py-0.5 text-[10px] font-medium text-emerald-600 dark:text-emerald-400'>
            <Check className='size-3' />
            {t('wallet.tierCards.reached')}
          </span>
        )}
      </div>

      <div className='space-y-0.5'>
        <div className='text-foreground font-mono text-2xl font-bold tracking-tight tabular-nums'>
          ${option.amount}
        </div>
        <div className='text-foreground text-sm font-semibold'>
          {t(`wallet.tierCards.${option.tier}.name`)}
        </div>
        <div className='text-muted-foreground text-xs'>
          {t(`wallet.tierCards.${option.tier}.perks`)}
        </div>
      </div>

      <div className='text-primary flex items-center gap-1 text-xs font-medium'>
        {actionLabel}
      </div>
    </button>
  )
}
