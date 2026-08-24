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
import { cn } from '@/lib/utils'

interface HeaderLogoProps {
  src: string
  alt?: string
  loading: boolean
  logoLoaded: boolean
  className?: string
}

/**
 * Header wordmark logo. Renders a horizontally sized brand asset — height
 * is fixed and width is intrinsic ('w-auto object-contain') so wide
 * wordmark SVGs render at their real aspect ratio instead of being clipped
 * into a circular badge. Under dark mode the light-text wordmark is
 * swapped in for the packaged tokensheep-logo.svg pair. Any other src
 * (admin-configured external URL) is rendered as-is.
 */
const PACKAGED_LIGHT = '/tokensheep-logo.svg'
const PACKAGED_DARK = '/tokensheep-logo-dark.svg'

export function HeaderLogo({
  src,
  alt = 'logo',
  loading,
  logoLoaded,
  className,
}: HeaderLogoProps) {
  const isPackaged = (() => {
    try {
      return new URL(src, window.location.origin).pathname === PACKAGED_LIGHT
    } catch {
      return false
    }
  })()
  const opacity = !loading && logoLoaded ? 'opacity-100' : 'opacity-0'
  const base = 'h-9 w-auto object-contain transition-opacity duration-200'

  if (!isPackaged) {
    return (
      <img
        src={src}
        alt={alt}
        className={cn(base, opacity, className)}
      />
    )
  }

  return (
    <>
      <img
        src={PACKAGED_LIGHT}
        alt={alt}
        className={cn(base, opacity, 'block dark:hidden', className)}
      />
      <img
        src={PACKAGED_DARK}
        alt={alt}
        className={cn(base, opacity, 'hidden dark:block', className)}
      />
    </>
  )
}
