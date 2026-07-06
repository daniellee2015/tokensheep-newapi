import { PublicLayout } from '@/components/layout'
import { Footer } from '@/components/layout/components/footer'
import { useAuthStore } from '@/stores/auth-store'

import { FaqSection } from './sections/faq-section'
import { FeaturesSection } from './sections/features-section'
import { HeroSection } from './sections/hero-section'
import { PricingCardsSection } from './sections/pricing-cards-section'
import { StorySection } from './sections/story-section'
import { TierSection } from './sections/tier-section'
import { useHeadI18n } from './use-head-i18n'

export function TokenSheepLanding() {
  const { auth } = useAuthStore()
  const isAuthenticated = !!auth.user
  useHeadI18n()

  return (
    <PublicLayout showMainContainer={false}>
      <main className='min-h-screen'>
        <HeroSection isAuthenticated={isAuthenticated} />
        <StorySection />
        <FeaturesSection />
        <PricingCardsSection />
        <TierSection />
        <FaqSection />
      </main>
      <Footer />
    </PublicLayout>
  )
}
