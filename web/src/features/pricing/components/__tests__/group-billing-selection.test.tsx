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
import type { ReactNode } from 'react'
import { expect, test, vi } from 'vitest'

import { Pricing } from '../../index'
import type { PricingData, PricingModel } from '../../types'

const route = vi.hoisted(() => ({
  search: {} as Record<string, string | undefined>,
}))

vi.mock('@/lib/lobe-icon', () => ({ getLobeIcon: () => null }))
vi.mock('@/components/layout', () => ({
  PublicLayout: (props: { children: ReactNode }) => props.children,
}))
vi.mock('@/components/page-transition', () => ({
  PageTransition: (props: { children: ReactNode }) => props.children,
}))
vi.mock('@/hooks/use-status', () => ({
  useStatus: () => ({ status: { price: 1, usd_exchange_rate: 1 } }),
}))
vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  useSearch: () => route.search,
}))
vi.mock('../model-details-api', () => ({ ModelDetailsApi: () => null }))
vi.mock('../model-details-performance', () => ({
  ModelDetailsPerformance: () => null,
}))

const usage: PricingModel = {
  id: 1,
  model_name: 'gemini-4-argon',
  vendor_id: 3,
  quota_type: 0,
  model_ratio: 1,
  completion_ratio: 5,
  cache_ratio: 0.05,
  enable_groups: ['gemini-ultra'],
  supported_endpoint_types: ['openai'],
}
const call: PricingModel = {
  ...usage,
  quota_type: 1,
  model_price: 1,
  model_ratio: 0,
  completion_ratio: 0,
  cache_ratio: undefined,
  enable_groups: ['gemini-ultra-per-call'],
}
const groupRatio = { 'gemini-ultra': 0.3, 'gemini-ultra-per-call': 0.07 }

for (const scenario of [
  { name: 'call API row first', rows: [call, usage], search: {} },
  { name: 'usage API row first', rows: [usage, call], search: {} },
  {
    name: 'usage group selected',
    rows: [call, usage],
    search: { group: 'gemini-ultra' },
  },
  {
    name: 'per-call group selected',
    rows: [call, usage],
    search: { group: 'gemini-ultra-per-call' },
  },
  {
    name: 'table view selected',
    rows: [call, usage],
    search: { view: 'table' },
  },
]) {
  test(`one model opens both group prices with ${scenario.name}`, async () => {
    route.search = scenario.search
    const rows = scenario.rows
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const data: PricingData = {
      success: true,
      data: rows,
      vendors: [{ id: 3, name: 'Google' }],
      group_ratio: groupRatio,
      usable_group: {
        'gemini-ultra': { desc: '', ratio: 0.3 },
        'gemini-ultra-per-call': { desc: '', ratio: 0.07 },
      },
      supported_endpoint: {},
      auto_groups: [],
    }
    client.setQueryData(['pricing'], data)
    client.setQueryData(['perf-metrics-summary', 24], { data: { models: [] } })
    client.setQueryData(['perf-metrics', usage.model_name], {
      data: { groups: [] },
    })
    const user = userEvent.setup()
    const view = render(
      <QueryClientProvider client={client}>
        <Pricing />
      </QueryClientProvider>
    )
    try {
      if (scenario.search.view === 'table') {
        expect(screen.getAllByText('gemini-4-argon')).toHaveLength(1)
        await user.click(screen.getByText('gemini-4-argon'))
      } else {
        expect(screen.getAllByRole('button', { name: 'Details' })).toHaveLength(
          1
        )
        await user.click(screen.getByRole('button', { name: 'Details' }))
      }
      const detail = within(screen.getByRole('dialog'))
      const usageRow = detail.getByRole('row', { name: /0\.3x/ })
      const callRow = detail.getByRole('row', { name: /0\.07x/ })
      expect(usageRow).toHaveTextContent('Token-based')
      expect(within(usageRow).getByText('$0.6')).toBeInTheDocument()
      expect(within(usageRow).getByText('$3')).toBeInTheDocument()
      expect(usageRow).toHaveTextContent('$0.03')
      expect(callRow).toHaveTextContent('Per Request')
      expect(callRow).toHaveTextContent('$0.07')
      expect(
        detail.getByRole('heading', { name: 'gemini-4-argon', level: 1 })
      ).toBeInTheDocument()
    } finally {
      view.unmount()
      client.clear()
    }
  })
}
