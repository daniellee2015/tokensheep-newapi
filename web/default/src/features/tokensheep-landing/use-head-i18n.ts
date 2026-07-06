import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'

// Keeps <title>, meta title/description/keywords, and the og / twitter
// social preview tags in sync with the current i18n language.
//
// The initial values that ship in index.html are the Chinese variant
// (that's what search engine and social-card crawlers see, per our locale
// choice). When a real user switches language client-side, this hook
// rewrites the head so the browser tab, share-then-cache flows, and
// screen readers all pick up the localized copy.

function setMetaByName(name: string, value: string): void {
  const el = document.querySelector(`meta[name="${name}"]`)
  if (el) el.setAttribute('content', value)
}

function setMetaByProperty(prop: string, value: string): void {
  const el = document.querySelector(`meta[property="${prop}"]`)
  if (el) el.setAttribute('content', value)
}

export function useHeadI18n(): void {
  const { t, i18n } = useTranslation()

  useEffect(() => {
    const title = t('landing.head.title')
    const description = t('landing.head.description')
    const ogTitle = t('landing.head.ogTitle')
    const ogDescription = t('landing.head.ogDescription')

    document.title = title
    setMetaByName('title', title)
    setMetaByName('description', description)
    setMetaByName('twitter:title', ogTitle)
    setMetaByName('twitter:description', ogDescription)
    setMetaByProperty('og:title', ogTitle)
    setMetaByProperty('og:description', ogDescription)

    // html lang for a11y / SEO signal
    if (i18n.language) {
      document.documentElement.setAttribute(
        'lang',
        i18n.language.startsWith('zh') ? 'zh-CN' : 'en'
      )
    }

    // og:locale (main + alternate)
    const locale = i18n.language?.startsWith('zh') ? 'zh_CN' : 'en_US'
    const altLocale = locale === 'zh_CN' ? 'en_US' : 'zh_CN'
    setMetaByProperty('og:locale', locale)
    setMetaByProperty('og:locale:alternate', altLocale)
  }, [t, i18n.language])
}
