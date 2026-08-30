# super-grok 渠道状态调查 (R17-D)

生产: `https://free.tokensheep.fun` · 调查日: 2026-08-31 · 分支: `tokensheep-r17-grok-survey`

只读调查报告. 不涉及任何配置或代码改动.

## 1. 结论 (TL;DR)

`super-grok` 是一个**只以负面清单形式存在**的"幽灵分组":

- **每一处正向配置里都没有它** — GroupRatio, GroupGroupRatio, UserUsableGroups, AutoGroups, 任何 channel 的 `group` 字段, 任何模型的 `enable_groups` 都不含 `super-grok`.
- **GroupSpecialUsableGroup (GSU) 里每个用户组都有 `-:super-grok` 删除标记** (10/10 用户组), 但因为顶层 UUG 不含 `super-grok`, 每一条都是 no-op.
- **没有副作用, 没有 500 风险.** 唯一可能的入口是"手动把 token.Group 设成 super-grok", 走到 auth middleware `service.GetUserUsableGroups(userGroup)[tokenGroup]` 查不到, 立即 **403 "无权访问 super-grok 分组"** 结束.

它就是一个 defensive 占位符 — 上游历史上可能启用过, 现在退化成"每个用户组都记得要拒绝它"的负面清单. **没有真正启用的路径.**

---

## 2. 逐项证据

### 2.1 GroupRatio 里有 super-grok 吗?

**没有.** 生产 `GroupRatio` 20 个 key (整理排序):

```
GPT-Enterprise, GPT-Plus, GPT-Pro, GPT-Pro-Stable, GPT-Pro-sale,
aws-b, aws-q, aws-q-stable, bestie, claude-distill, claude-lowprice,
claude-max, claude-max-sale, claude-max-stable, claude-sale, claude-supporter,
fan, free, gemini-lowprice, gemini-sale, gemini-stable, gemini-supporter,
gpt, gpt-lowprice, grok-sale, grok-supporter, image, kirobus-api,
mix-lowprice, retail, supporter, vip, wholesale, wholesale-plus
```

`super-grok` **不在其中**.

副作用: `ratio_setting.ContainsGroupRatio("super-grok") == false`. 中间件里两个 gate (`service.IsUserSelectableGroup` 的第二个 `&&`, `middleware/auth.go:468` 的 `if !ContainsGroupRatio(tokenGroup)`) 都会拒绝它 — 但见 2.5, 第一个 gate 就已经拦下了.

### 2.2 GroupGroupRatio 里哪些用户组配了 super-grok 倍率?

**零个.** 生产 GGR 8 个 userGroup (fan/vip/free/promo/bestie/retail/supporter/wholesale/wholesale-plus), 每一个内部映射的 usingGroup 键都不含 `super-grok`. `retail` 里有 `grok-sale`/`grok-supporter`; `wholesale`/`wholesale-plus` 也有那两个 — **没有 super-grok**.

Ledger 视角: super-grok 从没被写入过任何倍率. 就算某个方案启用了 super-grok, 它的价格倍率会 fallback 到 `GetGroupRatio("super-grok")` — 而该函数在 key 不存在时会 `SysLog("group ratio not found: super-grok")` 并返回 `1.0`. 也就是说这是**默认 1x 定价**, 不是"死配置", 是"根本没配置".

### 2.3 channels 表里有 group 包含 super-grok 的渠道吗?

**零条.** `GET /api/channel/?p=0&page_size=500` 拉到 44 个 channel, group 分布:

```
GPT-Enterprise:1  GPT-Plus:3  GPT-Pro:2  GPT-Pro-Stable:2  GPT-Pro-sale:4
aws-b:1  aws-q:4  aws-q-stable:2  claude-distill:1  claude-lowprice:3
claude-max:3  claude-max-sale:3  claude-max-stable:5  claude-sale:1
gemini-lowprice,gemini-sale,gemini-stable:1  gpt-lowprice:2  gpt-stable:1
grok-sale:1  grok-supporter:1  mix-lowprice:3
```

关键词搜索 `keyword=super` 命中 0 条; `keyword=grok` 命中 2 条, 都是 `grok-sale` 或 `grok-supporter`. 其中 id=19 的 `own-cpa-grok-sale-grok-AD9x` 备注里写的是 "super grok 号池 · 成本待定" — **意图上 = super grok, 但实际 group = grok-sale**. 这就是"保留单独测"的物理残留.

### 2.4 GroupSpecialUsableGroup 里 super-grok 的分布

**权威源: dotted `group_ratio_setting.group_special_usable_group`** (每个 value = `"remove"`).
**Legacy mirror: flat `GroupSpecialUsableGroup`** (每个 value = `""`).

两份都存在, 结构完全一致. 每个用户组下的 `-:super-grok` 出现次数:

| 用户组 | 有 `-:super-grok`? |
|---|---|
| default | ✅ |
| free | ✅ |
| supporter | ✅ |
| fan | ✅ |
| bestie | ✅ |
| vip | ✅ |
| promo | ✅ |
| retail | ✅ |
| wholesale | ✅ |
| wholesale-plus | ✅ |

10/10, 全都有. 这符合"R21 全 GSU rebuild 时按 flat 拉平合并"的模式.

**每一条都是 no-op** — 见 `service/group.go:14-40`, `strings.HasPrefix(specialGroup, "-:")` 分支执行 `delete(groupsCopy, "super-grok")`, 而 `groupsCopy` (来自 UUG) 从来就没有 `super-grok` 这个 key.

### 2.5 半配置状态的副作用与失败模式推演

**输入路径**: 用户或 admin 硬把 `token.Group = "super-grok"`. 从 `controller/token.go:322-329, 446-455` 可以看出 **token.Group 在创建/更新时不做 UUG 校验** — 只有 `token.Group == "auto"` 才走 `setTokenAutoGroups` 校验; 直接手写 `super-grok` 会被接受落库.

请求到达时, `middleware/auth.go:459-475`:

```go
tokenGroup := token.Group   // "super-grok"
if tokenGroup != "" {
    if _, ok := service.GetUserUsableGroups(userGroup)[tokenGroup]; !ok {
        abortWithOpenAiMessage(c, http.StatusForbidden, fmt.Sprintf("无权访问 %s 分组", tokenGroup))
        return
    }
    if !ratio_setting.ContainsGroupRatio(tokenGroup) { ... }
    ...
}
```

**第一个 gate 一定拦下**:
- `GetUserUsableGroups(userGroup)` 返回 `UUG copy` + GSU 处理 + `userGroup` 自己. `super-grok` 不在 UUG 顶层, GSU 也没有 `+:super-grok` (只有 `-:super-grok` no-op), 所以 map 里没有 `super-grok`.
- 唯一例外: **如果 userGroup 本身 = "super-grok"**, 则 `GetUserUsableGroups` 的最后一步 `groupsCopy["super-grok"] = "用户分组"` 会补上. 但生产查询 `/api/user/?p=0` 显示所有用户 group 分布是 `default/free/kirobus-api/monitor/wholesale/wholesale-plus`, **没人的 userGroup 是 super-grok**.
- 就算真有个 user 被 admin 手动改成 `super-grok`, `ContainsGroupRatio("super-grok") == false` 的第二个 gate 会返回 `分组 super-grok 已被弃用` — 也是 403, 而不是 500.

**能不能通过 auto 路径 (AutoGroups 命中) 意外落到 super-grok?**
- 不能. `AutoGroups` 里没有 super-grok (2.1 里 pricing API 的 `auto_groups` 是 `["aws-q","gpt","free","grok-sale","claude-lowprice","claude-max","GPT-Pro","GPT-Pro-Stable"]`).
- 就算 admin 手改 AutoGroups 加上 super-grok, `service.GetUserAutoGroup` 会 filter 掉 — `IsUserSelectableGroup(userGroup, "super-grok")` 因为 UUG 没它, 直接 false, `GetRequestAutoGroups` 也一样过滤.

**能不能通过 playground `group` 字段命中?**
- `middleware/distributor.go:95-102` 也检查 `GroupInUserUsableGroups(usingGroup, playgroundRequest.Group)`, 命不中就 403.

**假如所有 gate 都被绕过 (纯假设)**, 走到 `model.GetRandomSatisfiedChannel("super-grok", ...)` — 0 channel 返回 `(nil, nil)`. Distributor 回到 `channel == nil` 分支, 返回 `MsgDistributorNoAvailableChannel` + `anthropicModelNotFoundStatus` (`middleware/utils.go:59-67`), 也就是 **503 (OpenAI 路径) 或 404 (`/v1/messages` 路径)**. 不是 500. 有日志, 用户看到 "no available channel", 客户端合理重试.

**结论**: 无副作用. GGR 里的 super-grok 倍率 = 死配置; UUG/AutoGroups 都命不中, 手动选也过不了 auth. `-:super-grok` 只是记忆化的 "不给" 语义.

### 2.6 相关: 上游 grok-sale/grok-supporter 定价

作为参考, ledger 视角当前 grok 系:

- `grok-sale`: 1 个 CPA 渠道 (`own-cpa-grok-sale-grok-AD9x`), 备注 "super grok 号池 · 成本待定".
- `grok-supporter`: 1 个自建号池 (`ext-qiaozhi-grok-supporter-grok-g2a_`), 备注 "免费无成本".
- 两者都在 UUG 顶层 (`Grok 特价` / `Members only`); GGR 里 retail/wholesale/wholesale-plus 都有明确倍率.

`super-grok` 如果启用, 语义上会是**第三档** (比 supporter 更专属), 但目前既没渠道也没上游成本, 就是**规划名**.

---

## 3. 启用 super-grok 的 Checklist

想真正把 super-grok 从 "幽灵分组" 变成可用分组, 按顺序:

### 3.1 数据层

1. **加 GroupRatio 条目**: `PUT /api/option/GroupRatio` 里增加 `"super-grok": 1` (或按定价). 之后 `ContainsGroupRatio("super-grok") == true`, auth 第二个 gate 通过.
2. **加 UserUsableGroups 条目**: `PUT /api/option/UserUsableGroups` 里加 `"super-grok": "Grok Ultra - 单独号池测试"` (或按文案).
3. **加 GroupGroupRatio (可选)**: 决定哪些用户组能用 super-grok, 在 `GroupGroupRatio` 里给这些用户组的 map 加 `"super-grok": <ratio>` — 只影响价格, 不影响权限.
4. **改 GroupSpecialUsableGroup**: 从每个 userGroup 的 config 里**删掉** `-:super-grok` (10 处, flat + dotted 两份都要). 或者反过来: 只给白名单里的用户组显式 `+:super-grok`, 其他保持 remove 就够 — remove 现在是 no-op, 加了 UUG 顶层后就变成"真删除".
5. **两份 GSU 同步**: 权威 dotted, mirror flat — 见 R17-B (task #37) 的调查, 走 pub/sub 让两 tab 同步.

### 3.2 渠道层

6. **创建 super-grok 渠道**: `POST /api/channel/` 至少 1 个, `group = "super-grok"`, 支持 grok-4.6 等模型. 或者复用 id=19 那个 CPA 渠道, 把 group 从 `grok-sale` 改成 `super-grok` (那正是当初起的用途).
7. **验证 CacheGetRandomSatisfiedChannel**: 建完后 `POST /v1/chat/completions` 用一个 token.Group=super-grok 的 key 测一次, 看是否走通.

### 3.3 UI/前端 (如需)

8. **TierCards 更新**: 如果要在 wallet UI 里露出, 需要在 `frontend` 那边加档位卡片 (跟 grok-supporter 的做法一样).
9. **Playground group 选择器**: 前端 group dropdown 自动从 `/api/pricing` 的 `usable_group` 拉, 加完 UUG 后自动出现, 无代码改动.

### 3.4 计量 / operator-ledger 面

10. **operator-ledger 侧**: 如果 super-grok 会成为 retail/wholesale 的一个成本档, 需要在 `products.slug`, `channels_upstream_unique` 那侧加一行, 保证 `pool_daily_pnl` / `usage_daily_group_channel` 能按新 group 分账. (跨仓库改动, 不在本 checklist 的执行范围内.)

### 3.5 灰度 / 回滚

11. **先只在 vip 或某个内测 userGroup 里放开** (只在那个 userGroup 的 GSU 里删除 `-:super-grok`), 别的组保留 remove 语义.
12. **备份**: 改 GSU 前留 `bak-YYYYMMDD-hhmmss-super-grok-enable` snapshot, 便于回滚.
13. **回滚方案**: 单个 PUT 把 GroupRatio / UUG 里的 super-grok 项删掉即可, 立即回到今天的状态.

### 3.6 不需要做的

- **不需要改代码**. 全部通过 admin PUT `/api/option/` 完成. `service/group.go`, `middleware/auth.go`, `middleware/distributor.go` 对这条路径已经完备.
- **不需要动 auto_groups**. 除非想让 auto token 也命中 super-grok — 但那意味着"随机进 super-grok", 通常 super-grok 是显式选的.

---

## 4. 附录: 一句话给下一个人

> super-grok 是 "记住不给" 的历史遗迹. 全站每个用户组都有 `-:super-grok` 删除标记, 但那个东西**从来没在正向配置里出现过** — 没渠道, 没定价, 没 UUG. 想删净可以, 想启用也可以 (checklist 见 §3), 保持现状零风险.
