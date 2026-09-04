# tokensheep 业务模型 vs 100b 现状对照

> **产出日期**：2026-09-04
> **前置阅读**：`docs/spec/business-model-explained.md`（tokensheep 业务模型讲解）
> **本文目的**：把 tokensheep 的四块拼图逐一对照 100b 已有的实现，回答"100b 遇到同样的问题吗？现有方案能不能覆盖？"最后才给出**分级提议**（值得引入 / 不值得引入 / 需要先讨论）。
> **写作纪律**：先讲事实（100b 有什么），再讲问题（100b 现有方案覆盖 tokensheep 的问题吗），最后才是提议。不设"必须移植"的默认立场。

---

## 0. TL;DR

100b **不是白纸**。它已经有：**消费驱动的 Tier 0–4 体系、完整的订阅系统、BillingSession + FundingSource 抽象、tier 感知的 RPM/并发限流、返利结算 cron**。

它**没有**：赠送池（gift pool）、捐赠字段（`total_donated`）、商业档隔离（`CommercialGroups`）、系统级并发上限（`SystemConcurrency`）、每日签到额度（有签到但直接进钱包）、订阅期间 group 保护逻辑。

**tokensheep 的四块拼图跟 100b 的对应关系不是"缺 vs 补"，而是"两种不同世界观并存"：**

| 维度 | tokensheep | 100b |
|---|---|---|
| Tier 触发机制 | **捐赠驱动**（`total_donated` 累加） | **消费驱动**（`used_quota` 累加） |
| Tier 表达 | `user.group` 字段（就是 tier） | `user.group` + `user_tier_ext.tier` 双字段 |
| 赠送池 | 独立 `QuotaGift` 池 | ❌ 单钱包，签到/兑换券都进 `User.Quota` |
| 商业档 | `CommercialGroups` 概念，签到/兑换/tier 全禁 | ❌ 靠 admin 手工改 group |
| 订阅到期回归 | 有 `PrevUserGroup` 快照 + 订阅期 group 保护 | 有 `PrevUserGroup` 但订阅期没保护 |
| 系统并发上限 | 全站 in-flight 上限 | ❌ 只有 per-user in-flight |
| 30 天不活跃降级 | 有 daily cron | ❌（tier 靠"7 天日均消费"，机制不同） |

**核心洞察**：**两边都在解决"防止号池被薅穿"，但用的是不同世界观**——tokensheep 是"捐得多 = 有资格用得多"，100b 是"用得多 = 服务得更好"。这是**两种商业模式**，不是"高低版本"。

---

## 1. 用户身份系统：单字段 vs 双字段

### tokensheep 怎么做
- **`user.group` 就是身份的全部**：`free/supporter/fan/bestie/vip/sub/retail/wholesale/wholesale-plus`
- 一个字段承载：tier 等级 + 商业分类 + 订阅状态
- **单值互斥**：不能同时是 fan 和 wholesale

### 100b 怎么做
- **双字段分离**：`user.group`（`default/vip/wholesale/internal/...`）+ `user_tier_ext.tier`（0-4 整数）
- Group 决定：定价、可用渠道、topup 单价、returned RPM 配置
- Tier 决定：RPM 上限、并发数、返利资格（volume rebate、invite rebate）
- **两者独立**：`default` 用户可以是 Tier 3，`wholesale` 用户可以是 Tier 0

### 100b 的方案覆盖了 tokensheep 的问题吗？

tokensheep 用 `user.group` 单字段解决的问题：
1. **"用户在系统里的身份是什么"** → 100b 用 `group + tier` 双字段解决，**更灵活**
2. **"这个身份对应什么权限"** → 100b 通过 `TierAwareRateLimits`/`TierAwareMaxSessions` 解决，**覆盖了**
3. **"权限怎么获得"** → tokensheep 靠捐赠、100b 靠消费，**世界观不同**

**100b 的双字段设计其实更好**：`group` 管商业维度、`tier` 管等级维度，正交而不是塞进一个字段。tokensheep 的 `user.group ∈ {fan, wholesale}` 互斥就是因为塞不下才互斥。

### 结论
**这一块不用移植**。100b 的双字段设计比 tokensheep 的单字段更清晰。如果 tokensheep 迁到 100b，反而应该把 tokensheep 的 `group=fan` 拆成 `group=default + tier=fan`。

---

## 2. 赠送池（quota_gift）：独立池 vs 单钱包

### tokensheep 怎么做
- **独立字段** `User.QuotaGift`，上限 $50
- **入口**：每日签到 + 兑换券（明确进 gift 池，不进钱包）
- **每日消耗上限** = 该 tier 的签到额度（fan 组 $3/天）
- **30 天不活跃清零**
- **UI 强制视觉分区**：钱包和 gift 分开显示

### 100b 怎么做
- **单字段** `User.Quota`
- 签到（有）→ 直接进 `Quota`
- 兑换券（有）→ 直接进 `Quota`
- **没有** gift 池、没有日消耗上限、没有过期清零

### 100b 的方案覆盖了 tokensheep 的问题吗？

tokensheep gift 池解决的问题：
1. **入门体验**：新用户不花钱能试 → 100b 用同样的签到/兑换券解决，只是**没有池的隔离**
2. **公益激励**：签到得的钱能感受到"每天有"，不会跟大额钱包混在一起 → 100b **不能解决**（合并在 `Quota` 里，用户感受不到"今天新到账 $3"）
3. **僵尸账号清理**：30 天不活跃清零 gift → 100b **不能解决**（`Quota` 是永久的）
4. **防高 tier 用户在低价渠道薅羊毛**：日限约束今天最多能烧多少 gift → 100b **不能解决**（`Quota` 可以一天烧光）
5. **可控成本**：所有免费额度加起来最多 $50 → 100b 靠"签到额度小 + admin 干预"解决，没有硬上限

### 结论
**这一块需要讨论**。gift 池是 tokensheep 的**核心运营工具**（8 个月踩坑迭代出来的），但它服务的是"公益站每天送用户免费额度"这个特定商业模式。100b 如果不做公益站，`Quota` 单池就够；如果 100b 也要做"每日签到 + 免费额度控制"，那需要引入 gift 池。

**触发引入的信号是"运营方向是不是要靠免费额度拉新和留存"**，不是"技术上要不要"。

---

## 3. 捐赠字段（total_donated）与 Tier 升级触发

### tokensheep 怎么做
- **触发**：每次充值 webhook → `total_donated += 充值额`
- **升级判定**：`TierForDonation(total_donated)` 查阈值表
- **降级判定**：daily cron 检查"近 30 天充值总额 == 0" → 降到 `free`
- **持续性要求**：门槛不是永久，30 天不捐必降级

### 100b 怎么做
- **无 `total_donated` 字段**（充值只累加 `Quota`）
- **Tier 升级触发**：`used_quota` 达到阈值 + 7 天日均消费达标（`service/tier_service.go`）
- **Tier 降级触发**：连续 N 天日均消费 < 门槛 → 降级
- **触发时机**：每次 API 消费的 async hook（`OnUserConsume`）

### 100b 的方案覆盖了 tokensheep 的问题吗？

tokensheep 捐赠模型解决的问题：
1. **持续激励付费**：不是"充一次终身特权"，是"要持续付才有特权" → 100b **也解决了**，只是激励的是"消费"而非"充值"
2. **量化贡献**：能看到用户累计付了多少 → 100b 用 `used_quota` 表达，语义不同但都是累计数字
3. **门槛+活跃度双条件**：达到阈值 + 30 天活跃 → 100b 是"阈值 + 7 天日均"，机制更严格

**核心差异是"用哪个指标衡量用户价值"**：
- tokensheep：`total_donated`（捐了多少钱） → 适合公益站
- 100b：`used_quota`（消费了多少 quota） → 适合商业站

**两者不能简单替换**，因为公益站可以"充了钱不用"（就是纯捐赠），商业站不行。反过来商业站的"用得多"跟"捐得多"在公益语境下也不等价（有人小额充值但重度使用，公益语境下他没贡献多少但消费多）。

### 结论
**这一块不能直接引入到 100b**，因为它跟 100b 的商业世界观冲突。如果 100b 也要做公益站，那需要把 tier 触发从"消费"改成"充值累加"，这是**根本性改动**，不是"补丁式引入"。

**但可以借鉴的小点**：tokensheep 的"30 天不活跃就降级"（活跃度=充值）对应 100b 的"7 天日均消费不达标就降级"（活跃度=消费），设计思路完全一致。如果 100b tier 降级机制未来要调整，可以参考 tokensheep 的"充值活跃度"作为**并列指标**（比如"消费 or 充值 任一活跃"）。

---

## 4. 商业档（CommercialGroups）：显式隔离 vs 隐式配置

### tokensheep 怎么做
- **显式的 `CommercialGroups` 配置**（原来硬编码，v4 迁到 option）
- **多点保护**（都读同一个 `CommercialGroups`）：
  - 充值不累加 `total_donated`
  - 充值不触发 tier 重算
  - Daily cron 跳过（不降级）
  - 前端订阅页 disable
  - 前端 BillingPreference 过滤订阅相关选项

### 100b 怎么做
- **没有 `CommercialGroups` 概念**
- `wholesale` / `internal` 只是普通 group 值，没有特殊行为
- Tier 系统会正常给 `wholesale` 用户升 tier（因为 tier 触发是消费，不是身份）

### 100b 的方案覆盖了 tokensheep 的问题吗？

tokensheep 商业档解决的问题：
1. **"合同客户不要被自动升 vip 打乱定价"** → 100b **有部分风险**：wholesale 用户如果消费达到 Tier 4 阈值也会升 Tier 4，可能拿到超出合同的 RPM/并发。**取决于 100b 的实际运营**，如果 wholesale 就是给大客户的标签、不介意他们同时是高 Tier，那不是问题
2. **"合同客户不要被 daily cron 降组"** → 100b **无风险**，100b 没有"group daily cron"
3. **"合同客户不要买订阅"** → 100b **无强制拦截**，但也没人期望 wholesale 用户去买 sub 套餐

### 结论
**部分值得引入，但成本极低**。如果 100b 的 wholesale/internal 用户群体只有几个（合同大客户），加不加 `CommercialGroups` 隔离都行——admin 手工把关就够了。

如果 100b 未来 wholesale 客户变多，或者出现"合同客户消费高被 tier 升级抢走合同价"的实际投诉，**再引入一个简单的 `TierSkipGroups` 配置**（tier 计算时跳过某些 group）就够了，不需要整套 `CommercialGroups`。

**不建议整套搬**，因为 tokensheep 那些保护点（签到/兑换/gift 日限）在 100b 场景下根本不需要（没有 gift 池、100b 的签到直接进钱包无差别）。

---

## 5. 订阅系统

### tokensheep 有的 vs 100b 有的

| 特性 | tokensheep | 100b |
|---|---|---|
| `SubscriptionPlan` 模型 | ✅ | ✅ |
| `UserSubscription` 模型 | ✅ | ✅ |
| `SubscriptionOrder` 支付订单 | ✅ | ✅ |
| Stripe/Creem/Waffo/Epay 支付 | ✅ | ✅ |
| 余额支付（用钱包买订阅） | ✅ | ✅ |
| `BillingSession + FundingSource` 抽象 | ✅ | ✅ |
| `BillingPreference` 4 种模式 | ✅ | ✅ |
| Pre-consume record（幂等） | ✅ | ✅ |
| 订阅到期回退 `PrevUserGroup` | ✅ | ✅ |
| 订阅池 quota 重置（daily/weekly/monthly） | ✅ | ✅ |
| Admin CRUD plans / bind / invalidate | ✅ | ✅ |
| `AllowWalletOverflow` | ✅ | ❓ 需再确认 |
| `DowngradeGroup` | ✅ | ❓ 需再确认 |
| **订阅期间贡献充值不改 group** | ✅ | ❌ 100b 无此保护（也无捐赠概念） |
| **商业用户禁购订阅** | ✅ | ❌ |
| Admin 强制重置某 plan 的所有订阅（版本更新） | ✅ | ❓ 需再确认 |

### 100b 的方案覆盖了 tokensheep 的问题吗？

**订阅系统主体功能：几乎完全覆盖**。100b 有一套完整的、经过生产验证的订阅系统。

**tokensheep 独有的边界处理**（4 项）：
1. `AllowWalletOverflow`：**待确认**——探索报告说 100b 有 `SubscriptionOrder` 但没提这个字段，需要再看
2. `DowngradeGroup`：**待确认**——同上
3. **订阅期间贡献充值不改 group**：这是 tokensheep 特有的 bug 修复（B12），因为 tokensheep 有 `total_donated` 触发的 group 自动改。100b 没有这个自动改路径，所以**天然不存在这个 bug**，不用移植
4. **商业用户禁购订阅**：100b 没有 `CommercialGroups` 概念，见 §4，跟商业档一起决策

### 结论
**主体不用移植**。100b 订阅系统已经很完整。

**可能要补的小项**（先确认 100b 是不是真的没有）：
- `AllowWalletOverflow` 字段：让 admin 决定订阅池用完后能不能自动去扣钱包
- `DowngradeGroup` 字段：让 admin 决定订阅到期后降到哪个 group
- 管理员强制刷新某 plan 所有订阅（版本更新用）

这些都是**小字段级增量**，不是架构级移植。

---

## 6. 并发限制

### tokensheep 4 层
1. 系统全站 in-flight 上限
2. 用户组 in-flight 上限
3. 用户个人 in-flight 上限
4. 会话/token 级限流

### 100b 现有层
1. ❌ 系统全站 in-flight 上限
2. ❌ 用户组 in-flight 上限
3. ✅ 用户个人 in-flight 上限（`ConcurrentLimit` middleware + `TierAwareMaxSessions`）
4. ✅ 会话/token 级限流 + RPM

### 100b 的方案覆盖了 tokensheep 的问题吗？

tokensheep 4 层并发解决的问题：
1. **保护上游 API 池被击穿**：全站有个物理上限（比如"整站同时不超过 500 个 in-flight"）→ 100b **不能解决**
2. **不同商业等级差异化**：wholesale 组同时能开多少 → 100b 用 tier `MaxSessions` 解决了 per-user 部分，但 per-group **不能解决**
3. **单用户不能撑爆自己**：per-user 上限 → 100b **完全覆盖**

**层 1 的价值：**
- 你有 20 个上游 key，每个 key 能承受 25 并发 → 全站上限 500 就防击穿
- 100b 现在如果 100 个用户每人 5 并发，也能到 500，然后上游炸了
- 但如果 100b 的上游供应充足、或者路由本身就能负载均衡到多 key，那层 1 也不是必需

**层 2 的价值：**
- tokensheep 用它做"wholesale 组同时最多 50 in-flight"，这是**合同承诺**
- 100b 如果没有 wholesale 场景或者用 tier `MaxSessions` 已经近似达成，层 2 不必单独加

### 结论
**层 1（系统全站 in-flight 上限）是唯一真正的缺口**。如果 100b 上游有击穿风险，值得加。

**加的方式很简单**：一个 middleware，`Redis INCR` + `EXPIRE`，请求进入前 +1、请求出时 -1，超过 `SystemConcurrency` 就 429。**不需要 tokensheep 那一整套 4 层结构**，只加缺的那一层。

已有的 `docs/spec/concurrency-porting-to-100b.md` 就是讲这个，可以直接用。

---

## 7. 分级提议

按"值得性 × 成本"分档：

### 🟢 高价值 / 低成本 —— 建议引入

| 项 | 理由 | 成本 |
|---|---|---|
| **系统全站 in-flight 上限**（并发第 1 层） | 100b 明确缺、上游有击穿风险时救命 | ~50 行 middleware + Redis 计数 |
| **订阅 `AllowWalletOverflow` 字段** | admin 想给的灵活性；如果 100b 真的没有 | 加字段 + BillingSession 分支 |
| **订阅 `DowngradeGroup` 字段** | 让订阅到期能降到指定组而不只是原组 | 加字段 + downgrade 逻辑分支 |
| **Admin 强制重置某 plan 的所有订阅** | plan 改价后同步生效，运营常用 | 一个 admin endpoint |

### 🟡 中价值 / 需先讨论 —— 取决于 100b 未来方向

| 项 | 触发引入的信号 | 引入代价 |
|---|---|---|
| **赠送池 `QuotaGift`** + 日消耗上限 + 30 天清零 | 如果 100b 要做"每日签到送免费额度但不想被薅穿" | 加字段 + 扣费顺序 + daily cron + UI 分区 |
| **`TierSkipGroups` 配置** | 如果 100b wholesale 客户增多、合同价被 tier 抢走 | 一个 option + tier 计算跳过 |
| **签到 + 兑换券进独立池的语义** | 如果 100b 要区分"送的钱"和"充的钱"的展示/使用规则 | 依赖 gift 池，见上 |

### 🔴 不建议引入 —— 世界观冲突或已被覆盖

| 项 | 不建议理由 |
|---|---|
| **`total_donated` + `TierForDonation`** | 跟 100b "消费驱动 tier" 冲突，是两种商业模式；引入会导致双 tier 触发路径混乱 |
| **完整 `CommercialGroups` + 多点保护** | 100b 没有 gift 池/签到日限，那些保护点都不适用；只需最小的 `TierSkipGroups` |
| **`user.group` 单字段承载所有身份** | 100b 双字段（`group + tier`）更清晰，反而是 tokensheep 应该学习 |
| **完整订阅系统** | 100b 已有，覆盖 95%+ |
| **完整 BillingSession + FundingSource** | 100b 已有 |
| **完整 `BillingPreference` 4 模式** | 100b 已有 |
| **完整 tier 升降级机制** | 100b 已有（消费驱动版） |

### 🟠 待验证 —— 需要先看代码再决策

| 项 | 需要确认什么 |
|---|---|
| 100b 订阅 `AllowWalletOverflow` 是否存在 | 直接读 `model/subscription.go SubscriptionPlan` 字段列表 |
| 100b 订阅 `DowngradeGroup` 是否存在 | 同上 |
| 100b 是否有 admin 强制重置某 plan 订阅的接口 | 看 `router/api-router.go` 里 subscription admin routes |
| 100b tier 系统能否处理"合同客户跳过 tier 计算"场景 | 看 tier 触发条件里有没有 group filter |

---

## 8. 操作路径建议

不建议一次性写"移植 PR"。建议顺序：

**阶段 1 — 验证 100b 现状（不改代码）**
- 把 🟠 4 项待验证读清楚，回来更新本文的分级
- 决定 🟡 3 项的运营方向：100b 是不是要做"公益签到"、要不要区分商业档

**阶段 2 — 只做 🟢**
- 每一项一个 PR，最小化改动
- `concurrency-porting-to-100b.md` 已经是现成的施工文档

**阶段 3 — 视运营方向决定 🟡**
- 如果决定不做公益签到 → 🟡 全部搁置
- 如果决定要做 → 引入 gift 池（有生产验证的踩坑清单在 v4 文档里）

**阶段 4 — 不做 🔴**
- 除非 100b 商业模型根本性转向（比如变成公益站），否则不要动
- 特别是 `total_donated` 一旦引入，跟 100b tier 会打架

---

## 9. 总结

**"要不要把 tokensheep 搬到 100b"的答案是"大部分不用搬"**：

- 订阅系统：100b 已有，几乎完全覆盖，只需补几个小字段
- BillingPreference / BillingSession / FundingSource：100b 已有，覆盖
- Tier 系统：100b 已有一套**不同世界观**的，不能替换
- 并发限制：100b 缺**顶层系统上限**这一层，值得补
- 赠送池：**取决于运营是否要做公益签到**，技术上能加但不一定该加
- 商业档：100b 靠 admin 手工够用，未来出问题再最小化引入

**tokensheep 8 个月踩的坑，60% 的价值在 v4 文档本身**（记录了做过什么、为什么改、哪些边界要小心），**40% 在具体代码**——而这 40% 大部分 100b 已经有。所以最有价值的其实是**把 tokensheep 的经济模型 spec 内化成 100b 的运营手册**，而不是搬代码。

---

## 附录：文档索引

- **`docs/spec/business-model-explained.md`**（tokensheep 业务模型讲解 — 前置阅读）
- `docs/spec/economy-model-v4.md`（tokensheep 生产权威业务模型）
- `docs/spec/subscription-porting-to-100b.md`（订阅技术细节，本文只提结论）
- `docs/spec/concurrency-porting-to-100b.md`（并发技术细节，本文只提结论）
- `docs/spec/upstream-rebase-plan.md`（更早的整体分歧全景）
