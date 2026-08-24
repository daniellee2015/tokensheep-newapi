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
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { BrandLogo } from '@/components/brand-logo'
import { Skeleton } from '@/components/ui/skeleton'
import { useSystemConfig } from '@/hooks/use-system-config'

type AuthLayoutProps = {
  children: React.ReactNode
}

export function AuthLayout({ children }: AuthLayoutProps) {
  const { t } = useTranslation()
  const { systemName, logo, loading } = useSystemConfig()
  const hasName = (systemName ?? '').trim().length > 0

  return (
    <div className='relative grid h-svh max-w-none'>
      <div className='container flex items-center pt-8 sm:pt-0'>
        <div className='mx-auto flex w-full flex-col justify-center space-y-6 px-4 py-8 sm:w-[480px] sm:p-8'>
          <Link
            to='/'
            className='flex flex-col items-center gap-2 transition-opacity hover:opacity-80'
            aria-label={t('Go to home')}
          >
            {loading ? (
              <Skeleton className='h-11 w-40 rounded-md' />
            ) : (
              <BrandLogo src={logo} alt={t('Logo')} heightClassName='h-11' />
            )}
            {loading ? (
              <Skeleton className='h-6 w-32' />
            ) : (
              hasName && (
                <h1 className='text-lg font-medium tracking-tight'>
                  {systemName}
                </h1>
              )
            )}
          </Link>
          {children}
        </div>
      </div>
    </div>
  )
}
