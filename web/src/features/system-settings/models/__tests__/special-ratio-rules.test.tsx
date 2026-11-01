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
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, test, vi } from 'vitest'

import { GroupRatioVisualEditor } from '../group-ratio-visual-editor'

function renderRules(
  initialRules = '{}',
  groupRatio = '{"vip":1,"retail":1,"gemini-ultra":2}'
) {
  const onChange = vi.fn()
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['system-options'], {
    success: true,
    message: '',
    data: [
      { key: 'tokensheep_economy.tier_thresholds', value: '{"vip":10000}' },
      { key: 'tokensheep_economy.commercial_groups', value: '{"retail":true}' },
    ],
  })
  function Editor() {
    const [rules, setRules] = useState(initialRules)
    return (
      <GroupRatioVisualEditor
        groupRatio={groupRatio}
        topupGroupRatio='{}'
        userUsableGroups='{}'
        groupGroupRatio={rules}
        autoGroups='[]'
        maxTokenAutoGroupsField={null}
        groupSpecialUsableGroup='{}'
        onChange={(field, value) => {
          onChange(field, value)
          if (field === 'GroupGroupRatio') setRules(value)
        }}
      />
    )
  }
  render(
    <QueryClientProvider client={client}>
      <Editor />
    </QueryClientProvider>
  )
  return onChange
}

describe('special ratio user group creation', () => {
  test('accepts a custom user identity while warning for a missing billing target', async () => {
    const user = userEvent.setup()
    renderRules('{"wholesale-ultra":{"missing-billing-group":0.35}}')
    expect(
      screen.queryByLabelText('Not in pricing table')
    ).not.toBeInTheDocument()
    await user.click(
      screen.getByRole('button', {
        name: 'Special ratio rules wholesale-ultra',
      })
    )
    expect(screen.getByLabelText('Not in pricing table')).toBeInTheDocument()
  })

  test('allows adding a custom user group absent from the pricing registry', async () => {
    const user = userEvent.setup()
    const onChange = renderRules('{"vip":{"gemini-ultra":1.5}}')
    await user.click(screen.getByRole('button', { name: 'Add user group' }))
    const dialog = within(
      screen.getByRole('dialog', { name: 'Add user group' })
    )
    const add = dialog.getByRole('button', { name: 'Add' })
    const name = dialog.getByRole('combobox', { name: 'User group name' })
    await user.type(name, 'customer-alice')
    expect(name).toHaveValue('customer-alice')
    await user.click(add)
    expect(onChange).toHaveBeenLastCalledWith(
      'GroupGroupRatio',
      JSON.stringify(
        { vip: { 'gemini-ultra': 1.5 }, 'customer-alice': {} },
        null,
        2
      )
    )
    expect(screen.getByText('customer-alice')).toBeInTheDocument()
  })

  test('keeps existing groups selectable with the keyboard', async () => {
    const user = userEvent.setup()
    const onChange = renderRules()
    await user.click(screen.getByRole('button', { name: 'Add user group' }))
    const dialog = within(
      screen.getByRole('dialog', { name: 'Add user group' })
    )
    const input = dialog.getByRole('combobox', { name: 'User group name' })
    await user.type(input, 'ret')
    expect(input).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByRole('option', { name: 'retail' })).toBeInTheDocument()
    await user.keyboard('{ArrowDown}{Enter}')
    expect(input).toHaveValue('retail')
    await user.click(dialog.getByRole('button', { name: 'Add' }))
    expect(onChange).toHaveBeenLastCalledWith(
      'GroupGroupRatio',
      JSON.stringify({ retail: {} }, null, 2)
    )
  })

  test('rejects whitespace and trims a new name when submitted with Enter without any registry', async () => {
    const user = userEvent.setup()
    const onChange = renderRules('{}', '{}')
    await user.click(screen.getByRole('button', { name: 'Add user group' }))
    const dialog = within(
      screen.getByRole('dialog', { name: 'Add user group' })
    )
    const input = dialog.getByRole('combobox', { name: 'User group name' })
    const add = dialog.getByRole('button', { name: 'Add' })
    expect(add).toBeDisabled()
    await user.type(input, '   ')
    expect(add).toBeDisabled()
    await user.type(input, 'customer-bob  ')
    await user.keyboard('{Enter}')
    expect(onChange).toHaveBeenLastCalledWith(
      'GroupGroupRatio',
      JSON.stringify({ 'customer-bob': {} }, null, 2)
    )
  })

  test('does not overwrite existing rules when the same group is entered', async () => {
    const user = userEvent.setup()
    const onChange = renderRules('{"vip":{"gemini-ultra":1.5}}')
    await user.click(screen.getByRole('button', { name: 'Add user group' }))
    const dialog = within(
      screen.getByRole('dialog', { name: 'Add user group' })
    )
    await user.type(
      dialog.getByRole('combobox', { name: 'User group name' }),
      ' vip '
    )
    await user.keyboard('{Escape}')
    expect(dialog.getByRole('status')).toHaveTextContent(
      'This user group already has special ratio rules.'
    )
    expect(dialog.getByRole('button', { name: 'Add' })).toBeDisabled()
    await user.keyboard('{Enter}')
    expect(onChange).not.toHaveBeenCalled()
  })

  test('cancel discards the draft and reopening starts empty', async () => {
    const user = userEvent.setup()
    const onChange = renderRules()
    await user.click(screen.getByRole('button', { name: 'Add user group' }))
    let dialog = within(screen.getByRole('dialog', { name: 'Add user group' }))
    const cancel = dialog.getByRole('button', { name: 'Cancel' })
    await user.type(
      dialog.getByRole('combobox', { name: 'User group name' }),
      'discard-me'
    )
    await user.click(cancel)
    expect(onChange).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Add user group' }))
    dialog = within(screen.getByRole('dialog', { name: 'Add user group' }))
    expect(
      dialog.getByRole('combobox', { name: 'User group name' })
    ).toHaveValue('')
    expect(dialog.getByRole('button', { name: 'Add' })).toBeDisabled()
  })

  test('a custom user group can receive a billing-group override without changing other prices', async () => {
    const user = userEvent.setup()
    const onChange = renderRules('{"vip":{"gemini-ultra":1.5}}')
    await user.click(screen.getByRole('button', { name: 'Add user group' }))
    const groupDialog = within(
      screen.getByRole('dialog', { name: 'Add user group' })
    )
    const add = groupDialog.getByRole('button', { name: 'Add' })
    await user.type(
      groupDialog.getByRole('combobox', { name: 'User group name' }),
      'customer-alice'
    )
    await user.click(add)
    await user.click(
      screen.getByRole('button', { name: 'Add ratio override customer-alice' })
    )
    const override = within(
      screen.getByRole('dialog', { name: 'Add ratio override' })
    )
    await user.click(override.getByRole('combobox'))
    await user.click(screen.getByRole('option', { name: 'gemini-ultra' }))
    await user.type(override.getByRole('textbox'), '0.8')
    await user.click(override.getByRole('button', { name: 'Add' }))
    expect(onChange).toHaveBeenLastCalledWith(
      'GroupGroupRatio',
      JSON.stringify(
        {
          vip: { 'gemini-ultra': 1.5 },
          'customer-alice': { 'gemini-ultra': 0.8 },
        },
        null,
        2
      )
    )
    expect(
      onChange.mock.calls.every(([field]) => field === 'GroupGroupRatio')
    ).toBe(true)
    await user.click(
      screen.getByRole('button', { name: 'Special ratio rules customer-alice' })
    )
    expect(
      screen.getByRole('button', { name: 'Special ratio rules customer-alice' })
    ).toHaveAttribute('aria-expanded', 'true')
    await user.click(
      screen.getByRole('button', { name: 'Delete customer-alice' })
    )
    expect(onChange).toHaveBeenLastCalledWith(
      'GroupGroupRatio',
      JSON.stringify({ vip: { 'gemini-ultra': 1.5 } }, null, 2)
    )
  })
})
