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
import { expect, test, vi } from 'vitest'

import type { PricingModel } from '../../types'
import { ModelCardGrid } from '../model-card-grid'

vi.mock('@/lib/lobe-icon', () => ({ getLobeIcon: () => null }))

test('same public model selects the pricing for the clicked group', async () => {
  const model: PricingModel = {
    id: 1,
    model_name: 'gemini-4-argon',
    description: '',
    quota_type: 0,
    model_ratio: 1,
    completion_ratio: 5,
    enable_groups: ['gemini-ultra'],
    supported_endpoint_types: [],
    tags: '',
  }
  const models = [
    model,
    {
      ...model,
      quota_type: 1,
      model_price: 1,
      enable_groups: ['gemini-ultra-per-call'],
    },
  ]
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['perf-metrics-summary', 24], { data: { models: [] } })
  const selected = vi.fn()
  const user = userEvent.setup()
  const view = render(
    <QueryClientProvider client={client}>
      <ModelCardGrid models={models} onModelClick={selected} />
    </QueryClientProvider>
  )
  try {
    const buttons = screen.getAllByRole('button', { name: 'Details' })
    await user.click(buttons[0])
    await user.click(buttons[1])
    expect(selected).toHaveBeenCalledTimes(2)
    expect(selected.mock.calls[0][0]).not.toEqual(selected.mock.calls[1][0])
    expect(screen.getAllByText('gemini-4-argon')).toHaveLength(2)
  } finally {
    view.unmount()
    client.clear()
  }
})
