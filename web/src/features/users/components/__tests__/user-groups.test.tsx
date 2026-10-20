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
import {
  act,
  render,
  renderHook,
  screen,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { useUpdateOption } from '@/features/system-settings/hooks/use-update-option'

import type { User } from '../../types'
import { UsersMutateDrawer } from '../users-mutate-drawer'
import { UsersProvider } from '../users-provider'

const http = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }))
vi.mock('@/lib/api', () => ({ api: http }))

const customer: User = {
  id: 35,
  username: 'oidc_32',
  display_name: 'Customer',
  role: 1,
  status: 1,
  group: 'wholesale-plus',
  quota: 1000,
  used_quota: 0,
  request_count: 0,
}
let groups: string[]
let clients: QueryClient[] = []

function renderEditor() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  const onOpenChange = vi.fn()
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <UsersProvider>{children}</UsersProvider>
    </QueryClientProvider>
  )
  const drawer = (open: boolean) => (
    <UsersMutateDrawer
      open={open}
      onOpenChange={onOpenChange}
      currentRow={customer}
    />
  )
  const view = render(drawer(true), { wrapper })
  return {
    wrapper,
    onOpenChange,
    setOpen: (open: boolean) => view.rerender(drawer(open)),
  }
}

beforeEach(() => {
  groups = ['free', 'wholesale-plus']
  http.get.mockReset()
  http.put.mockReset()
  http.get.mockImplementation(async (path: string) => {
    if (path === '/api/group/tiers' || path === '/api/group/user-groups') {
      return { data: { success: true, data: [...groups] } }
    }
    if (path === '/api/user/35') {
      return { data: { success: true, data: customer } }
    }
    if (path === '/api/authz/catalog') {
      return { data: { success: true, data: { resources: [], roles: [] } } }
    }
    throw new Error(`Unexpected API request: ${path}`)
  })
  http.put.mockResolvedValue({ data: { success: true } })
})
afterEach(() => {
  clients.forEach((client) => client.clear())
  clients = []
})

describe('assignable groups in the user editor', () => {
  test('loads custom pricing groups and submits the selected group for the existing user', async () => {
    groups.push('wholesale-ultra')
    const user = userEvent.setup()
    renderEditor()
    const picker = await screen.findByRole('combobox', { name: 'Group' })
    await waitFor(() => expect(picker).toHaveTextContent('wholesale-plus'))
    await user.click(picker)
    await user.click(
      await screen.findByRole('option', { name: 'wholesale-ultra' })
    )
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    await waitFor(() =>
      expect(http.put).toHaveBeenCalledWith(
        '/api/user/',
        expect.objectContaining({ id: 35, group: 'wholesale-ultra' })
      )
    )
    expect(http.get).toHaveBeenCalledWith('/api/group/user-groups')
  })

  test('fetches newly saved groups when reopening the mounted drawer', async () => {
    const user = userEvent.setup()
    const editor = renderEditor()
    const picker = await screen.findByRole('combobox', { name: 'Group' })
    await user.click(picker)
    expect(
      await screen.findByRole('option', { name: 'free' })
    ).toBeInTheDocument()
    await user.keyboard('{Escape}')
    editor.setOpen(false)
    groups.push('wholesale-ultra')
    editor.setOpen(true)
    await user.click(await screen.findByRole('combobox', { name: 'Group' }))
    expect(
      await screen.findByRole('option', { name: 'wholesale-ultra' })
    ).toBeInTheDocument()
  })

  test('refreshes an open drawer after saving special ratio rules', async () => {
    const user = userEvent.setup()
    const editor = renderEditor()
    await user.click(await screen.findByRole('combobox', { name: 'Group' }))
    expect(
      await screen.findByRole('option', { name: 'free' })
    ).toBeInTheDocument()
    await user.keyboard('{Escape}')
    const mutation = renderHook(() => useUpdateOption(), {
      wrapper: editor.wrapper,
    })
    http.put.mockImplementation(async (path: string) => {
      if (path === '/api/option/') {
        groups.push('wholesale-ultra')
      }
      return { data: { success: true } }
    })
    await act(async () => {
      await mutation.result.current.mutateAsync({
        key: 'GroupGroupRatio',
        value: '{"wholesale-ultra":{"aws-q":0.35}}',
      })
    })
    await user.click(screen.getByRole('combobox', { name: 'Group' }))
    expect(
      await screen.findByRole('option', { name: 'wholesale-ultra' })
    ).toBeInTheDocument()
  })
})
