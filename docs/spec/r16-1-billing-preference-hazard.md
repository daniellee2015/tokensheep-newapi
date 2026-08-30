# R16-1 subscription_only 语义-显示不一致 hazard 分析

**状态**: 待决策，未实施
**归档时间**: 2026-08
**关联**: R16-1 (BillingPreferenceCard commercial disable-not-filter)、R17 verify 阶段 wz0a1p1l1

---

## 一、问题场景

### 触发条件

用户 A 满足下面三个条件时会踩坑：

1. 在**未被划入商业分组**的时期，通过钱包页 BillingPreferenceCard 把 `billing_preference` 显式改为 `subscription_only`（或已经处于该值，例如用户手动测试过、或历史脚本迁移遗留）；
2. 之后管理员把用户 A 划入某个商业分组（`retail` / `wholesale` / `wholesale-plus`，即 `EconomySetting.CommercialGroups` 中命中的 key），或者用户 A 因订单晋升自动进入商业分组；
3. 用户 A 没有主动重新打开钱包页 BillingPreferenceCard 并显式改回 `wallet_first` / `wallet_only`。

### 具体链路

- **持久化层**：`user_setting.billing_preference` 列（通过 `model.UpdateUserSetting` 写入）保留 `subscription_only`，无任何 hook / 迁移会因分组变更而 rewrite。
- **前端读取**：`GET /api/subscription/self` 返回 `billing_preference = "subscription_only"`（`controller/subscription.go:94` 经过 `NormalizeBillingPreference` 但该值本身合法，原样返回）。
- **前端显示 coerce**：`BillingPreferenceCard` 检测到 `isCommercial === true`，把 trigger label coerce 成 `wallet_first`（`web/src/features/wallet/components/billing-preference-card.tsx:187-190`）。dropdown 里 `subscription_first` / `subscription_only` 两项 disable 并挂 `(订阅池不可用)` 说明。用户看到的是「优先钱包」这四个字。
- **前端持久值**：`setPreference(raw)` 仍把 `subscription_only` 存入组件本地 state；显示层 coerce 不写回 `user_setting`。
- **后端 dispatch**：任意一次真实计费请求进入 `service.NewBillingSession`（`service/billing_session.go:369`）时，`pref := NormalizeBillingPreference(...)` 仍取到 `subscription_only`，switch 落到 `service/billing_session.go:424` 的 `case "subscription_only": return trySubscription()`。
- **hard 429**：商业用户没有活跃订阅（`AdminBindSubscription` 极少覆盖商业用户；`SubscriptionRequestBalancePay` 被 `rejectSubscriptionForCommercialUser` 拒绝）。`trySubscription` 里 `SubscriptionFunding.PreConsume` 走 `PreConsumeUserSubscription`，无活跃订阅时抛 `no active subscription`，`preConsume` 里被 `strings.Contains(errMsg, "no active subscription")` 命中（`service/billing_session.go:233-234`），返回 `types.ErrorCodeInsufficientUserQuota` + `http.StatusForbidden`。

  注：这实际是 **403** 而不是 429（用户在描述里说的「硬 429」是口头说法，真实响应是 403 + `订阅额度不足或未配置订阅: no active subscription`，重试无意义）。用户体感一致：钱包里有钱、UI 说「优先钱包」、请求全挂。

### 用户观察到的现象

- 钱包页 BillingPreferenceCard 显示「优先钱包」，dropdown 展开可看到 subscription_* 灰色 + 「(订阅池不可用)」；
- 钱包余额充足；
- 任意一次 API 调用都拿到 403 + `订阅额度不足或未配置订阅` 类信息；
- 用户困惑：UI 明明说走钱包，为什么后端说没订阅？

---

## 二、代码根因

三处代码对 `subscription_only` 的处理不一致：

### 根因 1: 前端只 coerce 显示，不 coerce 值

`web/src/features/wallet/components/billing-preference-card.tsx:184-190`

```tsx
const isSubscriptionOption = (p: Preference) =>
  p === 'subscription_first' || p === 'subscription_only'

const displayValue: Preference =
  isCommercial && isSubscriptionOption(preference)
    ? 'wallet_first'
    : preference
```

`displayValue` 只影响 `<Select value={...}>` 和 label 渲染。`preference` state（源自 `getSelfSubscriptionFull()` 返回的 `raw`）保持 `subscription_only`。用户不触发 `onValueChange` 就没有 `PUT /api/subscription/self/preference` 调用，DB 值不变。

组件 header 注释 (line 180-183) 明确写了这是有意的：「the persisted preference value stays as-is until they explicitly change it」。R16-1 当时的取舍是「不要在用户没确认的情况下改 DB」。

### 根因 2: 后端 `NormalizeBillingPreference` 不感知商业身份

`common/str.go:111-119`

```go
func NormalizeBillingPreference(pref string) string {
    switch strings.TrimSpace(pref) {
    case "subscription_first", "wallet_first", "subscription_only", "wallet_only":
        return strings.TrimSpace(pref)
    default:
        return "subscription_first"
    }
}
```

这个函数只做「合法四选一」clamp，不接受 `*User` 或 group 参数，无法根据商业身份 coerce。`controller/subscription.go:94`（`GetSubscriptionSelf`）和 `controller/subscription.go:122`（`UpdateSubscriptionPreference`）以及 `service/billing_session.go:369`（`NewBillingSession`）三处调用都一视同仁。

### 根因 3: dispatch switch 没有商业分组分支

`service/billing_session.go:423-464`

```go
switch pref {
case "subscription_only":
    return trySubscription()
case "wallet_only":
    return tryWallet()
case "wallet_first":
    session, err := tryWallet()
    if err != nil {
        if err.GetErrorCode() == types.ErrorCodeInsufficientUserQuota {
            return trySubscription()
        }
        return nil, err
    }
    return session, nil
case "subscription_first":
    fallthrough
default:
    hasSub, subCheckErr := model.HasActiveUserSubscription(relayInfo.UserId)
    ...
    if !hasSub {
        return tryWallet()
    }
    ...
}
```

注意 `subscription_first` 分支特意做了 `HasActiveUserSubscription` 检查，无订阅时自动回退 `tryWallet()`。`subscription_only` 分支**没有类似的兜底**，直接进 `trySubscription()`，无活跃订阅就 403。设计意图是「_only 就是用户显式要求硬性走这个池，没就报错」，对个人用户合理，对商业用户被卡在旧值时不合理。

### 三处结论

- 前端：**显示 coerce，值不 coerce**（R16-1 有意为之）；
- `NormalizeBillingPreference`：**不感知身份**（v4 §3.1 定的四选一语义，通用函数）；
- dispatch：**`subscription_only` 分支无兜底**（v4 语义「\_only 硬性」的直接体现）。

三处独立看都自洽，合起来在「商业用户 + DB 里有 subscription_only 旧值」这一交集上产生 hazard。

---

## 三、影响范围

### 谁会踩

- **已存在的商业用户**中，`user_setting.billing_preference` 值为 `subscription_only` 或 `subscription_first` 的人。
  - `subscription_first` 因为 dispatch 里的 `HasActiveUserSubscription` 检查 + `tryWallet()` fallback，实际表现和 `wallet_first` 一致，**不产生 hazard**；
  - `subscription_only` 是唯一会产生 403 的路径。
- **未来新晋升到商业分组的用户**：晋升前若他们改过 preference 到 `subscription_only`，晋升后会立刻踩坑。

### 生产影响估算

- 目前 `billing_preference` 默认值是 `subscription_first`（`NormalizeBillingPreference` 的 default 分支）。**只有主动改过的用户**才会命中 `subscription_only`。
- 通过 SQL 可以精确列出踩坑用户集合（未实施；示例 query 供决策时参考）：

  ```sql
  SELECT u.id, u.username, u."group", us.setting
  FROM users u
  JOIN user_settings us ON us.user_id = u.id
  WHERE us.setting::jsonb->>'billing_preference' = 'subscription_only'
    AND u."group" IN ('retail','wholesale','wholesale-plus');
  ```

  （具体表名/列名以本仓库 schema 为准，未实测。）

- 影响面**大概率个位数**——需要「用户曾主动改过 preference」+「后被划入商业组」两个低概率事件同时发生。但一旦踩到就是 100% 请求失败，对该用户是重度影响。
- Playground / 无令牌调用同样受影响，因为 dispatch 是入口层。

### 其他附加影响

- 用户改到 `subscription_only` 后**自己升级到商业组**（例如通过订单晋升，如果 tokensheep 有这条路径）也会即时踩坑；
- 商业用户被 admin 从商业组划出（回到普通组）后，`subscription_only` 会恢复原语义，此时若没订阅仍是 403，但这是**用户主动选择的语义**（他要求硬性走订阅池），不算 hazard。

---

## 四、三种修法 tradeoff

### 修法 A: 前端 `useEffect` 加载时自动写回 wallet_first

**思路**：BillingPreferenceCard 加载 `getSelfSubscriptionFull()` 拿到 `raw` 后，如果 `isCommercial && isSubscriptionOption(raw)`，立刻 fire `updateBillingPreference('wallet_first')` 把 DB 值写掉。

**动到的文件**（估算行数）:
- `web/src/features/wallet/components/billing-preference-card.tsx` line 121-139（`useEffect` 内 `load()` 分支加 ~10 行）；
- `web/src/features/wallet/components/__tests__/billing-preference-card.test.tsx` 新增 1 个 case（~30 行）。

**需要什么测试**:
- Vitest 单测：mock `getSelfSubscriptionFull` 返回 `subscription_only` + `isCommercial=true`，assert `updateBillingPreference` 被以 `'wallet_first'` 调用一次；
- 反向测：`isCommercial=false` 时不调；`raw=wallet_first` 时不调（防止 idempotent 循环）。

**potential regression**:
- 用户在**未加载完钱包页**的情况下发起 API 请求时，DB 仍是 `subscription_only`——前端修法救不了「后端 dispatch 早于用户开 wallet」的场景（例如 CI / MCP 客户端从来不开 UI）。
- 万一 `isCommercial` prop 因上游 tier 查询延迟而在首次 render 时是 `false`、之后变 `true`，`useEffect` deps 需要跟着写；否则第一次 load 已经跑完，切到 commercial 时不再触发 rewrite。当前 `useEffect(..., [])` 只跑一次，需要改成 `[isCommercial]` 并加去重 flag。

**优点**:
1. 改动范围最小，仅前端一个文件；
2. 一次纠正、永久生效——用户下次任何请求都拿到正确 preference；
3. 不影响 v4 §3.1 语义定义，`subscription_only` 对个人用户仍是硬性含义。

**缺点**:
1. **不触发 UI 的用户救不到**（纯 API 用户、被 admin 划入商业组但没登过 web 的老用户）——最严重的场景反而没覆盖；
2. 静默写 DB，用户没收到 toast 就被改了 preference，事后翻 UI 看不出变化，审计追踪需要看 admin log；
3. 需要处理 race（`isCommercial` 异步到达 + useEffect deps）容易漏边界，回归风险来自这一点。

---

### 修法 B: 后端 dispatch 层本地 coerce（不写 DB）

**思路**：`service/billing_session.go` `NewBillingSession` 在 switch 之前先查用户分组，如果 `IsCommercialGroup(group) && (pref == "subscription_only" || pref == "subscription_first")`，把局部变量 `pref` 覆盖成 `"wallet_first"`，然后正常进 switch。**不写 DB**，只影响当前请求的路由决策。

**动到的文件**（估算行数）:
- `service/billing_session.go` line 364-370 附近加 ~10 行（查 group + coerce + log）；
- 新增或扩展 `service/billing_session_test.go`（当前仓 grep 未发现该文件，可能需要新建；~50 行测试）。

新增依赖：从 `relayInfo` 或 `model.GetUserGroup(userId, true)` 拿到 group。查看代码，`NewBillingSession` 已经能拿到 `relayInfo.UserId`，`model.GetUserGroup` 有缓存版本可用（`controller/subscription.go:30` 有先例）。

**需要什么测试**:
- Unit：mock `model.GetUserGroup` 返回 `wholesale`，`relayInfo.UserSetting.BillingPreference = "subscription_only"`，assert 走 `tryWallet` 而不是 `trySubscription`；
- Integration：起 gin 用 test DB，一个 wholesale 用户 + `subscription_only` 的 setting，POST `/v1/chat/completions` 拿到 200 而不是 403；
- 反向：非商业用户 `subscription_only` 仍走 `trySubscription`（v4 §3.1 原语义不能破坏）。

**potential regression**:
- **每次计费请求多一次 `GetUserGroup` 调用**——虽然 `common/model.GetUserGroup(id, true)` 走缓存，但热路径新增缓存 lookup 需要 profile；
- 如果 `GetUserGroup` 报错，需要决定 fail-open（当作非商业）还是 fail-closed（拒绝请求）。fail-open 会重新引入 hazard（缓存挂了就 403），fail-closed 会把 group 查询变成计费的硬依赖（当前 `subscription_first` 分支里 `HasActiveUserSubscription` 已经是硬依赖，因此加一次不算破坏对称性）；
- 用户看到的 UI 和实际执行开始一致（好事），但 `user_setting.billing_preference` 仍是旧值——如果他们**退出商业组**后，`subscription_only` 会立刻恢复原语义，可能给用户「怎么突然变了」的困惑。

**优点**:
1. **覆盖全部触发路径**：API / MCP / 未登陆 web 的用户都能被救到，前端不触及也生效；
2. 不改 DB，可逆——万一发现 coerce 规则错了，回滚只需删代码，不用洗数据；
3. 语义上贴合 v4 §3.1 意图：「商业用户根本不参与订阅池」被明确编码进 dispatch。

**缺点**:
1. **热路径新增缓存查询**，每次请求都过（虽然是 in-memory cache，但 profiling 前存在心智负担）；
2. UI 显示（coerce 到 wallet_first）和后端行为一致了，但 `user_setting` DB 值仍是 `subscription_only`——用户如果通过 `GET /api/subscription/self` 或数据库直查会看到不一致，长期存在污染；
3. `NormalizeBillingPreference` 保持通用（好），但商业身份的判断散在 dispatch 层，未来加第 5 个 preference 时容易漏 coerce 分支。

---

### 修法 C: 扩 `NormalizeBillingPreference` 签名 + 修所有调用点

**思路**：给 `common.NormalizeBillingPreference` 加参数（`*User`、`isCommercial bool` 或直接 `group string`），把商业身份的 coerce 规则**塞进函数本身**。所有调用点（`controller/subscription.go` 两处 + `service/billing_session.go` 一处）跟着改。可选地：`UpdateSubscriptionPreference` PUT 时也 coerce，把 DB 值一起洗掉。

**动到的文件**（估算行数）:
- `common/str.go` line 111-119（函数签名 + body，~20 行）；
- `controller/subscription.go` line 94、line 122 两处调用（每处 ~5 行改动，需要先查 group）；
- `service/billing_session.go` line 369（改调用，~3 行）；
- 视是否走「PUT 时洗 DB」路线，额外改 `UpdateSubscriptionPreference` 使写入的值也是 coerce 后的；
- 测试文件：`common/str_test.go`（如存在）扩展 `TestNormalizeBillingPreference`，`controller/subscription_test.go` 和 `service/billing_session_test.go` 各加 case。

（当前仓库没 grep 出 `common/str_test.go`，可能需要新建。）

**需要什么测试**:
- Unit：`NormalizeBillingPreference("subscription_only", isCommercial=true)` 返回 `"wallet_first"`；`isCommercial=false` 返回 `"subscription_only"`；空字符串两侧都返回 `"subscription_first"`；
- Integration：三个调用点（`GetSubscriptionSelf` GET、`UpdateSubscriptionPreference` PUT、`NewBillingSession` 计费）分别验证 coerce 生效；
- 反向：普通用户所有 4 个值正确 clamp、正确 return，无 regression。

**potential regression**:
- **签名破坏**——`NormalizeBillingPreference` 是通用工具，如果有其他调用者（当前 grep 只见 3 处但存量项目常有）没跟着改，会编译失败。相对而言 Go 编译期能兜住，风险是「改动 blast radius 大」而非「运行时挂」；
- 如果选「PUT 也洗 DB」路线：用户在**未来退出商业组**后，之前想选的 `subscription_only` 就永久丢失了（DB 里已经是 `wallet_first`），要恢复得再手动改；
- 需要小心 v4 §3.1 规范：把商业身份融进「一个专门做四选一 clamp 的函数」在**语义上是 overloading** ——`Normalize` 名字暗示「合法性 clamp」，加身份 coerce 后名字应该是 `ResolveBillingPreference(pref, user) string` 之类，否则未来的人读到 `NormalizeBillingPreference("subscription_only", user)` 会困惑「Normalize 怎么会返回 wallet_first」；改名又是一次调用点扫荡。

**优点**:
1. **单一事实源**——商业身份 + preference 的映射只在一处定义，所有调用点自动一致；
2. 可以选择性地在 PUT 时洗 DB，从根上消除污染（若选此路线）；
3. 未来加 preference 值时新逻辑只在 `NormalizeBillingPreference` 里改，dispatch 和 controller 不用碰。

**缺点**:
1. **改动面最大**（4-5 个文件 + 测试），review 成本最高；
2. 函数改名建议同时进行（`NormalizeBillingPreference` → `ResolveBillingPreference`），进一步扩大 blast radius；
3. 「PUT 时洗 DB」如果选择，用户退出商业组后原 preference 无法恢复；不选则和修法 B 一样存在 DB 污染。

---

## 五、推荐

**倾向**：**短期 B 快速止血，长期 C 是正解**。修法 A 因「不触发 UI 的用户救不到」不适合作为唯一方案，可作为 B 的补充（B 已经能兜底所有请求，A 主要是让用户在 UI 层看到值真的被写回，减少困惑，但不是必需的）。

理由一句话：**hazard 的关键是「后端 dispatch 在错误的 pref 值上走了错误的路径」，止血必须发生在 dispatch 层（B）**；C 只是把 B 的 coerce 规则抽出来变成可复用工具，是重构而非修 bug。

---

## 六、待用户决策的问题

1. **要不要洗 DB？** —— 修法 B 只在请求路径上 coerce，DB 里 `subscription_only` 长期残留。修法 C 里可以选择在 PUT / 计费时都写回 DB。你偏好可逆（B）还是干净（C+洗）？
2. **`GetUserGroup` fail-open 还是 fail-closed？** —— 修法 B 里如果 group 查询报错，我们是当作非商业用户放行（现有行为，可能 403）还是拒绝请求（保守但会把 group 查询变成计费硬依赖）？
3. **修法 A 要不要作为 UI 层补充？** —— 即使选 B 或 C，前端要不要在加载 wallet 页时同步 fire 一次 PUT，让用户下次进 UI 看到值已改？还是就让 UI 一直显示 coerce 后的 label、DB 保留原值？
4. **`NormalizeBillingPreference` 改名（修法 C）？** —— 如果选 C，要不要顺便改名成 `ResolveBillingPreference` 以反映「不只是 clamp」这一语义？
5. **要不要单独对 `subscription_first` 也 coerce？** —— 目前 `subscription_first` 对商业用户有 `tryWallet` 兜底（`HasActiveUserSubscription` 返回 false 时），实际不产生 403。但显示层已经 coerce 了 `subscription_first` 和 `subscription_only` 两个。要不要保持显示/dispatch 对称，还是只处理 `subscription_only` 一个真实 hazard？
