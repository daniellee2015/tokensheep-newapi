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
import { render, screen } from '@testing-library/react'
import i18next from 'i18next'
import { beforeAll, describe, expect, test } from 'vitest'

import { GroupSpecialUsableRulesEditor } from '../group-special-usable-editor'

// R19-C: the GroupSection header renders each user-group name as the section
// title. That title is user-visible and must go through formatGroupName so
// tier-level keys like `claude-max` show localized copy (e.g. "Claude-Max
// Flagship") instead of the raw key. Prime the global i18next test instance
// (see test-setup.ts) with the tier.name.* keys we need.
beforeAll(() => {
  i18next.addResourceBundle('en', 'translation', {
    'tier.name.claude-max': 'Claude-Max Flagship',
    'tier.name.bestie': 'Bestie',
    'Not in pricing table': 'Not in pricing table',
    rules: 'rules',
    'Extra visible': 'Extra visible',
    Hidden: 'Hidden',
    'Group name': 'Group name',
    Description: 'Description',
    'Special usable group rules': 'Special usable group rules',
    'Make extra groups visible to, or hide default groups from, users of a specific group.':
      'Make extra groups visible to, or hide default groups from, users of a specific group.',
    'Add rules for a user group': 'Add rules for a user group',
    'No rules yet. Add a group below to get started.':
      'No rules yet. Add a group below to get started.',
  })
})

describe('GroupSpecialUsableRulesEditor — 分组头显示走 formatGroupName', () => {
  test('claude-max user group 显示成 tier.name.claude-max 翻译', () => {
    // Fixture: 单条规则挂在 claude-max user group 下面。若头部渲染 props.groupName
    // 原样 (bug #6), 我们看到 "claude-max"; 走 formatGroupName 后应看到 "Claude-Max Flagship"。
    const value = JSON.stringify({
      'claude-max': { '+:default': 'welcome' },
    })
    render(
      <GroupSpecialUsableRulesEditor
        value={value}
        groupOptions={['claude-max', 'default']}
        onChange={() => undefined}
      />
    )
    expect(screen.getByText('Claude-Max Flagship')).toBeInTheDocument()
    expect(screen.queryByText('claude-max')).toBeNull()
  })

  test('普通 user group (bestie) 也走翻译, 未知 group 仍显示原 key', () => {
    // bestie 有 tier.name 兜底 → 翻译; unknown-family 无翻译 → 保留原样 (helper 走 (d) fall-through)
    const value = JSON.stringify({
      bestie: { '+:default': 'bestie perks' },
      'unknown-family': { '+:default': 'no lookup' },
    })
    render(
      <GroupSpecialUsableRulesEditor
        value={value}
        groupOptions={['bestie', 'unknown-family', 'default']}
        onChange={() => undefined}
      />
    )
    expect(screen.getByText('Bestie')).toBeInTheDocument()
    // unknown-family 没有 tier.name.* 也没有 known suffix → formatGroupName fall through
    expect(screen.getByText('unknown-family')).toBeInTheDocument()
  })
})
