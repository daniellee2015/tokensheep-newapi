/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { getSelf } from '@/lib/api'

import { TokensheepTierCards } from '../wallet/components/tokensheep-tier-cards'
import { useTopupInfo, useWaffoPancakePayment } from '../wallet/hooks'
import type { UserWalletData } from '../wallet/types'

type ContributionTier = 'supporter' | 'fan' | 'bestie'

export function Contribute() {
  const { t } = useTranslation()
  const [user, setUser] = useState<UserWalletData | null>(null)
  const [userLoading, setUserLoading] = useState(true)
  const [loadingTier, setLoadingTier] = useState<ContributionTier | null>(null)
  const { topupInfo, loading: topupLoading } = useTopupInfo()
  const { processWaffoPancakePayment } = useWaffoPancakePayment()

  const fetchUser = useCallback(async () => {
    try {
      setUserLoading(true)
      const response = await getSelf()
      if (response.success && response.data) {
        setUser(response.data as UserWalletData)
      }
    } finally {
      setUserLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchUser()
  }, [fetchUser])

  const handleSelectTier = async (amount: number, tier: ContributionTier) => {
    setLoadingTier(tier)
    try {
      const success = await processWaffoPancakePayment(amount)
      if (success) await fetchUser()
    } finally {
      setLoadingTier(null)
    }
  }

  const enabled = topupInfo?.enable_waffo_pancake_topup === true

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('wallet.tierCards.title')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='mx-auto w-full max-w-5xl'>
          <TokensheepTierCards
            enabled={enabled}
            currentTier={user?.group}
            onSelect={handleSelectTier}
            loadingTier={loadingTier}
            loading={topupLoading || userLoading}
          />
          {!enabled && !topupLoading && (
            <Alert>
              <AlertDescription>
                {t(
                  'No payment methods available. Please contact administrator.'
                )}
              </AlertDescription>
            </Alert>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
