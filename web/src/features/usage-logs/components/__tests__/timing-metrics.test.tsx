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
import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { afterEach, describe, expect, test } from 'vitest'

import zh from '@/i18n/locales/zh.json'

import { usageLogSchema } from '../../data/schema'
import { parseLogOther } from '../../lib/format'
import { DetailsDialog } from '../dialogs/details-dialog'
import { StreamTpsCell, TimingMetricsCell } from '../timing-metrics-cell'

afterEach(async () => {
  await i18next.changeLanguage('en')
})

describe('log timing provenance', () => {
  test('does not call an unclassified legacy event first-token latency', () => {
    render(
      <TimingMetricsCell
        useTimeSec={16}
        completionTokens={120}
        frtMs={300}
        isStream
      />
    )
    expect(screen.getByText('Legacy timing')).toBeInTheDocument()
    expect(screen.queryByText('First token')).not.toBeInTheDocument()
    expect(screen.getByText('0.3s')).toBeInTheDocument()
  })

  test('distinguishes first thinking from first text in the emitted log fields', async () => {
    const user = userEvent.setup()
    render(
      <TimingMetricsCell
        useTimeSec={16}
        completionTokens={120}
        frtMs={4500}
        isStream
        timing={{
          frt_metric: 'downstream_first_content',
          frt_event_ms: 300,
          first_thinking_ms: 4500,
          first_text_ms: 15000,
          upstream_first_content_ms: 4488,
        }}
      />
    )
    expect(screen.getByText('First content')).toBeInTheDocument()
    expect(screen.queryByText('First token')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Timing details' }))
    const popup = screen.getByRole('dialog')
    expect(within(popup).getByText('First event')).toBeInTheDocument()
    expect(within(popup).getByText('First thinking')).toBeInTheDocument()
    expect(within(popup).getByText('First text')).toBeInTheDocument()
    expect(within(popup).getByText('0.3s')).toBeInTheDocument()
    expect(within(popup).getByText('15.0s')).toBeInTheDocument()
    expect(
      within(popup).getByText(
        'Server flush timing; client display may be later.'
      )
    ).toBeInTheDocument()
  })

  test('preserves event-only provenance and does not infer missing text timing', async () => {
    const user = userEvent.setup()
    render(
      <TimingMetricsCell
        useTimeSec={4}
        completionTokens={20}
        frtMs={300}
        isStream
        timing={{ frt_metric: 'upstream_first_event' }}
      />
    )
    expect(screen.getByText('First event')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Timing details' }))
    const popup = screen.getByRole('dialog')
    expect(
      within(popup).getByRole('definition', { name: 'First event' })
    ).toHaveTextContent('0.3s')
    expect(
      within(popup).getByRole('definition', { name: 'First text' })
    ).toHaveTextContent('Not recorded')
    expect(
      within(popup).getByRole('definition', { name: 'First content' })
    ).toHaveTextContent('Not recorded')
  })

  test('metadata-only streams do not display the start event as content', async () => {
    const user = userEvent.setup()
    render(
      <TimingMetricsCell
        useTimeSec={4}
        completionTokens={0}
        frtMs={-1}
        isStream
        timing={{ frt_metric: 'downstream_first_content', frt_event_ms: 300 }}
      />
    )
    expect(screen.getByText('No content')).toBeInTheDocument()
    expect(screen.queryByText('0.3s')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Timing details' }))
    expect(
      screen.getByRole('definition', { name: 'First content' })
    ).toHaveTextContent('No content')
    expect(
      screen.getByRole('definition', { name: 'First event' })
    ).toHaveTextContent('0.3s')
  })

  test('tool-only timing stays separate from missing text and thinking', async () => {
    const user = userEvent.setup()
    const other = parseLogOther(
      JSON.stringify({
        frt: 800,
        frt_metric: 'downstream_first_content',
        frt_event_ms: 100,
        first_tool_ms: 800,
      })
    )
    render(
      <TimingMetricsCell
        useTimeSec={1}
        completionTokens={15}
        frtMs={other?.frt}
        timing={other ?? undefined}
        isStream
      />
    )
    await user.click(screen.getByRole('button', { name: 'Timing details' }))
    expect(
      screen.getByRole('definition', { name: 'First tool' })
    ).toHaveTextContent('0.8s')
    expect(
      screen.getByRole('definition', { name: 'First text' })
    ).toHaveTextContent('Not recorded')
    expect(
      screen.getByRole('definition', { name: 'First thinking' })
    ).toHaveTextContent('Not recorded')
  })

  test.each([
    [0, '0.0s'],
    [undefined, 'N/A'],
    [-2, 'N/A'],
    [Number.NaN, 'N/A'],
    [Number.POSITIVE_INFINITY, 'N/A'],
  ])(
    'renders zero or unavailable timing without inventing a fast value: %s',
    (value, expected) => {
      render(
        <TimingMetricsCell
          useTimeSec={4}
          completionTokens={20}
          frtMs={value}
          isStream
          timing={{ frt_metric: 'downstream_first_content' }}
        />
      )
      expect(
        screen.getByRole('button', { name: 'Timing details' })
      ).toHaveTextContent(expected)
    }
  )

  test('unknown metric identifiers retain an explicit legacy label', () => {
    render(
      <TimingMetricsCell
        useTimeSec={4}
        completionTokens={20}
        frtMs={300}
        isStream
        timing={{ frt_metric: 'unknown-provider-metric' }}
      />
    )
    expect(screen.getByText('Legacy timing')).toBeInTheDocument()
    expect(screen.queryByText('First content')).not.toBeInTheDocument()
  })

  test('mobile timing details open by keyboard and return focus on Escape', async () => {
    const user = userEvent.setup()
    render(
      <TimingMetricsCell
        useTimeSec={4}
        completionTokens={20}
        frtMs={900}
        isStream
        indicator='dot'
        timing={{ frt_metric: 'downstream_first_content', first_text_ms: 900 }}
      />
    )
    const trigger = screen.getByRole('button', { name: 'Timing details' })
    await user.tab()
    expect(trigger).toHaveFocus()
    await user.keyboard('{Enter}')
    expect(trigger).toHaveAttribute('aria-expanded', 'true')
    expect(
      screen.getByRole('definition', { name: 'First text' })
    ).toHaveTextContent('0.9s')
    await user.keyboard('{Escape}')
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
    expect(trigger).toHaveFocus()
  })

  test('non-stream rows show duration without a fabricated first-content metric', () => {
    render(
      <TimingMetricsCell
        useTimeSec={4}
        completionTokens={20}
        frtMs={300}
        isStream={false}
      />
    )
    expect(screen.getByText('Duration')).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Timing details' })
    ).not.toBeInTheDocument()
  })

  test('language changes update timing labels using real locale resources', async () => {
    i18next.addResourceBundle('zhCN', 'translation', zh.translation, true, true)
    render(
      <TimingMetricsCell
        useTimeSec={4}
        completionTokens={20}
        frtMs={900}
        isStream
        timing={{ frt_metric: 'downstream_first_content' }}
      />
    )
    await act(async () => {
      await i18next.changeLanguage('zhCN')
    })
    expect(screen.getByText('首内容')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '计时详情' })).toBeInTheDocument()
  })

  test('log details consume the same semantic fields rather than displaying generic FRT', () => {
    const log = usageLogSchema.parse({
      id: 1,
      user_id: 1,
      created_at: 1793500000,
      type: 2,
      content: '',
      is_stream: true,
      use_time: 16,
      other: JSON.stringify({
        frt: 4500,
        frt_metric: 'downstream_first_content',
        frt_event_ms: 300,
        first_thinking_ms: 4500,
        first_text_ms: 15000,
      }),
    })
    render(
      <DetailsDialog
        log={log}
        isAdmin={false}
        open
        onOpenChange={() => undefined}
      />
    )
    expect(
      screen.getByRole('definition', { name: 'First content' })
    ).toHaveTextContent('4.5s')
    expect(
      screen.getByRole('definition', { name: 'First text' })
    ).toHaveTextContent('15.0s')
    expect(screen.queryByText(/FRT:/)).not.toBeInTheDocument()
  })

  test('describes existing throughput as an end-to-end rate without changing it', () => {
    render(<StreamTpsCell isStream tokensPerSecond={6} />)
    const rate = screen.getByLabelText('End-to-end output rate')
    expect(rate).toHaveTextContent('6 t/s')
    expect(rate).toHaveAttribute(
      'title',
      'Output tokens / total duration; not model generation speed.'
    )
  })
})
