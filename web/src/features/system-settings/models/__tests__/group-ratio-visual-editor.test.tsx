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
import i18next from 'i18next'
import type { ReactNode } from 'react'
import { beforeAll, describe, expect, test } from 'vitest'

import { GroupRatioVisualEditor } from '../group-ratio-visual-editor'

// The pricing table classifies each group name via the shared /api/option/
// payload. Prime that cache so it can tell a user tier from a channel group
// without a network call; keys match setting/tokensheep_setting/economy.go.
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

function renderEditor(ui: ReactNode) {
  return render(
    <QueryClientProvider client={buildQueryClient()}>{ui}</QueryClientProvider>
  )
}

// R19-C: the auto-assignment order card renders each queued group name as a
// chip. That chip is a user-visible label and must go through formatGroupName
// so tier-level keys like `bestie` and `aws-q` show localized copy instead of
// raw keys.
beforeAll(() => {
  i18next.addResourceBundle('en', 'translation', {
    'tier.name.bestie': 'Bestie',
    'tier.name.aws-q': 'AWS-Q Bulk',
    'Not in pricing table': 'Not in pricing table',
    'Auto assignment order': 'Auto assignment order',
    'Priority order for tokens in the auto group. The system tries groups from top to bottom.':
      'Priority order for tokens in the auto group. The system tries groups from top to bottom.',
    'Add group': 'Add group',
    'Pricing groups': 'Pricing groups',
    'All group names live here. Ratio applies when calls are billed as this group; top-up ratio applies to users whose account is in this group.':
      'All group names live here. Ratio applies when calls are billed as this group; top-up ratio applies to users whose account is in this group.',
    'No groups yet. Add a group to get started.':
      'No groups yet. Add a group to get started.',
    'Special ratio rules': 'Special ratio rules',
    'Each rule reads as a sentence: users of one group pay a special ratio when billed as another group. Without a rule, the billing group base ratio applies.':
      'Each rule reads as a sentence: users of one group pay a special ratio when billed as another group. Without a rule, the billing group base ratio applies.',
    'Add user group': 'Add user group',
    'Group name': 'Group name',
    Ratio: 'Ratio',
    'Top-up ratio': 'Top-up ratio',
    'User selectable': 'User selectable',
    Description: 'Description',
    Actions: 'Actions',
    Details: 'Details',
    Delete: 'Delete',
    'Not set': 'Not set',
    'Group description': 'Group description',
  })
})

describe('GroupRatioVisualEditor — Auto 分组 chip 走 formatGroupName', () => {
  test('bestie / aws-q chip 显示 tier.name.* 翻译, 未知 key 保持原样', () => {
    // Fixture: autoGroups 里放两个有 tier.name.* 兜底的 key + 一个未知 key
    // ("stray-random"). 前两个应翻译, 第三个 fall-through 到原 key。
    const groupRatio = JSON.stringify({
      bestie: 1.0,
      'aws-q': 0.5,
      'stray-random': 1.0,
    })
    const autoGroups = JSON.stringify(['bestie', 'aws-q', 'stray-random'])

    renderEditor(
      <GroupRatioVisualEditor
        groupRatio={groupRatio}
        topupGroupRatio='{}'
        userUsableGroups='{}'
        groupGroupRatio='{}'
        autoGroups={autoGroups}
        maxTokenAutoGroupsField={<div data-testid='max-token-slot' />}
        groupSpecialUsableGroup='{}'
        onChange={() => undefined}
      />
    )

    // The pricing table renders row names inside <Input value="bestie" />
    // which is an attribute, not textContent, so getByText will only match
    // the chip <span>s in the auto-assignment card. No scoping needed.
    expect(screen.getByText('Bestie')).toBeInTheDocument()
    expect(screen.getByText('AWS-Q Bulk')).toBeInTheDocument()
    // Unknown key falls through to raw
    expect(screen.getByText('stray-random')).toBeInTheDocument()
  })
})
