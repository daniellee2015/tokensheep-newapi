import { Link } from '@tanstack/react-router'
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

interface HeroSectionProps {
  isAuthenticated: boolean
}

export function HeroSection({ isAuthenticated }: HeroSectionProps) {
  const { t } = useTranslation()

  return (
    <section className='relative overflow-hidden px-6 pt-24 pb-16 sm:pt-32 sm:pb-24'>
      {/* soft ambient background */}
      <div
        aria-hidden
        className='pointer-events-none absolute inset-x-0 top-0 h-[520px] bg-gradient-to-b from-sky-100/40 via-transparent to-transparent dark:from-sky-500/10'
      />
      <div
        aria-hidden
        className='pointer-events-none absolute -top-24 left-1/2 -z-10 h-[400px] w-[720px] -translate-x-1/2 rounded-full bg-fuchsia-200/40 blur-3xl dark:bg-fuchsia-800/20'
      />

      <div className='relative mx-auto flex max-w-3xl flex-col items-center gap-8 text-center'>
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
