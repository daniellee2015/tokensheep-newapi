# tokensheep 并发限制 —— 面向 100b 的移植设计规格

> **产出日期**：2026-09-04
> **交付目的**：tokensheep 的并发限制作为参考设计，用于在 `/Users/danlio/Repositories/new-api`（100b fork，`main`）上做**最小增量**移植。
> **配套文档**：`docs/spec/subscription-porting-to-100b.md`（订阅系统移植设计）。
> **读者假设**：你是 100b 上的实现者，已经理解 gin middleware、Redis Lua、并发控制的基本概念。

---

## 0. 关键判断：只搬一层，其他都不动

如果你只想读一句话就走：

> **tokensheep 的并发限制有 4 层，其中 3 层 100b 已有等价物或者不适用；真正缺的只有"系统级 in-flight 天花板"这一层，估计 ~50 LOC，纯新增，不碰 tier、不碰现有 middleware，风险极低。**

其他 3 层（用户级、分组级、session 级）**不要搬**，理由见 §3。

---

## 1. 四层并发限制对照

### 1.1 tokensheep 的 4 层结构

在 tokensheep 里，"并发限制"其实是 4 个正交层次的合称：

| 层 | 计数维度 | 触发 | 存储 |
|---|---|---|---|
| **系统级 in-flight** | 全站唯一计数器 | 每个 relay 请求进 middleware 就 +1 | Redis `ts:system:active` |
| **分组级 in-flight** | `SessionLimits[group]` 按组配置上限 | 每个 relay 请求 +1 到 `ts:session:active:{userId}`，用**该用户的 group 对应的上限**比对 | Redis `ts:session:active:{userId}` |
| **用户级 in-flight** | 同上（tokensheep 里分组级和用户级共用一个计数器，只是配置维度不同） | 同上 | 同上 |
| **Session 级** | 浏览器登录会话数（`UserSession` 表） | 用户新登录时 | DB |

**注意**：tokensheep 里"分组级"和"用户级"实际上**共用一个 Redis 计数器**（`ts:session:active:{userId}`），只是 group 决定了"这个用户的上限是多少"。所以只有 3 个计数器（系统、用户、session），不是 4 个。

### 1.2 100b 的现状

| 层 | tokensheep | 100b | 差距 |
|---|---|---|---|
| **系统级 in-flight** | ✅ `SystemConcurrency` + `ts:system:active` | ❌ **完全没有** | **纯新增，本文档要搬的就是这一层** |
| **分组级 in-flight** | ✅ `SessionLimits[group]` | ⚠️ `tier_setting.Limits[group][tier].MaxSessions`（二维查表） | 100b 已有，且更精细（多一维 tier） |
| **用户级 in-flight** | ✅ `ts:session:active:{userId}` Lua 原子 | ✅ `concurrent:{userId}` Pipeline INCR | **100b 已有** |
| **Session 级（浏览器）** | ⚠️ `UserSession` 表 | ⚠️ passkey + upstream session | 跟 relay 并发无关，不用管 |

**100b 完整度：70%**。缺一层（系统级），其他 3 层结构合理。

### 1.3 顺带说：RPM ≠ 并发

**这两个是不同概念，别搞混：**

- **RPM**（Requests Per Minute）：滑动窗口，1 分钟内接了多少请求
- **In-flight 并发**：此刻同时有多少个请求**正在运行还没返回**

举例：一个用户开了 20 个 SSE 长连接每个跑 5 分钟，RPM = 20/5 = 4 很低，但 in-flight = 20 很高。RPM 拦不住这种。

**100b 的 RPM 体系反而比 tokensheep 更完整**（4 层叠加取严：global/group/user/tier），所以 RPM 这块完全不用动。

---

## 2. 要搬的东西：系统级 in-flight 天花板

### 2.1 为什么这一层重要

场景：100b 的用户 RPM 和 tier 并发都配置正常，但**没有全站上限**。风险：
- 管理员在 admin panel 把某个 tier 的 `max_sessions` 手滑从 100 改成 100000
- 单一账户就能占满上游连接池、DB 连接池、gin worker
- 无兜底 → 服务连锁挂

tokensheep 就是被这个坑过一次，才加了 R16-4 的 `SystemConcurrency`。

### 2.2 实现规格

**新增文件（推荐位置）**：`middleware/system-concurrency-limit.go`

**核心逻辑（伪代码）**：

```
SystemConcurrencyLimit() gin.HandlerFunc {
    return func(c) {
        limit := setting.GetSystemConcurrency()
        if limit <= 0 { c.Next(); return }   // fail-open：0 = 禁用

        release, ok := acquireSystemSlot(ctx, limit)
        if !ok {
            c.JSON(503, {
                error: {
                    message: "station is at capacity (%d concurrent requests)",
                    type: "system_busy",
                    code: "system_concurrency_limit_exceeded",
                }
            })
            c.Abort()
            return
        }
        defer release()

        c.Next()
    }
}
```

**关键约束**：

1. **返回 503 而不是 429**——这是"服务过载"不是"用户配额"，客户端应该 backoff 后重试，跟 429（用户 quota 超）是不同信号。tokensheep 就是这么区分的（`session_concurrency.go` L96-108）。

2. **fail-open 语义**：`limit <= 0` 时**必须直接放行**。理由：老部署升级后 option 表里没这个 key → 默认 0 → 如果 fail-close 就变成"升级后所有请求 503"，事故。tokensheep 在注释里刻意强调（`session_concurrency.go` L84 附近）。

3. **Redis 失败也 fail-open**：Redis 抖动一下时 Eval 报错，此时应该返回 `func(){}, true`（一个空的 release + 允许通过），而不是让请求全挂。tokensheep 明确做了这个选择（`session_concurrency.go` L178-184），跟 upstream `rate-limit.go` 的行为一致。

4. **TTL 兜底**：每个计数器加 15 分钟 TTL。理由：万一 panic 或 handler 死锁了 defer release 没跑到，Redis key 会永久滞留导致计数偏高。TTL 兜底自动清 —— 15 分钟足够长（正常请求 60s 内结束），足够短（挂了几十分钟内自动恢复）。

### 2.3 关键代码片段（可直接抄的 Lua）

**Acquire**（原子 INCR + 边界检查 + TTL 刷新）：

```lua
local n = redis.call('INCR', KEYS[1])
if n > tonumber(ARGV[1]) then
    redis.call('DECR', KEYS[1])
    return 0
end
redis.call('EXPIRE', KEYS[1], ARGV[2])
return 1
```

**Release**（原子 DECR + 防负数）：

```lua
local n = redis.call('DECR', KEYS[1])
if n < 0 then redis.call('SET', KEYS[1], 0) end
return n
```

**为什么必须用 Lua**：普通 Pipeline 版的 `INCR + Expire + 检查` 是**非原子**的——检查值太大时你已经 INCR 了，DECR 回滚之间可能被别人看到 count=N+1 → 别人也失败 → 无谓拒绝。Lua 保证 INCR + 检查 + 回滚在同一事务里。

**100b 的 `ConcurrentLimit` 用的是 pipeline 不是 Lua**（`middleware/concurrent-limit.go` L98-119）。功能能跑，但在真正的高并发下会有边缘 case。这是一个附带可以升级的地方，但不影响本次系统级层的移植。

### 2.4 Redis 不可用时的 fallback

tokensheep 提供了单机内存 fallback（`session_concurrency.go` L204-217）：

```go
var (
    fallbackMu sync.Mutex
    fallback   = make(map[string]int)
)

func acquireMemory(mu *sync.Mutex, m map[string]int, key string, limit int) (func(), bool) {
    mu.Lock()
    defer mu.Unlock()
    if m[key] >= limit {
        return nil, false
    }
    m[key]++
    return func() {
        mu.Lock(); defer mu.Unlock()
        if m[key] > 0 { m[key]-- }
    }, true
}
```

**语义警告**：内存 fallback **只在单节点部署下正确**。多副本部署没有 Redis 时，各节点各自计数，绕过限制。tokensheep 的注释明确说 `Redis is the intended deployment`。

**100b 场景**：100b 的 `ConcurrentLimit` 也用了同样的 sync.Map fallback（`concurrent-limit.go` L60-80），所以本次移植可以对齐它的做法，保持一致性。

### 2.5 配置入口

**推荐做法**：加一个 option key `SystemConcurrency`（int），admin panel 里放到 "System Settings → Request Limits" 那个页面（100b 已有这个页面）。

**默认值**：0（禁用），跟 tokensheep 的 fail-open 语义一致。

**推荐运营值**（tokensheep 生产经验）：

```
SystemConcurrency ≈ sum(per-tier max_sessions × 预期活跃用户数) × 1.2
```

例如 100b 默认 tier 0-4 的 MaxSessions = 5/10/20/40/80，如果预期同时 30 个活跃用户平均在 tier 2 上，那大约 30×20×1.2 = 720。**这个数字要根据实际上游连接池、DB 池、goroutine 池的容量调**——是"系统资源上限"，不是"业务上限"。

### 2.6 挂载位置

**推荐挂法**：加到 `router/relay-router.go` 里 `/v1`、`/v1beta` 的 middleware 链**最前面**（在 `ConcurrentLimit` 之前）：

```go
relayV1Router.Use(middleware.SystemPerformanceCheck())
relayV1Router.Use(middleware.SystemConcurrencyLimit())    // ← 新增，加在这
relayV1Router.Use(middleware.TokenAuth())
relayV1Router.Use(middleware.ModelRequestRateLimit())
relayV1Router.Use(middleware.ConcurrentLimit())
```

**顺序解释**：
1. `SystemPerformanceCheck` 拦 CPU/内存过载（最外层）
2. `SystemConcurrencyLimit` 拦并发数过载（新增）
3. `TokenAuth` 认证
4. RPM 限流（业务规则）
5. `ConcurrentLimit` 用户级并发（业务规则）

**为什么系统层要在 auth 之前**：系统过载时应该**尽早**拒绝，别让请求走完 auth 消耗 DB 再拒。tokensheep 也是这个顺序（先 system 层，再 auth，再 group 层）。

### 2.7 顺带看一眼：/mj 和 /suno

盘点 100b 时发现：**`/mj` 和 `/suno` 路由完全没挂 `ConcurrentLimit` 和 `ModelRequestRateLimit`**（`router/relay-router.go` L209-210 附近的分组只有 `SystemPerformanceCheck`）。这算 100b 的一个已知缺口。如果你加 `SystemConcurrencyLimit` 时顺手把这两个路由也挂上，收益更大。

不用挂 tier 相关的东西（那是可选的），至少 SystemConcurrency 挂上。

---

## 3. 不要搬的东西

### 3.1 不要搬：tokensheep 的 `SessionLimits[group]` 分组配置

**原因**：group 语义完全不一样。

| tokensheep group 值 | 100b group 值 |
|---|---|
| `free` / `supporter` / `fan` / `bestie` / `vip` / `promo` | `default` / `wholesale` / `internal` |
| 表示"你的 tier"（捐赠档位） | 表示"你的用户类型"（跟 tier 正交） |
| 值域是**开放集**（管理员可加） | 值域是**闭合集**（业务约定的几种） |

硬搬会导致 100b 的 group 名字里塞满 tokensheep 的 tier 名，破坏 tier_setting 二维查表的语义。

**100b 已有替代方案**：`tier_setting.Limits[group][tier].MaxSessions` 已经能表达"某个 group 的某个 tier 上限是 N"，比 tokensheep 的一维 map 表达力更强。**不需要再加一维。**

### 3.2 不要搬：tokensheep 的 tier 定义

**原因**：两套 tier 是**互斥**的经济模型，见 `docs/spec/subscription-porting-to-100b.md` §8。

- tokensheep tier = 捐赠触发（`total_donated` + 活跃度 cron）
- 100b tier = 消费触发（`user_tier_ext.tier` + 日均门槛）

这两套逻辑跑在同一数据库上会**互相打架**（比如捐赠 cron 想把用户降到 free，消费 cron 想把用户升到 tier 4，用户 group 每天在 vip ↔ default 反复横跳）。**别混。**

### 3.3 不要搬：tokensheep 的 `UserSession` 浏览器会话限制

**原因**：那是限制"用户能同时登录几个浏览器"，跟 API relay 的 in-flight 并发**是完全不同的功能**。100b 有自己的 passkey / OAuth session 管理，不需要 tokensheep 那套。

### 3.4 不要搬：`middleware/concurrent-limit.go` 现有实现

**原因**：100b 已经有一个（用户级 in-flight），且已跟 `TierAwareMaxSessions` 接了。不要动它。本次只在**它前面加一层**（系统级），不改它本身。

如果将来想升级 100b 的用户级并发从 pipeline 改成 Lua 原子，那是另一件事。

---

## 4. 落地检查清单

按顺序打勾：

- [ ] **新建** `middleware/system-concurrency-limit.go`（~50-80 LOC）
  - [ ] `SystemConcurrencyLimit() gin.HandlerFunc`
  - [ ] `acquireSystemSlot(ctx, limit)` 支持 Redis Lua + 内存 fallback
  - [ ] fail-open：limit<=0 直接 next
  - [ ] fail-open：Redis Eval 错误直接放行
  - [ ] Release 用 Lua 防负数
  - [ ] TTL 15 分钟兜底
- [ ] **新增 option key** `SystemConcurrency`（int，默认 0）
  - [ ] `model/option.go` 里加 `optionMap["SystemConcurrency"] = "0"`
  - [ ] `setting/` 下加 getter（可放到 `setting/tier_setting/` 或独立新建 `setting/concurrency_setting/`，别放到 tokensheep_setting 那种耦合命名下）
- [ ] **挂载到 relay 路由**
  - [ ] `router/relay-router.go` 里 `/v1`、`/v1beta` 组挂上，位置在 `ConcurrentLimit` 之前
  - [ ] 【可选、建议】顺手给 `/mj`、`/suno` 也挂上 `SystemConcurrencyLimit`（这两个路由目前无任何并发限制）
- [ ] **前端 Admin UI**
  - [ ] `web/default/src/features/system-settings/request-limits/rate-limit-section.tsx`（或类似）加一个 input：`SystemConcurrency`（int，说明"整个服务同时允许的 in-flight 请求数，0=禁用"）
- [ ] **测试**（至少这 4 个）
  - [ ] limit=0 → 无限制，所有请求 pass
  - [ ] limit=2 → 第 3 个 concurrent 请求返回 503（`system_concurrency_limit_exceeded`）
  - [ ] request panic 后 counter 能通过 defer release 归零
  - [ ] Redis 不可达时 fail-open（新起请求都能过）

---

## 5. 预估工作量

| 项 | 工作量 |
|---|---|
| Middleware 代码 | ~50 行（照 tokensheep `session_concurrency.go` 的系统层部分抄，改改常量名） |
| Option key 注册 + getter | ~15 行 |
| 路由挂载 | ~2 行 |
| 前端 Admin UI 字段 | ~30 行 tsx |
| 单元测试 | ~80 行 |
| **合计** | **~180 LOC，专注 1-2 小时** |

---

## 6. 参考文件

### 6.1 tokensheep 参考实现

- `middleware/session_concurrency.go` L75-114 — 系统层实现（这就是你要抄的部分）
- `middleware/session_concurrency.go` L164-217 — Lua 脚本 + 内存 fallback
- `setting/tokensheep_setting/economy.go` L52-64 — `SystemConcurrency` 字段定义
- `setting/tokensheep_setting/economy.go` L150-164 — `GetSystemConcurrency()` fail-open 语义

### 6.2 100b 现有实现（对齐用）

- `middleware/concurrent-limit.go` — 用户级并发（作为兄弟层参考）
- `middleware/rate-limit.go` L90-102 — 全局 RPM（fail-open 惯例）
- `router/relay-router.go` L69-75 — middleware 链挂载顺序
- `main.go` L290-294 — Tier hook 注册（了解现有 tier 是怎么接进 middleware 的，不用动）

### 6.3 100b 相关设计文档

- `docs/zh-CN/tier-and-rebate-spec.md` — 100b 自己的 tier 体系（读一遍，确认为什么不能混）
- `docs/spec/subscription-porting-to-100b.md`（本仓库）— 订阅系统移植设计，配套阅读

---

## 附录：为什么 tokensheep 有 4 层但一起讲

在 tokensheep 里"session concurrency" 一词涵盖了系统 + group + user 三层（session_concurrency.go 一个文件搞定），是因为它们**共用一套** middleware 和 Redis 计数器，只是 group 决定"上限是多少"、system 是独立的全站计数。这种写法方便，但把 group 和 tier 语义绑死了 —— 这也是为什么 tokensheep 的 group 名和 tier 名是同一套字符串。

100b 拆开了两个概念（group + tier 二维），所以自然分成两个层：
- **Group + Tier 的组合决定用户级 max_sessions** → 由现有 `ConcurrentLimit` + `TierAwareMaxSessions` hook 实现
- **系统级天花板** → 本次要新增的这一层

**结论**：本次移植不是"照搬 4 层"，而是"补齐 100b 缺的 1 层，其他 3 层保持 100b 原样"。
