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
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { fetchLogsByCategory } from '../../lib/utils'
import { CommonLogsFilterBar } from '../common-logs-filter-bar'
import { UsageLogsProvider } from '../usage-logs-provider'

const queryClients: QueryClient[] = []
const pointerCaptureDescriptor = Object.getOwnPropertyDescriptor(
  HTMLElement.prototype,
  'setPointerCapture'
)

function FilterPage() {
  const table = useReactTable({
    data: [],
    columns: [],
    getCoreRowModel: getCoreRowModel(),
  })
  return (
    <UsageLogsProvider>
      <CommonLogsFilterBar table={table} />
    </UsageLogsProvider>
  )
}

async function renderFilters(initialEntry = '/usage-logs/common?page=3') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  queryClients.push(queryClient)
  queryClient.setQueryData(['groups'], { success: true, data: ['free'] })
  const root = createRootRoute({ component: Outlet })
  const authenticated = createRoute({
    getParentRoute: () => root,
    id: '_authenticated',
    component: Outlet,
  })
  const logs = createRoute({
    getParentRoute: () => authenticated,
    path: '/usage-logs/$section',
    validateSearch: (search: Record<string, unknown>) => search,
    component: FilterPage,
  })
  const router = createRouter({
    routeTree: root.addChildren([authenticated.addChildren([logs])]),
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
  })
  await router.load()
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  await screen.findByRole('button', { name: 'Search' })
  return router
}

beforeEach(async () => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => undefined)
  useAuthStore.getState().auth.setUser({ id: 1, username: 'reader', role: 1 })
  i18next.addResourceBundle('zhCN', 'translation', {
    'group.suffix.stable': '稳定',
    'tier.name.claude-max': 'Claude-Max 旗舰',
  })
  i18next.addResourceBundle('en', 'translation', {
    'group.suffix.stable': 'Stable',
    'tier.name.claude-max': 'Claude-Max Flagship',
  })
  await i18next.changeLanguage('zhCN')
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/user/self/groups') {
      return {
        data: {
          success: true,
          data: {
            'GPT-Pro-Stable': { desc: '', ratio: 1 },
            'claude-max': { desc: '', ratio: 1 },
          },
        },
      }
    }
    if (url === '/api/group/') {
      return {
        data: {
          success: true,
          data: ['GPT-Pro-Stable', 'claude-max', 'admin-only'],
        },
      }
    }
    if (url.startsWith('/api/log')) {
      return { data: { success: true, data: { quota: 0, rpm: 0, tpm: 0 } } }
    }
    throw new Error(`Unexpected GET ${url}`)
  })
})

afterEach(async () => {
  cleanup()
  if (pointerCaptureDescriptor) {
    Object.defineProperty(
      HTMLElement.prototype,
      'setPointerCapture',
      pointerCaptureDescriptor
    )
  } else {
    Reflect.deleteProperty(HTMLElement.prototype, 'setPointerCapture')
  }
  for (const client of queryClients.splice(0)) client.clear()
  useAuthStore.getState().auth.reset()
  await i18next.changeLanguage('en')
})

test.each([
  ['GPT-Pro-Stable', 'GPT-Pro-Stable'],
  ['GPT-Pro 稳定', 'GPT-Pro-Stable'],
  ['GPT-Pro Stable', 'GPT-Pro-Stable'],
  ['Claude-Max 旗舰', 'claude-max'],
])(
  'submitting %s directly searches logs and statistics with %s',
  async (input, expectedKey) => {
    const user = userEvent.setup()
    const router = await renderFilters()
    await user.type(screen.getByPlaceholderText('Group'), input)
    await user.click(screen.getByRole('button', { name: 'Search' }))
    await waitFor(() =>
      expect(router.state.location.search).toMatchObject({
        group: expectedKey,
        page: 1,
      })
    )
    await waitFor(() =>
      expect(api.get).toHaveBeenCalledWith(
        expect.stringMatching(
          new RegExp(`/api/log/self/stat\\?.*group=${expectedKey}`)
        )
      )
    )
    await fetchLogsByCategory({
      logCategory: 'common',
      isAdmin: false,
      page: 1,
      pageSize: 20,
      columnFilters: [],
      searchParams: router.state.location.search,
    })
    expect(api.get).toHaveBeenCalledWith(
      expect.stringMatching(
        new RegExp(`/api/log/self\\?.*group=${expectedKey}`)
      )
    )
    expect(api.get).not.toHaveBeenCalledWith('/api/group/')
  }
)

test('a translated draft still resolves after switching language and pressing Enter', async () => {
  const user = userEvent.setup()
  const router = await renderFilters()
  await user.type(screen.getByPlaceholderText('Group'), 'GPT-Pro 稳定')
  await act(() => i18next.changeLanguage('en'))
  await user.keyboard('{Enter}')
  await waitFor(() =>
    expect(router.state.location.search.group).toBe('GPT-Pro-Stable')
  )
})

test('a historical raw key remains usable when the group catalog fails, and Reset clears it', async () => {
  const user = userEvent.setup()
  vi.mocked(api.get).mockRejectedValue(new Error('Group catalog unavailable'))
  const router = await renderFilters('/usage-logs/common?group=retired-pool')
  expect(screen.getByPlaceholderText('Group')).toHaveValue('retired-pool')
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Search' })).toBeEnabled()
  )
  await user.click(screen.getByRole('button', { name: 'Search' }))
  await waitFor(() =>
    expect(router.state.location.search.group).toBe('retired-pool')
  )
  await user.click(screen.getByRole('button', { name: 'Reset' }))
  await waitFor(() =>
    expect(router.state.location.search.group).toBeUndefined()
  )
  expect(screen.getByPlaceholderText('Group')).toHaveValue('')
})

test('administrator searches resolve group names using the full catalog instead of personal groups or user tiers', async () => {
  const user = userEvent.setup()
  useAuthStore.getState().auth.setUser({ id: 2, username: 'admin', role: 10 })
  const router = await renderFilters()
  await user.type(screen.getByPlaceholderText('Group'), 'Claude-Max 旗舰')
  await user.click(screen.getByRole('button', { name: 'Search' }))
  await waitFor(() =>
    expect(router.state.location.search.group).toBe('claude-max')
  )
  expect(api.get).toHaveBeenCalledWith('/api/group/')
  expect(api.get).not.toHaveBeenCalledWith('/api/user/self/groups')
  await waitFor(() =>
    expect(api.get).toHaveBeenCalledWith(
      expect.stringMatching(/\/api\/log\/stat\?.*group=claude-max/)
    )
  )
})

test('the mobile drawer accepts a translated name without selecting a dropdown item', async () => {
  const user = userEvent.setup()
  Object.defineProperty(HTMLElement.prototype, 'setPointerCapture', {
    configurable: true,
    value: () => undefined,
  })
  const matchMedia = window.matchMedia
  vi.spyOn(window, 'matchMedia').mockImplementation((query) => ({
    ...matchMedia(query),
    matches: query === '(max-width: 640px)',
  }))
  const router = await renderFilters()
  await user.click(screen.getByRole('button', { name: 'Filter' }))
  await user.type(await screen.findByPlaceholderText('Group'), 'GPT-Pro 稳定')
  await user.click(screen.getByRole('button', { name: 'Search' }))
  await waitFor(() =>
    expect(router.state.location.search.group).toBe('GPT-Pro-Stable')
  )
})

test('a translated search waits for the catalog instead of submitting the display name prematurely', async () => {
  const user = userEvent.setup()
  const originalGet = vi.mocked(api.get).getMockImplementation()
  if (!originalGet) throw new Error('API fixture not installed')
  let resolveGroups!: (response: {
    data: { success: boolean; data: Record<string, unknown> }
  }) => void
  const pendingGroups = new Promise<{
    data: { success: boolean; data: Record<string, unknown> }
  }>((resolve) => {
    resolveGroups = resolve
  })
  vi.mocked(api.get).mockImplementation((url, config) => {
    if (url === '/api/user/self/groups') return pendingGroups
    return originalGet(url, config)
  })
  const router = await renderFilters()
  await user.type(screen.getByPlaceholderText('Group'), 'GPT-Pro 稳定')
  expect(screen.getByRole('button', { name: 'Search' })).toBeDisabled()
  await user.keyboard('{Enter}')
  expect(router.state.location.search.group).toBeUndefined()
  await act(async () =>
    resolveGroups({
      data: {
        success: true,
        data: { 'GPT-Pro-Stable': { desc: '', ratio: 1 } },
      },
    })
  )
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Search' })).toBeEnabled()
  )
  await user.click(screen.getByRole('button', { name: 'Search' }))
  await waitFor(() =>
    expect(router.state.location.search.group).toBe('GPT-Pro-Stable')
  )
})

test('an exact raw key takes precedence when another group has the same translated name', async () => {
  const user = userEvent.setup()
  vi.mocked(api.get).mockResolvedValue({
    data: {
      success: true,
      data: {
        'GPT-Pro-Stable': { desc: '', ratio: 1 },
        'GPT-Pro 稳定': { desc: '', ratio: 1 },
      },
    },
  })
  const router = await renderFilters()
  await user.type(screen.getByPlaceholderText('Group'), 'GPT-Pro 稳定')
  await user.click(screen.getByRole('button', { name: 'Search' }))
  await waitFor(() =>
    expect(router.state.location.search.group).toBe('GPT-Pro 稳定')
  )
})
