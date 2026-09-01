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
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { useChannelGroupOptions } from '../use-channel-group-options'

const getMock = vi.fn()

vi.mock('@/lib/api', () => ({
  api: {
    get: (...args: unknown[]) => getMock(...args),
  },
}))

// Production shape: /api/group/ returns every GroupRatio key, which mixes
// channel groups with user tiers and commercial identities.
const ALL_GROUP_NAMES = [
  'GPT-Pro-sale',
  'claude-max-sale',
  'aws-q',
  'free',
  'supporter',
  'fan',
  'bestie',
  'vip',
  'retail',
  'wholesale',
  'wholesale-plus',
]

const TIER_NAMES = ['supporter', 'fan', 'bestie', 'vip']
const COMMERCIAL_NAMES = ['retail', 'wholesale', 'wholesale-plus']

function buildWrapper(options: { withClassification: boolean }) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  if (options.withClassification) {
    qc.setQueryData(['system-options'], {
      success: true,
      message: '',
      data: [
        {
          key: 'tokensheep_economy.tier_thresholds',
          value: JSON.stringify(
            Object.fromEntries(TIER_NAMES.map((name) => [name, 1000]))
          ),
        },
        {
          key: 'tokensheep_economy.commercial_groups',
          value: JSON.stringify(
            Object.fromEntries(COMMERCIAL_NAMES.map((name) => [name, true]))
          ),
        },
      ],
    })
  }
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  }
}

describe('useChannelGroupOptions', () => {
  beforeEach(() => {
    getMock.mockReset()
    getMock.mockResolvedValue({
      data: { success: true, data: ALL_GROUP_NAMES },
    })
  })

  test('keeps channel groups and drops tiers and commercial identities', async () => {
    const { result } = renderHook(() => useChannelGroupOptions(), {
      wrapper: buildWrapper({ withClassification: true }),
    })

    await waitFor(() => expect(result.current.groups.length).toBeGreaterThan(0))

    expect(result.current.groups).toEqual([
      'GPT-Pro-sale',
      'claude-max-sale',
      'aws-q',
    ])
    // free is a tier even though it carries no threshold row, matching
    // groupKind() in controller/group.go.
    expect(result.current.groups).not.toContain('free')
    for (const hidden of [...TIER_NAMES, ...COMMERCIAL_NAMES]) {
      expect(result.current.groups).not.toContain(hidden)
    }
  })

  test('reads the channel-group endpoint, not the user-tier one', async () => {
    const { result } = renderHook(() => useChannelGroupOptions(), {
      wrapper: buildWrapper({ withClassification: true }),
    })

    await waitFor(() => expect(result.current.groups.length).toBeGreaterThan(0))

    expect(getMock).toHaveBeenCalledWith('/api/group/')
    expect(getMock).not.toHaveBeenCalledWith('/api/group/tiers')
  })

  test('degrades to the full list when classification data is unavailable', async () => {
    // Without the option payload the tier sets are empty, so every name
    // classifies as a channel group. Showing too much beats silently dropping
    // a group the operator needs to select.
    const { result } = renderHook(() => useChannelGroupOptions(), {
      wrapper: buildWrapper({ withClassification: false }),
    })

    await waitFor(() => expect(result.current.groups.length).toBeGreaterThan(0))

    expect(result.current.groups).toContain('GPT-Pro-sale')
    expect(result.current.groups).toContain('bestie')
  })
})
