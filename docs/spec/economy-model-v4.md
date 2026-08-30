# TokenSheep 经济模型 Spec v4

**状态**: **Draft v4** · 待审 · 落地代码修 bug 前的锚点
**最后更新**: 2026-08-29
**前身**: v3 (2026-07-06) 见 `economy-model.md`，已过时

---

## 变更摘要 (v3 → v4)

v3 是"注册 → 领欢迎码 → 贡献升 tier"单链路。v4 增加两条平行链：**订阅制**和**商业档**，并明确三者互斥/协作规则。

| 主题 | v3 | v4 |
|---|---|---|
| 用户身份 | 5 档 tier (free/supporter/fan/bestie/vip) 单一体系 | 三体系: **Tier** / **Subscription** / **Commercial**，互斥 |
| VIP | 门槛 $500 自动升组，landing 不公开显示 | **默认关闭**，通过 `DisabledTiers` 开关；配置保留可随时开 |
| 商业档 | 不存在 | 引入 `retail` / `wholesale` / `wholesale-plus`，管理员手工分配 |
| 订阅 | 不存在 | 引入 `SubscriptionPlan` + `UserSubscription`；套餐买入自动进 `sub` 组，到期回退 |
| 扣费顺序 | 只有 gift → paid | 加入 subscription 池，用户可选 4 种 `BillingPreference` |
| 兑换券 | 进 `quota_gift` | 保持进 `quota_gift`（澄清: 非"钱包"，是独立赠送池） |
| 商业用户 tier | — | **商业用户不参与 tier 升组、不购买订阅套餐** |

---

## 一、目标（v3 保留 + 增补）

- **公益属性**：核心用户来自邀请制社群，不做公开注册商业化
- **可持续**：号池成本由自愿贡献者反哺，防止号池被薅穿
- **门槛感**：注册即用没有意义，贡献者要能感受到明显的等级差
- **可控性**：任何单个用户都不能一次性榨干号池，任何"僵尸账号"不能永久占资源
- **[v4 新增]** **商业化通道**：允许下游转售商作为独立身份存在，走完全不同的定价 + 扣费 + 限流规则，与公益 tier 体系互不干扰
- **[v4 新增]** **稳定订阅通道**：为普通付费用户提供"买断 30 天池子"选项，替代"看余额随时可能没"的钱包体验

---

## 二、总览：三体身份系统

用户 `user.group` 是**单值字段**，永远只能落到以下三类之一：

```
┌──────────────────────────────────────────────────┐
│  A. Tier 身份 (公益 + 贡献解锁)                    │
│  free / supporter / fan / bestie / [vip 可开关]   │
│  - 走 quota_gift 池 (签到 + 兑换券)                │
│  - 也可走 quota_paid (贡献充值直接进)              │
│  - 升组机制: total_donated 累计 + 30 天有贡献      │
│  - GGR: {"free": 0.01}，只能路由 free 渠道         │
└──────────────────────────────────────────────────┘
             ↓ 主动购买订阅套餐
┌──────────────────────────────────────────────────┐
│  B. Subscription 身份 (个人付费 + 稳定池)          │
│  group = 'sub' (由 SubscriptionPlan.UpgradeGroup) │
│  - 走 UserSubscription.AmountTotal 独立池          │
│  - 30 天有效期，到期回 PrevUserGroup               │
│  - 可选 fallback 到 quota_paid                     │
│  - GGR: 中档折扣 (待定，见 §五)                    │
└──────────────────────────────────────────────────┘
             ↓ admin 手动分配 (合同/大额订单)
┌──────────────────────────────────────────────────┐
│  C. Commercial 身份 (下游供应商)                   │
│  retail / wholesale / wholesale-plus              │
│  - 只走 quota_paid                                 │
│  - GGR: 商业池折扣 (18 项渠道详见 GroupGroupRatio)│
│  - 不参与 tier 升组、不购买订阅                    │
│  - 手工 admin 分配，非自动进入                     │
└──────────────────────────────────────────────────┘
```

**关键规则**：

1. **A ↔ C 互斥**：商业用户 (`user.group ∈ CommercialGroups`) 的充值**不累加 total_donated**、**不触发 tier 升组**；返回 A 需要 admin 手工改组
2. **A ↔ B 兼容 (临时)**：Tier 用户购买订阅 → group 临时改 `sub`；到期回退到订阅前的 tier (`PrevUserGroup`)
3. **B ↔ C 互斥**：商业用户不能购买订阅套餐（前后端双拒）；已是 `sub` 的用户不能被自动升成商业档

---

## 三、三种资金池

用户账户里三笔独立 quota，按不同规则消耗：

| 池 | 字段 | 来源 | 上限 | 时效 | 备注 |
|---|---|---|---|---|---|
| **赠送池** | `User.QuotaGift` | 每日签到 + 兑换券 | $50 | 30 天无请求清零 | 独立，UI 应展示为 "Gift Balance" 而非 "Wallet" |
| **订阅池** | `UserSubscription.AmountTotal - AmountUsed` | 购买订阅套餐 | 套餐配置 | 30 天/按 plan | 到期归零；套餐可续 |
| **付费钱包** | `User.Quota` | 贡献充值 (Pancake) + 标准充值 | 无 | 永久 | 传统钱包，A/B/C 都可用 |

### 3.1 扣费顺序 (关键)

由用户 `UserSetting.BillingPreference` 决定，四种模式，两级路由：

**第一级：资金来源选择** (`service/billing_session.go` 里的 4 个 switch case)

| 模式 | 尝试顺序 | 何时用 |
|---|---|---|
| `subscription_first` **(默认)** | 有 sub → sub → wallet (当 `allow_wallet_overflow=true`) | 有订阅的用户默认 |
| `wallet_first` | wallet → sub | 想优先烧钱包保留订阅额度 |
| `subscription_only` | sub → 429 | 卡死订阅池，不动 wallet |
| `wallet_only` | wallet → 429 | 卡死 wallet，不动 sub |

**第二级：wallet 内部 gift-paid 组合** (`model/user.go decreaseUserQuota`)

`wallet` 这条路径 **天然是 gift-first-then-paid**，无关 `BillingPreference`：
1. 先看 `quota_gift`（今日剩余额度 = `min(quota_gift, GiftDailyLimit[user.group] - gift_quota_used_today)`）
2. 不够再扣 `quota_paid`
3. 都空 → 返回 `ErrInsufficientUserQuota`

**结论**：`wallet_only` **仍然吃 gift 池**（这是 v3 遗留设计的合理延续，因为 gift 属于 wallet 的一个子池）。用户想彻底跳过 gift 池，唯一方式是等 gift 日限用完/池子清空。这个语义 v4 保留。

**完整决策树** (修正版)：

```
请求进入
  ↓
[BillingPreference 检查]
  ├─ wallet_only        → tryWallet (=gift优先+paid回落) → 429
  │                        任何时候都不查 subscription
  │
  ├─ subscription_only  → trySubscription → 429
  │                        任何时候都不查 wallet (包括 gift)
  │
  ├─ subscription_first → 有 sub ? → trySubscription
  │  (默认)              │           ├─ 成功 → 完成
  │                     │           └─ 不够 + allow_wallet_overflow → tryWallet → 429
  │                     │
  │                     └─ 无 sub → tryWallet → 429
  │
  └─ wallet_first       → tryWallet
                          ├─ 成功 → 完成
                          └─ 不够 → trySubscription → 429

其中 tryWallet 内部:
  gift 池今日剩余额度 → 扣 gift → 不够 → 扣 paid → 都空 → 429
  gift 日限 = CheckinAwardByGroup[user.group] (free/supporter/... 各不同, v4 §4.2)
```

**为什么不给 `wallet_only` 添加"跳过 gift"选项**：
- Gift 池是**免费额度**，用户没花钱买，视觉上等于"wallet 里的一部分"
- 添加"skip gift"会让运营侧多一个语义要维护（"用户是否想跳过 gift"），没实际价值
- 用户如果想强制走 paid，可以先把 gift 用完 (等日限或让池子清零)，或者临时切成 `subscription_only`

### 3.2 赠送池日限 (v3 保留)

`quota_gift` 每日消耗上限 = 该 tier 的 `CheckinAwardByGroup` 值：

| Tier | 日限 (= 每日签到额) |
|---|---|
| free | $0.5 (实际无赠送，值不生效) |
| supporter | $0.5 |
| fan | $3 |
| bestie | $5 |
| vip (关闭中) | $10 |

触发日限 → 停止从 `quota_gift` 扣，切到 `quota_paid` (或按 BillingPreference)。

### 3.3 订阅池的日限

**v4 决议**：订阅池**无日限**。用户买 30 天池子，自己决定烧节奏。如需限流靠 RPM + 并发。

---

## 四、Tier 系统 (公益 A)

### 4.1 Tier 表

| Tier | 门槛 (total_donated) | RPM | 并发 | 每日签到 | 可用模型分组 |
|---|---|---|---|---|---|
| free | 拥有欢迎码 (无门槛) | 10 | 1 | 无 | basic |
| supporter | ≥ $10 + 30d 有贡献 | 20 | 3 | $0.5 | basic + premium |
| fan | ≥ $50 + 30d 有贡献 | 30 | 5 | $3 | basic + premium |
| bestie | ≥ $100 + 30d 有贡献 | 40 | 8 | $5 | basic + premium + flagship |
| **vip (关闭中)** | ≥ $500 + 30d 有贡献 | 60 | 15 | $10 | basic + premium + flagship |

**vip 关闭机制**：新增 option `tokensheep_economy.disabled_tiers = {"vip": true}`
- `TierForDonation()`: 遍历 `TierThresholds` 时跳过 `disabled_tiers[name]==true`
- `TierCardsSorted()`: 同上跳过
- 已经在 `vip` 组的老用户: 不动 group、不降组；但重算 tier 时会命中下一档 (bestie)，daily cron 会把他们降到 bestie

**RPM/并发调整 (相对 v3)**：v4 把 bestie 从 `50/8` 调整为 `40/8`，vip 从 `120/15` 调整为 `60/15`（vip 关闭，无实际影响）。

### 4.2 每日签到 (v3 保留)

- 用户主动 `POST /api/user/checkin`
- free 组直接返回错误 "升级 tier 才能签到"
- 其他 tier 得到对应额度 → `quota_gift += 额度`
- 单账户 `quota_gift` 封顶 $50，超封顶签到失败提示"赠送池已满"

### 4.3 兑换券 (v3 保留 + UI 澄清)

- 单一类型 "入群欢迎码"，`quota_gift += $2`
- 单账号一次
- 受 $50 封顶约束
- **不动 quota_paid、不动 group、不累加 total_donated**

**v4 UI 修正**：钱包页 / 兑换页面**必须清楚展示 "Gift Balance" 是独立池，非钱包**。文案:
- Redemption 卡: "兑换码进入 Gift Pool (独立赠送池)，不进钱包"
- Wallet 页: Gift 独立那格加 subtitle "免费额度，$50 上限，30 天无请求清零，扣费优先"

### 4.4 升组 (v3 保留 + 商业档保护)

**贡献 webhook 触发** (`model/topup.go:704`)：
```pseudo
if user.group NOT in CommercialGroups:
    user.total_donated += quotaToAdd
    new_tier = TierForDonation(user.total_donated)
    if new_tier != user.group and user.role < admin:
        user.group = new_tier
else:
    # 商业用户: 不累加 total_donated, 不动 group
    pass
```

**⚠️ 当前代码 bug (B2)**：不管商业不商业都累加。修法见 §八 bug 清单。

### 4.5 降组 (v3 保留 + 商业档保护)

**每日 0 点 cron** (`model/tokensheep_maintenance.go`)：
```pseudo
for user in users:
    if user.group in CommercialGroups:
        continue  # 商业档不动
    last_30d = sum(top_ups.money where user_id=? and created_time > now-30d)
    if last_30d == 0:
        new_group = 'free'
    else:
        new_group = TierForDonation(user.total_donated)
    user.group = new_group
    if now - user.last_request_at > 30 days:
        user.quota_gift = 0
```

**⚠️ 待验证 (B9)**：当前 cron 是否已经跳过 CommercialGroups？如果没有，bug。

---

## 五、Subscription 系统 (个人付费 B)

### 5.1 套餐结构

`SubscriptionPlan` 表 (`model/subscription.go:146`)：

| 字段 | 语义 |
|---|---|
| `PriceAmount` | 售价 |
| `TotalAmount` | 池子总额度 (quota units) |
| `DurationValue + DurationUnit` | 有效期 |
| `UpgradeGroup` | 买入后 user.group 临时改成什么 (默认 `sub`) |
| `DowngradeGroup` | 到期改成什么 (empty = revert to PrevUserGroup) |
| `AllowWalletOverflow` | 订阅额度用完能否 fallback 到钱包 |
| `QuotaResetPeriod` | 池子重置周期 (never = 一次性；monthly = 月度自动补) |

### 5.2 套餐种子值 (参考截图)

| 套餐名 | 售价 | 池子额度 | 有效期 | UpgradeGroup | Overflow |
|---|---|---|---|---|---|
| 通用订阅 A | ¥100 | ¥110 (+10%) | 30d | sub | ✅ |
| 通用订阅 B | ¥200 | ¥220 (+10%) | 30d | sub | ✅ |
| 通用订阅 C | ¥400 | ¥440 (+10%) | 30d | sub | ✅ |

（具体数值可后台改）

### 5.3 `sub` 组自身的属性

`sub` 也是一个用户组，需要配 shield/GGR/RPM/session：

| 属性 | 建议值 (待确认) |
|---|---|
| `session_limits.sub` | 10 |
| `ModelRequestRateLimitGroup.sub` | [100, 100] |
| `GGR.sub` | 中档折扣，比如 `{"aws-q": 0.30, "claude-lowprice": 0.15, "mix-lowprice": 0.10, "GPT-Pro": 0.40, "gemini-lowprice": 0.20}` |
| shield: 见到哪些渠道 | 商业池子集，不含 GPT-Enterprise/claude-max-stable 等企业专线 |

### 5.4 到期回退 (v4 澄清)

`SubscriptionExpiryDowngrade` (`model/subscription.go:1195`)：
- 优先用 `DowngradeGroup` (plan 里配的)
- 否则 revert 到 `PrevUserGroup` (购买时快照)
- **前提**: 当前 group 仍是 `UpgradeGroup` (证明订阅还在生效)

**⚠️ B12**：订阅期间 (`sub` 组) 用户又贡献充值，`total_donated` 累加 → `TierForDonation()` 可能返回更高档 → **但代码里贡献 webhook 会强制改 group** (`model/topup.go:722`)，把 `sub` 覆盖成 tier 名，订阅到期时 group 已不是 `sub`，`SubscriptionExpiryDowngrade` 不会触发回退。

**v4 决议**: 订阅期间的贡献充值 → 累加 `total_donated`（正常），**但不改 `user.group`**（保留 `sub`）；订阅到期后再由 daily cron 按最新 `total_donated` 重算 tier。这需要改 `model/topup.go`。

### 5.5 商业用户禁购订阅

`controller/subscription*.go` 订阅创建入口加：
```go
if tokensheep_setting.CommercialGroups[user.Group] {
    return error("商业用户请通过合同调整档位，不参与订阅套餐")
}
```

前端订阅页对商业用户显示 banner + disable 按钮。

---

## 六、Commercial 系统 (下游 C)

### 6.1 分组

| Group | 门槛 (哨兵) | RPM | 并发 | 可见渠道 |
|---|---|---|---|---|
| retail | 999999999999 | [200, 200] | 30 | 7 项 (GPT-Enterprise, GPT-Pro-Stable, claude-lowprice, gemini-lowprice, grok-sale, grok-supporter, mix-lowprice) |
| wholesale | 999999999999 | [800, 800] | 50 | 14 项 |
| wholesale-plus | 999999999999 | [1000, 1000] | 100 | 14 项 (比 wholesale 单价更低) |

### 6.2 GGR 单价 (完全由 admin 谈判决定，代码不干预)

见当前 `GroupGroupRatio` option 中 `retail/wholesale/wholesale-plus` 三个 map。

### 6.3 入组流程

**只有 admin 手工分配**。无自动升级路径。

前端 tier / subscription 页面对商业用户显示 banner: "您已是商业档 (retail/wholesale/wholesale-plus)，如需调整请联系管理员"。

### 6.4 商业用户充值流程

1. 走 Standard top-up 入口
2. `quota_paid += 充值额`
3. **不累加 `total_donated`**
4. **不触发 tier 升组判定**
5. **不影响 group**

---

## 七、关键 Option Key 索引

| Option Key | 类型 | 作用 | 备注 |
|---|---|---|---|
| `tokensheep_economy.tier_thresholds` | `map[string]int` | tier 门槛 (含商业档哨兵) | v4 新增: 加 `disabled_tiers` 过滤 |
| `tokensheep_economy.session_limits` | `map[string]int` | 每组并发上限 | 全 group |
| `tokensheep_economy.checkin_award_by_group` | `map[string]int` | 每日签到额 = gift 日限 | tier only |
| `tokensheep_economy.disabled_tiers` **(v4 新增)** | `map[string]bool` | 关闭某 tier (UI 隐藏 + 升组跳过) | 用于 vip 开关 |
| `tokensheep_economy.commercial_groups` **(v4 新增，替换硬编码)** | `map[string]bool` | 商业档名单 | 目前硬编码在 `setting/tokensheep_setting/economy.go:110`，v4 迁移到 option |
| `ModelRequestRateLimitGroup` | `map[string][2]int` | 每组 RPM | 全 group |
| `GroupRatio` | `map[string]float64` | 组名注册表 (fallback 倍率) | 全部 = 1 |
| `GroupGroupRatio` | `map[string]map[string]float64` | 用户组 × 渠道组单价 | 核心定价 |
| `GroupSpecialUsableGroup` (flat) | `map[string]map[string]string` | 屏蔽表 (UI 兼容) | 值不影响逻辑 |
| `group_ratio_setting.group_special_usable_group` (dotted) | 同上 | 代码读的权威源 | in-memory |
| `UserUsableGroups` | `map[string]string` | 顶层可见渠道全集 | 15 项 |
| `AutoGroups` | `[]string` | 自动路由候选 | 10 项 |
| `subscription_plans` | 表 `subscription_plans` | 套餐配置 | admin CRUD |

---

## 八、Bug / 待修清单

### 🔴 高优先

| # | 位置 | 现状 | 期望 |
|---|---|---|---|
| **B1** | `setting/tokensheep_setting/economy.go:110` | `CommercialGroups` 硬编码缺 `wholesale-plus` | 加 `"wholesale-plus": true`，或迁到 option `commercial_groups` |
| **B2** | `model/topup.go:704` | 所有充值累加 `total_donated` + 重算 tier | 商业用户跳过累加和重算 |
| **B7** | `docs/spec/economy-model.md` | v3 已过时 | 本文件即为 v4 |
| **B10** | `model/topup.go:704` vs Standard top-up 前端文案 | 文案说不累加，代码累加 | 或改代码或改文案 (v4 决议: 商业用户不累加，社群用户累加) |

### 🟡 中优先

| # | 位置 | 现状 | 期望 |
|---|---|---|---|
| **B3** | `controller/subscription*.go` | 商业用户可购订阅 | 拒绝 |
| **B4** | tier_cards | vip 硬活着 | `disabled_tiers.vip = true` 开关 |
| **B5** | `service/billing_session.go:369` | 4 种 pref 只讲 sub vs wallet，未涵盖 gift | ✅ 代码本来正确 (gift 嵌在 wallet path 里)，spec §3.1 已修正为"两级路由" |
| **B9** | `model/tokensheep_maintenance.go` | 降组 cron 是否跳过商业档待验证 | 必须跳过 |
| **B12** | `model/topup.go:722` + subscription 到期 | 订阅期间贡献充值会覆盖 `sub` group | 订阅期间贡献只累加 total_donated，不改 group |
| **B13** | `service/billing_session.go:426` | `wallet_only` 是否吃 gift? spec 未定 | v4 决议: `wallet_only` = 只 quota_paid，跳过 gift/sub |
| **B14** | `service/tokensheep_daily_task.go` | 30d 无请求清零 gift，订阅池是否也清? | v4 决议: 订阅池不清 (它有自己的 expiry) |
| **B15** | `TierForDonation` | vip 关闭后老 vip 用户走什么 | v4 决议: daily cron 会重算成 bestie，group 自动降 |

### 🟢 低优先 (UI)

| # | 位置 | 现状 | 期望 |
|---|---|---|---|
| **B6** | Redemption Code 卡 + wallet stats | "gift balance" 未解释 | 加文案 + 视觉分区，说明是独立池 |
| **B16** | Add Funds 卡片 | 单卡不区分 tier / standard 充值 | 拆双卡: "贡献充值 (进 total_donated + 触发 tier)" vs "标准充值 (只进钱包)" |
| **B17** | `PUT /api/option/` 只更新命中容器, blue/green in-memory drift | 加 Redis pub/sub 广播 (`common/option_broadcast.go`), 写者 publish 到 `newapi:v1:option-update`, 兄弟节点 subscribe 后本地 apply. NodeID 过滤 self-echo. Redis 关掉时天然降级为单节点行为. 发现于 Round 5 生产实测 v4 `disabled_tiers` 开关 |

### 🎯 R3 收尾 (Round 7, 2026-08-30)

| # | 议题 | 结果 |
|---|---|---|
| **R3-2** | GGR / AutoGroups 里的死码渠道 | ✅ 生产已清: `wholesale` / `wholesale-plus` 里删除 `GPT-Plus / gpt-lowprice / claude-supporter` 三个 0-enabled-channel entry (原来商业档 GGR 挂着但对应渠道全部 disabled). `AutoGroups` 里下线 `claude-sale / gemini-official` (0-channel, 命中就 500). 商业档 GGR 其他值原样, super-grok 保留(单独测). 备份 `bak-20260830-005351-r3-2-deadcode`. 通过 B17 pub/sub 两容器一次同步 |
| **R3-2b** | 商业用户看订阅页塌陷成单栏 | ↩️ **reverted (2026-08-30)**. 起初改成 banner "Commercial tier — contract negotiated" 显示, 用户反馈: **贡献解锁 Tier 卡本身就是订阅套餐**（tier 是订阅, 一次贡献解锁 tier + RPM/并发/日赠, 归 tokensheep 的经济模型 §四）; 单独的 SubscriptionPlansCard 只是 upstream new-api 保留的 30-day 订阅池, 在 TokenSheep 场景下 `subscription_plans` 表本来就空. 让它 `return null` 保持不显示才对. `wallet/index.tsx` 里 `useQuery(['my-tier'])` 也一并撤 (tier-card.tsx 仍消费同一 cache, 无孤儿) |
| **R10** (tier UI 完整对齐) | 3 处漂移 | ✅ (a) tier-card i18n `perks` 硬编码 v3 时代的 RPM 50/vip RPM 120/vip \$10, 跟生产 (40/60/\$1.6) 脱节 → 后端 `controller/topup.go enrichedTierCards()` 把 live RPM+并发+dailyGiftUSD 塞进 `tier_cards` payload, 前端从 API 拿实值渲染 (i18n perks 只做 backfill). (b) tier-card 标题从 "贡献解锁 Tier" 改为 "订阅套餐"/"Subscription Plans", 副标题解释一次贡献解锁 tier 永久生效. (c) BillingPreference 4-way 选择器过去只在 SubscriptionPlansCard 里, R9 让它 return null 之后用户看不到, R10 新增 `BillingPreferenceCard` 单独一张卡渲染在 tier ladder 下方, 4 个选项每个有描述文案说明订阅池/wallet/gift 的扣费顺序 |
| **R3-3** | default 组无 RPM/session_limit 兜底 | ✅ 生产已配 (`session_limits.default=1`, `RPM.default=[10,10]`, 备份 `bak-20260830-005351-round3-default-backfill`). Seed 也补 `default:1` + `promo:2` 保护冷启动 (values 与线上一致, 无 restart 影响) |
| **R3-4** | flat vs dotted GSU 语义反向 | ⚠️ 已文档化, 未完整对齐. 扁平 key `GroupSpecialUsableGroup` 199 条 value 全空串, 点分 key `group_ratio_setting.group_special_usable_group` 同 199 条全是 `remove`. 骨架一致, 语义看似反向. 完整对齐需要 grep in-memory var 双写路径 + 敲定权威源, 单独立项. 当前:代码路径以 dotted (in-memory reflect) 为权威, 扁平留作 UI 兼容 |
| **R3-5** | ledger 无 governance drift 报警 | ✅ operator-ledger 新加 `internal/pricing/governance_diff.go` (DiffGovernance + LogGovernanceDrift), scheduler.captureOne 和 syncPricingStation 都挂钩. `grep 'governance drift'` 抓漂移. 首次快照不报警(避免假警报), key 顺序不同不报警(admin panel 重序列化). 生产已验证 |
| **R3-6** | RPM 单表混装用户组 + 渠道组 | 🟡 dropped 本轮. 拆表影响 setting/middleware/controller/前端 6 处 ~250-300 行, 带来"限流分了 usable_groups 权限没分"的错位. 现网混装能跑, 只是 admin 面板视觉乱. 单独立项到 tokensheep-newapi 处理 (只做前端分区展示更简单) |
| **Add Funds 双卡** | UI 现状已满足 B16 | ✅ 前端已双卡分离 (`TokensheepTierCards` 与 `RechargeFormCard` 独立渲染, tier 卡走 `WAFFO_PANCAKE_TIER-` prefix, 标准卡走 `WAFFO_PANCAKE-`). 4 条 UI 微调 (grid-cols-2 / chip 化 / hint 加 total_donated / 中间确认) 边际收益低, 不进本轮 |

### 验证清单

- [ ] B9: 查 daily cron 是否跳过商业档
- [ ] B11: 验证 redemption **不** 累加 `total_donated` (v3 spec 说不动，需要代码确认) — **已确认 (`model/redemption.go:206` 只更新 `quota_gift`)**
- [ ] B12: 订阅期间贡献充值路径完整跑一遍

---

## 九、扣费顺序完整决策树 (v4 权威版)

```
[请求进入]
    ↓
[Auth + RPM + Session 中间件通过]
    ↓
[pref = user.setting.billing_preference]
    ↓
┌── wallet_only ───┐
│  尝试 quota_paid  │─→ 够 → 扣 → 完成
│                   │─→ 不够 → 429 (不 fallback)
└───────────────────┘

┌── subscription_only ─┐
│  尝试 sub pool       │─→ 够 → 扣 → 完成
│                      │─→ 不够 → 429
└──────────────────────┘

┌── wallet_first ──────────────────┐
│  尝试 quota_paid → 够 → 扣 → 完成 │
│                  ↓ 不够            │
│  尝试 sub pool → 够 → 扣 → 完成    │
│                ↓ 不够              │
│  尝试 quota_gift (未触日限) → 扣   │
│                            ↓ 触限   │
│  429                              │
└──────────────────────────────────┘

┌── subscription_first (默认) ───────┐
│  有活跃 sub?                       │
│  ├─ 是 → sub pool → 够 → 扣 → 完成  │
│  │       ↓ 不够 + allow_overflow   │
│  │       quota_paid → quota_gift   │
│  │       ↓ 全空 → 429              │
│  │       ↓ 不够 + !allow_overflow  │
│  │       429                       │
│  └─ 否 → quota_gift → quota_paid   │
│         ↓ 全空 → 429               │
└────────────────────────────────────┘
```

**quota_gift 日限**：任何路径命中 `quota_gift` 时先查 `today_gift_used < CheckinAwardByGroup[tier]`，超了就跳过 gift 层。

---

## 十、UI 归位 (v4 目标)

### 10.1 钱包页

四栏并排（**视觉分区加强**）：
- **付费钱包 Wallet** (大号 · 主色 · 图标 wallet)
- **赠送池 Gift Pool** (小号 · 灰色 · 图标 gift · subtitle "免费额度，$50 上限，30 天无请求清零")
- **今日消耗 Usage** (数据)
- **API 请求 Requests** (数据)

### 10.2 兑换页

Redemption Code 卡文案:
```
兑换码 → 赠送池 (Gift Pool)
不进钱包。赠送池是独立的免费额度，每日消耗有上限
($X/day per tier)，30 天无请求自动清零。
```

### 10.3 贡献 unlock tier 页

**只展示活跃的 tier**（`disabled_tiers` 排除后 + 商业档排除后）。v3 时代 5 张卡（supporter/fan/bestie/vip/wholesale-plus 哨兵），v4 修完只显示 3 张（supporter/fan/bestie）。

商业用户访问此页 → 显示 banner: "您已是商业档，充值不影响 tier。如需调整档位请联系管理员。"

### 10.4 订阅页

商业用户访问此页 → 显示 banner + disable "立即订阅" 按钮。

### 10.5 Add Funds 页 (拆双卡)

- **Card A: 贡献充值 (Contribution)**
  - 3 档 Pass: Supporter $10 / Fan $50 / Bestie $100
  - 计入 total_donated，可触发 tier 升组
  - 商业用户不可见此卡
- **Card B: 标准充值 (Standard Top-up)**
  - 100/200/500/1000 金额选择
  - 只进 quota_paid，不计入 total_donated
  - 所有用户可见

---

## 十一、v4 决策日志

| 日期 | 决策 |
|---|---|
| 2026-08-29 | Draft v4 启动，覆盖 v3 |
| 2026-08-29 | vip 层用 `disabled_tiers` 开关，不删配置 |
| 2026-08-29 | 商业档 `retail/wholesale/wholesale-plus` 独立体系，不参与 tier 升组 |
| 2026-08-29 | 商业用户禁购订阅套餐 |
| 2026-08-29 | 兑换券**明确**进 `quota_gift`（不进钱包），UI 强化说明 |
| 2026-08-29 | 商业用户 top-up **不** 累加 `total_donated`、不触发升组 |
| 2026-08-29 | 订阅期间贡献充值只累加 `total_donated`、不改 group |
| 2026-08-29 | `wallet_only` 语义 (修正): 只跳过 subscription; gift 池是 wallet 的子池, 仍然优先扣。想强制走 paid 需切 subscription_only 或等 gift 用完 |
| 2026-08-29 | 订阅池不受 30d 无请求清零约束（有自己的 expiry） |
| 2026-08-29 | `CommercialGroups` 从硬编码迁到 option `commercial_groups` |
| 2026-08-29 | RPM 调整: wholesale 300→800, wholesale-plus 2000→1000, bestie 50→40, vip 120→60 (关闭) |
| 2026-08-30 | 加 Redis pub/sub 跨容器 option 广播 (B17): 修 blue/green in-memory drift, 无需重启即可让 v4 治理开关在整个集群一致生效. Redis 断开天然降级 |
| 2026-08-30 | R3 收尾: (R3-2) 清 GGR 里 3 个 0-channel 死码 + AutoGroups 里 2 个 0-channel; (R3-2b) 商业用户订阅页银行卡替代 return null; (R3-3) default+promo 补 SessionLimits seed; (R3-5) ledger governance_diff.go 落地, `grep 'governance drift'` 抓漂移; (R3-4) flat/dotted GSU 已文档化未完整对齐; (R3-6) RPM 拆表 dropped 本轮 |
| 2026-08-30 | R3-2b **reverted**: 澄清 tier 卡就是订阅套餐, 独立的 SubscriptionPlansCard 是 upstream 剩下的 30-day 订阅池概念, TokenSheep 场景下应保持 `return null` 不显示 (`subscription_plans` 表本来就空). 撤 `isCommercial` prop + wallet/index.tsx 的 useQuery. 用户在 image #23 明确标示 |
| 2026-08-30 | R10: tier-card perks 从 i18n 硬编码转 live server payload (RPM/concurrency/dailyGiftUSD), 标题改 "订阅套餐", 新增 BillingPreferenceCard 独立卡曝光 4-way 扣费优先级. 用户 image #24 标出 v3 时代 i18n 值 (RPM 50/vip \$10) 跟生产 (40/\$1.6) 不一致 + 找不到扣费优先级选项 |
| 2026-08-30 | R11: 修 R10 的 3 处 shipping bug: (a) i18n key 写到 JSON 顶层而不是 `translation` namespace, 前端渲染 raw key (image #25 中 wallet.billingPreference.title/subtitle 直接显示 key 名); (b) 旧 wallet.tierCards.title/perks 保留在 translation.* 里遮住新值; (c) BillingPreferenceCard 放在 tier ladder 和 Add Funds 之间视觉不连贯 — 移到 wallet balance card 正下方, 跟它控制的池并列. python 迁 18 个 key 到 translation namespace + dedupe |
| 2026-08-30 | R12: 再补 2 处漏掉的 i18n: (a) BillingPreferenceCard 下拉 trigger 用默认 `<SelectValue />`, 显示 raw enum 值 `subscription_first` 而不是 label (image #27) — 改成显式 `<SelectValue>{labelFor(...)}</SelectValue>`; (b) WalletStatsCard 的 gift pool description 硬编码英文 "Separate from paid wallet..." (image #28) — 补 zh / zh-TW 翻译, 中英同时 self-map 以对齐 source-string-as-key i18n pattern |
| 2026-08-30 | R13: BillingPreferenceCard 对商业用户 (retail / wholesale / wholesale-plus) 完全隐藏. 商业档不参与订阅池, 显示带"无生效"标签的选项 (image #29) 读起来像 UI 坏了. 组件加 isCommercial prop 直接 `return null`, wallet/index.tsx 复用 tier-card 的 `useQuery(['my-tier'])` cache 拿 `MyTierView.commercial` 传下去 |

---

## 十二、v3 遗留决策 (保留)

见 `economy-model.md` 底部"决策日志"，所有 v3 的 2026-07-06 决议 v4 默认继承，除非 v4 明确覆盖。
