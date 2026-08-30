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
/**
 * R19-B: 修 P2 Pricing 页 3 处 group i18n 的组件测试。
 *
 * 场景 —— 用户切到定价页时看到侧边过滤器、模型卡、模型详情三处都必须走
 * formatGroupName / GroupBadge，而不是把 `claude-max` / `GPT-Pro-Stable`
 * 这种 raw key 硬吐给终端用户。三个用例分别锁三处曾经的 raw 显示位。
 */
import { render, screen } from '@testing-library/react'
import i18next from 'i18next'
import { beforeAll, describe, expect, test, vi } from 'vitest'

/*
 * @lobehub/icons peer-depends on @lobehub/ui，后者链上会 import
 * @emoji-mart/data 的 native.json —— vitest 的 ESM loader 不带 JSON import
 * attribute 时会直接崩掉。这里把 getLobeIcon 桩成空组件，
 * 让 ModelCard / ModelBackendProviderSection 的加载路径绕开这条链。
 */
vi.mock('@/lib/lobe-icon', () => ({
  getLobeIcon: () => null,
}))

// model-details.tsx 里带 shiki / react-query / performance 图表这些重依赖，
// 只要 import 那个模块就会全部落地。测试只关心
// ModelBackendProviderSection，其它子组件桩掉不影响断言。
vi.mock('../model-details-api', () => ({
  ModelDetailsApi: () => null,
}))
vi.mock('../model-details-charts', () => ({
  LatencyTrendChart: () => null,
  UptimeTrendChart: () => null,
}))
vi.mock('../model-details-performance', () => ({
  ModelDetailsPerformance: () => null,
}))
vi.mock('../model-details-uptime-sparkline', () => ({
  UptimeSparkline: () => null,
}))

import type { PricingModel } from '../../types'
import { ModelCard } from '../model-card'
import { ModelBackendProviderSection } from '../model-details'
import { PricingSidebar, type PricingSidebarProps } from '../pricing-sidebar'

/**
 * 用测试环境自带的 i18next 实例灌 tier.name.* 兜底 key，
 * 让 formatGroupName 命中整体名翻译；其它未命中的 key 会 fallback 到 key 本身。
 */
beforeAll(() => {
  i18next.addResourceBundle('en', 'translation', {
    'tier.name.claude-max': 'Claude-Max Flagship',
    'tier.name.bestie': 'Bestie',
    'tier.name.aws-q': 'AWS-Q Bulk',
    'group.suffix.stable': 'Stable',
  })
})

function makeSidebarProps(groups: string[]): PricingSidebarProps {
  return {
    quotaTypeFilter: 'all',
    endpointTypeFilter: 'all',
    vendorFilter: 'all',
    groupFilter: 'all',
    tagFilter: 'all',
    onQuotaTypeChange: () => undefined,
    onEndpointTypeChange: () => undefined,
    onVendorChange: () => undefined,
    onGroupChange: () => undefined,
    onTagChange: () => undefined,
    vendors: [],
    groups,
    tags: [],
    models: [],
    hasActiveFilters: false,
    onClearFilters: () => undefined,
  }
}

function makeModel(overrides: Partial<PricingModel> = {}): PricingModel {
  return {
    id: 1,
    model_name: 'gpt-test',
    description: '',
    quota_type: 0,
    model_ratio: 1,
    completion_ratio: 1,
    enable_groups: [],
    supported_endpoint_types: [],
    tags: '',
    ...overrides,
  }
}

describe('R19-B pricing group i18n', () => {
  test('PricingSidebar 分组过滤器不再吐 raw group key', () => {
    // 场景：mock 三个真实业务分组，断言 chip label 是翻译后名字而不是 key 本身。
    render(
      <PricingSidebar
        {...makeSidebarProps(['claude-max', 'GPT-Pro-Stable', 'bestie'])}
      />
    )

    // claude-max → tier.name.claude-max 兜底
    expect(screen.getByText('Claude-Max Flagship')).toBeInTheDocument()
    // GPT-Pro-Stable → family+suffix 拆分
    expect(screen.getByText('GPT-Pro Stable')).toBeInTheDocument()
    // bestie → tier.name.bestie 兜底
    expect(screen.getByText('Bestie')).toBeInTheDocument()

    // 反向断言：raw key 不能出现在 chip 上（bestie 翻译后是 'Bestie' 首字母大写）
    expect(screen.queryByText('claude-max')).not.toBeInTheDocument()
    expect(screen.queryByText('GPT-Pro-Stable')).not.toBeInTheDocument()
  })

  test('ModelCard primary group 走 GroupBadge，显示翻译后名字', () => {
    // 场景：模型的 enable_groups[0] 决定卡片底部左侧 chip。
    // 修复前是 `{primaryGroup}` 直接吐字符串，现在应该经 GroupBadge → formatGroupName。
    render(
      <ModelCard
        model={makeModel({ enable_groups: ['claude-max'] })}
        onClick={() => undefined}
      />
    )

    expect(screen.getByText('Claude-Max Flagship')).toBeInTheDocument()
    expect(screen.queryByText('claude-max')).not.toBeInTheDocument()
  })

  test('ModelCard 无 groups 时不渲染 GroupBadge（不引入空 badge）', () => {
    // 场景：primaryGroup === undefined 时，条件渲染应该整体跳过 badge。
    // 保护 GroupBadge 内部的 isEmptyGroup 语义不被这里错误触发。
    const { container } = render(
      <ModelCard
        model={makeModel({ enable_groups: [] })}
        onClick={() => undefined}
      />
    )

    // GroupBadge 空 group 会渲染 t('User Group')；如果 primaryGroup 逻辑漏了
    // undefined 检查，这里会看到该文案。反向断言它不该出现。
    expect(container.textContent ?? '').not.toContain('User Group')
  })

  test('ModelBackendProviderSection groups pill 走 formatGroupName', () => {
    // 场景：详情面板 Backend / Provider 分组格 —— 修复前 CatalogPillList 直接
    // 吃 raw enable_groups；现在应该 map 一次 formatGroupName。
    render(
      <ModelBackendProviderSection
        model={makeModel({ enable_groups: ['claude-max', 'aws-q'] })}
      />
    )

    expect(screen.getByText('Claude-Max Flagship')).toBeInTheDocument()
    expect(screen.getByText('AWS-Q Bulk')).toBeInTheDocument()
    // 反向断言：raw key 不再作为文本节点存在
    expect(screen.queryByText('claude-max')).not.toBeInTheDocument()
    expect(screen.queryByText('aws-q')).not.toBeInTheDocument()
  })
})
