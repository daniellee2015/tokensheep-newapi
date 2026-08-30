/*
 * 分组 i18n helper
 *
 * 分组 key 形如 `<family>-<suffix>` (例: GPT-Pro-Stable / claude-max-sale)。
 * 前端展示时：家族名保留原样 (`GPT-Pro` / `claude-max`)，只翻译最末的语义后缀
 * (`stable → 稳定`, `sale → 特惠` 等)。无后缀的分组名 (如 `aws-q`) 原样返回。
 *
 * 分组描述 (UserUsableGroups value) 走 t() 直接翻译；缺 key 时 i18next 会 fallback
 * 到英文或原字符串。
 */

// 已知后缀白名单：只有末段命中这里才翻译，否则整个 key 保持原样。
// 大小写敏感 —— 因为业务里同时有 `-sale`（新分组）和 `-Sale` 混用。
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
}

export interface FormattedGroup {
  /** 家族名 + 已翻译后缀，或原 key（当 key 无匹配后缀时） */
  displayName: string
  /** 已翻译分组描述，如果 desc 为空则返回空串 */
  displayDesc: string
}

type TFunction = (key: string, opts?: Record<string, unknown>) => string

/**
 * 把 group key 拆成 family + suffix 并本地化。
 * - 单段 (`free`, `default`, `aws-q`) → 原样返回
 * - 多段 (`GPT-Pro-Stable`) → `GPT-Pro <t('group.suffix.stable')>`
 */
export function formatGroupName(groupKey: string, t: TFunction): string {
  if (!groupKey) return ''
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
