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

/**
 * Central brand logo primitive. Renders the configured wordmark logo at its
 * real aspect ratio (no forced square/circle) and swaps in the light-text
 * variant under dark mode when the src matches our packaged asset pair.
 *
 * Any custom (admin-configured, non-packaged) URL is rendered as-is with no
 * theme swap — that admin owns their own asset.
 */
const PACKAGED_LIGHT = '/tokensheep-logo.svg'
const PACKAGED_DARK = '/tokensheep-logo-dark.svg'

interface BrandLogoProps {
  src?: string
  alt?: string
  className?: string
  /** height class name (default 'h-9'). Width auto-scales. */
  heightClassName?: string
}

function resolveSrc(src: string | undefined): string {
  if (!src || src.length === 0) return PACKAGED_LIGHT
  return src
}

function isPackaged(src: string): boolean {
  try {
    return new URL(src, window.location.origin).pathname === PACKAGED_LIGHT
  } catch {
    return false
  }
}

export function BrandLogo({
  src,
  alt = 'Logo',
  className,
  heightClassName = 'h-9',
}: BrandLogoProps) {
  const resolved = resolveSrc(src)
  const base = `${heightClassName} w-auto object-contain`

  if (!isPackaged(resolved)) {
    return <img src={resolved} alt={alt} className={cn(base, className)} />
  }

  return (
    <>
      <img
        src={PACKAGED_LIGHT}
        alt={alt}
        className={cn(base, 'block dark:hidden', className)}
      />
      <img
        src={PACKAGED_DARK}
        alt={alt}
        className={cn(base, 'hidden dark:block', className)}
      />
    </>
  )
}
