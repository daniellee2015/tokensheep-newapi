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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import { GroupRatioVisualEditor } from '../group-ratio-visual-editor'

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
          vip: 10_000,
        }),
      },
      {
        key: 'tokensheep_economy.commercial_groups',
        value: JSON.stringify({
          retail: true,
          wholesale: true,
          'wholesale-plus': true,
        }),
      },
    ],
  })
  return qc
}

// A fixture shaped like production: channel groups mixed into the same flat map
// as user tiers and commercial identities.
const GROUP_RATIO = JSON.stringify({
  'GPT-Pro-sale': 1,
  'claude-max-sale': 1,
  free: 1,
  supporter: 1,
  fan: 1,
  bestie: 1,
  vip: 1,
  retail: 1,
  wholesale: 1,
  'wholesale-plus': 1,
})

function renderEditor(onChange = vi.fn()) {
  render(
    <QueryClientProvider client={buildQueryClient()}>
      <GroupRatioVisualEditor
        groupRatio={GROUP_RATIO}
        topupGroupRatio='{}'
        userUsableGroups={JSON.stringify({ 'GPT-Pro-sale': 'Value first' })}
        groupGroupRatio='{}'
        autoGroups='[]'
        maxTokenAutoGroupsField={<div data-testid='max-token-slot' />}
        groupSpecialUsableGroup='{}'
        onChange={onChange}
      />
    </QueryClientProvider>
  )
  return onChange
}

/** Row names render as input values, so read them off the DOM that way. */
function visibleGroupNames(): string[] {
  return [...document.querySelectorAll('input')]
    .map((input) => (input as HTMLInputElement).value)
    .filter((value) => GROUP_RATIO.includes(`"${value}"`))
}

describe('group pricing table hides non-channel groups', () => {
  test('channel groups are listed', () => {
    renderEditor()

    const names = visibleGroupNames()
    expect(names).toContain('GPT-Pro-sale')
    expect(names).toContain('claude-max-sale')
  })

  test('user tiers and commercial groups are not listed', () => {
    renderEditor()

    const names = visibleGroupNames()
    // Group pricing is about what a channel group costs; a tier's price lives
    // in GroupGroupRatio as a (tier x channel-group) pair, so a tier row here
    // is always 1 and only adds noise.
    for (const hidden of [
      'free',
      'supporter',
      'fan',
      'bestie',
      'vip',
      'retail',
      'wholesale',
      'wholesale-plus',
    ]) {
      expect(names).not.toContain(hidden)
    }
  })
})

describe('hidden entries survive a save', () => {
  test('editing a channel group preserves every tier entry in GroupRatio', async () => {
    const user = userEvent.setup()
    const onChange = renderEditor()

    // Touch a visible row so the editor serializes and emits.
    const proRow = [...document.querySelectorAll('input')].find(
      (input) => (input as HTMLInputElement).value === 'GPT-Pro-sale'
    )
    expect(proRow).toBeDefined()
    await user.type(proRow as HTMLInputElement, '-x')

    const groupRatioCalls = onChange.mock.calls.filter(
      ([field]) => field === 'GroupRatio'
    )
    // Guard against a vacuous pass: if the edit never emitted, the assertions
    // below would never run.
    const lastCall = groupRatioCalls.at(-1)
    expect(lastCall).toBeDefined()

    const written = JSON.parse(lastCall?.[1] as string)

    // A group missing from GroupRatio is rejected as deprecated in
    // middleware/auth.go, so dropping these on save would 403 every token
    // whose group is a tier name.
    for (const tier of [
      'free',
      'supporter',
      'fan',
      'bestie',
      'vip',
      'retail',
      'wholesale',
      'wholesale-plus',
    ]) {
      expect(written).toHaveProperty(tier, 1)
    }
    // The untouched channel group is still there too.
    expect(written).toHaveProperty('claude-max-sale', 1)
  })

  test('adding a group never reuses a hidden tier name', async () => {
    const user = userEvent.setup()
    const onChange = renderEditor()

    const addButton = screen.getByRole('button', { name: /add group/i })
    await user.click(addButton)

    const groupRatioCalls = onChange.mock.calls.filter(
      ([field]) => field === 'GroupRatio'
    )
    const lastCall = groupRatioCalls.at(-1)
    expect(lastCall).toBeDefined()

    const written = JSON.parse(lastCall?.[1] as string)
    // Every tier keeps its original ratio: the generated name must not have
    // landed on one of them.
    for (const tier of ['free', 'bestie', 'wholesale-plus']) {
      expect(written).toHaveProperty(tier, 1)
    }
  })
})
