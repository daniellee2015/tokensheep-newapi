# tokensheep 订阅系统 —— 面向 100b 的移植设计规格

> **产出日期**：2026-09-04
> **交付目的**：tokensheep 订阅逻辑作为参考设计，用于在 `/Users/danlio/Repositories/new-api`（100b fork，`main`）上重新实现。
> **交付形式**：不搬代码，只交付"如果我要在 100b 上做同一件事，应该怎么做"的设计规格。
> **读者假设**：你是 100b 上的实现者，已理解 GORM、Gin、new-api 项目结构，需要一份浓缩的"设计决策 + 边界 case + 已踩过的坑"。

---

## 0. 关键事实：100b 已经有订阅系统

在开始重新实现之前，请先接受这个事实：

- **100b 的 `model/subscription.go` 就是 tokensheep 订阅系统的同源代码**，只是各自演进了一段
- 100b 已具备 `SubscriptionPlan` / `SubscriptionOrder` / `UserSubscription` / `SubscriptionPreConsumeRecord` 全套
- 100b 已具备 Stripe / Creem / Epay / Waffo Pancake 四种订阅支付接入
- 100b 已具备用户级 CRUD、管理员级 CRUD、订阅到期任务

**所以本文档的价值**不在"教你怎么建订阅系统"，而在**"把 tokensheep 相对 100b 演进出来的 5 个语义增量说清楚"**——这些增量是 tokensheep 生产环境验证过的、值得移植的、也是踩过坑总结出来的。

---

## 1. 五个语义增量（浓缩摘要）

| # | 增量 | 一句话解释 | 复杂度 |
|---|---|---|---|
| A | **`AllowWalletOverflow` 双字段** | 允许套餐用完后自动回退到钱包（plan 定义 + user_subscription snapshot） | 低 |
| B | **`DowngradeGroup` 显式降级组** | 套餐到期时降到指定组，而非只能回退到"购买前的组" | 低 |
| C | **`AdminResetPlanSubscriptions` 批量重置** | 管理员一键把某个套餐下所有用户的已用配额清零 | 低 |
| D | **`resolveBillingPreferenceForCommercial` 商业组禁购** | retail/wholesale 分组的用户不能买订阅，路由层就地 coerce | 中（涉及 tokensheep_setting 依赖，可精简） |
| E | **`BillingSession` 统一计费会话 + `FundingSource` 抽象** | 钱包/订阅两种资金来源用同一套预扣→结算→退款生命周期，支持 subscription_first/wallet_first 四种偏好回退 | 高（这是 tokensheep 相对 100b 最有价值的重构） |

**建议移植顺序**：A → B → C → 【是否需要商业组语义？】→ E（如果 100b 现有 billing 路径已经足够就跳过）。

---

## 2. 数据模型增量（对齐差异）

**这些是 tokensheep 相对 100b 多的字段**，直接 `ALTER TABLE ... ADD COLUMN` 就行（三种数据库都必须能跑 —— SQLite/MySQL/PostgreSQL）：

### 2.1 `subscription_plans` 表新增列

| 列名 | 类型 | GORM tag | 语义 |
|---|---|---|---|
| `allow_wallet_overflow` | `TINYINT/BOOLEAN`（可空） | `gorm:"..."` **不写 default**（避免 GORM 反复 ALTER，见 AGENTS.md 项目规约） | 该套餐用完后是否允许自动回退到钱包，`nil` 视为 `true` |
| `downgrade_group` | `varchar(64)` | `gorm:"type:varchar(64);default:''"` | 到期时降级到指定组；空字符串走 legacy 行为（回退到 `PrevUserGroup`） |

**为什么 `AllowWalletOverflow` 用 `*bool` 而不是 `bool`？**
因为需要区分"管理员显式设为 false"和"字段未填（默认 true）"两种语义。老套餐从库里加载时，`nil` → `NormalizeDefaults()` → `true`。**这是刻意的兼容策略**，避免 migration 期间历史套餐突然拿不到钱包 fallback。

### 2.2 `user_subscriptions` 表新增列

| 列名 | 类型 | 语义 |
|---|---|---|
| `downgrade_group` | `varchar(64)` | **snapshot from plan** —— 购买时把 plan 的 downgrade_group 拷贝进来，之后即使 plan 改了，用户手上的订阅仍按购买时的规则降级 |
| `allow_wallet_overflow` | `bool` | **snapshot from plan** —— 同上 |

**为什么要 snapshot？**
- 用户购买订阅是**契约关系**，plan 后续被管理员改动不应该影响已购用户
- 到期降级、钱包 fallback 都要读 UserSubscription 而不是 Plan
- 如果读 Plan，会出现 "用户买时 plan 允许 overflow，用完时管理员改成 false 于是他被卡住" 的诡异体验

### 2.3 无需新表

四张表 100b 都有：`subscription_plans` / `subscription_orders` / `user_subscriptions` / `subscription_pre_consume_records`。**只有字段增量**，没有表增量。

---

## 3. 关键行为增量（按优先级展开）

### 3.1 增量 A：钱包 Overflow

**行为**：用户订阅额度用完，自动降级去扣钱包余额，而不是直接返回"额度不足"。

**实现要点**：
- 在 `CreateUserSubscriptionFromPlanTx` 里把 `plan.AllowWalletOverflow` 拷贝到 `UserSubscription.AllowWalletOverflow`
- 新增查询函数 `UserActiveSubscriptionsAllowWalletOverflow(userId) (bool, error)`：
  - 返回条件：**任一活跃订阅** 允许 overflow 即返回 true
  - 语义：这是"用户视角"的问题，只要有一份订阅愿意接管失败请求，就允许 fallback
- 在计费路径（下面 §3.5 会讲的 `BillingSession`）里，`subscription_first` 模式下遇到订阅额度不足时：
  1. 先调 `UserActiveSubscriptionsAllowWalletOverflow` 判断
  2. `true` → 回退到 wallet 计费
  3. `false` → 返回 402/403 "订阅额度不足"

**tokensheep 相关文件（参考实现）**：
- `model/subscription.go` L1500+ `PostConsumeUserSubscriptionDelta`
- `service/billing_session.go` L482-506 `subscription_first` 分支

**踩过的坑**：早期没 snapshot，管理员改 plan 后老用户被卡住，投诉过一次。所以 §2.2 里那个"snapshot from plan"是**血泪教训**。

### 3.2 增量 B：显式 DowngradeGroup

**行为**：套餐到期时，用户被自动降到指定组（而不是"回退到购买前的组"）。

**为什么需要**：
- Legacy 行为：`ExpireDueSubscriptions` 到期后把 user.group 恢复到 `PrevUserGroup`
- 问题：如果用户 A 分组 → 买了套餐升到 B → 后来管理员手动把他改成了 C → 套餐到期后**他会突然被打回 A**，管理员的手动调整丢了
- 修复：加显式 `DowngradeGroup`，优先级：**显式 downgrade_group > 回退到 PrevUserGroup**

**实现要点**（在 `downgradeUserGroupForSubscriptionTx` 和 `ExpireDueSubscriptions` 里）：

```
1. 如果用户还有其他"活跃且带 upgrade_group"的订阅，don't touch group（多套餐叠加场景）
2. 找到最近一条 expired 且带 downgrade_group 或 upgrade_group 的订阅
3. 如果它有显式 downgrade_group：直接切到那里
4. 否则走 legacy：只有当"当前组 == 当时 upgrade 到的组"时，才恢复到 prev_user_group
    （防止管理员中途手动调整被覆盖）
5. 目标组 == 当前组 → no-op
6. 切完记得 `RefreshUserGroupCache(userId)`，否则 auth middleware 缓存里还是旧组
```

**tokensheep 相关文件**：
- `model/subscription.go` L440-482 `downgradeUserGroupForSubscriptionTx`
- `model/subscription.go` L1140-1235 `ExpireDueSubscriptions`

### 3.3 增量 C：`AdminResetPlanSubscriptions`

**行为**：管理员在后台点一个按钮，把某个 plan 下的**所有**用户的 `amount_used` 清零。

**使用场景**：
- 促销活动（"本月所有订阅用户 quota 翻倍" —— 直接改 plan.TotalAmount 然后 reset 所有人）
- 服务事故补偿
- 计费 bug 修复后统一回滚

**实现要点**：
- 分批 tx（避免长事务锁太多行）：每批 `limit` 条
- 需要参数 `advanceResetTime bool`：
  - `false`：只清零 `amount_used`，`next_reset_time` 不动
  - `true`：清零的同时把 `next_reset_time` 重算（相当于开始新一个 reset 周期）
- 需要参数 `planId` 或 `userId+planId`（两个 admin API）

**tokensheep 相关方法**：
- `AdminResetPlanSubscriptions(planId, advanceResetTime) → SubscriptionResetResult`
- `AdminResetUserSubscriptionsByPlan(userId, planId, advanceResetTime)`

**路由**：
- `POST /api/subscription/admin/plans/:id/subscriptions/reset`
- `POST /api/subscription/admin/users/:id/subscriptions/reset`

### 3.4 增量 D：商业组禁购（可选）

**这条要不要移植取决于 100b 的运营模式**。tokensheep 是公益站，有专门的"商业分组"（retail / wholesale / wholesale-plus）通过合同定制配额，不适合走订阅体系。如果 100b 只有普通用户，这条可以跳过。

**如果需要移植，最小实现**：

1. **配置层**：定义一个 `IsCommercialGroup(group string) bool` 的开关（tokensheep 放在 `setting/tokensheep_setting/economy.go` 的 `EconomySetting.CommercialGroups`，100b 可以放到 option 表里一个逗号分隔字符串）

2. **购买路径守卫**（所有 `SubscriptionRequestXxxPay` 和 `SubscriptionRequestBalancePay` 开头调一次）：
   ```go
   if IsCommercialGroup(user.Group) {
       common.ApiErrorMsg(c, "商业用户请通过合同调整档位，不参与订阅套餐")
       return
   }
   ```
   注意：**Admin 手动 `AdminCreateUserSubscription` 要绕过这个检查**，允许运营给商业客户塞赠送订阅。

3. **计费路径 coerce**（`NewBillingSession` 开头）：
   ```go
   if IsCommercialGroup(user.Group) && (pref == "subscription_only" || pref == "subscription_first") {
       pref = "wallet_first"  // 仅当次请求生效，不写回 DB
       logger.LogInfo(...)   // 一次 coerce 一行日志，便于审计
   }
   ```
   **为什么不直接写回 DB？** 因为用户可能只是临时晋升到商业组，退回普通组后他的 preference 应该恢复原状。写 DB 是不可逆操作，语义会丢。

4. **Plan 列表过滤**：`GetSubscriptionPlans` 里如果调用方是商业用户，直接返回空列表（不是硬报错，是让前端 UI 自然渲染"你已在商业档，无需订阅"的引导）。

**踩过的坑**：`IsCommercialGroup` 必须 **fail-open**（group 为空字符串或缓存 miss 时视为 false）。否则如果 group cache 挂了，全站用户会突然都变成"商业用户"→ 全部无法购买订阅→ 客服爆炸。

### 3.5 增量 E：`BillingSession` 统一计费会话（这个最值得学）

**这是 tokensheep 相对 100b 最有价值的重构**，也是最有工程量的。如果只挑一个移植，挑这个。

**核心思想**：把"钱包"和"订阅"两种资金来源抽象成同一个接口 `FundingSource`，让业务代码不用到处 `if wallet else subscription`。

#### 3.5.1 `FundingSource` 接口

```go
type FundingSource interface {
    Source() string                    // "wallet" or "subscription"
    PreConsume(amount int) error       // 从该来源预扣 amount
    Settle(delta int) error            // 请求结束后差额结算（正数补扣，负数退还）
    Refund() error                     // 请求失败，退还全部预扣
}
```

两个实现：
- `WalletFunding{userId, consumed QuotaDebit}` — 钱包
- `SubscriptionFunding{requestId, userId, amount, subscriptionId, preConsumed, ...}` — 订阅

#### 3.5.2 `BillingSession` 生命周期

```
NewBillingSession(c, relayInfo, preConsumedQuota)
        │
        ├─ 解析 pref（wallet_only/wallet_first/subscription_only/subscription_first）
        ├─ 【可选】商业组 coerce（增量 D）
        ├─ 根据 pref 决定 tryWallet / trySubscription 顺序
        │       │
        │       ├─ tryWallet:
        │       │   ├─ GetUserQuota
        │       │   ├─ session.preConsume(c, preConsumedQuota)
        │       │   │       ├─ shouldTrust(c)？跳过预扣（信任额度旁路）
        │       │   │       ├─ PreConsumeTokenQuota（令牌配额）
        │       │   │       └─ funding.PreConsume（wallet 扣款）
        │       │   │       └─ 失败时原子回滚令牌
        │       │   └─ 返回 session
        │       │
        │       └─ trySubscription:
        │           ├─ session.preConsume（同上，subscription 分支）
        │           │       ├─ ！！！订阅不能启用信任旁路
        │           │       └─ SubscriptionFunding.PreConsume → PreConsumeUserSubscription
        │           └─ 返回 session
        │
        └─ subscription_first 特殊回退：
                trySubscription 返回 ErrorCodeInsufficientUserQuota
                  → 查 UserActiveSubscriptionsAllowWalletOverflow
                  → true → tryWallet
                  → false → 返回原错误

【业务处理请求...】

session.Settle(actualQuota)
        │
        ├─ delta = actualQuota - preConsumedQuota
        ├─ if delta == 0: mark settled, return
        ├─ funding.Settle(delta)   ← 资金来源提交
        ├─ 令牌配额 Increase/Decrease by delta
        └─ mark settled

【请求失败】
session.Refund(c)  ← 幂等，异步
        │
        └─ 若已 fundingSettled 或 settled → 不退
        └─ 否则 gopool.Go 里退：funding.Refund + 令牌 Increase
```

#### 3.5.3 值得记录的具体设计决策

**1. `trusted` 信任旁路只对 wallet 生效，对 subscription 不生效。**

原因：`PreConsumeUserSubscription` 要求 `amount > 0` 才能创建预扣记录并锁定订阅（防并发）。如果信任旁路把 `effectiveQuota` 设成 0，会导致：
- `SubscriptionFunding.amount` 用的仍是构造时的 `subConsume`
- `preConsumedQuota` 变成 0
- 三者不一致 → 结算时 delta 算错 → 订阅**多扣或少扣**

代码里刻意 return false 并注释了原因（`billing_session.go` L326-334）。

**2. `wallet.consumed` 用 `model.QuotaDebit{Gift, Paid}` 而非单一 int。**

原因：tokensheep 用户额度是双池（`quota` + `quota_gift`），扣款要按顺序（gift 优先），退款也要按对应池退回去。如果只记录总数，退款时不知道哪部分从哪个池扣的。

**100b 场景**：100b 是单池 wallet，这条**可以简化**，`consumed` 就是 int。但把接口留成 `QuotaDebit` 结构，将来加双池也不用重构 —— 或者直接就用 int，反正是 100b。

**3. `Settle` 分两步且要标记 `fundingSettled`。**

场景：`funding.Settle(delta)` 成功了，但下一步 `DecreaseTokenQuota` 失败（比如 DB 暂时挂了）。此时：
- 资金来源已经真的扣了钱
- 但 token quota 没同步

如果不区分，`defer session.Refund()` 会把已经提交的资金来源再退一次 → **多退了**。所以 `fundingSettled=true` 后 `needsRefundLocked()` 返回 false，`Refund()` 直接短路。

**4. `Refund` 是异步的（`gopool.Go`）。**

因为要退令牌配额、退资金来源、可能还要退额外预留（`extraReserved`），涉及多个 DB 事务。同步做会阻塞用户拿到失败响应。异步做前提是所有子操作**必须幂等**：
- `RefundSubscriptionPreConsume(requestId)` 幂等（依赖 requestId 唯一）
- `IncreaseTokenQuota` 幂等（但要注意 double-refund，所以入口靠 `refunded` flag 卫）

**5. `Reserve(targetQuota)` 是中途追加预扣。**

用于 tiered retry 之类的场景：请求跑到一半发现需要额外的额度（模型比预估的更贵）。这时候：
- 增量 `extraReserved`
- 追加扣 funding + 追加扣 token
- 任一失败原子回滚

**6. 关键错误码约定**：
- 钱包扣款失败：`ErrInsufficientWalletQuota` 或 `model.ErrInsufficientUserQuota`
- 订阅扣款失败：`ErrNoActiveSubscription` 或字符串 `"no active subscription"` / `"subscription quota insufficient"`
- 上层 `preConsume` 统一映射成 `ErrorCodeInsufficientUserQuota`（403），标 `SkipRetry` 和 `NoRecordErrorLog`
- Skip retry 是因为额度不足重试没意义，No record error log 是因为这是用户错误不是系统错误

**tokensheep 参考文件**：
- `service/billing_session.go`（完整 508 行）
- `service/funding_source.go`（完整 194 行）
- `common/str.go` L111+ `NormalizeBillingPreference`

---

## 4. 支付接入四件套（模板化）

tokensheep 有 4 个支付网关的订阅接入，**结构完全一致**，可以当模板套用（100b 已经有对应的 topup 版本可以参考，也已经有 subscription 版本）：

```
controller/subscription_payment_{gateway}.go
├─ SubscriptionRequest{Gateway}Pay(c *gin.Context)
│     ├─ requirePaymentCompliance(c)  ← 合规确认门槛
│     ├─ 参数校验 + plan 校验
│     ├─ rejectSubscriptionForCommercialUser(c, userId)  ← 增量 D
│     ├─ 检查 plan.{Gateway}ProductId 非空
│     ├─ 检查 gateway 配置（key、webhook secret）
│     ├─ 检查 MaxPurchasePerUser
│     ├─ 生成 tradeNo（`sub-{gateway}-ref-{userId}-{ms}-{random}`）
│     ├─ 调 gateway SDK 拉起支付页
│     ├─ 创建 SubscriptionOrder（status=pending，携带 PaymentProvider）
│     └─ 返回支付链接 / QR code
│
└─ {Gateway}Webhook(c *gin.Context)  ← 通常在 controller/topup_{gateway}.go 里，共享同一个 webhook
      ├─ 验签
      ├─ 解析 tradeNo
      ├─ 判断是订阅订单还是充值订单（前缀）
      └─ 调 model.CompleteSubscriptionOrder(tradeNo, providerPayload, provider, method)
              ├─ tx 里 lockForUpdate SubscriptionOrder
              ├─ 检查 expectedPaymentProvider 匹配（防跨网关伪造）
              ├─ 检查 Status == Pending（防重放）
              ├─ CreateUserSubscriptionFromPlanTx（升组、set snapshot）
              ├─ upsertSubscriptionTopUpTx（同步一条 topup 记录）
              ├─ order.Status = Success
              └─ RefreshUserGroupCache（如果升组了）
```

**几个关键的踩坑经验**：

1. **`CompleteSubscriptionOrder` 必须锁用户行**（`lockForUpdate(tx).Select("id").Where("id = ?", order.UserId).First(&userRow)`）。原因：同一用户并发完成两个订单（多实例部署下）时，`MaxPurchasePerUser` 检查会绕过 —— 因为两个 tx 各看到 count=0。加行锁后按用户串行。

2. **`upsertSubscriptionTopUpTx` 是把订阅订单同步写一条 TopUp 记录**，让钱包页面的"账单历史"能统一展示订阅购买。100b 如果有类似的历史合并需求，需要这一步；否则可以省略。

3. **`ProviderPayload` 存原始 webhook 负载**（varchar/text），方便事后审计和退款对账。tokensheep 生产上救过命，别省。

4. **`expectedPaymentProvider` 是防跨网关伪造攻击**：如果 Order 是 Stripe 创建的，Creem webhook 拿到同一个 tradeNo 也不能完成它。

5. **前端跳转 URL 一定要走 `paymentReturnPath("/wallet")`**（拼上 host 前缀），不能直接写 `/wallet`。原因：Stripe/Creem 服务端调回 success URL 时如果拼错了域名，用户会看到 404。

---

## 5. Cron 任务

一个 goroutine，只在 master node 跑，1 分钟 tick：

```go
// service/subscription_reset_task.go
func StartSubscriptionQuotaResetTask() {
    // 只 master 跑 + sync.Once
    gopool.Go(func() {
        ticker := time.NewTicker(1 * time.Minute)
        for range ticker.C {
            runSubscriptionQuotaResetOnce()
        }
    })
}

func runSubscriptionQuotaResetOnce() {
    // CompareAndSwap 防重入
    if !running.CompareAndSwap(false, true) { return }
    defer running.Store(false)

    // 1. 分批处理到期订阅（<= batchSize 就退出循环）
    for { n, _ := ExpireDueSubscriptions(300); if n < 300 { break } }

    // 2. 分批处理该 reset 的订阅
    for { n, _ := ResetDueSubscriptions(300); if n < 300 { break } }

    // 3. 每 30 分钟清理一次老的 pre_consume_records（>7 天）
    if lastCleanup + 30min <= now {
        CleanupSubscriptionPreConsumeRecords(7 * 24 * 3600)
    }
}
```

**在 `main.go` 启动阶段调 `StartSubscriptionQuotaResetTask()`**（对应 100b 的启动流程，找一个类似"启动其他 cron 任务"的位置塞进去）。

**踩过的坑**：
- Tick 间隔 1 分钟是**特意选的**。原本是 5 分钟，导致按小时 reset 的 plan 有最多 5 分钟延迟。1 分钟够快、不给 DB 压力。
- `batchSize=300` 是经验值，MySQL/Postgres 事务体积可控。
- `CompareAndSwap` 那步不能省 —— 万一 tick 时上一次还没跑完（reset 数量特别大时），并发跑会互相锁表。

---

## 6. 缓存

`SubscriptionPlan` 有两层缓存（`pkg/cachex.HybridCache`：Redis + 本地 LRU）：

- `subscription_plan:v1` —— 主体 plan 数据，TTL 300s、cap 5000
- `subscription_plan_info:v1` —— 精简版（只有 PlanId + PlanTitle），TTL 120s、cap 10000，专供计费日志用

**关键点**：`InvalidateSubscriptionPlanCache(planId)` 必须在**所有** plan 变更后调（create/update/enable/disable/delete）。tokensheep 早期漏了 disable 那条路径，导致 disable 后前端仍能列出该 plan 好几分钟。

**如果 100b 计费日志不需要 plan_title 展示**，可以省掉第二层缓存，只留第一层。

---

## 7. 前端

tokensheep 前端订阅相关（`web/src/`）：

```
features/subscriptions/                     ← Admin 管理套餐（CRUD、绑定、reset、invalidate）
├─ api.ts
├─ components/subscriptions-mutate-drawer.tsx    ← 套餐编辑抽屉，字段最全
└─ components/dialogs/*                            ← 各种确认对话框

features/wallet/                            ← 用户端购买 + 计费偏好
├─ components/subscription-plans-card.tsx        ← 套餐列表 + 购买按钮
├─ components/billing-preference-card.tsx        ← 4 种 preference 切换
└─ hooks/use-payment.ts                          ← 各支付网关拉起
```

**100b 的前端在 `web/default/`（双主题结构），需要注意路径差异。** 100b 已有 `web/default/src/features/subscriptions/` 和 `wallet/`，只需要按上面 §3 的字段增量补 UI 字段：
- 套餐编辑表单加 `allow_wallet_overflow` toggle + `downgrade_group` 输入框
- 用户端 wallet preference card 保持不变（4 种 pref 100b 应该已经有）
- Admin 面板加"reset all subscriptions of this plan"按钮

---

## 8. 我不建议移植的（避坑）

tokensheep 里跟订阅相关但**不建议照搬到 100b** 的东西：

1. **`setting/tokensheep_setting/economy.go` 全套**——那是公益站的经济模型（tier 阈值、gift pool、checkin 奖励），100b 有自己的 `user_tier_ext` + rebate 体系，两者互斥。别混。

2. **`middleware/session_concurrency.go`**——tokensheep 的三层并发限制（user/group/system），跟计费/订阅无直接关系，如果 100b 不需要就跳过。

3. **`quota_gift` / `total_donated` 字段**——tokensheep 的捐赠双池模型，跟订阅解耦，别一起搬。

4. **`AlphaSearchRequest`**——tokensheep 独有的 Codex web search DTO，跟订阅无关，跳过。

5. **`relaykit/` 独立模块**——tokensheep 独有的架构层，跟订阅无直接关系，工程量巨大（368 files），别一起搬。

---

## 9. 移植检查清单（放到 100b 那边执行）

按顺序打勾：

- [ ] **Schema**：`subscription_plans` 加 `allow_wallet_overflow`、`downgrade_group`；`user_subscriptions` 加 `allow_wallet_overflow`、`downgrade_group`。三种 DB 都验证 migrate。
- [ ] **Model 增量方法**：
  - [ ] `NormalizeDefaults()` 里 `AllowWalletOverflow == nil → true`
  - [ ] `CreateUserSubscriptionFromPlanTx` 里 snapshot plan → user_subscription
  - [ ] `downgradeUserGroupForSubscriptionTx` 优先读 `sub.DowngradeGroup`
  - [ ] `ExpireDueSubscriptions` 同上
  - [ ] `UserActiveSubscriptionsAllowWalletOverflow(userId)` 新查询
  - [ ] `AdminResetPlanSubscriptions(planId, advanceResetTime)` 新方法
  - [ ] `AdminResetUserSubscriptionsByPlan(userId, planId, advanceResetTime)` 新方法
- [ ] **Router**：新增 admin 路由 `POST /subscription/admin/plans/:id/subscriptions/reset` 和 `POST /subscription/admin/users/:id/subscriptions/reset`
- [ ] **Controller**：新增两个 handler（薄，直接调 model）
- [ ] **【可选】商业组禁购**：如果 100b 有商业分组概念，加 §3.4 那 4 处守卫；否则跳过
- [ ] **【可选】BillingSession 重构**：如果 100b 现有 billing 路径已经支持 subscription_first / wallet_first 四种 pref、支持 overflow 回退，就不用动；否则参考 §3.5 重构
- [ ] **前端**：套餐编辑表单加 2 个字段，Admin 面板加 reset 按钮
- [ ] **测试**：至少覆盖以下场景
  - [ ] 订阅到期，显式 downgrade_group 生效
  - [ ] 订阅到期，无 downgrade_group，legacy 回退到 prev_user_group
  - [ ] `subscription_first` + overflow=true + 订阅额度不足 → 回退到钱包
  - [ ] `subscription_first` + overflow=false + 订阅额度不足 → 返回 403
  - [ ] `AdminResetPlanSubscriptions(planId, false)` → amount_used 清零，next_reset_time 不动
  - [ ] `AdminResetPlanSubscriptions(planId, true)` → 都清零

---

## 10. 参考文档

tokensheep 仓库里其他相关规格（读一遍能补齐更多上下文）：

- `docs/spec/economy-model.md` — 经济模型 v1
- `docs/spec/economy-model-v4.md` — 经济模型 v4（当前生产）
- `docs/spec/vip-tier-decision.md` — VIP tier 决策
- `docs/spec/r16-1-billing-preference-hazard.md` — R16-1 billing preference hazard 分析
- `docs/spec/upstream-rebase-plan.md` — 与 upstream 的 rebase 计划（很关键，交代了 tokensheep 相对 upstream 的分歧）

---

## 附录：文件对照表（tokensheep 参考 → 100b 目标位置）

| tokensheep | 100b 目标位置 | 备注 |
|---|---|---|
| `model/subscription.go` | `model/subscription.go` | 增量合并，别覆盖 |
| `service/billing_session.go` | `service/billing_session.go` | 100b 若无，则新建；若有，谨慎合并 |
| `service/funding_source.go` | `service/funding_source.go` | 同上 |
| `service/subscription_reset_task.go` | `service/subscription_reset_task.go` | 100b 应该已经有等价物 |
| `controller/subscription.go` | `controller/subscription.go` | 增量合并 admin handler |
| `controller/subscription_payment_*.go` | `controller/subscription_payment_*.go` | 100b 已有 4 个网关；对照字段增量补 |
| `router/api-router.go` L166-201 | `router/api-router.go` 订阅段 | 加 2 条 admin 路由 |
| `common/str.go` L111 `NormalizeBillingPreference` | `common/str.go` 或类似 | 4 种 pref 常量 |
| `web/src/features/subscriptions/` | `web/default/src/features/subscriptions/` | 路径差异 |
| `web/src/features/wallet/components/subscription-plans-card.tsx` | `web/default/src/features/wallet/` 对应文件 | 同上 |

---

**总结一句话**：tokensheep 相对 100b 的订阅系统增量本质上是**"钱包 overflow + 显式 downgrade + admin 批量 reset + BillingSession 抽象"**四件事。前三件是低难度的增量合并，第四件是有工程量但价值最高的重构。全部合并大概是**净新增 800-1200 LOC**（不含 UI），预估 **1-2 天** 专注工作量。
