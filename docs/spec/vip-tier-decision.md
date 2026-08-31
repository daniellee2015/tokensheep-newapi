# vip 档最终去留 + bestie/vip 差异化决策

**状态**: 待用户拍板 (R20-B 起草, 2026-08-31)
**关联 issues**: #34 (vip 档最终去留), #35 (bestie/vip 差异化设计)
**关联 spec**: `docs/spec/economy-model-v4.md` §2.2 / §4.1 / §四 B4 / §B15

---

## 一、现状

### 1.1 参数对比

代码 seed 位置: `setting/tokensheep_setting/economy.go`

| 维度 | bestie | vip | 差距 | 生效位置 |
|---|---|---|---|---|
| 每日签到 gift | $5.00 (2_500_000 quota) | $10.00 (5_000_000 quota) | 2× | `economy.go:78-79` `CheckinAwardByGroup` |
| 升组门槛 (累计贡献) | $100 (50_000_000 quota) | $500 (250_000_000 quota) | 5× | `economy.go:86-87` `TierThresholds` |
| session 并发 | 8 | 15 | ~1.9× | `economy.go:109-110` `SessionLimits` |
| RPM (每分钟请求) | 40 | 60 | 1.5× | 生产 DB `ModelRequestRateLimitGroup` (v4 §2.2 表格) |
| usable_groups (渠道可见性) | 与 vip 完全相同 (basic + premium + flagship) | 与 bestie 完全相同 | 无 | `GroupSpecialUsableGroup` (`ratio_setting` 点分 key) |
| 客服/运营 | 无区分 | 无区分 | 无 | (纯运营, 无代码) |

**关键观察**: 门槛 5× 但 gift 只 2×、并发只 1.9× — 从 ROI 看 vip 的性价比明显低于 bestie, 这可能就是它当前被 admin 关掉的直接原因.

### 1.2 生产 admin 已关闭 vip

- R16-2 (`web/src/features/system-settings/integrations/tokensheep-economy-section.tsx`) 落了 admin UI 让运营编辑 `disabled_tiers` map, 生产 admin 已通过该 UI 设置 `disabled_tiers = {"vip": true}`.
- 关闭机制 (`setting/tokensheep_setting/economy.go:411-413` `TierForDonation`): 遍历 `TierThresholds` 时跳过 `DisabledTiers[name]==true` 的档.
- 关闭生效路径:
  - `TierForDonation()` 不再返回 `"vip"` → 用户新贡献不会升到 vip
  - `TierCardsSorted()` 跳过 → 前端 tier ladder 不显示 vip 卡
  - 老 vip 用户: `model/tokensheep_maintenance.go:99` 每日 cron 重算 `TierForDonation(total_donated)` 会命中下一档 (`bestie`), `UPDATE users SET group='bestie'` 自动降组
### 1.3 三处不一致

| 位置 | 状态 |
|---|---|
| `economy.go` seed | vip 仍在 `CheckinAwardByGroup` / `TierThresholds` / `SessionLimits` 里, `DisabledTiers` 默认空 |
| 生产 admin UI (option) | `disabled_tiers = {"vip": true}` |
| `docs/spec/economy-model-v4.md` | §2.2 表格里 vip 一行标 "**vip (关闭中)**", 定位 "invite-only whale tier ($500)" |

三处口径不一致但**当前语义可以自洽** (seed 是冷启动 default, option 覆盖 seed, spec 里显式标注关闭中). R16-2 admin UI 弹窗里也明确说明这是"配置保留但隐藏". 只有当 admin 忘记维护 option、或代码 seed 后续被修改时才会漂移.

---

## 二、决策必须回答的两个问题

1. **保留还是删?** — 保留意味着未来某天可以 flip 一个 checkbox 让它复活; 删意味着 seed / spec / admin 三处同步移除, 未来重开需要重新引入配置行.

2. **如果保留, 差异化在哪?** — 相对 bestie 的独特权益是什么? 光靠"$500 门槛 + 2× gift + 1.9× 并发"在 ROI 上说不通, 用户升 vip 缺乏动力.

---

## 三、方案 A: 彻底删 vip

### 动作清单

1. **代码 seed 清理** (`setting/tokensheep_setting/economy.go`)
   - `CheckinAwardByGroup` 删掉 `"vip": 5_000_000`
   - `TierThresholds` 删掉 `"vip": 250_000_000`
   - `SessionLimits` 删掉 `"vip": 15`
   - `DisabledTiers` 保持 `map[string]bool{}` (无需列 vip=true, seed 里已没有 vip)

2. **daily cron 白名单** (`model/tokensheep_maintenance.go:66`)
   - `IN ('free','supporter','fan','bestie','vip')` 中的 `'vip'` 保留一段时间兜底 (处理已经在 vip 组的老用户); 半年后清理.

3. **admin UI 一次性 reset** (生产运营手动跑)
   - 到 admin 系统设置面板, 把 `disabled_tiers` 从 `{"vip": true}` 清空成 `{}`.
   - **必须**: 如果代码 seed 已删 vip, admin 侧 `disabled_tiers` 里若残留 `{"vip":true}` 是无害的 (`TierForDonation` 遍历 seed 找不到 vip, 自然不会返回它), 但 UI 会显示一个不存在的 tier 名 → 建议同步清理.

4. **数据迁移** (生产运营 tomorrow 手动)
   - SQL 帮 admin 备好:
     ```sql
     -- 1. 先查有多少老 vip
     SELECT id, username, `group`, total_donated
     FROM users
     WHERE `group` = 'vip';

     -- 2. 决策后再跑 (降到 bestie, 保留 total_donated 便于将来审计)
     UPDATE users SET `group` = 'bestie'
     WHERE `group` = 'vip';
     ```
   - 或者不跑手动 SQL, 依赖 daily cron 自然收敛 (`tokensheep_maintenance.go` 已经会重算): 但如果 seed 里 vip 已删, cron 里的白名单需保留一次 `'vip'` 才能兜底重算.

5. **spec 更新** (`docs/spec/economy-model-v4.md`)
   - §2.2 表格删除 vip 行
   - §四 B4 (tier_cards vip 硬活着 → `disabled_tiers` 开关) 标注为 obsolete, 因为 vip 已不存在
   - §B15 (vip 关闭后老 vip 用户走什么) 标注为 obsolete, 但保留历史脚注

6. **ledger governance 同步** (`operator-ledger` 侧)
   - `internal/pricing/governance_diff.go` 会检测到 `TierThresholds` / `SessionLimits` / `CheckinAwardByGroup` 三处 vip 键消失 → 首次 diff 会报 "vip removed", 属于预期漂移, 观察一次即可.

### 优点

- **代码/UI/config/spec 四处一致**, 减少心智负担
- 未来 audit 时不再需要解释"为什么 seed 有 vip 但 UI 无档"
- 3 张 tier 卡 (supporter/fan/bestie) 心智更简洁, 与 v4 §10 UI 章节匹配

### 缺点

- 未来若真出现愿意 pay $500/月 的独享通道用户, 要重新引 seed + spec + admin 一致性
- "vip" 是互联网产品最高档名的通用心智锚点, 品牌角度删了可惜
- 迁移逻辑要有 fallback: 如果 admin 忘记 reset `disabled_tiers`, 或代码 seed 删了但 daily cron 白名单没保留 `'vip'`, 老 vip 用户可能卡在 vip 组永远降不下来 (`TierForDonation` 返回 bestie 但老用户 `group='vip'` 不在 cron 遍历白名单里)

### 风险

- **cron 白名单遗漏**: 步骤 2 里如果忘了在 `[]string{"free","supporter","fan","bestie","vip"}` 中保留 `"vip"`, 老 vip 用户即使 `total_donated < $500` 也不会被扫到降组. 必须保留至少一个 release cycle.
- **一次性 CLI 迁移 vs migration**: 目前没有专门的 economy tier migration 机制. 推荐用手动 SQL (方案 A 步骤 4) 而不是加一个 goose migration, 因为这是一次性运维动作, 不是 schema 变更.

---

## 四、方案 B: 保留 vip, 加差异化

保留 seed 和门槛不变, 但**必须**给 vip 相对 bestie 一个说得通的独享权益, 否则 $500 门槛无人买单. 以下 5 条候选维度, 每条列具体参数对比 + 实现成本 + 潜在坑, 让用户挑 (可多选).

### 维度 1: session 并发 (concurrency) 显著提升

| 项 | bestie | vip (现) | vip (改) |
|---|---|---|---|
| session_limits | 8 | 15 | **25** |

- **用户体感**: 高并发场景 (agent workflow, 批量脚本调用) 下 vip 明显优于 bestie, 是最直接的付费 justification.
- **实现成本**:
  - 代码: `economy.go:110` seed 改数字, admin option `session_limits.vip = 25`; 需一起看 `SystemConcurrency` (当前 seed 260) 是否需要相应上调.
  - 无迁移, 无 spec 大改.
- **潜在坑**:
  - `SystemConcurrency` 260 是按 1+1+3+5+8+15+2 + 30+50+100 计算的, vip 15→25 相当于 +10 并发, 需要把 SystemConcurrency 上调到 270+ 才不撞天花板.
  - 上游 (upstream new-api) 若有自己的 rate limit, 高并发 vip 可能更快踩到 upstream 429.

### 维度 2: 独家 channel access

| 项 | bestie | vip |
|---|---|---|
| usable_groups | basic + premium + flagship | basic + premium + flagship + **vip-exclusive** (e.g. `claude-max-stable`, `gpt-enterprise`) |

- **实现路径**: `GroupSpecialUsableGroup` 点分 key (R17-B 已澄清是权威源, `service/group.go:GetUserUsableGroups` 读它) — admin 面板配置 `vip.claude-max-stable = ""` 之类, R16-5 也已把 group tier 分类标签的 UI 铺好.
- **用户体感**: vip 用户看得到 bestie 看不到的独家渠道 (更稳定/更快/更高 quality). **最有品牌力的差异化**.
- **实现成本**:
  - 代码: 零 (基础设施已就绪).
  - 运营: 需先确定"哪些 upstream channel 归 vip 独享" — 目前 upstream 的 channel 名单还没有 vip-exclusive 项.
  - i18n: perks 文案要加"独家通道"说明 (R10 payload 已从 backend 拿 live RPM/concurrency, channel 名单可以顺路加).
- **潜在坑**:
  - 独家 channel 单价通常更贵, vip $10 daily gift 打不了几次就烧完, 用户可能反而抱怨"独家但不够用".
  - 上游账号成本: 独家通道意味着 tokensheep 侧要维护一个只给 vip 用的 upstream 账号 (成本沉在小池子里), 用户少时 ROI 极差.

### 维度 3: 客服/运营优先级

| 项 | bestie | vip |
|---|---|---|
| 客服响应 | 群内公告 / 有空回 | 群主客服直连, 24h SLA |
| 问题 escalation | 与其他用户共享排队 | 优先处理 |

- **实现成本**: 零代码, 纯运营承诺.
- **用户体感**: 单人运营场景下这条最容易兑现. 但也最难量化, 用户不见得能感受到价值.
- **潜在坑**:
  - 承诺 SLA 但一个人扛着就是负担, 生病/请假时兜不住反而砸招牌.
  - 无代码痕迹 → 未来接手的运营容易忘, 差异化悄悄消失.

### 维度 4: 每日 gift 更高 (调整 ROI)

**现状 ROI 问题**: 门槛 $100→$500 差 5×, gift $5→$10 只 2×, 从"回本速度"看极不划算.

调整方案:

| 项 | bestie | vip (现) | vip (改 4a) | vip (改 4b) |
|---|---|---|---|---|
| daily gift | $5 | $10 | **$25** (5×, 匹配门槛) | **$16.67** (回本 30 天) |
| checkin quota | 2_500_000 | 5_000_000 | 12_500_000 | 8_333_333 |

- **4a (5× 匹配门槛)**: 用户 30 天回本 $500 / (每日 $25) = 20 天. 与 bestie 30 天回本 $100 / $5 逻辑一致.
- **4b (30 天回本)**: 保守派. $500 / 30 天 = $16.67 daily gift, 与 bestie 一致的"30 天回本节奏".
- **实现成本**: 代码改一行 (`economy.go:79` 数字), 无迁移.
- **潜在坑**:
  - 提高 gift 意味着 tokensheep 侧的 gift 池支出线性上升. 需要看当前 `daily-global-pool.md` 里的每日 pool cap 能否承受 (若有 5 个 vip, 每天多支出 $75).
  - `GiftPoolCap: 25_000_000` (=$50) 是 gift 池上限, vip 25/day 会更快撞顶, 但按 v3 语义 `GiftPoolCap` 是"单用户 gift 累积上限", vip $25/day × 2 天就撞顶, 再签到就无效, 反而糟糕. 需要相应上调 GiftPoolCap.

### 维度 5: 私聊配置定制模型

| 项 | bestie | vip |
|---|---|---|
| upstream 定制 | 无 | 可提需求, 群主协助上 upstream 通道 (e.g. 某个新模型 preview, 或某个专属账号池) |

- **实现成本**: 零代码, 纯运营承诺 (类似维度 3, 但物件是 "定制通道" 而不是 "客服 SLA").
- **用户体感**: 类似 "concierge" 服务, 对折腾模型的技术型用户最有吸引力.
- **潜在坑**: 与维度 3 同 (无代码痕迹, 承诺兑现依赖运营人肉).

### 维度小结

| # | 差异化 | 代码改动 | 运营改动 | 用户可感 | 推荐组合 |
|---|---|---|---|---|---|
| 1 | 并发 15→25 | seed + option + SystemConcurrency | 无 | 高 | ⭐ 基础款 |
| 2 | 独家 channel | 无 (基建已就绪) | 需先建独家 channel | 极高 | ⭐⭐ 品牌款 |
| 3 | 客服 SLA | 无 | SLA 承诺 | 中 | 可选 |
| 4 | gift 提高 (5× 或 30d 回本) | seed + option + GiftPoolCap | 无 | 高 | ⭐ ROI 修补 |
| 5 | 定制通道 | 无 | concierge 承诺 | 中 | 可选 |

**最小可行组合**: 1 (并发) + 4a/4b (gift ROI). 只改数字, 不引入独家 channel 的运营复杂度.
**品牌满配组合**: 1 + 2 + 3 (并发 + 独家 channel + 客服 SLA). 门槛 $500 值回票价.

---

## 五、方案 C: 混合 (推荐)

- **保留 seed** (与方案 B 相同, 不改动 `economy.go`)
- **保留 admin `disabled_tiers={"vip":true}`** (继续关闭状态, 不做 reset)
- **不做**方案 B 的差异化 (不改 concurrency, 不建独家 channel, 不加 SLA)
- **不做**方案 A 的迁移 (老 vip 用户在 cron 里自然降到 bestie)
- 未来某一天真出现付得起 $500 的用户 → admin UI flip 一个 checkbox 复活即可, 期间不引入代码 churn

### 优点

- **灵活**: 未来复活成本 = 1 次 admin 点击
- **无 migration**: 生产老 vip 用户已被 daily cron 自然降到 bestie (若还有残留), 无需一次性 SQL
- **无 spec 大改**: `economy-model-v4.md` §2.2 里"**vip (关闭中)**"的定位已经预留了这个模式, 不需要重写

### 缺点

- **心智不一致**: 代码有 seed 但 admin UI 无档 → 新人 onboarding 需要解释 "为什么 seed 里有 vip 但 tier ladder 只 3 张卡"
- R16-2 admin UI 里的 `disabled_tiers` 编辑面板会长期挂着 vip 一行, 需要维护
- 长期看未使用的 seed 是"死代码", governance drift 检查会一直标注 vip 参数漂移 (若代码 seed 与 admin 微调不一致)

### 何时选 C

- 你**不确定**未来是否需要 $500 档
- 你希望**避免 migration** (哪怕是一次性 SQL)
- 你能接受**长期"配置保留但隐藏"**的心智负担

---

## 六、待用户决策问题

1. **保留 vip 名字/位置吗?** (A / B / C)
   - A = 删 seed + spec + admin UI (含一次性 SQL migration)
   - B = 保留 seed 并加差异化 (选一个或多个维度)
   - C = 保留 seed, 继续 `disabled_tiers={"vip":true}`, 不加差异化

2. **如果选 B, 差异化用哪个维度?** (可多选)
   - [ ] 维度 1: 并发 15→25
   - [ ] 维度 2: 独家 channel access
   - [ ] 维度 3: 客服 SLA 承诺
   - [ ] 维度 4a/4b: gift 提高 (5× 匹配 / 30d 回本)
   - [ ] 维度 5: 私聊定制通道

3. **现在 `disabled_tiers={"vip":true}` 是否维持?**
   - 方案 A: 应清空 → `{}`
   - 方案 B: 应清空 → `{}` (让 vip 重新对新用户开放)
   - 方案 C: 维持 `{"vip":true}`

4. **bestie 是否需要相应上调?** (吸引原本考虑 vip 的用户群体)
   - 场景: 方案 A 删 vip 后, 原 vip 用户降到 bestie, bestie 相当于成了新最高档 — 是否需要略微上调 bestie 的 concurrency / gift 来给它"最高档"应有的差异?
   - 目前 bestie 8 并发 / $5 gift, 若删 vip, 建议**保持不变**, 因为门槛 $100 不足以支持更高福利.

5. **是否需要一次性 migration?** (方案 A 独有)
   - 手动 SQL (§三步骤 4) 或依赖 cron 自然收敛
   - 若依赖 cron, `tokensheep_maintenance.go:66` 白名单中 `'vip'` 至少要保留一个 release cycle 才能兜底扫到老用户

---

## 七、推荐

**方案 C (保留 seed 关闭) 是当前最省事、最灵活的方案.** 只有当 $500 档明确无未来 (未来 6 个月内确定不会尝试) 时才走方案 A.

理由:

- 生产已经跑在 C 的状态 (`disabled_tiers={"vip":true}`) 一段时间, 未见问题
- v4 spec §2.2 表格里 "**vip (关闭中)**" 已经预留了这种"隐藏但保留"的语义
- R16-2 admin UI 已经把开关暴露, 未来复活成本 ≈ 1 次点击
- 方案 A 的 migration 成本 (即使是一次性 SQL) 比"配置保留但隐藏"的心智负担高很多
- 方案 B 的差异化需要先想清楚 vip 定位 (whale / concierge / infra 独享?), 不适合仓促决定

**如果用户偏 A 或 B**: 建议先跑一次 SQL 确认当前生产 `users.group='vip'` 的实际人数 —

```sql
SELECT COUNT(*) FROM users WHERE `group` = 'vip';
```

- 若 = 0: 方案 A 或 B 都可零风险执行
- 若 > 0 但 < 10: 手动降组 (方案 A) 简单直接
- 若 ≥ 10: 需要更认真的迁移计划 + 用户通知

---

## 八、决策记录 (待填)

> 用户决策后, 请在此填 (A / B+维度 / C), 并将结论回填到:
> - `docs/spec/economy-model-v4.md` §2.2 表格 (删行 / 保留 / 加差异化说明)
> - `setting/tokensheep_setting/economy.go` (若方案 A, 删 seed; 若方案 B, 改数字)
> - `web/src/features/system-settings/integrations/tokensheep-economy-section.tsx` (若方案 A/B, admin UI 上手动清 `disabled_tiers`; 若方案 C, 不动)

| 决策日期 | 结论 | 决策人 | 备注 |
|---|---|---|---|
| YYYY-MM-DD | (A / B+维度 / C) | | |

---

## 参考

- `docs/spec/economy-model-v4.md` §2.2 (tier 表格), §4.1 (每日签到), §四 B4 & B15 (关闭机制)
- `setting/tokensheep_setting/economy.go:78-127` (seed) / `:395-420` (`TierForDonation` 关闭逻辑)
- `model/tokensheep_maintenance.go:60-105` (daily cron 白名单 + 重算)
- `web/src/features/system-settings/integrations/tokensheep-economy-section.tsx` (R16-2 admin UI)
- `service/group.go:GetUserUsableGroups` (channel 权限, 供维度 2 参考)

