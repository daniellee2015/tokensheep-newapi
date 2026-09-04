# tokensheep 业务模型讲解

> **产出日期**：2026-09-04
> **权威来源**：`docs/spec/economy-model-v4.md`（生产 v4，已迭代 20+ 轮）
> **本文目的**：把 tokensheep 里"订阅收款 / 月收款 / 赠送账户 / tier"这四件事**是什么、怎么互相关联、解决什么问题**讲清楚。**不比对任何其他仓库，不做移植建议**。
> **读者假设**：你可能是新加入的运营、开发或者未来的自己。你打开这份文档时脑子里有一堆概念但不知道它们怎么串起来。

---

## 0. 一句话总览

tokensheep 是一个**公益 API 站**，用户不能白嫖但也不追求毛利。它靠"**用户自愿捐赠反哺号池成本**"跑起来，所以整个业务模型的核心矛盾是：

> **怎么让"愿意付钱的人"觉得值，同时防止"零成本用户"和"高价值用户"都薅穿号池？**

答案是一个四层结构：**赠送账户**做入门体验、**贡献充值**换 tier 等级、**订阅套餐**买稳定池子、**商业档**走合同定制。四个概念不是并列的，而是**一个用户在系统里的四种存在方式**。

---

## 1. 用户身份三分类（这是理解一切的前提）

数据库里 `user.group` 是**单值字段**，永远只能是三类之一：

```
┌──────────────────────────────────────────────────────┐
│  A. Tier 身份（公益 + 贡献解锁）                       │
│  group ∈ { free, supporter, fan, bestie, [vip] }     │
│  → 靠捐赠累计升级                                     │
│  → 使用赠送池 + 付费钱包                              │
└──────────────────────────────────────────────────────┘
                     ↓ 主动买订阅
┌──────────────────────────────────────────────────────┐
│  B. Subscription 身份（个人付费 + 稳定池）             │
│  group = 'sub'                                        │
│  → 买断 30 天配额池子                                 │
│  → 到期回退到之前的 tier                              │
└──────────────────────────────────────────────────────┘
                     ↓ admin 手动分配
┌──────────────────────────────────────────────────────┐
│  C. Commercial 身份（下游转售商）                      │
│  group ∈ { retail, wholesale, wholesale-plus }        │
│  → 只走付费钱包                                       │
│  → 不参与 tier、不能买订阅                            │
│  → 走合同价，admin 手工配置                           │
└──────────────────────────────────────────────────────┘
```

**关键互斥关系：**

| 关系 | 规则 |
|---|---|
| A ↔ B | **兼容（临时）**：A 类用户可以买订阅临时变 B；到期回 A |
| A ↔ C | **互斥**：C 类用户的充值**不累加捐赠额**、**不触发升级** |
| B ↔ C | **完全互斥**：C 类不能买订阅（前后端双拒） |

这三类身份是理解后面所有细节的骨架。**每一笔钱进来、每一次扣费、每一个 tier 变动，都要先问"这用户是 A/B/C 哪一类"**。

---

## 2. 三个资金池（钱在系统里的三种存在形式）

一个用户的账户里最多同时有 **3 笔独立的余额**：

| 池 | DB 字段 | 钱从哪来 | 上限 | 时效 |
|---|---|---|---|---|
| **赠送池** | `User.QuotaGift` | 每日签到 + 兑换券 | $50 | 30 天无请求清零 |
| **付费钱包** | `User.Quota` | 贡献充值 + 标准充值 | 无 | 永久 |
| **订阅池** | `UserSubscription.AmountTotal - AmountUsed` | 购买订阅套餐 | 套餐配置 | 套餐有效期 |

**这三个池是完全独立的余额**，不是同一个数字。UI 上必须**分开展示**（这是踩过坑的——早期赠送池和钱包合并显示，用户以为兑换券是"充钱"，实际不是）。

---

## 3. 四块业务拼图

现在把"订阅收款 / 月收款 / 赠送账户 / tier"逐个说清楚。

### 3.1 赠送账户（quota_gift）—— 用户体验的入门

**它是什么？**  
一个**独立的免费额度池**，写入 `User.QuotaGift` 字段，上限 $50。

**钱怎么进来？**（两条路，都不花钱）

1. **每日签到** —— 用户每天点一次签到按钮：
   - `free` 组：**不能签到**（返回错误，提示"升级 tier 才能签到"）
   - `supporter` 组：签到得 $0.5
   - `fan` 组：签到得 $3
   - `bestie` 组：签到得 $5
   - `vip` 组（已关闭）：签到得 $10
2. **兑换券**（"入群欢迎码"）—— 一次性给新用户 $2 起步额度，每账号一次

**签到额度 = 你在这个 tier 每日能烧的 gift 上限**：这是同一个数字扮演两个角色——既是"你今天签到能拿多少"，又是"你今天能烧多少 gift"。

例：`fan` 用户 $3 签到额，即使 gift 池里有 $50，今天最多也只能烧 $3；剩下的会被跳过、自动切到付费钱包扣。

**钱怎么出去？**  
被 API 请求消耗。**gift 池永远是第一个被扣的**（除非用户显式选择 `subscription_only` 模式）。

**触发限额时会发生什么？**  
今天 gift 用超日限 → 自动跳过 gift → 去扣付费钱包 → 都没了 → 429。**不影响明天的签到额度**。

**过期怎么算？**  
30 天没有任何 API 请求 → 每日 cron 直接把 `QuotaGift` 清零（防止僵尸账号占资源）。

**它解决什么问题？**  
- **公益站的入门体验**：新用户没花钱也能试试
- **签到激励活跃**：每天登录才有免费额度进账
- **不给白嫖党撑腰**：日限 + 30 天过期 + free 不能签到，三重限制

**关键设计细节：**
- 兑换券**不进钱包**、**不累加捐赠额**、**不改 group**
- 赠送池是"钱包的子池"，视觉上属于余额但语义上完全独立
- `wallet_only` 模式下**仍然吃 gift 池**（因为 gift 是 wallet 的一部分）——想强制走付费余额只能等 gift 用完

---

### 3.2 Tier（贡献解锁的等级）—— 用户身份的核心

**它是什么？**  
用户在公益体系里的等级，直接就是 `user.group` 字段的值。

**5 个档位（vip 目前关闭）：**

| Tier | 升级门槛 | RPM | 并发数 | 每日签到 | 可访问模型 |
|---|---|---|---|---|---|
| `free` | 无门槛（有欢迎码即可） | 10 | 1 | 无（不能签到） | basic |
| `supporter` | 累计捐赠 ≥ $10 + 30 天有贡献 | 20 | 3 | $0.5 | basic + premium |
| `fan` | 累计捐赠 ≥ $50 + 30 天有贡献 | 30 | 5 | $3 | basic + premium |
| `bestie` | 累计捐赠 ≥ $100 + 30 天有贡献 | 40 | 8 | $5 | basic + premium + flagship |
| `vip`（关闭中）| 累计捐赠 ≥ $500 + 30 天有贡献 | 60 | 15 | $10 | basic + premium + flagship |

**关键：tier 是"用户组名字符串"，同一个字段还是并发限制、RPM、模型访问权限的 key**。改 tier 就是改 group，所有关联规则自动跟着走。

**怎么升级？**

**触发点**：每次充值成功的 webhook 回调（`model/topup.go`）

**升级公式**：
```
IF user.group NOT IN CommercialGroups:
    user.total_donated += 本次充值金额
    new_tier = TierForDonation(user.total_donated)
    IF new_tier > user.group AND user.role < admin:
        user.group = new_tier
```

**注意**：
- `total_donated` 是**累计捐赠额**，只升不降，永久留在用户身上
- 商业用户（C 类）的充值**不累加**这个字段（这是为什么 A/C 互斥）
- 升级是**单向自动**的，管理员可以手工降但普通用户降不了

**怎么降级？**（这是"活跃度"机制的关键）

**每日 0 点 cron**（`model/tokensheep_maintenance.go`）：
```
FOR user in all_users:
    IF user.group IN CommercialGroups: skip
    近 30 天充值总额 = sum(top_ups.money WHERE user_id=? AND created_time > now-30d)
    IF 近 30 天充值总额 == 0:
        user.group = 'free'            ← 30 天不捐直接掉回 free
    ELSE:
        user.group = TierForDonation(user.total_donated)   ← 按累计重算
    IF now - user.last_request_at > 30d:
        user.quota_gift = 0            ← 顺便清 gift 池
```

**这里最重要的规则**：**"tier 门槛"不是永久的**——就算你累计捐了 $500 达到 vip 门槛，如果最近 30 天没再捐一分钱，daily cron 会**把你降到 free**。想保持 tier 得**持续贡献**。

这就是文档里 `LiveTier = ThresholdMet AND Recent30dActive` 的意思。

**tier 具体影响什么？**（这是它的价值）

| 影响面 | 高 tier 用户拿到什么 |
|---|---|
| **每日签到额度** | $0 → $0.5 → $3 → $5 → $10 |
| **RPM 上限** | 10 → 20 → 30 → 40 → 60 |
| **并发数上限** | 1 → 3 → 5 → 8 → 15 |
| **可用模型档次** | basic → +premium → +flagship |
| **渠道 GroupGroupRatio 折扣** | 高 tier 走更便宜的上游 |

所以"升 tier"给用户的价值是**四位一体**：**更多签到 + 更快 RPM + 更多并发 + 更好模型**。这是为什么用户愿意持续捐赠。

**它解决什么问题？**
- **激励持续贡献**：不是"充一次终身特权"，是"要持续捐才有特权"，钱能持续进来
- **门槛感**：不同层级之间有明显差距（并发从 1 → 15 差 15 倍），愿意付费的人真的能感受到升级
- **防僵尸账号**：30 天不活跃 → 降组 + 清 gift，不永久占号池资源

---

### 3.3 订阅套餐（月收款）—— 稳定池子的买断制

**它是什么？**  
一次性买断一个"30 天的独立配额池"，写入 `UserSubscription` 表。

**跟贡献充值有什么区别？**

| 维度 | 贡献充值（→ tier） | 订阅套餐 |
|---|---|---|
| 钱的去向 | 进 `User.Quota`（付费钱包） | 进 `UserSubscription.AmountTotal`（独立池） |
| 是否累加 `total_donated` | ✅ 是，累加就为了升 tier | ❌ 不累加（**订阅不影响 tier 升级**） |
| 是否改 group | 只有升 tier 时改 | 立刻改成 `sub` |
| 有效期 | 永久 | 套餐配置（默认 30 天） |
| 到期怎么办 | 无到期 | 回退到 `PrevUserGroup`（购买前的 tier） |
| 池子上限 | 无 | 套餐配置（比如买 $110 池） |

**订阅套餐的经济模型（举例）：**

| 套餐 | 售价 | 池子额度 | 有效期 | 加成 |
|---|---|---|---|---|
| 通用订阅 A | ¥100 | ¥110 | 30 天 | 送 10% |
| 通用订阅 B | ¥200 | ¥220 | 30 天 | 送 10% |
| 通用订阅 C | ¥400 | ¥440 | 30 天 | 送 10% |

**订阅用户的身份（`sub` 组）：**

买了订阅后，`user.group` 立刻改成 `sub`。这个 `sub` 组也有自己的属性配置：

| 属性 | 值 |
|---|---|
| RPM | 100 |
| 并发 | 10 |
| 可访问模型 | 中档商业池（比商业档少，比 fan/bestie 多） |
| GroupGroupRatio | 中档折扣 |

**订阅池不受"gift 日限"约束，也不受"30 天无请求清零"约束**——买了就是你的，30 天内自己决定烧节奏。

**订阅到期发生什么？**

Cron 检测到订阅过期 → 触发 `SubscriptionExpiryDowngrade`：
- 优先降到套餐的 `DowngradeGroup`（管理员配置的）
- 否则降到 `PrevUserGroup`（购买时快照的原 tier）

**关键陷阱（v4 明确修复的）**：订阅期间用户又贡献了充值 → 累加 `total_donated` → 但**不改 group**（保持 `sub`），等订阅到期后 daily cron 按最新 `total_donated` 重算 tier。

**它解决什么问题？**
- **稳定预期**：付一次买 30 天，不用天天盯着钱包余额
- **给不愿贡献但想稳定用的用户一条路**：不用非得走"捐赠升 tier"，直接买断
- **合规化商业场景**：明码标价的月费，比"捐赠"更适合企业报销

**订阅 vs Tier 的选择路径：**

```
用户想稳定用 API
    ↓
不想每次担心额度不够
    ↓
有两种选择：
├─ 走 tier 路径：持续贡献 → 升 tier → 每日签到 + 高 RPM
│   → 适合活跃用户、能坚持"每天用一下"的人
│
└─ 走订阅路径：买 30 天池子 → 期间稳定
    → 适合"每月固定用一批"、"不想想那么多"的人
```

**这两条路径是并列的、可切换的、可共存的**。同一个用户可以是 `fan`（累计捐赠 $50）然后又买了一个订阅套餐，此时他 group 变 `sub`、订阅到期后回 `fan`。

---

### 3.4 商业档（下游转售）—— 完全独立的合同制

**它是什么？**  
`retail` / `wholesale` / `wholesale-plus` 三个组，**只能由 admin 手工分配**。给下游转售商用的。

**为什么完全独立？**  
下游转售商的商业模式跟公益站完全不同：
- 他们**批量买**，走合同价（GroupGroupRatio 里管理员单独谈的）
- 他们**不需要 tier 升级**（也不参与，不然合同价就乱了）
- 他们**不能买订阅**（订阅池的经济模型跟合同价冲突）

**RPM/并发（高得多）：**

| Group | RPM | 并发 | 可见渠道 |
|---|---|---|---|
| `retail` | 200 | 30 | 7 项 |
| `wholesale` | 800 | 50 | 14 项 |
| `wholesale-plus` | 1000 | 100 | 14 项（单价更低） |

**它解决什么问题？**  
让"下游 API 分销商"作为一等公民存在于系统里，不用为他们单独部署实例，也不会干扰公益 tier 体系的经济学。

---

## 4. 四块拼图的关系图（把上面的东西串起来）

```
                    ┌─────────────────────┐
                    │  用户注册 → free    │
                    │  (可能领欢迎码 +$2  │
                    │   到 quota_gift)    │
                    └──────────┬──────────┘
                               │
              ┌────────────────┼────────────────┐
              │                │                │
              ▼                ▼                ▼
        ┌─────────┐      ┌─────────┐      ┌──────────┐
        │ 什么都  │      │ 贡献    │      │ 买订阅   │
        │ 不做    │      │ 充值    │      │ 套餐     │
        └────┬────┘      └────┬────┘      └────┬─────┘
             │                │                │
             ▼                ▼                ▼
       每日签到得         total_donated       立刻变 sub
       gift 额度          累加 → 达到          获得独立
       (要升到            门槛 → 自动          30 天池子
        supporter+)        升 tier
             │                │                │
             │                │                │
             ▼                ▼                ▼
       烧 quota_gift    tier 决定：         烧订阅池
       (今日签到额        - 签到多少        (不受 gift
        为上限)            - RPM/并发         日限约束)
                          - 模型访问
                          - 渠道折扣
                                              │
                          ▲                   ▼
                          │              30 天后到期
                          │              回退到 PrevUserGroup
                          └──────────────────┘

    并行的 admin 手动通道：
    admin 手工改 user.group = 'retail'/'wholesale'/'wholesale-plus'
        ↓
    进入商业档：只走 quota_paid，走合同价，不参与上面任何自动化
```

---

## 5. 扣费顺序（`BillingPreference` 4 种模式）

这是 tokensheep 的**最关键运行时逻辑**。每次 API 请求都要走一遍。

用户可以自己在设置里选（默认 `subscription_first`）：

### 5.1 `wallet_only`
只烧钱包（付费余额 + gift 池），永远不动订阅池。

```
tryWallet:
    gift 池今日剩余额度 > 0 ？→ 扣 gift → 完成
    否则 → 扣 quota_paid → 完成
    都空 → 429
```

**注意：gift 池仍然会被扣**——因为 gift 是"钱包"的一个子池。要跳过 gift 只能等它用完或切模式。

### 5.2 `subscription_only`
只烧订阅池，永远不动钱包。

```
trySubscription:
    有活跃订阅 ？→ 扣订阅 → 完成
    否则 → 429
```

### 5.3 `wallet_first`
先钱包后订阅。

```
tryWallet → 成功 → 完成
       ↓ 不够
trySubscription → 成功 → 完成
       ↓ 不够
429
```

### 5.4 `subscription_first`（**默认**）
先订阅后钱包（但只在订阅套餐允许 `AllowWalletOverflow` 时才 fallback）。

```
有活跃订阅 ？
├─ 是 → trySubscription
│       ├─ 成功 → 完成
│       └─ 不够 + allow_overflow → tryWallet → 完成
│                                 └ 都不够 → 429
│       └─ 不够 + !allow_overflow → 429（不 fallback）
└─ 否 → tryWallet → 完成 / 429
```

---

## 6. 每个字段的完整生命周期

四个业务概念背后是 4 个关键字段。这些字段**在什么时候被谁改写**是排查 bug 的必备知识：

### 6.1 `User.QuotaGift`（赠送账户余额）

| 触发点 | 动作 |
|---|---|
| 每日签到 | `+= CheckinAwardByGroup[tier]` |
| 兑换码 | `+= 兑换券面额` |
| 每次 API 请求成功 | `-= 消耗`（今日累计不超日限） |
| 每日 cron 检查 | 30 天无请求 → 清零 |
| 超过 $50 上限 | 签到失败（"赠送池已满"） |

### 6.2 `User.Quota`（付费钱包余额）

| 触发点 | 动作 |
|---|---|
| 贡献充值 webhook | `+= 充值额`（同时累加 `total_donated`） |
| 标准充值 webhook | `+= 充值额`（**不**累加 `total_donated`） |
| 兑换码 | **不改**（兑换券只进 gift 池） |
| 每次 API 请求成功 | `-= 消耗`（在 gift 用完/超限之后被扣） |
| 退款场景 | `+= 退款额` |

### 6.3 `User.TotalDonated`（累计捐赠额，只升不降）

| 触发点 | 动作 |
|---|---|
| A 类用户的贡献充值 | `+= 充值额` |
| C 类（商业档）用户充值 | **不动**（这是 A/C 互斥的实现） |
| 兑换码 | **不动** |
| 订阅购买 | **不动**（订阅不算捐赠） |
| **永久保留**，账号删除前不会减 |

### 6.4 `User.Group`（用户身份）

| 触发点 | 动作 |
|---|---|
| 注册 | `= 'default'`（后续被 append_free hook 改成 `free`） |
| 贡献充值后达到新 tier 门槛 | `= new_tier`（A 类才触发） |
| 买订阅套餐 | `= 'sub'`（订阅期内固定） |
| 订阅到期 | `= 'PrevUserGroup'` 或 plan 的 `DowngradeGroup` |
| 每日 cron 检测 30 天无捐赠 | `= 'free'` |
| 每日 cron 按最新 total_donated 重算 | `= TierForDonation(total_donated)` |
| Admin 手工改 | `= 任意组`（包括商业档 `retail/wholesale/wholesale-plus`） |

---

## 7. 常见运营场景的完整数据流示例

### 7.1 场景 A：新用户注册 → 领欢迎码 → 试用

```
1. 用户注册
   → user.group = 'default' → append_free hook → 'free'
   → user.quota_gift = 0, quota = 0, total_donated = 0

2. 用户点击兑换码 → 输入欢迎码
   → user.quota_gift += $2
   → total_donated 不变，group 仍为 'free'

3. 用户发起 API 请求
   → BillingPreference 默认 subscription_first
   → 无订阅 → tryWallet
   → gift 池有 $2 > 今日日限 $0（free 组）→ 跳过 gift
   → quota_paid = 0 → 429 "quota insufficient"
   
   ⚠️ 关键：free 组签到额是 0，所以 gift 池的 $2 实际上他今天一分都不能用。
   要真的能用得升到 supporter+。
```

### 7.2 场景 B：新用户捐 $10 → 升级 supporter → 开始活跃

```
1. 用户走贡献充值 $10（比如 Waffo Pancake 通道）
   → webhook 回调 topup.go
   → user.quota += $10（付费钱包 +$10）
   → user.total_donated += $10
   → TierForDonation($10) = 'supporter'
   → user.group = 'supporter'（自动升组）

2. 用户第二天签到
   → CheckinAwardByGroup['supporter'] = $0.5
   → user.quota_gift += $0.5

3. 用户发起 API 请求
   → tryWallet
   → gift 今日剩余 $0.5 → 扣 gift → 完成

4. 一天烧了 $0.5 gift + $1 钱包 = $1.5 消耗

5. 后续 29 天用户没再捐款
   → daily cron 第 30 天检测：近 30 天充值 = 0
   → user.group = 'free'（降级）
   → 用户又变回 free，签到额度重置为 0
```

### 7.3 场景 C：fan 用户买订阅 → 30 天后回归

```
1. 用户当前 user.group = 'fan', total_donated = $50

2. 买订阅 A（¥100 = ~$14）
   → webhook 回调 subscription_payment_stripe.go
   → 创建 UserSubscription（amount_total = $15.4, end_time = now+30d）
   → user.group = 'sub'（临时切换）
   → user.PrevUserGroup（快照，存在 UserSubscription 里）= 'fan'
   → total_donated **不变**（订阅不算捐赠）

3. 订阅期间用户又捐了 $60
   → user.quota += $60
   → user.total_donated += $60 = $110
   → TierForDonation($110) = 'bestie'
   → 但 user.group 保持 'sub'（v4 特意修的：订阅期间不改 group）

4. 30 天后订阅到期
   → subscription_reset_task cron 检测
   → subscription.status = 'expired'
   → SubscriptionExpiryDowngrade 触发：
     - DowngradeGroup 为空 → 走 PrevUserGroup 逻辑
     - 但当前 group 已经被 daily cron 重算成 'bestie' 了
     - 所以什么都不做
   → 用户以 bestie 身份继续
```

### 7.4 场景 D：商业档新用户接入

```
1. admin 后台把 user.group 改成 'wholesale'
   → 无 webhook、无自动化

2. 用户充值 $500（走标准充值卡，不是贡献充值卡）
   → webhook 回调 topup.go
   → user.quota += $500
   → 检查 user.group IN CommercialGroups → 是
     → total_donated **不累加**（B2 修复项）
     → 不重算 tier
   → user.group 保持 'wholesale'

3. 用户前端访问订阅页
   → 前端检查 isCommercial → true
   → 显示 banner "您已是商业档"
   → disable "立即订阅" 按钮

4. 用户 API 请求
   → tryWallet
   → gift 池为 0（商业用户不签到）
   → 扣 quota_paid
   → 走 wholesale 组的 GroupGroupRatio 单价（管理员谈的合同价）
   → RPM 800，并发 50
```

---

## 8. 关键约束和踩过的坑（v4 生产验证过的）

这些是运营 8 个月踩出来的，不写清楚以后会重踩：

1. **兑换券必须进 gift 池，不能进钱包**——早期进钱包时用户以为"兑换 = 充值"，实际不是（不累加捐赠、不升 tier），投诉过。

2. **gift 池 UI 必须跟钱包**视觉分开**——早期合并展示，用户以为余额有 $50 实际不能全花（今日日限 $0.5），投诉过。

3. **商业用户充值不能累加 total_donated**——早期没区分，导致大合同用户"意外升 vip"，权限混乱。B2 修复。

4. **订阅期间贡献充值不能改 group**——早期改了会覆盖 `sub`，订阅到期时 `SubscriptionExpiryDowngrade` 找不到 `sub` 就不触发回退。B12 修复。

5. **`wallet_only` 仍然吃 gift 池**——运营刚开始困惑过，v4 明确写清楚：gift 是 wallet 的子池，要跳过只能等 gift 用完或切 `subscription_only`。

6. **daily cron 必须跳过商业档**——否则 30 天不"贡献"的商业用户会被降到 free，合同破产。B9 验证项。

7. **tier 门槛不是永久的**——就算累计捐了 $500，30 天不再捐就自动降到 free。这是"活跃度"机制。

8. **`sub` 组自己也是一个用户组**，也要配 RPM/并发/GGR，不然订阅用户拿不到合理的限流值。

9. **多容器部署的 option 同步靠 Redis pub/sub 广播**——早期没这个，改 tier 阈值后蓝绿容器行为不一致，B17 修复。

---

## 9. 结语

这四块（**赠送账户 + tier + 订阅 + 商业档**）看起来是四个独立功能，实际上是**一个用户在系统里四种共存状态**的表达：

- **赠送账户** = 你的"免费试用余额"，永远存在但受日限约束
- **tier** = 你在公益体系里的"等级 + 权限档位"，靠持续贡献维持
- **订阅** = 你可以临时给自己买一个"稳定 30 天池子"，跳过 tier 的贡献要求
- **商业档** = 你如果是下游转售商，可以走完全独立的合同价通道

它们通过 `user.group` 单字段串起来，通过 `BillingPreference` 4 种模式让用户自己选扣费顺序，通过 `total_donated` + daily cron 实现"公益激励 + 活跃度惩罚"的平衡。

**理解这四块的关系比理解任何单块的实现更重要**——因为大部分 bug 出在"边界处"（升 tier 时对 sub 组的处理、商业用户误触发 tier cron、gift 池日限跟 BillingPreference 交互等），而不是单块内部。

---

## 附录：文档索引

- **`docs/spec/economy-model-v4.md`**（权威业务模型，本文档基于它总结）
- `docs/spec/economy-model.md`（v3，已过时但决策日志可查）
- `docs/spec/vip-tier-decision.md`（vip 层为什么关闭）
- `docs/spec/daily-global-pool.md`（每日全局池特殊逻辑）
- `docs/spec/r16-1-billing-preference-hazard.md`（商业用户 BillingPreference coerce 分析）
- `docs/spec/upstream-rebase-plan.md`（与 upstream 的分歧全景）
- **`docs/spec/subscription-porting-to-100b.md`**（订阅系统技术移植设计）
- **`docs/spec/concurrency-porting-to-100b.md`**（并发限制技术移植设计）

**建议阅读顺序**：本文档 → 4 个 spec → 2 个 porting 文档。先业务后技术。
