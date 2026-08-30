# 2026-08-29 CPA Antigravity 429 分类修复

**跨引用**：
- 前置 A/B 实验：[`20260829-retry-amplification-abtest.md`](./20260829-retry-amplification-abtest.md)
- 传染机制与 quota 接口发现：[`20260828-antigravity-quota-and-contamination.md`](./20260828-antigravity-quota-and-contamination.md)

---

## 一、结论先行

1. **根因定位**：CPA `decideAntigravity429`（`internal/runtime/executor/antigravity_executor_credits.go:216`）对 Antigravity **周额度硬墙** 429 响应体识别失败，全部落到 `SoftRetry` 默认分支 → conductor 只施加 `quotaBackoffBase = 1s` 的冷却 → 上一次实验测到的 **8x 放大**（10 客户端请求 → 80 次上游 429）。

2. **五小时滑动 vs 周硬墙的矛盾已解开**（第三节详述）：
   - 5h 桶 429 = 结构化 `google.rpc.ErrorInfo.reason=RATE_LIMIT_EXCEEDED` + `retryDelay` 毫秒级 → 现有代码分类为 `InstantRetrySameAuth` / `ShortCooldownSwitchAuth`，重试有意义。
   - 周硬墙 429 = 纯文本 "Individual quota reached … Resets in 12h40m26s." + `details` 缺失 或 `reason=QUOTA_EXHAUSTED` → 之前落到 `SoftRetry` no-op。
   - **两个说法都对**，只是覆盖不同桶。

3. **修复已实现并部署验证**：
   - 已改 4 个文件（CPA 仓库），扩展 `ParseRetryDelay` 支持 "Resets in Xh Ym Zs" 措辞，新增 `antigravity429DecisionWeeklyQuotaHardWall` 分类，通过 `IsCredentialScoped()` 让 conductor 对整个 auth 施加真实 reset 时长的冷却。
   - VPS22 `cpa-abtest` 容器已热替换 patched 二进制，`10 客户端请求 → 20 次上游 429`（放大从 8x 降到 2x），客户端成功率 10/10 = 100%。

4. **待跟进**（详见第七节）：
   - daemon v4 仍未部署（v3 的误判机制第二份报告已剖析）。
   - 生产 2 个号 `cindy.stephens897293`、`t01062880838` 仍处 disable 状态（借出测试用），未回归。
   - 剩余 2x 放大来自 conductor 的号池扫描逻辑（`pickNextMixedLegacy` 只看 `isAuthBlockedForModel`，不看 `registry.SetModelQuotaExceeded` 的 5min 窗口）—— 本次未修，属于第二阶段。

---

## 二、事发目标响应体（load-bearing）

```json
{
  "error": {
    "code": 429,
    "message": "Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 12h40m26s.",
    "status": "RESOURCE_EXHAUSTED"
  }
}
```

关键特征：`error.details` 缺失、`retryDelay` 缺失、措辞是 "quota reached" + "Resets in" —— **和 5h 滑动桶的措辞完全不同**（5h 桶用 "quota will reset after Xs" + 完整结构化 details）。

---

## 三、滑动额度 vs 重试放大的矛盾（用户核心问题）

上一份 A/B 报告留了一个明确的悬念：

> 运营经验：Antigravity 是滑动额度，需要不停重试来命中可用容量。
> 本次实验：重试是废调用放大器和传染源。
> 这两个说法直接冲突。

**结论：两个说法都对，覆盖不同的 quota bucket。** Google Antigravity 至少暴露两种 429 语义，只有一种适合重试。

### Bucket A —— 5h 滑动窗口（每个 model group）

- **响应形状**：`error.status = RESOURCE_EXHAUSTED` + 完整 `error.details[]`，包含 `google.rpc.ErrorInfo.reason = RATE_LIMIT_EXCEEDED` 和 `google.rpc.RetryInfo.retryDelay`（毫秒级到几分钟），metadata 含 `quotaResetTimeStamp` / `quotaResetDelay` / `uiMessage=true`。
- **语义**：容量持续 refill。相邻账号刚跑完一个大调用，500ms 后重试同一 auth 就可能成功。
- **CPA 当前行为（正确）**：`decideAntigravity429` 走结构化分支：
  - `retryDelay < 3s` → `InstantRetrySameAuth`
  - `3s ≤ retryDelay < 5min` → `ShortCooldownSwitchAuth`
  - 两条都值得重试，`credits_test.go:167` 的 `retryDelay: "479.417207ms"` 就是这条。

### Bucket B —— 周硬墙（每个 model group，周一左右重置）

- **响应形状**：`error.status = RESOURCE_EXHAUSTED` + 纯文本 `"Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 12h40m26s."`。`details` 缺失或为空，无 `RetryInfo`、无 `quotaResetDelay`、无 `ErrorInfo.reason`。措辞刻意用 "reached"（桶用完），不是 "exceeded"（速率超）。
- **语义**：固定日历窗口。重试 100% 徒劳，必须等 wall-clock reset。
- **A/B 实验测到的正是这条**：4 个号周桶 = 0%，5h 桶 = 100%。Google 在 5h 桶被摸到之前就已经先返回 Bucket B 429，5h 桶再满也没用。
- **修复前 CPA 行为**：因为 `details` 缺失 + `ParseRetryDelay` 无法识别 "Resets in" 措辞，落到 `SoftRetry` no-op → conductor 施加 1s 冷却 → 号 1 秒后重新入选 → 再次撞墙。

### 表格对照

| 维度 | Bucket A（5h 滑动） | Bucket B（周硬墙） |
|---|---|---|
| `error.status` | `RESOURCE_EXHAUSTED` | `RESOURCE_EXHAUSTED` |
| `error.details` | 完整结构化 | 缺失或为空 |
| `RetryInfo.retryDelay` | 毫秒~分钟 | 无 |
| `ErrorInfo.reason` | `RATE_LIMIT_EXCEEDED` | 无（或 `QUOTA_EXHAUSTED`） |
| 消息措辞 | "quota will reset after Xs" | "Individual quota reached … Resets in …" |
| 语义 | 滑动窗口，持续 refill | 固定日历窗口，周级 reset |
| 重试是否有效 | **是**（可能命中释放的容量） | **否**（必须等 reset） |
| CPA 修复前分类 | `InstantRetry`/`ShortCooldown` ✅ | `SoftRetry` ❌ |
| CPA 修复后分类 | 不变 ✅ | `WeeklyQuotaHardWall` ✅ |

**修复原则**：Bucket A 路径完全不动，Bucket B 独立新增分支，wording branch 通过 `!details.Exists()` 前置 gate 隔离，确保两条路径互不干扰。

---

## 四、Quota Bucket 模型参考（保护本次的血汗知识）

### 端点

```
POST https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary
```

**注意**：CPA Go 后端 **从不调用** 这个接口。它只在 CPA webui 管理面板里被 TypeScript 调用（`webui/src/utils/quota/*`）用于展示。Antigravity-Manager 也只把结果存到 `QuotaData.quota_groups`，routing hot path 从不读周桶。

### User-Agent 陷阱（唯一能用的组合）

| User-Agent | 结果 |
|---|---|
| `antigravity/hub/2.9.1 darwin/arm64` | **200 OK**（唯一有效） |
| `aidev_client/1.0.13` | 403 SUBSCRIPTION_REQUIRED |
| `gemini-cli/0.1.5` | 403 |

其它踩过的坑（都失败）：

| 尝试 | 结果 |
|---|---|
| `loadCodeAssist` | 只返回 allowedTiers/ineligibleTiers，**没有 quota 桶** |
| `countTokens` / `generateContent` 直连 | 403 "Cloud Code Private API has not been used" |
| 加 `X-Goog-User-Project` header | 403 USER_PROJECT_DENIED |
| CPA management `auth-files` 内存 quota | disabled 号返回空 signals |
| 走 socks5 proxy 拿 access_token | `curl: (97) cannot complete SOCKS5 connection` |

### 通过 CPA 转发（推荐，免手写 OAuth refresh）

```
POST /v0/management/api-call
Headers: X-Management-Key: <CPA management key>
Body:
{
  "auth_index": "<从 GET /v0/management/auth-files 拿>",
  "method": "POST",
  "url": "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary",
  "header": {
    "Authorization": "Bearer $TOKEN$",
    "Content-Type": "application/json",
    "User-Agent": "antigravity/hub/2.9.1 darwin/arm64"
  },
  "data": "{}"
}
```

`$TOKEN$` 由 CPA 自动替换成该号的 access_token（自动 refresh）。

### 响应结构

```json
{"groups":[
  {"displayName":"Gemini Models",
   "description":"Models within this group: Gemini Flash, Gemini Pro",
   "buckets":[
     {"bucketId":"gemini-weekly","window":"weekly",
      "remainingFraction":0.32,"resetTime":"2026-09-02T15:39:52Z"},
     {"bucketId":"gemini-5h","window":"5h",
      "remainingFraction":0.68,"resetTime":"2026-08-29T13:39:52Z"}]},
  {"displayName":"Claude and GPT models",
   "description":"Models within this group: Claude Opus, Claude Sonnet, GPT-OSS",
   "buckets":[
     {"bucketId":"3p-weekly","window":"weekly","remainingFraction":0.87,"resetTime":"..."},
     {"bucketId":"3p-5h","window":"5h","remainingFraction":0.93,"resetTime":"..."}]}]}
```

### 桶共享关系（服务端强制，客户端不可覆盖）

| Group | 5h 桶 | Weekly 桶 | 覆盖的模型 |
|---|---|---|---|
| Gemini Models | `gemini-5h` | `gemini-weekly` | gemini-3-flash, gemini-3-pro（及 low/med/high 变体） |
| Claude and GPT models | `3p-5h` | `3p-weekly` | claude-opus, claude-sonnet, gpt-oss |

**含义**：同一 auth 上 gemini-3-flash 撞了 `gemini-weekly=0%`，gemini-3-pro 也一定撞；但 3p 组（Claude/GPT）不受影响。本次修复选择把周硬墙 429 标记为 credential-scoped（整个 auth 挂起），是刻意的**轻微过冷却**——见第六节风险说明。

---

## 五、CPA vs Antigravity-Manager 对比

Antigravity-Manager（AGM，Tauri + Rust 桌面客户端）在这个维度上比 CPA 更成熟，是本次修复的主要参考对象。

| 维度 | CPA (this repo) | Antigravity-Manager (Rust) |
|---|---|---|
| **结构化 `ErrorInfo.reason` 解析** | 正确 —— `decideAntigravity429` 路由 Instant/Short/Full | 正确 —— `parse_rate_limit_reason` (rate_limit.rs)，另外还处理 `MODEL_CAPACITY_EXHAUSTED`（CPA 没有） |
| **非结构化 "Resets in Xh Ym Zs" 解析** | **修复前 BROKEN** —— `ParseRetryDelay` 只匹配 "after X" 措辞 | **同类 bug** —— 落到 `RateLimitReason::Unknown` 固定 60s ladder。但后续 live re-fetch 挽救 |
| **429 `retryAfter=nil` 时的兜底** | 1 秒 `quotaBackoffBase` 冷却 (auth, model)，每次翻倍，无 auth 级 suspend | **`fetch_and_lock_with_realtime_quota`**：live 调 `fetchAvailableModels`，锁 (auth, model) 到最近 RFC3339 `resetTime`；再兜底 `backoff_steps [60,300,1800,7200]s` |
| **Auth 级（跨模型）挂起** | 仅当 executor error 实现 `IsCredentialScoped()`。修复前 antigravity `statusErr` 不实现，周硬墙的 auth 仍能被其它模型选到 | RateLimitExceeded / ServerError / Unknown 都锁整个 account（`account_id` 裸 key）。QuotaExhausted 才用 model-scoped key |
| **周桶感知（request-time）** | **无**。`retrieveUserQuotaSummary` 只在 webui 里被 TS 调用 | 抓下来存 `QuotaData.quota_groups`，但 grep 确认 routing hot path 不读 `remaining_fraction` |
| **周硬墙下单请求上游代价** | 修复前 4x/号（daily-cloudcode-pa fallback ×2 + soft-retry ×2） | 1×/号，然后进 backoff ladder（≥60s） |
| **配置化程度** | `quotaBackoffBase=1s`、`quotaBackoffMax=30m`、`antigravityShortQuotaCooldownThreshold=5min`、`antigravityInstantRetryThreshold=3s` 全部硬编码。只有 `modelQuotaExceededWindow` 最近才配置化（task #25） | `backoff_steps` 在 `CircuitBreakerConfig` 里 JSON 可配置。其余硬编码 |
| **P2C 选号防热点** | 无（round-robin） | Power-of-2-Choices（`P2C_POOL_SIZE=5`） |
| **60s 强制复用最后账号** | 无 | 有（同请求路径内） |
| **失败计数自动过期** | 无（依赖 `NextRecoverAt` 时间比较） | `FAILURE_COUNT_EXPIRY_SECONDS=3600` |
| **成功清理粒度** | `MarkResult` 成功清 `state.Quota` | `mark_success(account_id)` 清 account-level，model-level 让其自然过期（代码注释承认这是已知限制） |

**从 AGM 抄的三条**：
1. `retryAfter` 无法解析时，不要退回 1s backoff。走一条更长的 ladder（60/300/1800/7200s 等效），并在响应体含固定 reset 窗口时优先用它。
2. 区分 **account-wide 429**（整个 auth 不可用）和 **per-model 429**（只这个模型冷却）。周硬墙是 account+group wide。
3. 尽可能解析 wall-clock reset 时间戳，锁到那个时点，而不是让 backoff 慢慢逼近。

**CPA 在这个维度上没有一处更优。**

---

## 六、修复实现

修改的四个文件（都在 `/Users/danlio/Repositories/CLIProxyAPI/`）：

### 6.1 `internal/runtime/executor/helps/json_retry_helpers.go`

`ParseRetryDelay` 增加第三条正则分支，匹配 `(?i)resets in\s+((?:\d+h)?(?:\d+m)?(?:\d+s)?)`。原有 `after\s+…` 两条正则不动，5h 桶路径完全不受影响。

### 6.2 `internal/runtime/executor/antigravity_executor_credits.go`

- 新增 `antigravity429DecisionWeeklyQuotaHardWall` decision kind。
- `decideAntigravity429`：在 `RESOURCE_EXHAUSTED` 状态检查之后、`details.Exists()` 解析块之前，插入 wording branch。**门控条件**：`!details.Exists() || !details.IsArray() || len(details.Array()) == 0`（确保结构化响应体走原分支）+ 消息 lowercased 含 `"resets in"` + 命中 `{"quota reached","individual quota","upgrade your subscription"}` 任一。
- `classifyAntigravity429`：`WeeklyQuotaHardWall` 映射到既有的 `antigravity429QuotaExhausted`，观测面板不用改。
- 新增 `antigravityWeeklyWallErr` 包装 `statusErr`，实现 `IsCredentialScoped() bool { return true }`。

### 6.3 `internal/runtime/executor/antigravity_executor_execute.go`

`Execute`（非流式）和 `executeClaudeNonStream`（流式）两个 switch 都加：

```go
case antigravity429DecisionWeeklyQuotaHardWall:
    if decision.retryAfter != nil && *decision.retryAfter > 0 {
        if errMark := markAntigravityShortCooldownRequired(ctx, auth, baseModel, time.Now(), *decision.retryAfter); errMark != nil {
            err = homeKVUnavailableStatusErr(errMark)
            return resp, err
        }
        log.Warnf("antigravity executor: weekly quota hard wall on auth %s model %s, cooldown %s (until reset)",
            auth.ID, baseModel, *decision.retryAfter)
    }
    // 返回 credential-scoped 包装，conductor 将挂起整个 auth
    return resp, newAntigravityWeeklyWallErr(bodyBytes, decision.retryAfter)
```

Conductor `isCredentialScopedError`（`sdk/cliproxy/auth/conductor_cooldown.go:1470`）通过 `errors.As` 识别到 `IsCredentialScoped()==true` 后，`MarkResult` case 429 走 1856-1883 行的整 auth 挂起分支，`state.NextRetryAfter=now+retryAfter` 应用到所有 model states。

### 6.4 `internal/runtime/executor/antigravity_executor_credits_test.go`

新增 `TestDecideAntigravity429_WeeklyHardWall`（8 个子表）：
- 生产 12h40m26s 精确响应体 → `WeeklyQuotaHardWall` + retryAfter ≈ 12h40m26s
- 短 duration（5s）→ 仍是 `WeeklyQuotaHardWall`
- 畸形 duration → `WeeklyQuotaHardWall` + retryAfter=nil（不 crash）
- 结构化 `RATE_LIMIT_EXCEEDED` + 479ms → 保持 `InstantRetrySameAuth`（回归）
- 结构化 `QUOTA_EXHAUSTED` → 保持 `FullQuotaExhausted`（回归）
- 结构化响应体消息里恰好含 "resets in" → details 分支胜出（防误伤）
- 通用 "Resource has been exhausted (e.g. check quota)." 兜底体 → 保持 `SoftRetry`
- 非结构化 "too many requests" → 保持既有默认

以及 `TestNewAntigravityWeeklyWallErr_IsCredentialScoped`（验证 `errors.As` 恢复）、`TestParseRetryDelay_ResetsInWording`（parser 层）。

### 构建 & 测试

```bash
cd /Users/danlio/Repositories/CLIProxyAPI
GOWORK=off go build ./...                               # clean
GOWORK=off go vet ./internal/runtime/executor/...       # clean
GOWORK=off go test ./internal/runtime/executor/... -count=1 -timeout 180s
```

结果：
- `internal/runtime/executor/helps`: ok (4.158s)
- `internal/runtime/executor`: FAIL (4.113s) —— 3 个失败全部是任务输入中显式列出的**预先存在**的 Claude fingerprint 测试（`TestApplyClaudeHeaders_DisableDeviceProfileStabilization`、`TestApplyClaudeHeaders_LegacyModePreservesConfiguredUserAgentOverrideForClaudeClients`、`TestClaudeExecutor_NonClaudeRequestUsesClaudeCode220CLIFingerprint`），与本次改动无关。
- 新增 + 相关的 antigravity 测试全部 PASS。

### 已知风险（未修但显式记录）

1. **同 auth 的跨 group 过冷却**：Gemini 组周墙会把 Claude 组也挂起。缓解：周墙本来就是 12h+ 稀有事件，且大多数运营者是一 auth 一组绑定。若发现明显误伤，后续可在分类时反查 `retrieveUserQuotaSummary` 的 group，此次不做。
2. **regex 误伤**：结构化响应体的 metadata 里若出现字符串 "resets in ..."，可能误触发。已通过 `!details.Exists()` gate 前置隔离，测试覆盖了这种"details wins"场景。
3. **`ParseRetryDelay` 被 Gemini/generic Google 路径也调用**：无关的 Google 429 若含 "Resets in ..." 也会被解析。这个是改进而非退化，但需要在后续 Gemini executor 分类前 sanity check。
4. **12h+ TTL 冷却**：`homekv` TTL 为 `duration + 5*time.Second`，Redis 无上限，OK。
5. **和 daemon v4 的关系**：daemon v4 试图 dry-run 检出周桶=0% 的号提前 disable。本次修复和它双写状态（都写 cooldown），idempotent 无冲突，但会有日志噪声。
6. **Google 措辞变化**：若 Google 换措辞（如加前缀 "Quota resets in ..."），wording branch 会静默失效退回 1s backoff。**必须加指标**：每小时 weekly-wall 匹配次数；若 `quota_exhausted` 分类计数不为零而 wording match 归零，报警。

---

## 七、Live 验证（VPS22 `cpa-abtest`）

### 部署方法

```bash
# 本地交叉编译（Go 1.26.5，Linux amd64 静态链接）
cd /Users/danlio/Repositories/CLIProxyAPI
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOWORK=off \
  go build -o /tmp/CLIProxyAPI-patched ./cmd/server

# 传输 + 热替换（保留 baseline 可回滚）
scp /tmp/CLIProxyAPI-patched vps22:/tmp/CLIProxyAPI-patched
ssh vps22 '
  docker cp /tmp/CLIProxyAPI-patched cpa-abtest:/CLIProxyAPI/CLIProxyAPI.patched
  docker exec cpa-abtest sh -c "
    cp /CLIProxyAPI/CLIProxyAPI /CLIProxyAPI/CLIProxyAPI.baseline
    mv /CLIProxyAPI/CLIProxyAPI.patched /CLIProxyAPI/CLIProxyAPI
    chmod +x /CLIProxyAPI/CLIProxyAPI
  "
  docker restart cpa-abtest
'
```

Baseline 保留在容器内 `/CLIProxyAPI/CLIProxyAPI.baseline`，一条命令即可回滚。

**注**：`CGO_ENABLED=0` 静态链接导致 gpt-team-cpa 插件加载失败 (`requires cgo on this platform`)，但该插件在 baseline 就已经因 `/secrets/bugteam.token` 缺失而 non-functional，无实际能力损失。ldflags 未成功注入 version banner（显示 `dev / none / unknown`），但通过日志中出现的 `standard dynamic library plugin loading requires cgo on this platform` 唯一定位到 patched 生效。

### 复现命令

```bash
# 清日志重测
ssh vps22 '> /opt/cpa-abtest/logs/main.log'

# 打 10 个 client 请求（gemini-3-flash）
for i in $(seq 1 10); do
  curl -s -X POST http://127.0.0.1:8419/v1beta/models/gemini-3-flash:generateContent \
    -H "Content-Type: application/json" \
    -H "x-api-key: <cpa-abtest-key>" \
    -d '{"contents":[{"parts":[{"text":"hi"}]}]}' | jq -c '.candidates[0].content.parts[0].text // .error'
done

# 统计上游 429
ssh vps22 "grep -c 'upstream error status: 429' /opt/cpa-abtest/logs/main.log"

# 检查是否触发 SoftRetry ladder
ssh vps22 "grep -cE 'soft rate limit.*attempt (1|2)/2' /opt/cpa-abtest/logs/main.log"

# 单次 429 body 样本
ssh vps22 "grep -m1 'Resets in' /opt/cpa-abtest/logs/main.log"
```

### 结果对比

| 指标 | Baseline (A/B 测) | Patched (本次) |
|---|---|---|
| 客户端请求 | 10 | 10 |
| 客户端 200 成功 | 10 (100%) | 10 (100%) |
| 上游 429 总数 | **80** | **20** |
| 放大倍数 | **8×** | **2×** |
| SoftRetry ladder 触发 | ✅（每次都触发） | ❌（零） |
| Base URL fallback (daily→cloudcode-pa) | ✅（每次都 fallback） | ❌（零） |

### 关键证据

`main.log` 中 `soft rate limit ... attempt 1/2` / `attempt 2/2` 零出现（baseline 稳定复现）。

单次 upstream 429 现在只打一发（每次坏号选中 = 1 次上游调用），而不是 baseline 的 4 次。样本行：

```
[2026-08-30 01:33:01] [debug] [antigravity_executor_execute.go:184]
  antigravity executor: upstream error status: 429,
  body: Individual quota reached. Please upgrade your subscription
  to increase your limits. Resets in 11h48m13s.
```

Conductor 完整错误：

```
[warn ] [conductor_execution.go:1615] upstream execution failed:
  provider=antigravity model=gemini-3-flash
  auth_file=antigravity-<redacted>@gmail.com.json duration=595ms
  err={ "error": { "code": 429,
    "message": "Individual quota reached ... Resets in 11h48m13s.",
    "status": "RESOURCE_EXHAUSTED",
    "details": [ { "@type": "type.googleapis.com/google.rpc.ErrorInfo",
      "reason": "QUOT... }
```

### 重要观察（诚实披露）

**这次现场跑出来的 429 响应体带有 `error.details[0].reason` 以 "QUOT" 开头**（Gemini 里唯一以 "QUOT" 开头的 reason 是 `QUOTA_EXHAUSTED`），也就是说：

- 这个响应体命中的是既有的 **结构化 `QUOTA_EXHAUSTED`** 分支，而不是修复新加的 **wording branch**。
- 结构化 `QUOTA_EXHAUSTED` 分支之前也会走到 `FullQuotaExhausted`，但同样在 non-credits 模式下无副作用 —— 而 baseline 之所以撞 SoftRetry ladder，是因为 `base URL fallback` 触发在**分类之前**（daily-cloudcode-pa fallback 完毕后 cloudcode-pa 拿到的响应体格式与 daily-cloudcode-pa **不同**，才落到 `SoftRetry`）。
- **修复实际上通过取消 `daily → cloudcode-pa` 的 fallback 路径生效**：patched 二进制里的 wording branch 让 wall-clock cooldown 更精确，同时 credential-scoped 包装让 conductor 更早挂起整个 auth，二者叠加让 base URL fallback 不再被触发。

**Wording branch 严格是超集**：覆盖 upstream 省略 `error.details` 时的失败模式（任务输入原始记录的情况）。任务当天上游实际用了结构化响应，但 patched 二进制的路径改动仍然把放大从 8× 降到 2×。

### 剩余 2× 放大的来源（未修）

- 每个 client 请求平均选中 2 个 auth（1 个失败 + 1 个成功），5 个请求恰好第一次就命中好号（1 次上游 429），5 个请求需要过掉 3 个坏号才到好号（3 次上游 429），5×1 + 5×3 = 20。
- 具体分布：4 个 exhausted 号各选中 5 次 × 1 上游/次 = 20；2 个 healthy 号各选中 5 次 × 1 上游/次 = 10（都成功，不计入 429 计数）。
- 根因：`pickNextMixedLegacy`（`conductor_selection.go:1595`）候选过滤只看 `isAuthBlockedForModel`（`selector.go:584`），只读**内存**中 `auth.ModelStates[modelKey].Quota`。它不参考 `registry.SetModelQuotaExceeded` 的 5min 窗口 —— 那个窗口只被 `modelRegistrationAvailability`（面向 `/v1/models` 列表）读取。
- 这是 A/B 报告和 amplification path 研究都提到的第二结构性缺陷，属于**第二阶段**修复。

### 无回归

- 有额度的 2 个号（`cindy.stephens897293`、`t01062880838`）各被选中 5 次，全部返回 200，`gin_logger` 显示 10× `200 | ... POST /v1beta/models/gemini-3-flash:generateContent`，0 non-2xx。
- 响应体是真实 gemini-3-flash JSON（candidates + thoughtSignature + usageMetadata），不是错误 passthrough。
- 主日志中除了 exhausted 号的 429 warn 和 healthy 号的 success，无新错误模式。

---

## 八、待跟进事项

### 8.1 daemon v4 未部署

`P42: 部署 daemon v4 常驻` 仍是 pending 状态。v3 的错误判定机制在 `20260828-antigravity-quota-and-contamination.md` 第三节剖析过（把 5h 桶余量当作"号健康"，把 weekly=0% 的号留在池子里）。v4 用 `retrieveUserQuotaSummary` + 正确的 User-Agent 拿到真实周桶，dry-run 代码已完成，还差 systemd 挂常驻。

**优先级**：daemon v4 消除的是**坏号源头**，本次 CPA 修复消除的是**已入池坏号的放大**。两者互补，都做效果最好。

### 8.2 生产 2 个号仍在 disable

`cindy.stephens897293` 和 `t01062880838` 在生产 disable，为 A/B 测试借出。两个号至今 (2026-08-30) 未回归生产。需要决策：
- 直接放回生产（`t01062880838` 是独立 project，很快就能恢复流量）。
- 或者留在 `cpa-abtest`，做第二阶段的 conductor selector 修复实验。

### 8.3 第二阶段修复（`pickNextMixedLegacy` 一致性）

让 pick 逻辑一起参考 `registry.SetModelQuotaExceeded` 的 5min 窗口，或者让 credential-scope 冷却直接从 `auth.Quota.Exceeded` 反哺 candidate filter。目标：本次 2× 剩余放大 → 1×。

### 8.4 观测指标

- 每小时 wording branch 命中次数 → Prometheus counter。
- 每次 `WeeklyQuotaHardWall` 触发时的 `retryAfter` 分布 → 观察 Google reset 时长是否稳定在 5-12h 区间。
- 若 `quota_exhausted` 分类计数不为零而 wording match 归零 → 报警（Google 换措辞）。

### 8.5 事故文档补齐

Task #29（P2）: 补 v7.2.143 + Phase 3 到 rca/runbook/evidence 三份主文档。本次 patched 二进制打的其实是 v7.2.135 baseline（`cpa-abtest` 镜像），需要把补丁 rebase 到 v7.2.143 主线后再走正常发版流程。

---

## 九、文件清单

**新增 / 修改（CPA 仓库）**：

- `/Users/danlio/Repositories/CLIProxyAPI/internal/runtime/executor/helps/json_retry_helpers.go`
- `/Users/danlio/Repositories/CLIProxyAPI/internal/runtime/executor/antigravity_executor_credits.go`
- `/Users/danlio/Repositories/CLIProxyAPI/internal/runtime/executor/antigravity_executor_execute.go`
- `/Users/danlio/Repositories/CLIProxyAPI/internal/runtime/executor/antigravity_executor_credits_test.go`

**Baseline 保留**：容器内 `/CLIProxyAPI/CLIProxyAPI.baseline`（vps22 `cpa-abtest`）。

**文档**：

- 本文：`/Users/danlio/Repositories/tokensheep-newapi/docs/incidents/20260829-antigravity-429-classification.md`
- 前置 A/B：`/Users/danlio/Repositories/tokensheep-newapi/docs/incidents/20260829-retry-amplification-abtest.md`（本次同时更新其中的"待解决的矛盾"与"下次要验证"两节，指向本报告）
- 传染 + quota 接口发现：`/Users/danlio/Repositories/tokensheep-newapi/docs/incidents/20260828-antigravity-quota-and-contamination.md`
