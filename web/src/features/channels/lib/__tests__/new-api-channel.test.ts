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
import { describe, expect, test } from 'vitest'

import {
  CHANNEL_TYPE_NEW_API,
  CHANNEL_TYPE_OPTIONS,
  MODEL_FETCHABLE_TYPES,
} from '../../constants'
import type { Channel } from '../../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformChannelToFormDefaults,
  transformFormDataToUpdatePayload,
} from '../channel-form'
import { getChannelTypeConfig } from '../channel-type-config'
import { getChannelTypeIcon, getKeyPromptForType } from '../channel-utils'

function newAPIForm(baseUrl: string) {
  return {
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'New API upstream',
    type: CHANNEL_TYPE_NEW_API,
    base_url: baseUrl,
    key: 'test-key',
    models: 'gpt-5',
  }
}

describe('New API channel', () => {
  test('registers selection, ordering, model discovery, and icon metadata', () => {
    const option = CHANNEL_TYPE_OPTIONS.find(
      (item) => item.value === CHANNEL_TYPE_NEW_API
    )

    expect(option).toEqual({
      value: CHANNEL_TYPE_NEW_API,
      label: 'New API',
    })
    expect(
      CHANNEL_TYPE_OPTIONS.findIndex(
        (item) => item.value === CHANNEL_TYPE_NEW_API
      ) + 1
    ).toBe(CHANNEL_TYPE_OPTIONS.findIndex((item) => item.value === 58))
    expect(MODEL_FETCHABLE_TYPES.has(CHANNEL_TYPE_NEW_API)).toBe(true)
    expect(getChannelTypeIcon(CHANNEL_TYPE_NEW_API)).toBe('NewAPI')
    expect(getKeyPromptForType(CHANNEL_TYPE_NEW_API)).toBe(
      'Enter API key for this channel'
    )
    expect(getChannelTypeConfig(CHANNEL_TYPE_NEW_API).icon).toBe('NewAPI')
  })

  test('requires a non-blank Base URL', () => {
    const blankResult = channelFormSchema.safeParse(newAPIForm('  '))

    expect(blankResult.success).toBe(false)
    if (!blankResult.success) {
      expect(
        blankResult.error.issues.some(
          (issue) =>
            issue.path[0] === 'base_url' &&
            issue.message === 'Base URL is required for this channel type'
        )
      ).toBe(true)
    }

    expect(
      channelFormSchema.safeParse(newAPIForm('https://new-api.example')).success
    ).toBe(true)
  })

  test('keeps Sub2API Base URL validation unchanged', () => {
    const result = channelFormSchema.safeParse({
      ...newAPIForm(''),
      type: 59,
    })

    expect(result.success).toBe(true)
  })

  test('loads and saves the per-channel concurrency limit', () => {
    const channel = {
      id: 96,
      type: CHANNEL_TYPE_NEW_API,
      name: 'Limited upstream',
      status: 1,
      key: '',
      created_time: 0,
      test_time: 0,
      response_time: 0,
      balance_updated_time: 0,
      models: 'gpt-5',
      group: 'default',
      used_quota: 0,
      other: '',
      other_info: '',
      remark: '',
      max_input_tokens: 0,
      settings: JSON.stringify({ concurrency_limit: 50 }),
      channel_info: {
        is_multi_key: false,
        multi_key_size: 0,
        multi_key_polling_index: 0,
        multi_key_mode: 'random' as const,
      },
    } as Channel

    const values = transformChannelToFormDefaults(channel)
    expect(values.concurrency_limit).toBe(50)

    const payload = transformFormDataToUpdatePayload(values, channel.id)
    expect(JSON.parse(payload.settings ?? '{}').concurrency_limit).toBe(50)
  })

  test('rejects channel concurrency limits outside the supported range', () => {
    expect(
      channelFormSchema.safeParse({
        ...newAPIForm('https://new-api.example'),
        concurrency_limit: 10001,
      }).success
    ).toBe(false)
  })
})
