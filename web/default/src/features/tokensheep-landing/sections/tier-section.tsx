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
    chipTone: 'border-fuchsia-500/40 text-fuchsia-600 dark:text-fuchsia-400',
    thresholdKey: 'landing.tier.t2.threshold',
    perkKeys: [
      'landing.tier.t2.perk1',
      'landing.tier.t2.perk2',
      'landing.tier.t2.perk3',
      'landing.tier.t2.perk4',
    ],
    featured: true,
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

  return (
    <div
      className={
        'relative flex flex-col gap-4 rounded-2xl border p-6 shadow-md transition-all hover:-translate-y-1 hover:shadow-xl ' +
        (tier.featured
          ? 'border-fuchsia-500/40 bg-card shadow-fuchsia-500/[0.08] hover:shadow-fuchsia-500/[0.15]'
          : 'border-border/50 bg-card shadow-black/[0.04] hover:shadow-black/[0.08] dark:shadow-black/30 dark:hover:shadow-black/50')
      }
    >
      {tier.featured && (
        <span className='absolute -top-2.5 right-4 rounded-full bg-fuchsia-500 px-2 py-0.5 text-[10px] font-semibold tracking-wider text-white uppercase'>
          {t('landing.tier.popular')}
        </span>
      )}

      <div className='space-y-2'>
        <span
          className={
            'inline-flex items-center rounded-full border px-2.5 py-0.5 text-[11px] font-medium ' +
            tier.chipTone
          }
        >
          {t(tier.chipKey)}
        </span>
        <h3 className='text-foreground text-lg font-bold tracking-tight'>
          {t(tier.nameKey)}
        </h3>
        <p className='text-muted-foreground/80 text-xs'>
          {t(tier.thresholdKey)}
        </p>
      </div>

      <ul className='space-y-2 border-t border-border/50 pt-4 text-sm'>
        {tier.perkKeys.map((perkKey) => (
          <li key={perkKey} className='flex items-start gap-2'>
            <Check className='text-foreground/60 mt-0.5 size-3.5 shrink-0' />
            <span className='text-foreground/80'>{t(perkKey)}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}
