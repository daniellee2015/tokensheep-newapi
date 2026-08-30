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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, test } from 'vitest'

const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { api } = await import('@/lib/api')
const { ApiKeysProvider } = await import('../api-keys-provider')
const { ApiKeysMutateDrawer } = await import('../api-keys-mutate-drawer')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: { en: { translation: {} } },
})

type ApiMethod = (url: string, data?: unknown) => Promise<{ data: unknown }>
type MockableApi = {
  get: ApiMethod
  post: ApiMethod
}
type RenderedDrawer = {
  queryClient: InstanceType<typeof QueryClient>
}

const apiClient = api as unknown as MockableApi
const originalGet = apiClient.get
const originalPost = apiClient.post
let renderedDrawer: RenderedDrawer | null = null

function installApiFixtures(createdPayloads: Array<Record<string, unknown>>) {
  apiClient.get = async (url) => {
    switch (url) {
      case '/api/status':
        return { data: { data: { default_use_auto_group: true } } }
      case '/api/user/models':
        return { data: { success: true, data: [] } }
      case '/api/user/self/groups':
        return {
          data: {
            success: true,
            data: {
              auto: { desc: 'Automatic routing', ratio: 'auto' },
              default: { desc: 'Standard access', ratio: 1 },
              vip: { desc: 'Priority access', ratio: 2 },
            },
          },
        }
      case '/api/token/auto-groups':
        return {
          data: {
            success: true,
            data: { groups: ['vip', 'default'], max_count: 3 },
          },
        }
      default:
        throw new Error(`Unexpected GET ${url}`)
    }
  }
  apiClient.post = async (url, data) => {
    expect(url).toBe('/api/token/')
    expect(data && typeof data === 'object').toBeTruthy()
    createdPayloads.push(data as Record<string, unknown>)
    return { data: { success: true, data: {} } }
  }
}

async function renderCreateDrawer(): Promise<void> {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const freshAt = Date.now() + 60_000
  queryClient.setQueryData(
    ['status'],
    { default_use_auto_group: true },
    { updatedAt: freshAt }
  )
  queryClient.setQueryData(
    ['user-models'],
    { success: true, data: [] },
    { updatedAt: freshAt }
  )
  queryClient.setQueryData(
    ['user-groups'],
    {
      success: true,
      data: {
        auto: { desc: 'Automatic routing', ratio: 'auto' },
        default: { desc: 'Standard access', ratio: 1 },
        vip: { desc: 'Priority access', ratio: 2 },
      },
    },
    { updatedAt: freshAt }
  )
  queryClient.setQueryData(
    ['token-auto-groups'],
    {
      success: true,
      data: { groups: ['vip', 'default'], max_count: 3 },
    },
    { updatedAt: freshAt }
  )
  renderedDrawer = { queryClient }

  render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <ApiKeysProvider>
          <ApiKeysMutateDrawer open onOpenChange={() => undefined} />
        </ApiKeysProvider>
      </I18nextProvider>
    </QueryClientProvider>
  )
  await waitFor(
    () => {
      const saveButton = findButton('Save changes', false)
      expect(saveButton).toBeEnabled()
    },
    { timeout: 1500 }
  )
}

function findButton(text: string, required: true): HTMLButtonElement
function findButton(text: string, required: false): HTMLButtonElement | null
function findButton(text: string, required = true): HTMLButtonElement | null {
  const button = screen
    .queryAllByRole<HTMLButtonElement>('button')
    .find((candidate) => candidate.textContent?.includes(text))
  if (required && !button) {
    throw new Error(`Expected button containing "${text}"`)
  }
  return button ?? null
}

function getControlByLabel(labelText: 'Name' | 'Quantity'): HTMLInputElement
function getControlByLabel(labelText: 'Group'): HTMLButtonElement
function getControlByLabel(labelText: 'Auto group order'): HTMLElement
function getControlByLabel(labelText: string): HTMLElement {
  const label = [...document.querySelectorAll<HTMLLabelElement>('label')].find(
    (candidate) => candidate.textContent?.trim() === labelText
  )
  if (!label) {
    throw new Error(`Expected label "${labelText}"`)
  }

  const control =
    label.control ??
    label
      .closest('[data-slot="form-item"]')
      ?.querySelector<HTMLElement>(
        '[data-slot="form-control"], input, textarea, button[role="combobox"], [role="group"]'
      )
  if (!control) {
    throw new Error(`Expected control for label "${labelText}"`)
  }
  return control
}

function changeInput(input: HTMLInputElement, value: string): void {
  fireEvent.input(input, { target: { value } })
}

function selectComboboxOption(
  trigger: HTMLButtonElement,
  optionDescription: string
): void {
  fireEvent.click(trigger)
  const option = [
    ...document.querySelectorAll<HTMLElement>('[data-slot="command-item"]'),
  ].find((candidate) => candidate.textContent?.includes(optionDescription))
  if (!option) {
    throw new Error(`Expected option containing "${optionDescription}"`)
  }
  fireEvent.click(option)
}

afterEach(() => {
  apiClient.get = originalGet
  apiClient.post = originalPost
  localStorage.clear()
  if (renderedDrawer) {
    renderedDrawer.queryClient.clear()
    renderedDrawer = null
  }
})

// R19-A: 单独的 fixture, 打开 combobox 会看到已翻译的分组名. groupsData 里
// 混合了 tier 整体名 (`claude-max`) / kirobus-api / aws-q, 用于验证 helper
// 三条路径 (整体名 / family+suffix / 未知) 都被 drawer 的 useMemo 走到.
function installI18nApiFixtures() {
  apiClient.get = async (url) => {
    switch (url) {
      case '/api/status':
        return { data: { data: { default_use_auto_group: false } } }
      case '/api/user/models':
        return { data: { success: true, data: [] } }
      case '/api/user/self/groups':
        return {
          data: {
            success: true,
            data: {
              // 整体名: tier.name.claude-max 命中
              'claude-max': {
                desc: '顶级性能',
                ratio: 1,
                kind: 'channel',
              },
              // 整体名: tier.name.kirobus-api 命中
              'kirobus-api': { desc: '', ratio: 1, kind: 'channel' },
              // 整体名: tier.name.aws-q 命中 (q 不在 SUFFIX 白名单)
              'aws-q': { desc: '', ratio: 1, kind: 'channel' },
              // 单段无 tier.name 兜底, 保持原 key
              default: { desc: 'Standard access', ratio: 1, kind: 'tier' },
            },
          },
        }
      case '/api/token/auto-groups':
        return {
          data: {
            success: true,
            data: { groups: [], max_count: 5 },
          },
        }
      default:
        throw new Error(`Unexpected GET ${url}`)
    }
  }
  apiClient.post = async () => ({
    data: { success: true, data: {} },
  })
}

async function renderI18nDrawer() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  renderedDrawer = { queryClient }
  render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <ApiKeysProvider>
          <ApiKeysMutateDrawer open onOpenChange={() => undefined} />
        </ApiKeysProvider>
      </I18nextProvider>
    </QueryClientProvider>
  )
  await waitFor(
    () => {
      const saveButton = findButton('Save changes', false)
      expect(saveButton).toBeEnabled()
    },
    { timeout: 1500 }
  )
}

describe('API keys mutate drawer group i18n (R19-A)', () => {
  // 在 test 内动态注入 tier.name.* / group.suffix.* bundle, 覆盖不同 locale.
  // 用完必须 remove, 否则会污染后续 test (i18n 是 module-level singleton).
  const TEST_LANGS = ['en', 'zh'] as const

  afterEach(async () => {
    for (const lng of TEST_LANGS) {
      i18n.removeResourceBundle(lng, 'translation')
    }
    // 恢复默认 en, resources 为空 (与 module init 一致)
    i18n.addResourceBundle('en', 'translation', {}, true, true)
    await i18n.changeLanguage('en')
  })

  test('zh: combobox 里 claude-max 显示 Claude-Max 旗舰, 缺 key 的 desc fallback 原文', async () => {
    // 只加 tier 翻译 key, 不改 UI 文案 —— 否则 findButton('Save changes')
    // 找不到会 timeout. UI 文案缺 key 时 i18next fallback 到原 key, 已经是英文.
    i18n.addResourceBundle(
      'zh',
      'translation',
      {
        'tier.name.claude-max': 'Claude-Max 旗舰',
        'tier.name.kirobus-api': 'Kirobus API',
        'tier.name.aws-q': 'AWS-Q 批量',
      },
      true,
      true
    )
    await i18n.changeLanguage('zh')

    installI18nApiFixtures()
    await renderI18nDrawer()

    // trigger 收起态显示当前选中 (默认 default), 打开 combobox 看所有 option
    const groupTrigger = getControlByLabel('Group')
    fireEvent.click(groupTrigger)

    const items = [
      ...document.querySelectorAll<HTMLElement>('[data-slot="command-item"]'),
    ]
    const labels = items.map((item) => item.textContent ?? '')
    expect(labels.some((label) => label.includes('Claude-Max 旗舰'))).toBe(true)
    expect(labels.some((label) => label.includes('Kirobus API'))).toBe(true)
    expect(labels.some((label) => label.includes('AWS-Q 批量'))).toBe(true)
    // desc '顶级性能' 不在 dict, i18next fallback 到原文
    expect(labels.some((label) => label.includes('顶级性能'))).toBe(true)
  })

  test('en: 同样的 groups 翻译成英文 tier.name.*', async () => {
    i18n.addResourceBundle(
      'en',
      'translation',
      {
        'tier.name.claude-max': 'Claude-Max Flagship',
        'tier.name.kirobus-api': 'Kirobus API',
        'tier.name.aws-q': 'AWS-Q Bulk',
      },
      true,
      true
    )
    await i18n.changeLanguage('en')

    installI18nApiFixtures()
    await renderI18nDrawer()

    const groupTrigger = getControlByLabel('Group')
    fireEvent.click(groupTrigger)

    const labels = [
      ...document.querySelectorAll<HTMLElement>('[data-slot="command-item"]'),
    ].map((item) => item.textContent ?? '')
    expect(labels.some((label) => label.includes('Claude-Max Flagship'))).toBe(
      true
    )
    expect(labels.some((label) => label.includes('AWS-Q Bulk'))).toBe(true)
    expect(labels.some((label) => label.includes('Kirobus API'))).toBe(true)
  })
})

describe('API keys mutate drawer Auto group integration', () => {
  test('inherits the root Auto order and sends an empty override for every batch-created key', async () => {
    const createdPayloads: Array<Record<string, unknown>> = []
    installApiFixtures(createdPayloads)
    await renderCreateDrawer()

    const groupTrigger = getControlByLabel('Group')
    expect(groupTrigger.textContent?.includes('auto')).toBe(true)
    expect(
      document.body.textContent?.includes(
        'Using the complete global Auto order (2 groups)'
      )
    ).toBe(true)
    expect(
      [
        ...document.querySelectorAll('[data-slot="global-auto-order-name"]'),
      ].map((item) => item.textContent)
    ).toEqual(['vip', 'default'])
    expect(findButton('Restore global Auto', true).disabled).toBe(true)

    changeInput(getControlByLabel('Name'), 'batch')
    changeInput(getControlByLabel('Quantity'), '2')
    fireEvent.click(findButton('Save changes', true))
    await waitFor(() => expect(createdPayloads).toHaveLength(2))

    expect(createdPayloads.length).toBe(2)
    expect(createdPayloads[0]?.name).toBe('batch')
    for (const payload of createdPayloads) {
      expect(payload.group).toBe('auto')
      expect(payload.auto_groups).toEqual([])
      expect(payload.cross_group_retry).toBe(true)
    }
  })

  test('preserves an unsaved custom order and mode after Auto to ordinary to Auto changes', async () => {
    const createdPayloads: Array<Record<string, unknown>> = []
    installApiFixtures(createdPayloads)
    await renderCreateDrawer()

    const autoOrderControl = getControlByLabel('Auto group order')
    const addGroupTrigger = autoOrderControl.querySelector<HTMLButtonElement>(
      'button[role="combobox"]'
    )
    if (!addGroupTrigger) {
      throw new Error('Expected Auto group order combobox')
    }
    selectComboboxOption(addGroupTrigger, 'Priority access')

    expect(
      document.querySelector('button[aria-label="Remove vip"]')
    ).toBeTruthy()
    expect(document.body.textContent?.includes('1 / 3 groups selected')).toBe(
      true
    )
    expect(findButton('Restore global Auto', true).disabled).toBe(false)

    const groupTrigger = getControlByLabel('Group')
    selectComboboxOption(groupTrigger, 'Standard access')
    expect(document.querySelector('button[aria-label="Remove vip"]')).toBe(null)
    selectComboboxOption(groupTrigger, 'Automatic routing')

    expect(
      document.querySelector('button[aria-label="Remove vip"]')
    ).toBeTruthy()
    expect(document.body.textContent?.includes('1 / 3 groups selected')).toBe(
      true
    )
    expect(findButton('Restore global Auto', true).disabled).toBe(false)

    changeInput(getControlByLabel('Name'), 'custom')
    fireEvent.click(findButton('Save changes', true))
    await waitFor(() => expect(createdPayloads).toHaveLength(1))
    expect(createdPayloads[0]?.auto_groups).toEqual(['vip'])
  })
})
