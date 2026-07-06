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
// TokenSheep tier upgrade cards. Rendered inside the wallet Add Funds card
// when the operator has EnableTierCardsInRecharge on. Every card triggers
// the same Pancake checkout as the "Custom Amount" section — this is a
// UX shortcut, not a separate payment path.
//
// The tier list is NOT hardcoded here. It comes from /api/user/topup/info
// under `tier_cards`, which the server materializes from the operator-
// editable `TierThresholds` map (see setting/tokensheep_setting/economy.go).
// Adding, renaming or removing a tier from the admin panel makes this row
// update on the next page load with zero code changes.
//
// Per-tier copy (perks description, "featured" ribbon) is looked up in
// i18n locale files, keyed by the tier name — so a new tier "whale" only
// needs a locale entry `wallet.tierCards.whale.perks` to render nicely;
// missing entries fall back to a generic tier + amount label.
import { Check, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Card, CardContent } from '@/components/ui/card'
import { cn } from '@/lib/utils'

interface TierCardEntry {
  /** Tier key matching the backend user_group name (e.g. "supporter"). */
  tier: string
  /** Station-side dollar amount that lands the user in this tier. */
  amount: number
}

interface TokensheepTierCardsProps {
  /** Tier options sourced from `/api/user/topup/info#tier_cards`. */
  tiers: TierCardEntry[]
  /** Current user tier (server truth); used to badge already-reached options. */
  currentTier?: string
  /** Fires when the user picks a card — parent triggers Pancake checkout. */
  onSelect: (amount: number, tier: string) => void
  /** Which tier's card is currently mid-request; disables the whole row. */
  loadingTier?: string | null
}

export function TokensheepTierCards({
  tiers,
  currentTier,
  onSelect,
  loadingTier,
}: TokensheepTierCardsProps) {
  const { t } = useTranslation()

  if (!tiers || tiers.length === 0) return null

  // Rank tiers by their configured amount so "reached" logic is purely
  // amount-driven — no hardcoded ordering table. Same-rank ties (unlikely
  // in practice) fall through as "not reached" for that tier.
  const reachedAmount = currentTier
    ? tiers.find((t) => t.tier === currentTier)?.amount
    : undefined

  // Middle card visually "featured". If tiers.length is even we bias to
  // the lower-middle so a $10/$50/$100/$500 array still highlights $50.
  const featuredIndex =
    tiers.length >= 2 ? Math.floor((tiers.length - 1) / 2) : -1

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

        <div
          className={cn(
            'grid grid-cols-1 gap-3',
            tiers.length === 2 && 'sm:grid-cols-2',
            tiers.length === 3 && 'sm:grid-cols-3',
            tiers.length >= 4 && 'sm:grid-cols-2 lg:grid-cols-4'
          )}
        >
          {tiers.map((entry, index) => {
            const reached =
              reachedAmount !== undefined && entry.amount <= reachedAmount
            const isLoading = loadingTier === entry.tier
            const featured = index === featuredIndex
            return (
              <TierCard
                key={entry.tier}
                entry={entry}
                featured={featured}
                reached={reached}
                loading={isLoading}
                disabled={!!loadingTier && !isLoading}
                onSelect={() => onSelect(entry.amount, entry.tier)}
              />
            )
          })}
        </div>
      </CardContent>
    </Card>
  )
}

interface TierCardProps {
  entry: TierCardEntry
  featured: boolean
  reached: boolean
  loading: boolean
  disabled: boolean
  onSelect: () => void
}

function TierCard({
  entry,
  featured,
  reached,
  loading,
  disabled,
  onSelect,
}: TierCardProps) {
  const { t } = useTranslation()

  // Prefer tier-specific copy from i18n locales; fall back to a generic
  // "Unlock <tier>" label. New tiers ship with the tier name showing until
  // ops add the locale entry.
  const badgeKey = `wallet.tierCards.${entry.tier}.badge`
  const nameKey = `wallet.tierCards.${entry.tier}.name`
  const perksKey = `wallet.tierCards.${entry.tier}.perks`

  const badge = t(badgeKey, { defaultValue: entry.tier })
  const name = t(nameKey, {
    defaultValue: t('wallet.tierCards.unlockDefault', {
      defaultValue: `Unlock ${entry.tier}`,
      tier: entry.tier,
    }),
  })
  const perks = t(perksKey, { defaultValue: '' })

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
        featured
          ? 'border-fuchsia-500/40 bg-card shadow-md shadow-fuchsia-500/[0.08]'
          : 'border-border/60 bg-card'
      )}
    >
      {featured && (
        <span className='absolute -top-2 right-3 rounded-full bg-fuchsia-500 px-2 py-0.5 text-[10px] font-semibold tracking-wider text-white uppercase'>
          {t('wallet.tierCards.popular')}
        </span>
      )}

      <div className='flex items-center justify-between'>
        <span className='text-muted-foreground text-[11px] font-medium tracking-wider uppercase'>
          {badge}
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
          ${entry.amount}
        </div>
        <div className='text-foreground text-sm font-semibold'>{name}</div>
        {perks && (
          <div className='text-muted-foreground text-xs'>{perks}</div>
        )}
      </div>

      <div className='text-primary flex items-center gap-1 text-xs font-medium'>
        {actionLabel}
      </div>
    </button>
  )
}
