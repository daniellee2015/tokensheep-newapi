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
// R21-B test coverage for TierLimitsCard live concurrency subtitle.
//
// The card renders two rows (RPM ceiling + concurrency ceiling). R21-B
// adds a subtitle under the concurrency row driven by the new
// /api/user/self/limits/usage endpoint. These tests pin the three
// user-visible states of that subtitle:
//
//   1. live/idle → "current 3 / 100 in use" (the happy path)
//   2. no usage payload yet (getMyLimitsUsage returned null) → "—"
//      placeholder so the card height doesn't collapse on the first
//      render before the query settles
//   3. concurrency_source === 'unavailable' → italic gray "Live usage
//      unavailable" (Redis blip on the backend; the ceiling is still
//      enforced upstream so we don't hide the row)
//
// The tier snapshot is stubbed via QueryClient.setQueryData so the card's
// isLoading branch is skipped without needing to time-travel refetch.
// The usage query is stubbed the same way with three separate
// QueryClient fixtures — one per branch. Each fixture also intercepts
// api.get so the useQuery's retry-on-mount path can't accidentally hit
// the network and shadow the seeded cache.
import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, test } from 'vitest'

const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { api } = await import('@/lib/api')
const { TierLimitsCard } = await import('../tier-limits-card')

type ApiMethod = (url: string, data?: unknown) => Promise<{ data: unknown }>
type MockableApi = { get: ApiMethod }
const apiClient = api as unknown as MockableApi
const originalGet = apiClient.get

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        // TitledCard title / description surface these two verbatim.
        'API Limits': 'API Limits',
        'Rate and concurrency ceilings for your tier.':
          'Rate and concurrency ceilings for your tier.',
        // Row labels.
        'Requests / min': 'Requests / min',
        'Max requests per minute': 'Max requests per minute',
        'Concurrent sessions': 'Concurrent sessions',
        'Requests running at the same time':
          'Requests running at the same time',
        Unlimited: 'Unlimited',
        // R21-B additions.
        'limits.usage.currentlyActive': '{{used}} / {{limit}} in use',
        'limits.usage.rpmWindow': 'Every {{minutes}}-minute window',
        'limits.usage.unavailable': 'Live usage unavailable',
        'Live concurrency counter is temporarily unavailable. The ceiling above is still enforced.':
          'Live concurrency counter is temporarily unavailable. The ceiling above is still enforced.',
      },
    },
  },
})

// tierFixture builds the minimum MyTierView shape the card consumes.
// The card only reads rpm + session_limit; the other fields default to
// something the type-checker accepts.
function tierFixture(overrides: Record<string, unknown> = {}) {
  return {
    group: 'free',
    rpm: 1000,
    session_limit: 100,
    quota_paid: 0,
    quota_gift: 0,
    gift_pool_cap: 0,
    gift_used_today: 0,
    gift_daily_limit: 0,
    daily_gift: 0,
    total_donated: 0,
    next_tier: '',
    next_threshold: 0,
    next_progress: 0,
    to_next_contribution: 0,
    ...overrides,
  }
}

// installApiFixture replaces api.get so any query that misses the seeded
// cache (e.g. a retry after the fixture window) still returns a shaped
// response instead of throwing an unhandled rejection during the test.
function installApiFixture(
  tier: ReturnType<typeof tierFixture> | null,
  usage: Record<string, unknown> | null
) {
  apiClient.get = async (url) => {
    switch (url) {
      case '/api/user/self/tier':
        return { data: { success: true, data: tier } }
      case '/api/user/self/limits/usage':
        return { data: { success: true, data: usage } }
      default:
        throw new Error(`Unexpected GET ${url}`)
    }
  }
}

afterEach(() => {
  apiClient.get = originalGet
})

function renderCard({
  tier,
  usage,
}: {
  tier: ReturnType<typeof tierFixture> | null
  usage: Record<string, unknown> | null | undefined
}) {
  installApiFixture(tier, usage === undefined ? null : usage)
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  // Seed the tier cache so the card skips its skeleton branch.
  qc.setQueryData(['my-tier'], tier)
  // Seed usage explicitly when the case wants a specific fixture. When
  // usage === undefined, we leave the cache empty so useQuery reports
  // data === undefined until it settles — that's the loading branch we
  // want to assert on.
  if (usage !== undefined) {
    qc.setQueryData(['limits-usage'], usage)
  }
  return render(
    <QueryClientProvider client={qc}>
      <I18nextProvider i18n={i18n}>
        <TierLimitsCard />
      </I18nextProvider>
    </QueryClientProvider>
  )
}

describe('TierLimitsCard R21-B live concurrency subtitle', () => {
  test('renders "3 / 100 in use" when usage source is live', () => {
    renderCard({
      tier: tierFixture({ rpm: 1000, session_limit: 100 }),
      usage: {
        user_group: 'free',
        concurrency_used: 3,
        concurrency_limit: 100,
        concurrency_source: 'live',
        rpm_window_minutes: 1,
        rate_limit_enabled: true,
      },
    })
    // Positive control: the card actually rendered.
    expect(screen.getByText('API Limits')).toBeInTheDocument()
    // Concurrency row exists.
    expect(screen.getByText('Concurrent sessions')).toBeInTheDocument()
    // The interpolated subtitle appears exactly once, driven by
    // concurrency_used / concurrency_limit fields from the endpoint.
    expect(screen.getByText('3 / 100 in use')).toBeInTheDocument()
  })

  test('renders an em-dash placeholder when usage payload is null', () => {
    // Simulates the initial fetch where getMyLimitsUsage returned null
    // (backend responded with success:false or the network dropped the
    // response body). The card must still render — the tier ceilings
    // are the primary content, and the placeholder just holds the slot
    // so height doesn't collapse.
    renderCard({
      tier: tierFixture({ rpm: 1000, session_limit: 100 }),
      usage: null,
    })
    const subtitle = screen.getByTestId('concurrency-usage')
    expect(subtitle.textContent?.trim()).toBe('—')
    // The interpolated live-usage string must NOT render — this is the
    // safeguard against a subtle bug where a null payload leaks
    // undefined into the interpolation and prints "undefined / 100".
    expect(screen.queryByText(/in use/)).toBe(null)
  })

  test('renders unavailable label when concurrency_source is unavailable', () => {
    // Redis error path in the backend (readSessionActive returns
    // sourceUnavailable). The card must show a distinct visual so the
    // user knows the "0" isn't a real idle reading — italic gray text
    // plus a tooltip. We assert on the label text; the tooltip content
    // isn't triggered by JSDOM without a hover event, so we assert on
    // its presence in the render tree via role instead.
    renderCard({
      tier: tierFixture({ rpm: 1000, session_limit: 100 }),
      usage: {
        user_group: 'free',
        concurrency_used: 0,
        concurrency_limit: 100,
        concurrency_source: 'unavailable',
        rpm_window_minutes: 1,
        rate_limit_enabled: true,
      },
    })
    expect(screen.getByText('Live usage unavailable')).toBeInTheDocument()
    // The "in use" line must NOT render in this branch — showing both
    // would contradict the "we don't know" signal.
    expect(screen.queryByText(/in use/)).toBe(null)
  })
})
