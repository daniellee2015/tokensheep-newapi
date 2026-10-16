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
  getRouteApi,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { UsageLogsProvider } from '../usage-logs-provider'
import { UsageLogsTable } from '../usage-logs-table'

const clients: QueryClient[] = []
const todayStart = new Date(2026, 9, 16).getTime()
const todayEnd = new Date(2026, 9, 16, 23, 59, 59, 999).getTime()
const yesterdayQuery = `startTime=${new Date(2026, 9, 15).getTime()}&endTime=${new Date(2026, 9, 15, 23, 59, 59, 999).getTime()}`
let latestModel = 'yesterday-request'
function LogsPage() {
  const { section } = getRouteApi(
    '/_authenticated/usage-logs/$section'
  ).useParams()
  const category =
    section === 'drawing' || section === 'task' ? section : 'common'
  return (
    <UsageLogsProvider>
      <UsageLogsTable logCategory={category} />
    </UsageLogsProvider>
  )
}
async function renderLogs(initialEntry: string) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 10_000 } },
  })
  clients.push(queryClient)
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
    component: LogsPage,
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
  if (initialEntry.includes('/common')) {
    await screen.findByText('yesterday-request')
  }
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Search' })).toBeEnabled()
  )
  return router
}
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 16, 0, 30))
  vi.spyOn(window, 'scrollTo').mockImplementation(() => undefined)
  useAuthStore.getState().auth.setUser({ id: 1, username: 'reader', role: 1 })
  latestModel = 'yesterday-request'
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/user/self/groups') {
      return { data: { success: true, data: {} } }
    }
    if (url.startsWith('/api/log/self/stat?')) {
      return { data: { success: true, data: { quota: 0, rpm: 0, tpm: 0 } } }
    }
    if (url.startsWith('/api/log/self?')) {
      return {
        data: {
          success: true,
          data: {
            total: 1,
            items: [
              {
                id: 1,
                type: 2,
                model_name: latestModel,
                created_at: 1792080000,
                quota: 0,
                other: '{}',
                token_name: 'test',
                group: 'default',
              },
            ],
          },
        },
      }
    }
    if (url.startsWith('/api/mj/self?') || url.startsWith('/api/task/self?')) {
      return { data: { success: true, data: { total: 0, items: [] } } }
    }
    throw new Error(`Unexpected GET ${url}`)
  })
})
afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  useAuthStore.getState().auth.reset()
  vi.useRealTimers()
})

test('Today after midnight immediately replaces yesterday with current logs and local day boundaries', async () => {
  const user = userEvent.setup()
  const router = await renderLogs(`/usage-logs/common?${yesterdayQuery}`)
  latestModel = 'today-request'
  await user.click(screen.getByRole('button', { name: /2026-10-15 00:00/ }))
  await user.click(screen.getByRole('button', { name: 'Today' }))
  await waitFor(() =>
    expect(router.state.location.search).toMatchObject({
      timeRange: 'today',
      page: 1,
    })
  )
  await screen.findByText('today-request')
  expect(
    screen.getByRole('button', { name: /2026-10-16 00:00 ~ 2026-10-16 23:59/ })
  ).toBeVisible()
})

test('Reset after midnight clears old filters and fetches the current day without reloading', async () => {
  const user = userEvent.setup()
  const router = await renderLogs(
    `/usage-logs/common?model=old-model&group=old-group&${yesterdayQuery}`
  )
  latestModel = 'today-request'
  await user.click(screen.getByRole('button', { name: 'Reset' }))
  await screen.findByText('today-request')
  expect(router.state.location.search).toMatchObject({
    timeRange: 'today',
    page: 1,
  })
  expect(router.state.location.search.model).toBeUndefined()
  expect(router.state.location.search.group).toBeUndefined()
  latestModel = 'newer-request'
  await user.click(screen.getByRole('button', { name: 'Reset' }))
  await screen.findByText('newer-request')
})

test('Reset remains available when only the date was filtered and uses the new day after midnight', async () => {
  vi.setSystemTime(new Date(2026, 9, 15, 23, 59))
  const user = userEvent.setup()
  const router = await renderLogs(`/usage-logs/common?${yesterdayQuery}`)
  vi.setSystemTime(new Date(2026, 9, 16, 0, 30))
  latestModel = 'today-request'
  expect(screen.getByRole('button', { name: 'Reset' })).toBeEnabled()
  await user.click(screen.getByRole('button', { name: 'Reset' }))
  await screen.findByText('today-request')
  expect(router.state.location.search).toMatchObject({
    timeRange: 'today',
  })
})

test('Confirm applies manually entered local times to both logs and statistics', async () => {
  const user = userEvent.setup()
  const router = await renderLogs(`/usage-logs/common?${yesterdayQuery}`)
  await user.click(screen.getByRole('button', { name: /2026-10-15 00:00/ }))
  fireEvent.change(screen.getByDisplayValue('2026-10-15T00:00'), {
    target: { value: '2026-10-16T00:00' },
  })
  fireEvent.change(screen.getByDisplayValue('2026-10-15T23:59'), {
    target: { value: '2026-10-16T01:00' },
  })
  latestModel = 'today-request'
  await user.click(screen.getByRole('button', { name: 'Confirm' }))
  await screen.findByText('today-request')
  const end = new Date(2026, 9, 16, 1).getTime()
  expect(router.state.location.search).toMatchObject({
    startTime: todayStart,
    endTime: end,
  })
  for (const path of ['/api/log/self?', '/api/log/self/stat?']) {
    expect(api.get).toHaveBeenCalledWith(
      expect.stringMatching(
        new RegExp(
          `${path.replace('?', '\\?')}.*start_timestamp=${todayStart / 1000}&end_timestamp=${end / 1000}`
        )
      )
    )
  }
})

test.each(['drawing', 'task'])(
  '%s presets immediately search with the correct API timestamp units and Reset stays usable',
  async (category) => {
    const user = userEvent.setup()
    const router = await renderLogs(`/usage-logs/${category}?${yesterdayQuery}`)
    await user.click(screen.getByRole('button', { name: /2026-10-15 00:00/ }))
    await user.click(screen.getByRole('button', { name: 'Today' }))
    await waitFor(() =>
      expect(router.state.location.search).toMatchObject({
        timeRange: 'today',
      })
    )
    const path = category === 'drawing' ? '/api/mj/self' : '/api/task/self'
    const divisor = category === 'drawing' ? 1 : 1000
    await waitFor(() =>
      expect(api.get).toHaveBeenCalledWith(
        expect.stringMatching(
          new RegExp(
            `${path}\\?.*start_timestamp=${Math.floor(todayStart / divisor)}&end_timestamp=${Math.floor(todayEnd / divisor)}`
          )
        )
      )
    )
    expect(screen.getByRole('button', { name: 'Reset' })).toBeEnabled()
  }
)

test.each(['Today', 'Reset'])(
  '%s continues to mean the current local day after reloading its URL on the next day',
  async (action) => {
    const user = userEvent.setup()
    const router = await renderLogs(
      '/usage-logs/common?page=1&type=%5B%220%22%5D&startTime=1792108800000&endTime=1792195199999'
    )
    if (action === 'Today') {
      await user.click(screen.getByRole('button', { name: / ~ / }))
    }
    await user.click(screen.getByRole('button', { name: action }))
    await waitFor(() =>
      expect(
        screen.getByRole('button', {
          name: '2026-10-16 00:00 ~ 2026-10-16 23:59',
        })
      ).toBeVisible()
    )
    const savedUrl = router.state.location.href
    cleanup()
    clients.splice(0).forEach((client) => client.clear())
    vi.setSystemTime(new Date(2026, 9, 17, 0, 30))
    await renderLogs(savedUrl)
    expect(
      screen.getByRole('button', {
        name: '2026-10-17 00:00 ~ 2026-10-17 23:59',
      })
    ).toBeVisible()
    for (const prefix of ['/api/log/self?', '/api/log/self/stat?']) {
      expect(
        vi
          .mocked(api.get)
          .mock.calls.some(
            ([url]) =>
              url.startsWith(prefix) &&
              url.includes(
                `start_timestamp=${new Date(2026, 9, 17).getTime() / 1000}`
              )
          )
      ).toBe(true)
    }
  }
)

test('a manually selected historical range remains fixed after reloading', async () => {
  const user = userEvent.setup()
  const router = await renderLogs(`/usage-logs/common?timeRange=today`)
  await user.click(screen.getByRole('button', { name: /2026-10-16 00:00/ }))
  fireEvent.change(screen.getByDisplayValue('2026-10-16T00:00'), {
    target: { value: '2026-10-15T09:30' },
  })
  fireEvent.change(screen.getByDisplayValue('2026-10-16T23:59'), {
    target: { value: '2026-10-15T10:45' },
  })
  await user.click(screen.getByRole('button', { name: 'Confirm' }))
  await waitFor(() =>
    expect(router.state.location.search).toMatchObject({
      startTime: new Date(2026, 9, 15, 9, 30).getTime(),
      endTime: new Date(2026, 9, 15, 10, 45).getTime(),
    })
  )
  expect(router.state.location.search.timeRange).toBeUndefined()
  const savedUrl = router.state.location.href
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  vi.setSystemTime(new Date(2026, 9, 17, 12))
  await renderLogs(savedUrl)
  expect(
    screen.getByRole('button', { name: '2026-10-15 09:30 ~ 2026-10-15 10:45' })
  ).toBeVisible()
})

test.each(['drawing', 'task'])(
  '%s reload resolves Today again without persisting yesterday timestamps',
  async (category) => {
    const user = userEvent.setup()
    const router = await renderLogs(`/usage-logs/${category}?${yesterdayQuery}`)
    await user.click(screen.getByRole('button', { name: 'Reset' }))
    await waitFor(() =>
      expect(router.state.location.search.timeRange).toBe('today')
    )
    expect(router.state.location.search.startTime).toBeUndefined()
    expect(router.state.location.search.endTime).toBeUndefined()
    const savedUrl = router.state.location.href
    cleanup()
    clients.splice(0).forEach((client) => client.clear())
    vi.setSystemTime(new Date(2026, 9, 17, 12))
    await renderLogs(savedUrl)
    expect(
      screen.getByRole('button', {
        name: '2026-10-17 00:00 ~ 2026-10-17 23:59',
      })
    ).toBeVisible()
    const prefix = category === 'drawing' ? '/api/mj/self?' : '/api/task/self?'
    const divisor = category === 'drawing' ? 1 : 1000
    expect(
      vi
        .mocked(api.get)
        .mock.calls.some(
          ([url]) =>
            url.startsWith(prefix) &&
            url.includes(
              `start_timestamp=${new Date(2026, 9, 17).getTime() / divisor}`
            )
        )
    ).toBe(true)
  }
)
