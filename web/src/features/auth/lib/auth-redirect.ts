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
import type { AuthUser } from '@/stores/auth-store'

const allowedRedirectProtocols = new Set(['http:', 'https:'])

export function getSavedLanguage(user: AuthUser): string | undefined {
  if (typeof user.language === 'string') {
    return user.language
  }

  if (user.setting && typeof user.setting === 'object') {
    return typeof user.setting.language === 'string'
      ? user.setting.language
      : undefined
  }

  if (typeof user.setting !== 'string') {
    return undefined
  }

  try {
    const setting = JSON.parse(user.setting) as { language?: unknown }
    return typeof setting.language === 'string' ? setting.language : undefined
  } catch {
    return undefined
  }
}

// Paths that must not be treated as a valid post-login destination. tokensheep
// has a custom landing page at `/`, so an unauthenticated user who clicks the
// header "Sign in" link arrives at /sign-in?redirect=/. If we honor that
// redirect, login sends the user back to the landing page — the URL barely
// changes and no dashboard chrome renders, which reads as "login didn't work".
// The call sites use `sanitizeAuthRedirect(...) ?? '/dashboard'`, so returning
// null for these paths falls through to the intended destination.
const REJECTED_REDIRECT_PATHS = new Set([
  '/',
  '/sign-in',
  '/sign-up',
  '/otp',
])

export function sanitizeAuthRedirect(
  value: unknown,
  origin: string
): string | null {
  if (typeof value !== 'string') return null

  const target = value.trim()
  if (!target || target.includes('\\') || target.startsWith('//')) return null

  let trustedOrigin: URL
  try {
    trustedOrigin = new URL(origin)
  } catch {
    return null
  }
  if (!allowedRedirectProtocols.has(trustedOrigin.protocol)) return null

  let redirectURL: URL
  try {
    redirectURL = target.startsWith('/')
      ? new URL(target, trustedOrigin.origin)
      : new URL(target)
  } catch {
    return null
  }

  if (
    !allowedRedirectProtocols.has(redirectURL.protocol) ||
    redirectURL.origin !== trustedOrigin.origin
  ) {
    return null
  }

  const normalizedPath = redirectURL.pathname.replace(/\/+$/, '') || '/'
  if (REJECTED_REDIRECT_PATHS.has(normalizedPath)) {
    return null
  }

  return `${redirectURL.pathname}${redirectURL.search}${redirectURL.hash}`
}
