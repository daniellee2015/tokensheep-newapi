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
import { useNavigate } from '@tanstack/react-router'
import i18n from 'i18next'

import {
  getSavedLanguage,
  sanitizeAuthRedirect,
} from '@/features/auth/lib/auth-redirect'
import { applyAuthBundle } from '@/lib/api'
import type { AuthBundle } from '@/stores/auth-store'

/**
 * Hook for handling authentication redirects and user data management
 */
export function useAuthRedirect() {
  const navigate = useNavigate()

  /**
   * Handle successful login
   * @param userData - Optional user data from login response
   * @param redirectTo - Redirect path after login
   */
  const handleLoginSuccess = async (
    bundle: AuthBundle,
    redirectTo?: string
  ) => {
    applyAuthBundle(bundle)
    // History tokensheep users have "zh" saved in their profile from when we
    // only supported en/zh. Upstream's new locale set uses camelCase codes
    // (`zhCN` / `zhTW`), and calling i18n.changeLanguage() with an unsupported
    // value under `load: 'currentOnly'` leaves the promise hanging on a
    // missing bundle — which stalls the post-login navigate() below and looks
    // to the user like the login button did nothing. Guard the switch on the
    // configured supportedLngs so an unknown code is a silent no-op, and
    // isolate any remaining rejection so navigation always runs.
    const savedLang = getSavedLanguage(bundle.user)
    const supportedList = i18n.options?.supportedLngs
    const supported =
      Array.isArray(supportedList) && supportedList.length > 0
        ? new Set(supportedList.map((code) => String(code)))
        : null
    if (
      savedLang &&
      savedLang !== i18n.language &&
      (!supported || supported.has(savedLang))
    ) {
      try {
        await i18n.changeLanguage(savedLang)
      } catch {
        // Never let a language-load failure block the redirect.
      }
    }

    const targetPath =
      sanitizeAuthRedirect(redirectTo, window.location.origin) ?? '/dashboard'
    // Delay to next microtask so zustand's setBundle propagates to any React
    // subtree subscribed via useAuthStore before router re-evaluates route
    // matches. Without this, TanStack Router occasionally still sees the
    // pre-login auth snapshot, treats /dashboard as unauthenticated, and
    // silently drops the navigation — leaving the user on /sign-in until they
    // hard-refresh the page.
    await Promise.resolve()
    await navigate({ to: targetPath, replace: true, reloadDocument: false })
  }

  /**
   * Redirect to 2FA page
   */
  const redirectTo2FA = () => {
    navigate({ to: '/otp', replace: true })
  }

  /**
   * Redirect to login page
   */
  const redirectToLogin = () => {
    navigate({ to: '/sign-in', replace: true })
  }

  /**
   * Redirect to register page
   */
  const redirectToRegister = () => {
    navigate({ to: '/sign-up', replace: true })
  }

  return {
    handleLoginSuccess,
    redirectTo2FA,
    redirectToLogin,
    redirectToRegister,
  }
}
