import { Link } from '@tanstack/react-router'
import { ArrowRight, Check } from 'lucide-react'
import { useTranslation } from 'react-i18next'

// 4-tier ladder — the concrete numbers are copy, not config; when the real
// tier backend lands, drive these from /api/tiers instead.

interface Tier {
  nameKey: string
  chipKey: string
  chipTone: string
  thresholdKey: string
  perkKeys: string[]
  featured?: boolean
  span?: 1 | 2
}

const TIERS: Tier[] = [
  {
    nameKey: 'landing.tier.t0.name',
    chipKey: 'landing.tier.t0.chip',
    chipTone: 'border-border/60 text-muted-foreground',
    thresholdKey: 'landing.tier.t0.threshold',
    perkKeys: [
      'landing.tier.t0.perk1',
      'landing.tier.t0.perk2',
      'landing.tier.t0.perk3',
      'landing.tier.t0.perk4',
    ],
  },
  {
    nameKey: 'landing.tier.t1.name',
    chipKey: 'landing.tier.t1.chip',
    chipTone: 'border-sky-500/40 text-sky-600 dark:text-sky-400',
    thresholdKey: 'landing.tier.t1.threshold',
    perkKeys: [
      'landing.tier.t1.perk1',
      'landing.tier.t1.perk2',
      'landing.tier.t1.perk3',
      'landing.tier.t1.perk4',
    ],
  },
  {
    nameKey: 'landing.tier.t2.name',
    chipKey: 'landing.tier.t2.chip',
    chipTone: 'border-brand/40 text-brand',
    thresholdKey: 'landing.tier.t2.threshold',
    perkKeys: [
      'landing.tier.t2.perk1',
      'landing.tier.t2.perk2',
      'landing.tier.t2.perk3',
      'landing.tier.t2.perk4',
    ],
    featured: true,
    span: 2,
  },
  {
    nameKey: 'landing.tier.t3.name',
    chipKey: 'landing.tier.t3.chip',
    chipTone: 'border-orange-500/40 text-orange-600 dark:text-orange-400',
    thresholdKey: 'landing.tier.t3.threshold',
    perkKeys: [
      'landing.tier.t3.perk1',
      'landing.tier.t3.perk2',
      'landing.tier.t3.perk3',
      'landing.tier.t3.perk4',
    ],
  },
  {
    nameKey: 'landing.tier.t4.name',
    chipKey: 'landing.tier.t4.chip',
    chipTone: 'border-purple-500/40 text-purple-600 dark:text-purple-400',
    thresholdKey: 'landing.tier.t4.threshold',
    perkKeys: [
      'landing.tier.t4.perk1',
      'landing.tier.t4.perk2',
      'landing.tier.t4.perk3',
      'landing.tier.t4.perk4',
    ],
    span: 2,
  },
  {
    nameKey: 'landing.tier.standard.name',
    chipKey: 'landing.tier.standard.chip',
    chipTone: 'border-neutral-500/40 text-neutral-600 dark:text-neutral-400',
    thresholdKey: 'landing.tier.standard.threshold',
    perkKeys: [
      'landing.tier.standard.perk1',
      'landing.tier.standard.perk2',
      'landing.tier.standard.perk3',
      'landing.tier.standard.perk4',
    ],
  },
]

export function TierSection() {
  const { t } = useTranslation()

  return (
    <section
      id='tier'
      className='relative scroll-mt-20 border-t border-border/40 px-6 py-20 sm:py-24'
    >
      <div className='mx-auto max-w-6xl'>
        <div className='mb-12 space-y-3 text-center'>
          <p className='text-muted-foreground/60 text-xs font-semibold tracking-widest uppercase'>
            {t('landing.tier.eyebrow')}
          </p>
          <h2 className='text-foreground text-3xl font-bold tracking-tight sm:text-4xl'>
            {t('landing.tier.title')}
          </h2>
          <p className='text-muted-foreground mx-auto max-w-2xl text-sm sm:text-base'>
            {t('landing.tier.subtitle')}
            <span className='text-foreground/70 block pt-1'>
              {t('landing.tier.subtitleTail')}
            </span>
          </p>
        </div>

        <div className='grid gap-5 md:grid-cols-2 xl:grid-cols-4'>
          {TIERS.map((tier) => (
            <TierCard key={tier.nameKey} tier={tier} />
          ))}
        </div>

        <p className='mt-10 text-center text-sm'>
          <Link
            to='/pricing'
            className='text-muted-foreground hover:text-foreground inline-flex items-center gap-1 underline-offset-4 transition-colors hover:underline'
          >
            {t('landing.tier.moreLink')}
            <ArrowRight className='size-3.5' />
          </Link>
        </p>
      </div>
    </section>
  )
}

function TierCard({ tier }: { tier: Tier }) {
  const { t } = useTranslation()
  const scenarioKey = tier.perkKeys[3]
  const listPerkKeys = tier.perkKeys.slice(0, 3)

  return (
    <div
      className={
        'group relative flex flex-col overflow-hidden rounded-2xl border transition-all hover:-translate-y-1 hover:shadow-xl ' +
        (tier.span === 2 ? 'md:col-span-2 ' : '') +
        (tier.featured
          ? 'border-brand/50 bg-gradient-to-b from-brand/[0.04] to-card shadow-brand/10 shadow-lg'
          : 'border-border/40 bg-card shadow-sm hover:shadow-lg dark:shadow-black/30')
      }
    >
      {tier.featured && (
        <span className='bg-brand text-brand-foreground absolute -top-0 right-0 rounded-bl-xl px-3 py-1 text-[10px] font-semibold tracking-wider uppercase'>
          {t('landing.tier.popular')}
        </span>
      )}

      {/* Header block — chip + name + scenario */}
      <div className='space-y-2 px-6 pt-6 pb-4'>
        <span
          className={
            'inline-flex items-center rounded-full border px-2.5 py-0.5 text-[11px] font-medium ' +
            tier.chipTone
          }
        >
          {t(tier.chipKey)}
        </span>
        <div>
          <h3 className='text-foreground text-xl font-bold tracking-tight'>
            {t(tier.nameKey)}
          </h3>
          {scenarioKey && (
            <p className='text-muted-foreground mt-0.5 text-sm'>
              {t(scenarioKey)}
            </p>
          )}
        </div>
      </div>

      {/* Perk list — clean rows with subtle dividers */}
      <div className='mt-auto flex flex-col gap-0 border-t border-border/30 px-6 py-4'>
        {listPerkKeys.map((perkKey, idx) => (
          <div
            key={perkKey}
            className={
              'flex items-center gap-3 py-2.5' +
              (idx < listPerkKeys.length - 1
                ? ' border-b border-dashed border-border/30'
                : '')
            }
          >
            <span className='flex size-5 shrink-0 items-center justify-center rounded-full bg-emerald-500/10'>
              <Check className='size-3 text-emerald-500' />
            </span>
            <span className='text-foreground/90 text-sm'>{t(perkKey)}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
