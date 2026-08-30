import { describe, expect, test } from 'vitest'

import {
  formatGroupDesc,
  formatGroupDisplay,
  formatGroupName,
} from '../group-i18n'

/**
 * 迷你 i18next 桩：dict 命中返回翻译；未命中：
 * - 有 opts.defaultValue → 返回 defaultValue
 * - 否则 fallback 回 key 本身（与真实 i18next 行为一致）
 */
function makeT(dict: Record<string, string>) {
  return (key: string, opts?: Record<string, unknown>): string => {
    if (Object.prototype.hasOwnProperty.call(dict, key)) return dict[key]
    if (opts && 'defaultValue' in opts) {
      const dv = opts.defaultValue
      return typeof dv === 'string' ? dv : key
    }
    return key
  }
}

const zhDict: Record<string, string> = {
  'tier.name.claude-max': 'Claude-Max 旗舰',
  'tier.name.aws-q': 'AWS-Q 批量',
  'tier.name.aws-b': 'AWS Bedrock',
  'tier.name.kirobus-api': 'Kirobus API',
  'tier.name.bestie': '铁粉',
  'tier.name.wholesale-plus': '批发-Plus',
  'tier.name.retail': '零售',
  'group.suffix.stable': '稳定',
  'group.suffix.sale': '特惠',
  'group.suffix.max': '旗舰',
  'group.suffix.api': 'API',
  '默认分组': '默认分组',
  'vip分组': 'VIP 专享',
}

const enDict: Record<string, string> = {
  'tier.name.claude-max': 'Claude-Max Flagship',
  'tier.name.aws-q': 'AWS-Q Bulk',
  'tier.name.aws-b': 'AWS Bedrock',
  'tier.name.kirobus-api': 'Kirobus API',
  'tier.name.bestie': 'Bestie',
  'tier.name.wholesale-plus': 'Wholesale Plus',
  'tier.name.retail': 'Retail',
  'group.suffix.stable': 'Stable',
  'group.suffix.sale': 'Sale',
  'group.suffix.max': 'Max',
  'group.suffix.api': 'API',
  '默认分组': 'Default group',
  'vip分组': 'VIP-only group',
}

describe('formatGroupName — tier.name.* 整体名兜底 (b)', () => {
  test('claude-max 走整体名而非 family+suffix (zh)', () => {
    expect(formatGroupName('claude-max', makeT(zhDict))).toBe('Claude-Max 旗舰')
  })

  test('claude-max 走整体名而非 family+suffix (en)', () => {
    expect(formatGroupName('claude-max', makeT(enDict))).toBe('Claude-Max Flagship')
  })

  test('aws-q 命中整体名 (q 不进 SUFFIX 白名单)', () => {
    expect(formatGroupName('aws-q', makeT(zhDict))).toBe('AWS-Q 批量')
    expect(formatGroupName('aws-q', makeT(enDict))).toBe('AWS-Q Bulk')
  })

  test('aws-b 命中整体名 (b 不进 SUFFIX 白名单)', () => {
    expect(formatGroupName('aws-b', makeT(zhDict))).toBe('AWS Bedrock')
    expect(formatGroupName('aws-b', makeT(enDict))).toBe('AWS Bedrock')
  })

  test('kirobus-api 整体名命中优先于 family=kirobus + suffix=api', () => {
    expect(formatGroupName('kirobus-api', makeT(zhDict))).toBe('Kirobus API')
    expect(formatGroupName('kirobus-api', makeT(enDict))).toBe('Kirobus API')
  })

  test('单段 group bestie 命中 tier.name.bestie', () => {
    expect(formatGroupName('bestie', makeT(zhDict))).toBe('铁粉')
    expect(formatGroupName('bestie', makeT(enDict))).toBe('Bestie')
  })

  test('wholesale-plus 整体名命中优先于 family=wholesale + suffix=plus', () => {
    expect(formatGroupName('wholesale-plus', makeT(zhDict))).toBe('批发-Plus')
    expect(formatGroupName('wholesale-plus', makeT(enDict))).toBe('Wholesale Plus')
  })

  test('retail 单段名命中', () => {
    expect(formatGroupName('retail', makeT(zhDict))).toBe('零售')
    expect(formatGroupName('retail', makeT(enDict))).toBe('Retail')
  })
})

describe('formatGroupName — family + suffix 拆分 (c)', () => {
  test('GPT-Pro-Stable → family=GPT-Pro + suffix=stable', () => {
    // tier.name.GPT-Pro-Stable 不命中，走后缀白名单
    expect(formatGroupName('GPT-Pro-Stable', makeT(zhDict))).toBe('GPT-Pro 稳定')
    expect(formatGroupName('GPT-Pro-Stable', makeT(enDict))).toBe('GPT-Pro Stable')
  })

  test('claude-max-sale → family=claude-max + suffix=sale (整体名不命中)', () => {
    // 因为 tier.name.claude-max-sale 不存在，走后缀路径
    expect(formatGroupName('claude-max-sale', makeT(zhDict))).toBe('claude-max 特惠')
    expect(formatGroupName('claude-max-sale', makeT(enDict))).toBe('claude-max Sale')
  })

  test('some-max 走 suffix=max (新加入白名单)', () => {
    // tier.name.some-max 未定义 → 走后缀白名单
    expect(formatGroupName('some-max', makeT(zhDict))).toBe('some 旗舰')
    expect(formatGroupName('some-max', makeT(enDict))).toBe('some Max')
  })
})

describe('formatGroupName — fall through (d)', () => {
  test('空串返回空串', () => {
    expect(formatGroupName('', makeT(zhDict))).toBe('')
  })

  test('未知整体名 + 未知后缀 → 原样返回', () => {
    expect(formatGroupName('unknown-brand-newx', makeT(zhDict))).toBe('unknown-brand-newx')
  })

  test('单段无 tier.name.* 兜底 → 原样返回', () => {
    expect(formatGroupName('randomthing', makeT(zhDict))).toBe('randomthing')
  })

  test('双段无匹配 → 原样返回', () => {
    // random 不在 tier.name.random-single, single 不在 SUFFIX 白名单
    expect(formatGroupName('random-single', makeT(zhDict))).toBe('random-single')
  })

  test('tier.name.* 空串命中被当作 miss 处理', () => {
    // 边界：某 locale 里 value 为空，不应把空串当作合法翻译返回
    const dict = { 'tier.name.foo': '' }
    expect(formatGroupName('foo', makeT(dict))).toBe('foo')
  })
})

describe('formatGroupDesc — seed desc 前置查表', () => {
  test('中文 seed desc 在 en locale 里被翻译', () => {
    expect(formatGroupDesc('默认分组', makeT(enDict))).toBe('Default group')
    expect(formatGroupDesc('vip分组', makeT(enDict))).toBe('VIP-only group')
  })

  test('无 key 时 fallback 到原文 (i18next 默认行为)', () => {
    expect(formatGroupDesc('未知描述', makeT(zhDict))).toBe('未知描述')
  })

  test('空/undefined/null 返回空串', () => {
    expect(formatGroupDesc('', makeT(zhDict))).toBe('')
    expect(formatGroupDesc(undefined, makeT(zhDict))).toBe('')
    expect(formatGroupDesc(null, makeT(zhDict))).toBe('')
  })
})

describe('formatGroupDisplay — 组合入口', () => {
  test('claude-max + 中文 desc 一次性翻译', () => {
    const t = makeT(zhDict)
    expect(formatGroupDisplay('claude-max', '默认分组', t)).toEqual({
      displayName: 'Claude-Max 旗舰',
      displayDesc: '默认分组',
    })
  })

  test('en locale 下 kirobus-api + vip分组', () => {
    const t = makeT(enDict)
    expect(formatGroupDisplay('kirobus-api', 'vip分组', t)).toEqual({
      displayName: 'Kirobus API',
      displayDesc: 'VIP-only group',
    })
  })
})
