/*
 * 分组 i18n helper
 *
 * 分组 key 形如 `<family>-<suffix>` (例: GPT-Pro-Stable / claude-max-sale)。
 * 前端展示时优先按顺序:
 *   (1) 空串直接返回
 *   (2) tier.name.<groupKey> 整体名兜底 (claude-max / aws-q / bestie / wholesale-plus)
 *   (3) family-suffix 拆分，末段落在白名单里则翻译后缀
 *   (4) 都不命中返回原 key
 *
 * 分组描述 (UserUsableGroups value) 走 t() 直接翻译；缺 key 时 i18next 会 fallback
 * 到英文或原字符串。
 */

// 已知后缀白名单：只有末段命中这里才翻译，否则整个 key 保持原样。
// 大小写敏感 —— 因为业务里同时有 `-sale`（新分组）和 `-Sale` 混用。
//
// `max` / `api` 是 family+suffix 逻辑的补充 (例: some-max / kirobus-api)。
// `q` / `b` 是 upstream 通道单字母代号，不适合当 suffix 语义翻译，
// 因此不进这里 —— 走 tier.name.aws-q / tier.name.aws-b 整体名兜底更合适。
const SUFFIX_I18N_KEYS: Record<string, string> = {
  sale: 'group.suffix.sale',
  Sale: 'group.suffix.sale',
  stable: 'group.suffix.stable',
  Stable: 'group.suffix.stable',
  lowprice: 'group.suffix.lowprice',
  Lowprice: 'group.suffix.lowprice',
  supporter: 'group.suffix.supporter',
  Supporter: 'group.suffix.supporter',
  Enterprise: 'group.suffix.enterprise',
  enterprise: 'group.suffix.enterprise',
  Plus: 'group.suffix.plus',
  plus: 'group.suffix.plus',
  Pro: 'group.suffix.pro',
  pro: 'group.suffix.pro',
  distill: 'group.suffix.distill',
  Distill: 'group.suffix.distill',
  max: 'group.suffix.max',
  Max: 'group.suffix.max',
  api: 'group.suffix.api',
  API: 'group.suffix.api',
}

export interface FormattedGroup {
  /** 家族名 + 已翻译后缀，或整体名翻译，或原 key */
  displayName: string
  /** 已翻译分组描述，如果 desc 为空则返回空串 */
  displayDesc: string
}

type TFunction = (key: string, opts?: Record<string, unknown>) => string

/**
 * 把 group key 拆成 family + suffix 并本地化。
 * - 整体名 (`claude-max`, `aws-q`, `bestie`, `wholesale-plus`) → tier.name.* 兜底
 * - 家族+后缀 (`GPT-Pro-Stable`) → `GPT-Pro <t('group.suffix.stable')>`
 * - 单段无匹配 (`random`) → 原样
 */
export function formatGroupName(groupKey: string, t: TFunction): string {
  if (!groupKey) return ''

  // (b) 整体名兜底：优先于 family+suffix 拆分。
  // 用 defaultValue: '' 检测 miss —— i18next 拿不到 key 时会返回 defaultValue，
  // 命中时返回真实翻译；顺便隔离掉命中空串这种边界情况。
  const tierKey = `tier.name.${groupKey}`
  const tierTranslated = t(tierKey, { defaultValue: '' })
  if (tierTranslated && tierTranslated !== tierKey) {
    return tierTranslated
  }

  // (c) family-suffix 拆分
  const idx = groupKey.lastIndexOf('-')
  if (idx <= 0) return groupKey

  const suffix = groupKey.slice(idx + 1)
  const family = groupKey.slice(0, idx)
  const suffixKey = SUFFIX_I18N_KEYS[suffix]
  if (!suffixKey) return groupKey

  const translated = t(suffixKey)
  // i18next 找不到 key 时会 fallback 到 key 本身；这时保留原样更安全
  if (translated === suffixKey) return groupKey
  return `${family} ${translated}`
}

/**
 * 分组描述本地化。desc 本身作为 i18n key —— 缺 key 时 i18next fallback 到原文。
 */
export function formatGroupDesc(desc: string | undefined | null, t: TFunction): string {
  if (!desc) return ''
  return t(desc)
}

/** 一次性获取分组的展示信息 */
export function formatGroupDisplay(
  groupKey: string,
  desc: string | undefined | null,
  t: TFunction
): FormattedGroup {
  return {
    displayName: formatGroupName(groupKey, t),
    displayDesc: formatGroupDesc(desc, t),
  }
}
