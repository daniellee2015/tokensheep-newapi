import { Link } from '@tanstack/react-router'
import {
  Claude,
  DeepSeek,
  Gemini,
  Grok,
  Kimi,
  OpenAI,
  Qwen,
} from '@lobehub/icons'
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

interface HeroSectionProps {
  isAuthenticated: boolean
}


// Provider logos: GPT/Claude/Gemini in center, others flanking on the sides.
// Kimi uses the base (dark) icon — .Color renders white text invisible on
// the light chip background.
const HERO_PROVIDERS: {
  icon: React.ComponentType<{ size?: number }>
  label: string
}[] = [
  { icon: DeepSeek.Color, label: 'DeepSeek' },
  { icon: Qwen.Color, label: 'Qwen' },
  { icon: OpenAI, label: 'OpenAI' },
  { icon: Claude.Color, label: 'Claude' },
  { icon: Gemini.Color, label: 'Gemini' },
  { icon: Grok, label: 'Grok' },
  { icon: Kimi.Avatar, label: 'Kimi' },
]

export function HeroSection({ isAuthenticated }: HeroSectionProps) {
  const { t } = useTranslation()

  return (
    <section className='relative isolate min-h-[600px] overflow-hidden px-6 pt-24 pb-16 sm:min-h-[700px] sm:pt-32 sm:pb-24'>
      {/* Rainbow perspective background — designed SVG illustration. */}
      <div
        aria-hidden
        className='pointer-events-none absolute inset-0 z-0'
      >
        <img
          src='/hero-rainbow-bg.svg'
          alt=''
          className='h-full w-full translate-y-[8%] object-cover object-bottom'
        />
        {/* White gradient overlay: fades out the top half so the rainbow
            pillar doesn't interfere with headline text above. */}
        <div className='absolute inset-0 bg-gradient-to-b from-background via-background/60 via-18% to-transparent to-40%' />
      </div>

      <div className='relative z-10 mx-auto flex max-w-3xl flex-col items-center gap-8 text-center'>
        <span className='border-border/60 bg-background/60 text-muted-foreground inline-flex items-center gap-2 rounded-full border px-3 py-1 text-xs backdrop-blur-sm'>
          <span className='inline-block size-1.5 rounded-full bg-emerald-500' />
          {t('landing.hero.badge')}
        </span>

        <h1 className='text-foreground text-4xl leading-[1.05] font-black tracking-tight sm:text-6xl'>
          {t('landing.hero.titleLead')}{' '}
          <span className='bg-gradient-to-r from-sky-500 via-fuchsia-500 to-orange-500 bg-clip-text text-transparent'>
            {t('landing.hero.titleAccent')}
          </span>
          <br />
          {t('landing.hero.titleTail')}
        </h1>

        <p className='text-muted-foreground max-w-xl text-base leading-relaxed sm:text-lg'>
          {t('landing.hero.subtitle')}
          <br className='hidden sm:block' />
          {t('landing.hero.subtitleTail')}
        </p>

        {/* Provider logos floating above the tunnel — each in a frosted chip
            with a soft shadow, gently bobbing via .hero-float. */}
        <div className='flex flex-wrap items-center justify-center gap-3 pt-1 sm:gap-4'>
          {HERO_PROVIDERS.map((p, i) => {
            const Icon = p.icon
            return (
              <div
                key={p.label}
                className='hero-float border-border/50 bg-background/70 flex size-12 items-center justify-center rounded-2xl border shadow-lg shadow-black/[0.06] backdrop-blur-md sm:size-14 dark:shadow-black/30'
                style={{ animationDelay: `${i * 0.35}s` }}
                title={p.label}
              >
                <Icon size={28} />
              </div>
            )
          })}
        </div>

        <div className='flex flex-wrap items-center justify-center gap-3 pt-2'>
          {isAuthenticated ? (
            <Button
              size='lg'
              className='rounded-full px-6'
              render={<Link to='/console' />}
            >
              {t('landing.hero.ctaConsole')}
              <ArrowRight className='ml-1 size-4' />
            </Button>
          ) : (
            <Button
              size='lg'
              className='rounded-full px-6'
              render={<Link to='/sign-up' />}
            >
              {t('landing.hero.ctaSignUp')}
              <ArrowRight className='ml-1 size-4' />
            </Button>
          )}
          <Button
            variant='outline'
            size='lg'
            className='rounded-full px-6'
            render={<a href='#tier' />}
          >
            {t('landing.hero.ctaTiers')}
          </Button>
        </div>
      </div>
    </section>
  )
}
