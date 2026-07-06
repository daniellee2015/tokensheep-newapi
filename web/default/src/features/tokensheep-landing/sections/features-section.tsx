import { useTranslation } from 'react-i18next'

interface Feature {
  iconSrc: string
  iconAlt: string
  index: string
  titleKey: string
  descriptionKey: string
  /** Tailwind gradient class pair (from-x-N to-x-N) — used for the corner glow. */
  accent: string
}

// 4 pillars — each paired with a personality sheep sticker.
// The stickers are self-contained SVGs (already colored), so we don't tint them.
const FEATURES: Feature[] = [
  {
    iconSrc: '/sheep-cool.svg',
    iconAlt: 'cool sheep',
    index: '01',
    titleKey: 'landing.features.free.title',
    descriptionKey: 'landing.features.free.desc',
    accent: 'from-emerald-400 to-teal-500',
  },
  {
    iconSrc: '/sheep-helpless.svg',
    iconAlt: 'helpless sheep',
    index: '02',
    titleKey: 'landing.features.noNesting.title',
    descriptionKey: 'landing.features.noNesting.desc',
    accent: 'from-fuchsia-400 to-violet-500',
  },
  {
    iconSrc: '/sheep-confound.svg',
    iconAlt: 'confounded sheep',
    index: '03',
    titleKey: 'landing.features.fullContext.title',
    descriptionKey: 'landing.features.fullContext.desc',
    accent: 'from-sky-400 to-blue-500',
  },
  {
    iconSrc: '/sheep-evil.svg',
    iconAlt: 'evil sheep',
    index: '04',
    titleKey: 'landing.features.bestEffort.title',
    descriptionKey: 'landing.features.bestEffort.desc',
    accent: 'from-amber-400 to-orange-500',
  },
]

export function FeaturesSection() {
  const { t } = useTranslation()

  return (
    <section className='relative border-t border-border/40 px-6 py-20 sm:py-24'>
      <div className='mx-auto max-w-5xl'>
        <div className='mb-12 space-y-2 text-center'>
          <p className='text-muted-foreground/60 text-xs font-semibold tracking-widest uppercase'>
            {t('landing.features.eyebrow')}
          </p>
          <h2 className='text-foreground text-3xl font-bold tracking-tight sm:text-4xl'>
            {t('landing.features.title')}
          </h2>
        </div>

        <div className='grid gap-5 sm:grid-cols-2'>
          {FEATURES.map((f) => (
            <FeatureCard key={f.titleKey} feature={f} />
          ))}
        </div>
      </div>
    </section>
  )
}

function FeatureCard({ feature }: { feature: Feature }) {
  const { t } = useTranslation()

  return (
    <div className='border-border/50 bg-card group relative flex gap-5 overflow-hidden rounded-2xl border p-6 shadow-md shadow-black/[0.04] transition-all hover:-translate-y-1 hover:shadow-xl hover:shadow-black/[0.08] dark:shadow-black/30 dark:hover:shadow-black/50'>
      {/* Corner gradient glow — subtle in idle, blooms on hover */}
      <div
        aria-hidden='true'
        className={
          'pointer-events-none absolute -top-24 -right-24 h-56 w-56 rounded-full bg-gradient-to-br opacity-[0.12] blur-3xl transition-opacity duration-300 group-hover:opacity-25 dark:opacity-[0.18] dark:group-hover:opacity-40 ' +
          feature.accent
        }
      />

      {/* Watermark index in the top-right — decorative, non-interactive */}
      <span
        aria-hidden='true'
        className='text-foreground pointer-events-none absolute top-2 right-4 font-mono text-6xl font-black tracking-tighter opacity-[0.08] select-none sm:text-7xl dark:opacity-[0.12]'
      >
        {feature.index}
      </span>

      <div className='relative shrink-0'>
        <img
          src={feature.iconSrc}
          alt={feature.iconAlt}
          className='size-16 object-contain transition-transform duration-300 group-hover:scale-110 sm:size-20'
        />
      </div>

      <div className='relative flex-1'>
        <h3 className='text-foreground mb-2 text-xl font-semibold tracking-tight'>
          {t(feature.titleKey)}
        </h3>
        <p className='text-muted-foreground text-sm leading-relaxed'>
          {t(feature.descriptionKey)}
        </p>
      </div>
    </div>
  )
}
