import { Trans, useTranslation } from 'react-i18next'

import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'

interface FaqItem {
  qKey: string
  aRender: (t: (k: string) => string) => React.ReactNode
}

const FAQS: FaqItem[] = [
  {
    qKey: 'landing.faq.q1',
    aRender: (t) => t('landing.faq.a1'),
  },
  {
    qKey: 'landing.faq.q2',
    aRender: (t) => t('landing.faq.a2'),
  },
  {
    qKey: 'landing.faq.q3',
    aRender: (t) => t('landing.faq.a3'),
  },
  {
    qKey: 'landing.faq.q4',
    aRender: (t) => t('landing.faq.a4'),
  },
  {
    qKey: 'landing.faq.q5',
    aRender: (t) => t('landing.faq.a5'),
  },
  {
    qKey: 'landing.faq.q6',
    aRender: () => (
      <>
        <Trans i18nKey='landing.faq.a6.prefix' />
        <a
          href='https://qzcode.1iiu.com/'
          target='_blank'
          rel='noopener noreferrer'
          className='text-foreground/80 hover:text-foreground underline underline-offset-2'
        >
          qzcode.1iiu.com
        </a>
        <Trans i18nKey='landing.faq.a6.tail' />
      </>
    ),
  },
  {
    qKey: 'landing.faq.q7',
    aRender: (t) => t('landing.faq.a7'),
  },
]

export function FaqSection() {
  const { t } = useTranslation()

  return (
    <section className='relative border-t border-border/40 px-6 py-20 sm:py-24'>
      <div className='mx-auto max-w-3xl space-y-10'>
        <div className='space-y-2 text-center'>
          <p className='text-muted-foreground/60 text-xs font-semibold tracking-widest uppercase'>
            {t('landing.faq.eyebrow')}
          </p>
          <h2 className='text-foreground text-3xl font-bold tracking-tight sm:text-4xl'>
            {t('landing.faq.title')}
          </h2>
        </div>

        <Accordion className='w-full'>
          {FAQS.map((item, idx) => (
            <AccordionItem key={item.qKey} value={`faq-${idx}`}>
              <AccordionTrigger className='text-left text-base font-medium'>
                {t(item.qKey)}
              </AccordionTrigger>
              <AccordionContent className='text-muted-foreground text-sm leading-relaxed'>
                {item.aRender(t)}
              </AccordionContent>
            </AccordionItem>
          ))}
        </Accordion>
      </div>
    </section>
  )
}
