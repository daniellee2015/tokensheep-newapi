import { Claude, Gemini, OpenAI } from '@lobehub/icons'
import { Link } from '@tanstack/react-router'
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'

import {
  useLandingPricing,
  type LandingPriceModel,
} from './use-landing-pricing'

// Landing pricing showcase — provider tabs at the top (large colour logos +
// wordmark below), content region below is one elevated container that
// swaps its 3 model cards when the tab changes. Prices come from
// /api/pricing when available; falls back to a curated static list so the
// section never renders empty.

// Per-provider display: coloured icon whenever available, plain black
// wordmark for OpenAI (which has no Color variant — its identity IS
// monochrome).
const PROVIDER_DISPLAY: Record<
  'openai' | 'claude' | 'gemini',
  { icon: React.ComponentType<{ size?: number }>; label: string }
> = {
  openai: { icon: OpenAI, label: 'OpenAI' },
  claude: { icon: Claude.Color, label: 'Claude' },
  gemini: { icon: Gemini.Color, label: 'Gemini' },
}

function fmtUsd(v: number | undefined | null): string {
  if (v == null || !Number.isFinite(v)) return '—'
  if (v >= 1) return `$${v.toFixed(v % 1 === 0 ? 0 : 2)}`
  if (v >= 0.01) return `$${v.toFixed(2)}`
  return `$${v.toPrecision(2)}`
}

export function PricingCardsSection() {
  const { t } = useTranslation()
  const { providers } = useLandingPricing()
  if (providers.length === 0) return null

  return (
    <section className='relative border-t border-border/40 px-6 py-20 sm:py-24'>
      <div className='mx-auto max-w-6xl'>
        <div className='mb-12 space-y-2 text-center'>
          <p className='text-muted-foreground/60 text-xs font-semibold tracking-widest uppercase'>
            {t('landing.pricing.eyebrow')}
          </p>
          <h2 className='text-foreground text-3xl font-bold tracking-tight sm:text-4xl'>
            {t('landing.pricing.title')}
          </h2>
          <p className='text-muted-foreground text-sm'>
            {t('landing.pricing.subtitle')}
          </p>
        </div>

        <Tabs defaultValue={providers[0].id} className='items-center'>
          {/* Large colour provider logos, no boxes around each — tab state
              is shown by underline + opacity on the logo itself. */}
          <TabsList
            variant='line'
            className='mx-auto mb-10 flex h-auto flex-wrap items-center justify-center gap-8 bg-transparent p-0 sm:gap-14'
          >
            {providers.map((p) => {
              const disp = PROVIDER_DISPLAY[p.iconKey]
              const Icon = disp.icon
              return (
                <TabsTrigger
                  key={p.id}
                  value={p.id}
                  className={cn(
                    'group/logo-tab flex h-auto flex-col items-center justify-center gap-2 px-3 py-2 opacity-40 transition-all',
                    'data-[selected=true]:opacity-100',
                    'hover:opacity-100'
                  )}
                >
                  <Icon size={48} />
                  <span className='text-foreground text-base font-semibold tracking-tight'>
                    {disp.label}
                  </span>
                </TabsTrigger>
              )
            })}
          </TabsList>

          {providers.map((p) => (
            <TabsContent key={p.id} value={p.id} className='w-full'>
              <div className='grid gap-5 md:grid-cols-3'>
                {p.models.map((m) => (
                  <PricingCard key={m.displayName} model={m} />
                ))}
              </div>
            </TabsContent>
          ))}
        </Tabs>

        <p className='mt-10 text-center text-sm'>
          <Link
            to='/pricing'
            className='text-muted-foreground hover:text-foreground inline-flex items-center gap-1 underline-offset-4 transition-colors hover:underline'
          >
            {t('landing.pricing.more')}
            <ArrowRight className='size-3.5' />
          </Link>
        </p>
      </div>
    </section>
  )
}

function PricingCard({ model }: { model: LandingPriceModel }) {
  const { t } = useTranslation()
  const hasCache = model.cacheReadRatio != null || model.cacheWriteRatio != null

  return (
    <div className='border-border/50 bg-card group/pricing-card flex flex-col gap-5 rounded-2xl border p-6 shadow-md shadow-black/[0.05] transition-all hover:-translate-y-1 hover:shadow-xl hover:shadow-black/[0.08] dark:shadow-black/30 dark:hover:shadow-black/50'>
      <div className='space-y-2 text-center'>
        <span className='border-border/60 text-muted-foreground inline-flex items-center rounded-full border px-2.5 py-0.5 text-[11px] font-medium'>
          {t(model.tagKey)}
        </span>
        <h3 className='text-foreground text-xl font-bold tracking-tight sm:text-2xl'>
          {model.displayName}
        </h3>
      </div>

      <div className='grid grid-cols-2 gap-2.5'>
        <PriceTile
          tone='input'
          label={t('landing.pricing.card.input')}
          officialUsd={model.officialInputUsd}
        />
        <PriceTile
          tone='output'
          label={t('landing.pricing.card.output')}
          officialUsd={model.officialOutputUsd}
        />
      </div>

      {hasCache && (
        <div className='border-border/40 bg-muted/40 space-y-2 rounded-xl border p-3'>
          <CacheRow
            label={t('landing.pricing.card.cacheRead')}
            officialUsd={
              model.cacheReadRatio != null && model.officialInputUsd != null
                ? model.officialInputUsd * model.cacheReadRatio
                : undefined
            }
          />
          <div className='border-border/30 border-t' />
          <CacheRow
            label={t('landing.pricing.card.cacheWrite')}
            officialUsd={
              model.cacheWriteRatio != null && model.officialInputUsd != null
                ? model.officialInputUsd * model.cacheWriteRatio
                : undefined
            }
          />
        </div>
      )}
    </div>
  )
}

function PriceTile({
  tone,
  label,
  officialUsd,
}: {
  tone: 'input' | 'output'
  label: string
  officialUsd?: number
}) {
  const bg =
    tone === 'input'
      ? 'bg-sky-50 border-sky-100 dark:bg-sky-500/10 dark:border-sky-500/20'
      : 'bg-emerald-50 border-emerald-100 dark:bg-emerald-500/10 dark:border-emerald-500/20'

  return (
    <div className={cn('rounded-xl border px-3 py-3', bg)}>
      <div className='text-muted-foreground/80 mb-1 text-[11px] font-medium tracking-widest uppercase'>
        {label}
      </div>
      <div className='flex items-baseline gap-1.5'>
        <span className='text-muted-foreground/60 text-xs line-through decoration-1'>
          {fmtUsd(officialUsd)}
        </span>
        <span className='text-foreground text-lg font-bold tracking-tight'>
          <PriceFreeLabel />
        </span>
      </div>
      <div className='text-muted-foreground/60 mt-0.5 text-[10px]'>
        <PricePerMillionLabel />
      </div>
    </div>
  )
}

function CacheRow({
  label,
  officialUsd,
}: {
  label: string
  officialUsd?: number
}) {
  return (
    <div className='flex items-baseline justify-between'>
      <span className='text-muted-foreground text-xs font-medium tracking-wide'>
        {label}
      </span>
      <div className='flex items-baseline gap-2'>
        {officialUsd != null && (
          <span className='text-muted-foreground/60 text-[11px] line-through decoration-1'>
            {fmtUsd(officialUsd)}
          </span>
        )}
        <span className='text-foreground text-sm font-semibold tracking-tight'>
          <PriceFreeLabel />
        </span>
      </div>
    </div>
  )
}

function PriceFreeLabel() {
  const { t } = useTranslation()
  return <>{t('landing.pricing.card.free')}</>
}

function PricePerMillionLabel() {
  const { t } = useTranslation()
  return <>{t('landing.pricing.card.perMillion')}</>
}
