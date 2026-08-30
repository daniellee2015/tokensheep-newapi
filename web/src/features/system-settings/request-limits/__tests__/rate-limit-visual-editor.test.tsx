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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import i18next from 'i18next'
import { beforeAll, describe, expect, test } from 'vitest'

import { TooltipProvider } from '@/components/ui/tooltip'

import { RateLimitVisualEditor } from '../rate-limit-visual-editor'

// R17-C: minimal i18n bundle keyed on the strings the editor renders. All
// omitted keys fall back to the raw key, which is fine for assertion.
// Register on the global i18next instance used by test-setup.ts so the
// editor's useTranslation() picks these values up.
beforeAll(() => {
  i18next.addResourceBundle('en', 'translation', {
    'rateLimit.section.userTier': 'User tier RPM',
    'rateLimit.section.userTier.help': 'Rate limits keyed by user identity',
    'rateLimit.section.userTier.empty': 'No user-tier rate limits',
    'rateLimit.section.channel': 'Channel group RPM',
    'rateLimit.section.channel.help':
      'Second-wall throttle on upstream capacity',
    'rateLimit.section.channel.empty': 'No channel-group rate limits',
    'keys.groupKind.commercial': 'commercial',
    'Group Name': 'Group Name',
    'Max Requests (incl. failures)': 'Max Requests',
    'Max Success': 'Max Success',
    Unlimited: 'Unlimited',
    Actions: 'Actions',
    Edit: 'Edit',
    Delete: 'Delete',
    'Open menu': 'Open menu',
    'Add group': 'Add group',
    'Search group names...': 'Search',
  })
})

// Prime the /api/option/ query cache with tier/commercial fixtures so the
// editor can classify without making a network call. Keys match the
// backend option names (setting/tokensheep_setting/economy.go).
function buildQueryClient() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  qc.setQueryData(['system-options'], {
    success: true,
    message: '',
    data: [
      {
        key: 'tokensheep_economy.tier_thresholds',
        value: JSON.stringify({
          supporter: 1000,
          fan: 2500,
          bestie: 5000,
          vip: 10000,
        }),
      },
      {
        key: 'tokensheep_economy.commercial_groups',
        value: JSON.stringify({
          retail: true,
          wholesale: true,
        }),
      },
    ],
  })
  return qc
}

// TrackHarness swallows the child's parent-controlled `value` prop through
// a captor so we can assert what the editor writes back on save/delete
// without threading it through React state.
function EditorHarness({
  initial,
  onChangeSpy,
}: {
  initial: string
  onChangeSpy: (value: string) => void
}) {
  return (
    <TooltipProvider>
      <QueryClientProvider client={buildQueryClient()}>
        <RateLimitVisualEditor value={initial} onChange={onChangeSpy} />
      </QueryClientProvider>
    </TooltipProvider>
  )
}

describe('RateLimitVisualEditor', () => {
  test('splits rows into user-tier and channel sections', () => {
    // Fixture covers all three kinds: tier (bestie), commercial (retail),
    // channel (GPT-Pro, unknown-name).
    const initial = JSON.stringify({
      bestie: [0, 5000],
      retail: [200, 200],
      'GPT-Pro': [60, 60],
      'unknown-name': [1, 1],
    })
    render(<EditorHarness initial={initial} onChangeSpy={() => undefined} />)

    const userSection = screen.getByRole('region', { name: 'User tier RPM' })
    const channelSection = screen.getByRole('region', {
      name: 'Channel group RPM',
    })

    // bestie + retail land in the user section.
    expect(within(userSection).getByText('bestie')).toBeInTheDocument()
    expect(within(userSection).getByText('retail')).toBeInTheDocument()
    // GPT-Pro + unknown-name (fallback) land in the channel section.
    expect(within(channelSection).getByText('GPT-Pro')).toBeInTheDocument()
    expect(within(channelSection).getByText('unknown-name')).toBeInTheDocument()

    // Kind badges: commercial gets a visible label; tier and channel
    // stay unlabelled (only commercial is worth the visual noise).
    const badges = screen.getAllByTestId('rate-limit-kind-badge')
    expect(badges).toHaveLength(1)
    expect(badges[0]).toHaveAttribute('data-kind', 'commercial')
    expect(badges[0]).toHaveTextContent('commercial')

    // Cross-section leakage guard.
    expect(within(userSection).queryByText('GPT-Pro')).toBeNull()
    expect(within(channelSection).queryByText('bestie')).toBeNull()
  })

  test('unknown groups fall back to channel when classification data is missing', () => {
    // Fresh QueryClient with no cached options → both sets are empty.
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    const initial = JSON.stringify({
      free: [10, 10],
      'kirobus-api': [30, 30],
    })
    render(
      <TooltipProvider>
        <QueryClientProvider client={qc}>
          <RateLimitVisualEditor value={initial} onChange={() => undefined} />
        </QueryClientProvider>
      </TooltipProvider>
    )

    // Free is always a tier (fallback rule), even without a threshold row.
    const userSection = screen.getByRole('region', { name: 'User tier RPM' })
    expect(within(userSection).getByText('free')).toBeInTheDocument()

    // Everything unrecognised → channel.
    const channelSection = screen.getByRole('region', {
      name: 'Channel group RPM',
    })
    expect(within(channelSection).getByText('kirobus-api')).toBeInTheDocument()
  })

  test('renders empty state text per section when there are no matching rows', () => {
    render(
      <EditorHarness
        initial={JSON.stringify({ bestie: [0, 5000] })}
        onChangeSpy={() => undefined}
      />
    )

    // Only bestie exists → channel section shows its empty copy.
    expect(screen.getByText('No channel-group rate limits')).toBeInTheDocument()
    // User section is populated with bestie, so its empty copy is absent.
    expect(screen.queryByText('No user-tier rate limits')).toBeNull()
  })

  test('drops empty maps in both sections when the map is empty', () => {
    render(<EditorHarness initial='' onChangeSpy={() => undefined} />)

    expect(screen.getByText('No user-tier rate limits')).toBeInTheDocument()
    expect(screen.getByText('No channel-group rate limits')).toBeInTheDocument()
  })

  test('deleting a row from either section preserves rows in the other', async () => {
    // The visual editor stores everything in a single JSON blob. This
    // test pins the invariant "the visual split is UI-only" — deleting a
    // channel-side entry must not drop tier-side entries and vice versa,
    // otherwise a well-meaning admin edit could silently unset half the
    // RPM map (see task R17-C: "保存后 map 完整 - 不能丢 key").
    let latest: string | null = null
    const initial = JSON.stringify({
      bestie: [0, 5000],
      retail: [200, 200],
      'GPT-Pro': [60, 60],
    })

    render(
      <EditorHarness
        initial={initial}
        onChangeSpy={(v) => {
          latest = v
        }}
      />
    )

    // Delete GPT-Pro (channel section) via the row's action menu.
    const channelSection = screen.getByRole('region', {
      name: 'Channel group RPM',
    })
    const gptRow = within(channelSection)
      .getByText('GPT-Pro')
      .closest('tr') as HTMLElement
    expect(gptRow).not.toBeNull()

    const menuButton = within(gptRow).getByRole('button', { name: 'Open menu' })
    menuButton.click()

    const deleteItem = await screen.findByRole('menuitem', { name: 'Delete' })
    deleteItem.click()

    expect(latest).not.toBeNull()
    const parsed = JSON.parse(latest as unknown as string) as Record<
      string,
      unknown
    >
    // Deleted key is gone; the other two — one tier, one commercial —
    // survive with their exact values.
    expect(parsed).not.toHaveProperty('GPT-Pro')
    expect(parsed.bestie).toEqual([0, 5000])
    expect(parsed.retail).toEqual([200, 200])
    expect(Object.keys(parsed).sort()).toEqual(['bestie', 'retail'])
  })
})
