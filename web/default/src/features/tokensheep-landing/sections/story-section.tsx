import { useTranslation } from 'react-i18next'

export function StorySection() {
  const { t } = useTranslation()

  const pains: { key: string; label: string }[] = [
    { key: 'access', label: t('landing.story.pain.access') },
    { key: 'topup', label: t('landing.story.pain.topup') },
    { key: 'latency', label: t('landing.story.pain.latency') },
    { key: 'billing', label: t('landing.story.pain.billing') },
    { key: 'banned', label: t('landing.story.pain.banned') },
    { key: 'truncate', label: t('landing.story.pain.truncate') },
    { key: 'downgrade', label: t('landing.story.pain.downgrade') },
    { key: 'rugpull', label: t('landing.story.pain.rugpull') },
  ]

  return (
    <section className='relative border-t border-border/40 px-6 py-20 sm:py-24'>
      <div className='mx-auto grid max-w-5xl gap-10 md:grid-cols-[220px_1fr] md:gap-16'>
        <div className='space-y-2'>
          <p className='text-muted-foreground/60 text-xs font-semibold tracking-widest uppercase'>
            {t('landing.story.eyebrow')}
          </p>
          <h2 className='text-foreground text-3xl leading-tight font-bold tracking-tight'>
            {t('landing.story.titleLead')}{' '}
            <br className='hidden md:block' />
            <span className='bg-gradient-to-r from-fuchsia-500 to-orange-500 bg-clip-text text-transparent'>
              TokenSheep
            </span>
          </h2>
        </div>

        <div className='space-y-5 text-base leading-relaxed text-foreground/85'>
          <p>{t('landing.story.line1')}</p>
          <p>
            {t('landing.story.line2Prefix')}
            <strong className='text-foreground font-semibold'>
              {t('landing.story.line2Emphasis')}
            </strong>
            {t('landing.story.line2Suffix')}
          </p>

          {/* Pain points — visual chip row */}
          <ul className='not-prose flex flex-wrap gap-2 pt-1'>
            {pains.map((pain) => (
              <li
                key={pain.key}
                className='border-border/60 bg-muted/40 text-muted-foreground inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs'
              >
                <span className='inline-block size-1 rounded-full bg-rose-500/70' />
                {pain.label}
              </li>
            ))}
          </ul>
          <p className='text-muted-foreground/80 text-sm'>
            {t('landing.story.painsAftermath')}
          </p>

          <p>
            {t('landing.story.line3Prefix')}
            {t('landing.story.line3Direct')}
            <strong className='text-foreground font-semibold'>
              {t('landing.story.line3Full')}
            </strong>
            {t('landing.story.line3Tail')}
          </p>
          <p>
            {t('landing.story.line4Prefix')}
            <span className='bg-foreground/5 rounded px-1.5 py-0.5 font-medium'>
              {t('landing.story.line4Highlight')}
            </span>
            {t('landing.story.line4Suffix')}
          </p>
          <p className='text-muted-foreground text-sm'>
            {t('landing.story.fallbackNote')}
          </p>

          {/* Sibling-site fallback — promoted to its own emphasised badge */}
          <a
            href='https://qzcode.1iiu.com/'
            target='_blank'
            rel='noopener noreferrer'
            className='group border-border/60 bg-card hover:border-foreground/40 hover:bg-card/80 relative mt-1 inline-flex items-center gap-3 rounded-xl border px-4 py-3 shadow-sm transition-all hover:-translate-y-0.5 hover:shadow-md'
          >
            <span className='relative inline-flex size-2 items-center justify-center'>
              <span className='absolute inline-flex size-2 animate-ping rounded-full bg-emerald-500 opacity-60' />
              <span className='relative inline-flex size-2 rounded-full bg-emerald-500' />
            </span>
            <div className='flex flex-col text-left'>
              <span className='text-muted-foreground text-[11px] font-medium tracking-widest uppercase'>
                {t('landing.story.fallbackChipEyebrow')}
              </span>
              <span className='text-foreground font-mono text-base font-semibold tracking-tight'>
                qzcode.1iiu.com
              </span>
            </div>
            <span
              aria-hidden='true'
              className='text-muted-foreground group-hover:text-foreground ml-1 transition-transform group-hover:translate-x-0.5'
            >
              →
            </span>
          </a>
        </div>
      </div>
    </section>
  )
}
