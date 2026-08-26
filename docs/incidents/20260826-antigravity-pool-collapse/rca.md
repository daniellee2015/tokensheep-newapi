# 根因分析（RCA）

## 症状陈述

- 上游 antigravity 号池全部返回 429，实际下游成功 RPM 不到 100
- 下游用户反馈重试机制被触发，请求越失败越重试
- new-api 消费日志远少于实际上游承受的请求量（下游侧观察）
- 前一天调整 RPM/TPM/session 限流后，问题反而加剧

## 根因分类

分三类共 12 条已确认根因：

- **A 类 — CPA 侧放大**（决定单次下游请求消耗多少上游账号）
- **B 类 — new-api 限流交叉污染**（决定「量不进 new-api 记录」的现象）
- **C 类 — 全链路无超时**（决定挂死请求占用号槽的时长）

---

## A 类 — CPA 内部放大链

### A1. `request-retry: 3` + 三级 `quota-exceeded` fallback

**位置**：VPS196 `/CLIProxyAPI/config.yaml:83, 89-92`

```yaml
request-retry: 3
quota-exceeded:
  switch-project: true
  switch-preview-model: true
  antigravity-credits: true
```

**证据**：CPA error log 里单次下游请求出现 32~64 次 `=== API REQUEST` 记录。
以 `error-v1beta-models-gemini-3-flash-streamGenerateContent-2026-08-26T230255-8d422f01.log` 为例，
64 次调用横跨 5 个不同 auth_id（carmen、cindy、hugh、kubeq、louise），每个账号被打
8 或 16 次，正好是 `2 (endpoint) × 2 (project) × 2 (preview_model) = 8` 的乘积。

**放大公式**：`单次下游 = 换号次数(1~request-retry) × 每账号内 fallback(8)`

### A2. `daily-cloudcode-pa` 与 `cloudcode-pa` 双 endpoint 各打一遍

**位置**：CPA 源码（本仓库外）；`config.yaml` 无相关开关

**证据**：同一个 auth_id 在 error log 内交替出现两个 endpoint 的 URL：
`https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent` 与
`https://daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent`。
account × endpoint 二维消耗，8 倍放大里的 ×2 因子来自此处。

### A3. 每账号内 fallback 不区分模型配额来源

Google Gemini 的配额按 (account, model, project) 三维切分。CPA 的
`switch-preview-model` 在**同一账号内**换模型变体，仍然消耗该账号的配额；
`switch-project` 在**同一账号内**换 project，同样消耗该账号总配额。这两个开关
本意是提升成功率，但在多租户网关场景下等价于「把一个号的所有额度组合全部
在一次下游请求内烧完」。

### A4. 无 (account, model) 独立冷却

CPA 冷却池是账号级的：号 A 的 gemini-3-flash 打满后，号 A 整体被标记为
「刚失败」；此时号 A 的其他模型可用配额被忽略，直到冷却过期。

**放大规模的实测数据**：

| Log 时间戳 | 上游调用数 | 涉及账号数 | 每账号次数 |
|---|---|---|---|
| 23:03:32 | 32 | 2 | kubeq 24, louise 8 |
| 23:03:32（同秒第二条）| 32 | 2 | kubeq 24, louise 8 |
| 23:03:59 | 24 | 2 | kubeq 16, louise 8 |
| 23:03:59（同秒第二条）| 32 | 2 | kubeq 24, louise 8 |
| 23:05:18 | 64 | 5 | kubeq 24, hugh 16, carmen/cindy/louise 8 |

kubeq 号在 5 分钟内被打 24+16+24+24 = **88 次**，几乎每次都是 429，然后号已死；
然后 hugh 16 → carmen 8 → cindy 8 → louise 稳定被打。**号池扫穿速度：
59 个账号 / (5 个/请求 × 60 tps) ≈ 12 秒**。

---

## B 类 — new-api 限流交叉污染

### B1. Redis key 按 userId 建、额度按 token group 取

**位置**：`middleware/model-rate-limit.go:86, 100`

```go
successKey := fmt.Sprintf("rateLimit:%s:%s", ModelRequestRateLimitSuccessCountMark, userId)
totalKey := fmt.Sprintf("rateLimit:%s", userId)
```

vs

```go
// model-rate-limit.go:188-192
groupTotalCount, groupSuccessCount, found := setting.GetGroupRateLimit(group)
if found {
    totalMaxCount = groupTotalCount        // ← 从 token 组读取
    successMaxCount = groupSuccessCount
}
```

**证据**：user 19（`2825305047@qq.com`）持有 6 个 token 分属 6 个不同组：
`gemini-official [2000]`, `grok-supporter [1000]`, `gpt-lowprice [1000]`,
`claude-max [300]`, `aws-q [2000]`, `claude-lowprice [1000]`。所有 token
共用 `rateLimit:MRRLS:19` 一个 Redis key。

实测 `LLEN rateLimit:MRRLS:19 = 2000`，正好等于最高组 `aws-q`/`gemini-official`
的上限。高配额 token 跑量会把共享列表撑到 2000，此时 claude-max（上限 300）
的请求进来会因 `LLEN(2000) < 300 = false` 落到时间窗口判断，**永久 429**。

### B2. LTrim 用当前 token 上限裁共享列表 → 长度剧烈震荡

**位置**：`middleware/model-rate-limit.go:74`

```go
rdb.LPush(ctx, key, now)
rdb.LTrim(ctx, key, 0, int64(maxCount-1))   // maxCount 取自当前请求的 token 组
```

**证据**：两次连续查询 `rateLimit:MRRLS:1` 的 `LLEN`：738 → 15。列表长度
在毫秒级内被剧烈裁剪。原因是最近一次成功请求刚好来自低配额 token（如
`free` 上限 10），`LTrim(0, 9)` 把整个列表裁到 10。下一次高配额 token
请求成功又让列表长回去。

**后果**：任何 token 的实际可用额度，取决于「刚好是哪个别的 token 最后成功了
一次」，完全不可预测。

### B3. 限流拒绝在 relay 之前发生，不写任何日志

**位置**：`middleware/model-rate-limit.go:94, 118`

```go
abortWithOpenAiMessage(c, http.StatusTooManyRequests, "...")
return
```

**机制**：`abortWithOpenAiMessage` 在 relay 层之前 abort 请求。而消费日志
（type=2）和错误日志（type=5）都是在 `controller/relay.go` 里写入的
—— 中间件拒绝的请求根本走不到那些代码。

**证据**：近 10 分钟两个实例的容器日志内 `token RPM limit exceeded` 和
`您已达到` 计数都是 **0**。近 1 小时 logs 表内消费日志 3730 条 + 错误
日志 305 条 = 62 RPM，远低于下游侧观察到的 900 RPM（15/s）总量。差额
838 RPM 全部落在「被中间件拒绝、不进任何日志」的夹缝里。

这就是**「量根本不进 new-api 记录」**的完整解释。

### B4. 每次拒绝 `rdb.Expire` 续期窗口

**位置**：`middleware/model-rate-limit.go:58`

```go
if int64(subTime) < duration {
    rdb.Expire(ctx, key, ...)   // ← 拒绝时刷新 TTL
    return false, nil
}
```

**机制**：每次拒绝都刷新 key 的 TTL，让「最老一条」始终处于窗口内。持续
重试的下游会把窗口无限延后，形成自锁——除非重试完全停止，否则永远拒绝。

（注：本次事故中因为 `successMaxCount` 被临时上调到 5000，B1+B2+B4 组合
的自锁未触发；但在昨天的 1000 配置下必然生效。）

### B5. 探针 token 与生产 token 同属 user 1，共用计数器

**位置**：`tokens` 表；`middleware/model-rate-limit.go:86`

**证据**：user 1（`sheepmie`）持有 18 个 token，包含所有探针
（`monitor-free`, `monitor-gpt-supporter`, `monitor-aws-q`,
`monitor-gemini-pro`, `monitor-image` 等）+ 生产 token。全部共用
`rateLimit:MRRLS:1`。生产 token 跑量把列表撑到 738，`monitor-free`
（`free` 组上限 10）请求进来因 `738 < 10 = false` 被 429，探针误报。

Kuma 上「Gemini Pro 通道」和「GPT 通道」持续 503/429 的间歇性告警，
部分是由此产生的假阳性，而非上游真的挂。

### B6. `UpdateModelRequestRateLimitGroupByJSONString` 写操作误用读锁

**位置**：`setting/rate_limit.go:30-36`

```go
func UpdateModelRequestRateLimitGroupByJSONString(jsonStr string) error {
    ModelRequestRateLimitMutex.RLock()      // ← 写操作用 RLock
    defer ModelRequestRateLimitMutex.RUnlock()
    ModelRequestRateLimitGroup = make(map[string][2]int)  // 先清空
    return json.Unmarshal([]byte(jsonStr), &ModelRequestRateLimitGroup)
}
```

**问题**：`RLock()` 允许多个读者并发进入，也允许读者与「写」并发。且实现
是「先清空、后填充」——中间存在**空 map 窗口**。`SyncOptions` 周期性 reload
（`main.go:109`）会周期性触发这个空 map 窗口，期间所有 `GetGroupRateLimit`
返回 `found=false`，请求全部回落全局值（`totalMaxCount: 0` = 无限，
`successMaxCount: 5000/1000`）。

### B7. 总数桶 Redis key TTL = -1（永不过期）

**位置**：`common/limiter/` 令牌桶实现（未加 EXPIRE）

**证据**：`TTL rateLimit:1` 与 `TTL rateLimit:19` 返回 -1。用户注销或换
分组后旧桶不会自动清理，Redis 内存中留残余。当前规模下不是紧迫问题，
但长期累积。

---

## C 类 — 全链路无超时

### C1. caddy `streaming_proxy` 中 `response_header_timeout 0`

**位置**：VPS196 `/etc/caddy/Caddyfile` 中 `(streaming_proxy)` 片段

```caddyfile
transport http {
    read_buffer 16KB
    write_buffer 16KB
    response_header_timeout 0    # ← 显式「无限等待响应头」
    dial_timeout 30s
}
```

**证据**：caddy access log 内实测 `duration` 分布 1480~3168 秒（25~53 分钟）。
近 1 小时 1115 个 cpa 请求中 1133 条 `aborting with incomplete response`，
几乎 100% 挂死。

**含义**：只要 CPA 上游不返回响应头，caddy 会无限期挂住连接。配合 A 类
放大链，一次下游请求把 5 个账号打满 429 期间，caddy 侧的 goroutine
一直挂着。

### C2. new-api relay 无中继超时

**位置**：`service/http_client.go`（http.Client 构造处，未设 `ResponseHeaderTimeout`）

**证据**：VPS22 options 表内查询 `%timeout%` / `%stream%` **无任何记录**。
代码层也未设 `Transport.ResponseHeaderTimeout`。

**含义**：new-api 与 CPA 之间的 HTTP 连接也没有响应头超时。即使下游客户端
断开，new-api 侧的 HTTP 请求仍在跑，号槽被继续占用。

### C3. relay 请求未绑定下游 context

**位置**：`relay/channel/api_request.go` 中 `http.NewRequest` 调用

```go
req, err := http.NewRequest(method, url, body)   // ← 未传 context
```

**含义**：下游客户端主动断开（如超时、取消）时，new-api 无法感知，也无法
向上游 CPA 传播取消信号。上游连接继续跑到自身超时（若有）或永远。

C1+C2+C3 三者叠加 → 挂死请求可持续 25~53 分钟且无任何自愈机制。

---

## 放大倍数总表

| 层 | 因子 | 累计 |
|---|---|---|
| 下游客户端重试 | ~3× | 3 |
| new-api 内部重试（`RetryTimes=0`） | 1× | 3 |
| CPA `request-retry: 3` | 4×（初次 + 3 重试）| 12 |
| CPA `quota-exceeded` 三级 fallback | 8× | 96 |
| 挂死请求叠加占用（无超时） | 时间维度，不计倍数 | — |

上游账号池承受的实际 429 请求量 ≈ 客户端发出请求量 × 96×。

**这就是 60 tps 下游能把 59 个账号池瞬间打死的完整机制。**

---

## 已排除的假设及证据

| 假设 | 排除依据 |
|---|---|
| new-api 内部重试放大 | `common.RetryTimes = 0`（options 表实测） |
| antigravity-manager 换号链 | 用户确认已废弃项目 |
| uptime-kuma 探针频率 | 16 个监控中 11 个 60s 打状态端点不走 relay；5 个 300s 打 relay 但快速返回不挂 |
| 渠道自动探活 | `controller/channel-test.go:498` 会写消费日志且量小 |
| Status 页面页脚心跳 | 提交 7af9df93c 是纯前端 UI，链接到外部 status.tokensheep.fun |
| `disable-cooling: true` | 当前配置已确认为 `false` |
| caddy `response_header_timeout` 无超时是主因 | 降级为次因（延长挂死时间，不产生放大） |
| CPA 二进制不读 `disable-cooling` | 之前 `strings` 命令 grep 了错误的小写路径；实际二进制是 UPX 压缩，`strings` 返回 1 行不可信 |

---

## 修复优先级

见 [`runbook.md`](./runbook.md)：

- **Phase 1** — CPA config（A 类）：`switch-preview-model` / `switch-project` /
  `request-retry` 逐步关闭。预期放大倍数从 32~64× 降到 2~4×。
- **Phase 2** — new-api relay（C 类）：加 `ResponseHeaderTimeout: 120s` +
  `NewRequestWithContext`。预期挂死时长从 25~53min 降到 ≤2min。
- **Phase 3** — new-api 限流（B 类）：Redis key 加 tokenId 维度 + 限流拒绝
  写日志 + `rate_limit.go` 并发 bug + 探针 token 迁走。
- **Phase 4** — 结构性优化：Gemini 官方 fallback / 单模型限流 / 大请求
  差异化配额 / 加账号。
