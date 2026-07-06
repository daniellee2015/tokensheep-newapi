# TokenSheep 经济模型 Spec

**状态**: **Approved v3** · 已定稿 · 待落地
**最后更新**: 2026-07-06(v3 签到复用版,已批准)
**决策记录**: 见文末"决策日志"

---

## 一、目标

- **公益属性**:核心用户来自邀请制社群,不做公开注册商业化
- **可持续**:号池成本由自愿贡献者反哺,防止号池被薅穿
- **门槛感**:注册即用没有意义,贡献者要能感受到明显的等级差
- **可控性**:任何单个用户都不能一次性榨干号池,任何"僵尸账号"不能永久占资源

---

## 二、总览:钱包双池 + Tier 分组

### 2.1 钱包分两个池子

每个用户钱包同时有两笔独立的 quota:

| 字段 | 含义 | 来源 | 消耗顺序 | 约束 |
|---|---|---|---|---|
| `quota_paid` | **自付余额** | 用户 Pancake 贡献充值 | 后扣 | 永久保留,无日限,无过期,无封顶 |
| `quota_gift` | **赠送余额** | 每日赠送 + 兑换码 | 先扣 | 有封顶 $50,30 天无请求清零 |

**扣费顺序**: 先扣 `quota_gift`,扣光后再扣 `quota_paid`。

**用户体感**: "先花系统给的免费额度,自己的钱留到最后。"

### 2.2 Tier 分组表

| 分组 | 门槛 | RPM | 并发 | 每日赠送 | 首次升级即时赠送 | 可用模型分组 |
|---|---|---|---|---|---|---|
| **free** | 拥有欢迎码激活账号 | 10 | 1 | 无 | — | `basic` |
| **supporter** | 累计贡献 ≥ $10 **且** 最近 30 天内有贡献 | 20 | 3 | $0.5 | $0.5 | `basic` + `premium` |
| **fan** | 累计贡献 ≥ $50 **且** 最近 30 天内有贡献 | 30 | 5 | $3 | $3 | `basic` + `premium` |
| **bestie** | 累计贡献 ≥ $100 **且** 最近 30 天内有贡献 | 50 | 8 | $5 | $5 | `basic` + `premium` + `flagship` |
| **vip** | 累计贡献 ≥ $500 **且** 最近 30 天内有贡献 | 120 | 15 | $10 | $10 | `basic` + `premium` + `flagship` |

> **注**: 上表是默认种子值,所有分组的 RPM/并发/每日赠送额度/门槛/可用模型分组都在后台可编辑。代码不硬编码 tier 名单——`TierForDonation` 按 `TierThresholds` map 查最高档,管理员从后台新增/删除组无需改代码。

### 2.3 模型访问控制 (复用 new-api 原生 channel.group)

**架构说明**: new-api 用 `channel.group` 字段(逗号分隔字符串)声明"该渠道对哪些用户组开放"。不需要引入独立的"模型分组"抽象。

**每个 channel 配置时,在 `channel.group` 里指定允许的用户 tier**:

| 模型类型 | 示例模型 | channel.group |
|---|---|---|
| 便宜口粮 | haiku · gpt-5-mini · flash-lite | `"free,supporter,fan,bestie"` |
| 主流中档 | gpt-5 · sonnet · gemini-pro · grok-4 · deepseek · qwen | `"supporter,fan,bestie"` |
| 顶级旗舰 | claude-opus · o3 · o1 | `"bestie"` |

**优势**:
- 完全复用 upstream 原生机制,零架构改动
- 后台管理界面已有 UI 支持 channel.group 编辑
- 未来同步 upstream 无冲突

### 2.4 全局倍率

**`GroupRatio = 0.01`**(所有分组统一)

对用户扣费的完整公式:
```
每次请求扣费 = tokens × model_ratio × completion_ratio × 0.01
```

用户视角: **官方成本的 1%**,展示上等同"免费"。
你的成本视角: **1 美元用户 quota 对应 100 美元号池成本**(必须通过日限 + RPM + 号池 rpm 硬顶挡下最坏情况)。

---

## 三、用户全流程

```
公开注册 (Sign up)
    ↓
账号创建, quota_paid=0, quota_gift=0, group=free
    ↓
用户加入 Telegram/Discord 社群(人工审核)
    ↓
管理员发放"入群欢迎码"
    ↓
用户在网站输入欢迎码
    ↓
quota_gift += $2  (不动 quota_paid,不动 group)
    ↓
用户可以调用 `basic` 组模型(haiku/mini/flash-lite)
    ↓
用完 or 想解锁高级模型
    ↓
Pancake 贡献 $10 / $50 / $100
    ↓
quota_paid += 对应金额
累计贡献额检查 → 升组
触发一次"贡献即时赠送"(见 §4.2)
    ↓
用户享受更高 RPM/并发/更多模型/每日赠送
```

**关键机制**:
- 注册后光注册没有 quota,不能用 → 挡机器人
- 欢迎码是**你手工发**,不通过公开渠道
- 贡献是用户自主行为,直接影响 tier

---

## 四、贡献与赠送

### 4.1 贡献触发的事件

用户在 Pancake 完成一笔贡献,系统按顺序执行:

1. **入账**: `quota_paid += 贡献美元数`(1:1,展示为 $)
2. **累计**: `user.total_donated += 贡献美元数`(用于判定门槛)
3. **升组检查**: 根据"累计 + 最近 30 天有贡献" → 判定应属分组,如高于当前组则升级

**关键**: **贡献本身不送 quota_gift**。所有 quota_gift 都由每日 0 点 cron 统一发放。

用户想立即使用 → 用 quota_paid(贡献即入账,无日限,可立即使用)。
用户等赠送 → 第二天 0 点自动到账。

### 4.2 每日赠送机制 —— 复用 new-api 原生签到

**改造 new-api 原生 checkin 系统实现 tier 差异化每日签到**:

原生 checkin 系统已具备:
- `checkins` 表(user_id, checkin_date UNIQUE 约束)
- `POST /api/user/checkin` API
- `GET /api/user/checkin/status` API
- 前端页面已有

**改造点**:

1. **额度按 tier 决定**(不再是配置里的 min/max 随机):
   - free: **不能签到**(点击返回 "升级 tier 才能签到")
   - supporter: 签到即得 $0.5
   - fan: 签到即得 $3
   - bestie: 签到即得 $5
   - vip: 签到即得 $10
   - **额度由 `CheckinAwardByGroup` 后台配置**,新增分组时管理员在此 map 里加一行即可

2. **签到得到的 quota 走 `quota_gift`** 池(不是原生 `users.quota`)

3. **签到额度受封顶约束**: `if quota_gift + 签到额度 > 50 → 签到失败, 提示"赠送池已满"`

**天然优势**:
- 用户不主动签到 → 不送 quota_gift → **僵尸账号自动不送,不需要"30 天清零 cron"**
- 用户有仪式感,主动登录才领
- 表 + API + 前端都现成,复用现有代码

**签到机器人风险 = 不存在**:
- 免费组签到没奖励(直接被 tier 校验挡)
- 付费组签到 = 用户已经付过 $10 门槛,反哺赠送合理
- Turnstile 保护注册流程,机器人成本高

**Cron 简化**:
- 每日 0 点 cron **不再送 gift**,只做降组判定 + 30 天无请求清零(§4.4 保留)

### 4.3 赠送封顶

- **`quota_gift` 单账户累积上限 = $50**
- 每次赠送前判定: `if quota_gift + 本次赠送额 > 50 → 跳过本次赠送(不报错)`
- 用户 quota_gift 消耗到 < $50 后,下次赠送触发才生效

### 4.4 赠送清零 (30 天无请求) —— 仍保留

- 每天 0 点 cron 遍历所有用户
- `if now - user.last_request_at > 30 days → quota_gift = 0`
- **`quota_paid` 不动**(用户自己的钱永远保留)

**为什么还需要这条**:虽然签到天然过滤了不上线用户,但用户可能"上线签到不发起请求"(比如账号被泄露给他人签到累积)。这条兜底防止账户里 quota_gift 无限累积。

**如果用户上线只签到但不实际使用 API,quota_gift 也会累积到封顶 $50 但因为没有请求 →** 30 天后清零。

### 4.5 降组机制 (30 天无贡献)

- 每天 0 点 cron 遍历所有用户
- 判定"最近 30 天贡献额" = `sum(top_ups where created_time > now - 30d)`
- 判定"累计贡献额" = `user.total_donated`
- 根据下表决定用户 `group`:

```
if last_30d == 0:
    group = 'free'
elif total >= 100 and last_30d > 0:
    group = 'bestie'
elif total >= 50 and last_30d > 0:
    group = 'fan'
elif total >= 10 and last_30d > 0:
    group = 'supporter'
else:
    group = 'free'
```

**注意**: 降组后 `quota_paid` **不清空**(还可以用,但只能用 basic 模型和 free 组的 RPM/并发)。

---

## 五、兑换码

### 5.1 规格

**只 1 种兑换码,"入群欢迎码"**:
- `quota_gift += $2`
- 不动 `quota_paid`
- 不动 `group`(仍是 free)
- 一次性,单账号只能兑一次
- 兑换后 `quota_gift` 依然受封顶和 30 天清零约束

### 5.2 发放流程

- 管理员在后台批量生成
- 通过 Telegram/Discord 私信 or 群公告发放
- 每个用户手动索取,可结合社群认证机制

---

## 六、日限保护(号池防榨)

### 6.1 为什么需要日限

因为 `GroupRatio = 0.01`,用户 $1 quota 对应 $100 号池成本(100 倍杠杆放大)。

**最坏场景**: bestie 用户挂脚本刷 opus,RPM 50 × 60 × 24 × 每次 8k output × opus $75/M
```
= 72000 × 8000 × 75e-6 × 0.01(用户扣费)
= 用户扣费 $432/天(用户余额 $100 早烧完 → 靠 quota_gift $10/天 + quota_paid 撑)
= 号池实际成本 $43200/天(致命)
```

**RPM 硬顶保护**: RPM 50 卡在网关层,号池物理上限是号池账号 rate limit → 实际达不到 72000 请求

但**quota_paid 无日限**说明用户自付余额可以一次性烧光 → 就算号池撑住 rpm,单账户瞬时消耗依然可能拉爆队列

### 6.2 日限规则

**只对 `quota_gift` 扣费加日限**(自付无日限,自己烧自己承担):

| 分组 | 赠送池日限 |
|---|---|
| free | $0.5(实际无赠送,该值不生效) |
| supporter | $0.5 |
| fan | $3 |
| bestie | $5 |
| vip | $10 |

**日限 = 该分组的 `CheckinAwardByGroup` 值**,同一份配置驱动"签到额度"和"赠送池日限"两个语义,后台改一处即可。

**触发**: 当天 `quota_gift` 消耗达到日限 → 停止从 `quota_gift` 扣,自动切换到 `quota_paid` 扣

**注**: **日限 = 当日赠送额**,保证赠送池节奏一致,不会一天挤爆号池。

---

## 七、Pancake 收款

### 7.1 环境

- **live 环境**
- `MerchantID`: `MER_1nkyO95a8IUnDYgfpLJaXs`(与 100b 共用,不同 store 隔离)
- `PrivateKey`: `KEY_7mqj.pem`(存在服务器 `/data/tokensheep-newapi/keys/`)

### 7.2 Store & Product

**Store**: 新建 tokensheep store(区别于 100b 的 STO)

**1 个通用 Onetime Product**(与 100b 现有后端设计一致):

- 后端 `WaffoPancakeProductID` 只存单个 product ID
- Checkout 时按用户选中的金额动态传给 Pancake,Pancake 端 amount 是可变的
- 前端在贡献页展示 3 张卡片,每张对应一个"快捷金额"

| 卡片 | 站内价 | Pancake 实收 | 用户拿到 | 升组归宿 |
|---|---|---|---|---|
| **Supporter Pass** | $10 | $10 / 6.8 ≈ $1.47(汇率折算) | quota_paid +$10 | supporter |
| **Fan Pass** | $50 | $50 / 6.8 ≈ $7.35 | quota_paid +$50 | fan |
| **Bestie Pass** | $100 | $100 / 6.8 ≈ $14.71 | quota_paid +$100 | bestie |

站内展示 `$`, Pancake 实际收 USD 走 6.8 汇率折算(与 100b 一致机制)。累计贡献额 ≥ $500 会自动升到 vip(见 §2.2)。

### 7.3 汇率与手续费开关

- `WaffoPancakeApplyUSDExchangeRate = true` — 站内 $ 转 USD 时按 `USDExchangeRate` 折算
- `USDExchangeRate = 6.8`
- `WaffoPancakeSurchargePercent = 5.0` — 上送 Pancake 前加 5% 附加费(覆盖 pancake 平台费),用户站内充值到账仍是原额,附加费单独由用户在支付端付
- `WaffoPancakeUnitPrice = 1.0` — 站内 1 单位 = $1

### 7.4 Webhook

- `https://free.tokensheep.fun/api/waffo-pancake/webhook/live`
- 订阅事件: `payment.succeeded`, `payment.refunded`, `payment.disputed`

### 7.5 退款处理(未来阶段)

**当前不实现,列入 TODO**:

收到 `payment.refunded` → 期望行为:
1. 找到对应订单,`quota_paid -= 退款金额`(不小于 0)
2. `user.total_donated -= 退款金额`
3. 重新判定 tier → 可能自动降组
4. 若 `quota_paid` 变负数(用户已消耗完退款金额),需要人工介入判定

---

## 八、数据库模型变更

### 8.1 `users` 表新增字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `quota_gift` | bigint(same as quota) | 赠送池余额,cents |
| `total_donated` | bigint | 累计贡献额(仅通过 Pancake),cents |
| `today_gift_count` | int | 今日已触发赠送次数(0-5) |
| `last_request_at` | bigint(unix) | 最近一次 API 请求时间 |

**注**: `users.quota`(现有字段)重命名语义为 `quota_paid`,或不改字段名但在代码里区分含义。

**推荐**: **保留字段名 `users.quota` 作为 quota_paid**(避免大规模 migration),新增 `quota_gift`。

### 8.2 `top_ups` 表利用

new-api 原生已有 `top_ups` 表记录充值,已足够:
- 判定"最近 30 天贡献额" = `SELECT sum(money) FROM top_ups WHERE user_id=? AND created_time > NOW()-30d`
- 判定"累计贡献额" = `SELECT sum(money) FROM top_ups WHERE user_id=?` 或写入 `users.total_donated`

### 8.2.1 关键 map 配置(全部后台可改)

以下 5 张 map 都通过后台面板 `tokensheep_economy` 段编辑,key 是用户组名,value 是数值。新增/删除用户组只改这些 map,代码零改动:

| Map | 类型 | 语义 | 存储 |
|---|---|---|---|
| `SessionLimits` | `map[string]int` | 每组最大在飞请求数 | `EconomySetting.SessionLimits` |
| `CheckinAwardByGroup` | `map[string]int` | 每组每日签到奖励(quota cents),同时也是每日赠送池扣费上限 | `EconomySetting.CheckinAwardByGroup` |
| `TierThresholds` | `map[string]int` | 每组的累计贡献门槛(quota cents),`TierForDonation` 按此 map 判定升组 | `EconomySetting.TierThresholds` |
| `ModelRequestRateLimitGroup` | `map[string][2]int` | 每组 RPM(new-api 原生 option) | `setting.ModelRequestRateLimitGroup` |
| `channel.group` | `varchar` (逗号分隔) | 每个 channel 声明"哪些用户组可用",new-api 原生字段 | `channels` 表 |

**运维流程 —— 新增用户组** (例如未来加一个 `whale` 层):
1. 后台 -> 用户分组 新建 `whale`
2. 后台 -> Economy(tokensheep_economy) 面板给 `SessionLimits/CheckinAwardByGroup/TierThresholds` 各加一行 `"whale": <值>`
3. 后台 -> RateLimit 面板给 `ModelRequestRateLimitGroup` 加一行 `"whale": [total, success]`
4. 需要开放旗舰模型给 whale,就编辑对应 channel 的 `group` 字段加 `whale`

**运维流程 —— 删除用户组**:反向操作,`TierForDonation` 会自动降级到剩下的最高档。

### 8.3 新增 cron

**Cron 1: 每日 0 点主任务**(降组 + 30 天清零)

```pseudo
for user in users:
    # 1. 判定 tier
    total = user.total_donated
    last_30d = sum(top_ups[user_id=user.id, time > now-30d].money)
    
    if last_30d == 0:
        new_group = 'free'
    else:
        # 从 TierThresholds map 里挑门槛 <= total 的最高一档
        # (代码见 setting/tokensheep_setting/economy.go: TierForDonation)
        new_group = tier_for_donation(total)  # 默认: free < supporter($10) < fan($50) < bestie($100) < vip($500)
    
    user.group = new_group
    
    # 2. 30 天无请求 → 清零 gift
    if now - user.last_request_at > 30 * 86400:
        user.quota_gift = 0
    
    user.save()
```

**签到 gift 发放**:不走 cron,走用户主动调用 `POST /api/user/checkin`(见 §4.2 改造点)

### 8.4 关键逻辑注入点

- **扣费顺序** — `service/quota.go` 或 `middleware/distributor.go`,先扣 `quota_gift` 再扣 `quota`
- **贡献 webhook** — `controller/topup_waffo_pancake.go`,收到成功后触发升组(**不动 quota_gift**)
- **签到改造** — `model/checkin.go` 的 `UserCheckin()` 函数改造,按 user.group 决定 quotaAwarded,写入 `quota_gift` 而非 `quota`
- **签到 tier 校验** — free 组签到直接返回 "升级 tier 才能签到"
- **RPM/并发/model_group** — 已由 `abilities` 表和 `user_groups` 表原生支持

---

## 九、初始配置数据

### 9.1 用户分组(user_group)

```
free       : ratio=1, priority=0, enable_groups=["basic"],                          rpm=10,  concurrent=1
supporter  : ratio=1, priority=1, enable_groups=["basic","premium"],                rpm=20,  concurrent=3
fan        : ratio=1, priority=2, enable_groups=["basic","premium"],                rpm=30,  concurrent=5
bestie     : ratio=1, priority=3, enable_groups=["basic","premium","flagship"],     rpm=50,  concurrent=8
vip        : ratio=1, priority=4, enable_groups=["basic","premium","flagship"],     rpm=120, concurrent=15
```

**注**: 上表是首次部署时的种子值,后续所有 tier 的这些字段都在后台可编辑。`rpm/concurrent` 分别来自 `ModelRequestRateLimitGroup` 和 `SessionLimits`,`enable_groups` 是 `user_groups` 表原生字段。

**注**: 所有 tier 的 `ratio` 都是 1(不再用 group_ratio 做倍率),真实的 0.01 倍率通过 `GroupRatio` 全局配置或每个模型的 `model_ratio` 直接调整。

### 9.2 模型分组(model.group)

| 模型 | group |
|---|---|
| `claude-haiku-4-5` | basic |
| `gpt-5-mini` | basic |
| `gemini-2.5-flash-lite` | basic |
| `gpt-5` | premium |
| `claude-sonnet-5` | premium |
| `gemini-2.5-pro` | premium |
| `grok-4` | premium |
| `deepseek-r1` | premium |
| `qwen-max` | premium |
| `claude-opus-4-8` | flagship |
| `o3` | flagship |
| `o1` | flagship |

### 9.3 关键 options

```
QuotaForNewUser = 0                           # 注册不送
QuotaForInviter = 0                           # 邀请系统不启用
QuotaForInvitee = 0                           # 邀请系统不启用
GroupRatio = 0.01                             # 全局倍率
DefaultUserGroup = free                       # 默认组
CheckinEnabled = true                         # 签到启用(改造为 tier 差异化)
```

**邀请系统决策**: 不启用。用户唯一入口 = 注册 → 加社群 → 领欢迎码。防止邀请刷号漏洞。

---

## 十、UI 展示

### 10.1 Landing 页 Tier Section(已完成)

**当前公开展示的 3 档**(vip 通常不对外挂):
- T1 supporter: **$10** · RPM 20 · 并发 3 · 每日赠送 $0.5 · +premium
- T2 fan:      **$50** · RPM 30 · 并发 5 · 每日赠送 $3 · +premium (Popular)
- T3 bestie:   **$100** · RPM 50 · 并发 8 · 每日赠送 $5 · +flagship

**vip 层**($500 门槛,RPM 120 · 并发 15 · 每日赠送 $10)默认在 landing 上**不公开显示**,仅作为高贡献用户的自动升级归宿存在——由后台 map 配置驱动,前端展示是否挂出可运营层决定。

### 10.2 钱包页

需要展示两个池子:
- **自付余额** `$X.XX` (来自贡献,永久)
- **赠送余额** `$X.XX / $50` (今日已用 $Y.YY / 日限 $Z.ZZ)
- 展示"下次赠送": 若 today_gift_count < 5 → "下次贡献将获得 $X 赠送",否则"今日赠送已达 5 次上限"

### 10.3 贡献页

3 张卡片对应 3 个 Pancake product:
- Supporter Pass · $10 · 解锁 supporter tier
- Fan Pass · $50 · 解锁 fan tier(推荐,标 Popular)
- Bestie Pass · $100 · 解锁 bestie tier

**vip 升级路径**:累计贡献额 ≥ $500 自动升组,不做独立产品卡(靠反复贡献叠加达成)。

---

## 十一、风险清单

| 风险 | 缓解 |
|---|---|
| 用户挂脚本刷 opus 榨号池 | RPM 硬顶 50 + `quota_gift` 日限 $10 + `quota_paid` 用完停 |
| 一天多次贡献刷赠送 | 每日最多 5 次赠送触发 |
| 用户攒赠送不上线 | 封顶 $50 + 30 天无请求清零 |
| 用户贡献一次后长期白嫖 | 30 天无贡献自动降组 |
| 兑换码泄露 | 单账号只能用一次 + 手工发放不走公开 |
| 机器人批量注册 | 注册后无 quota,不能用 → 天然拦截 |
| Pancake chargeback 骗保 | (待办)webhook 收 refunded 事件 → 退款金额 → 降组 |
| 号池整体 rpm 到顶 | Tier priority 保证 bestie 用户拒 429 优先级最高 |

---

## 十二、TODO / 未来阶段

- [ ] Pancake 收 refund/chargeback 事件的退款降组
- [ ] Pancake 手续费 0.5% 加在上送金额里
- [ ] Landing 页 Tier Section 同步最终数字
- [ ] Wallet UI 展示 quota_paid + quota_gift 双池
- [ ] Admin 面板生成入群欢迎码批量脚本
- [ ] 用户自助查看"我离下一档 tier 还差多少"提示

---

## 决策日志

| 日期 | 决策 |
|---|---|
| 2026-07-06 | 全局倍率 0.01 |
| 2026-07-06 | 站内 `$` 展示按 ¥ 6.8 汇率折算到 Pancake USD |
| 2026-07-06 | Pancake live 环境, `KEY_7mqj.pem` |
| 2026-07-06 | Tier 门槛 累计 $10/$50/$100 + 最近 30 天有贡献 |
| 2026-07-06 | RPM 10/20/30/50, 并发 1/3/5/8 |
| 2026-07-06 | 每日赠送 supporter $0.5 / fan $3 / bestie $5 |
| 2026-07-06 | **贡献不送 quota_gift**,只做 quota_paid + 升组 |
| 2026-07-06 | **赠送 = 复用 new-api 签到系统 tier 差异化**,用户主动签到才送 |
| 2026-07-06 | free 组不能签到,支付 tier 签到得 $0.5/$3/$5 |
| 2026-07-06 | 赠送池封顶 $50, 30 天无请求清零 |
| 2026-07-06 | 只 1 种兑换码,入群欢迎码送 $2 到 quota_gift |
| 2026-07-06 | 钱包分池 quota_paid(永久) + quota_gift(约束) |
| 2026-07-06 | 扣费顺序 先 gift 再 paid |
| 2026-07-06 | 降组:30 天无贡献 → free;其他按累计门槛判定 |
| 2026-07-06 | 邀请系统不启用(QuotaForInviter=0, QuotaForInvitee=0) |
| 2026-07-06 | **Spec Approved v3**,准备落地 |
| 2026-07-06 | v3.1 补:vip 层加入(门槛 $500 / RPM 120 / 并发 15 / 每日赠送 $10);landing 默认不展示,仅内部升组归宿 |
| 2026-07-06 | v3.1 补:强调所有 tier 相关参数都是后台可改 map,`TierForDonation` 按 map 查最高档,不再硬编码 tier 名单 |
| 2026-07-06 | v3.1 补:Pancake 复用 100b 的"单一 product + 动态 amount"设计,不做 3 独立 product;3 档由前端卡片选择快捷金额 |
