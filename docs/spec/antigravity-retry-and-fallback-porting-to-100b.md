# Antigravity 重试、号池与模型降级：面向 100b 的移植规格

> **产出日期**：2026-09-21
> **状态**：按当前代码、生产配置与自然流量验证结果归档
> **交付目的**：记录 tokensheep new-api + CPA 的最终处理机制，供以后在 100b fork 上重新实现。
> **适用链路**：客户端 -> new-api（VPS22）-> CPA/CLIProxyAPI（VPS196）-> Google Antigravity。
> **安全约束**：本文不记录管理密钥、API key、OAuth token、账号邮箱或用户请求正文。

---

## 0. 结论

这套链路不能保证永远不向客户端返回 400、404、429 或 503。真实无额度、所有候选模型都无容量、请求本身非法、内容策略拒绝、客户端在流式响应中途断开等情况，最终错误必须如实返回。

可以保证的是：

1. 请求错误不会误伤整个账号池。
2. 单账号或单模型短暂拥塞时，CPA 会有界换号，并在换号之间退避。
3. CPA 已经尝试过仍失败时，new-api 才会切到低优先级的模型映射渠道。
4. 同一个 new-api 渠道 ID 在一个请求内不会重复命中。
5. 周桶或 5 小时桶不足的账号由 daemon 提前关闭，恢复到安全水位后再开启。
6. daemon 不会自动打开管理员手动关闭的账号。
7. 重试有明确上限，避免高并发时把一个客户端请求放大成无界的上游请求风暴。

最终结构是两层保护：

```text
客户端请求
    |
    v
new-api 渠道 12：真实模型，priority=0
    |
    v
CPA：真实模型，每次渠道调用最多选择 1 个 credential
    |
    | 最终失败且属于 new-api 可重试错误
    | 等待短 Retry-After（仅 429/503，最多 5 秒）
    v
new-api 渠道 87 或 88：模型映射，priority=-1
    |
    v
CPA：映射模型，每次渠道调用最多选择 1 个 credential
    |
    | 普通 429：1-2s、2-4s、4-8s 封顶的 equal-jitter 退避
    | 5h/weekly：按桶类型设置模型级或账号级冷却
    v
Google Antigravity
```

这里的两层职责必须保持分离：

- **CPA 是第一层**：管理账号池、额度桶、账号/模型冷却和换号节奏。
- **new-api 是第二层**：管理不同渠道 ID、模型映射、优先级和最终 fallback。
- **daemon 是池外控制面**：通过真实 quota 接口决定账号是否应该进入 CPA 可选号池。

---

## 1. 本次事故暴露的问题

### 1.1 旧链路会放大失败

旧行为中，一个坏账号可能在很短时间内连续触发：

1. CPA 内部 base URL fallback。
2. CPA 内部 soft retry。
3. CPA 立即换下一个账号。
4. new-api 在同一个逻辑渠道上再次重试。
5. new-api 再进入一个实际指向相同 CPA URL、相同 key、相同号池、相同映射的重复 fallback 渠道。

生产日志曾确认，同一个 request ID 在约 300-600ms 内连续撞到 3 个 CPA 账号，随后 new-api fallback 又形成第二轮扫池。单个客户端请求可形成约 6 次相关上游调用；更早的隔离实验还观察过周桶耗尽账号产生 8 倍甚至 16 次原始上游调用的情况。

因此，低流量下也会出现“大面积 429”。10-20 RPM 并不自动代表安全：如果失败在几百毫秒内被同步放大，瞬时请求密度仍然足以让多个账号同时进入 429。

### 1.2 429 不是一种错误

Antigravity 至少存在以下几类 429：

| 类型 | 典型信号 | 正确动作 |
|---|---|---|
| 5 小时滑动桶 | `RATE_LIMIT_EXCEEDED`，通常带短 `retryDelay` | 当前账号 + 模型短冷却，允许有界换号 |
| 周桶硬墙 | `QUOTA_EXHAUSTED`，或 `Individual quota reached ... Resets in ...` | 整个 credential 冷却到真实 reset，不做徒劳短重试 |
| 普通无结构 429 | `Resource has been exhausted (e.g. check quota).`，无可靠桶信息 | 视为短暂拥塞，1 秒基础退避后换号；模型至少冷却 10 秒 |
| 模型容量错误 | 有时返回 429，有时返回 503，并包含模型暂不可用语义 | 不污染整个账号；由 new-api 进入模型映射 fallback |

把所有 429 都解释成“账号没有周额度”会误关健康账号；把所有 429 都解释成“立刻换号”又会制造扫池风暴。分类是这套机制能工作的前提。

### 1.3 额度面板和请求结果可能看起来矛盾

一个账号 `gemini-weekly=100%` 仍可能返回 429，原因包括：

- `gemini-5h` 已耗尽。
- 请求命中模型瞬时限流或模型容量不足。
- 多个请求同时选中该账号，读取 quota 时健康，但发送请求时已拥塞。
- 客户端请求模型与实际映射模型不同，两个模型的可用性状态不同。
- quota 读取失败、refresh token 失效或 project 绑定异常。

因此不能只看周桶剩余量，也不能用一次 429 直接推断周桶耗尽。

---

## 2. 最终错误分类矩阵

| HTTP/错误 | CPA 行为 | new-api 行为 | 是否应换账号 | 是否应切映射渠道 |
|---|---|---|---|---|
| 400：空 `system_instruction.parts[0]` | 不适用；这是转换错误 | 转换时过滤空白 system/developer 消息，避免发出非法 Gemini 请求 | 否 | 否，应该在本地修正 |
| 400：`PROHIBITED_CONTENT`/SAFETY | 请求级错误 | 原样结束，不重试 | 否 | 否 |
| 400：其他明确 invalid request | 请求级错误 | 原样结束，不重试 | 否 | 否 |
| 404：`requested model or endpoint is unavailable` | 标为 request/model scoped，不扫账号池、不冷却账号 | 若生产 retry 状态码策略允许，选择下一个未使用的低优先级渠道 | 否 | 是 |
| 429：结构化 5h 短限流 | 按 `RetryInfo` 分类，冷却当前账号 + 模型并有界换号 | CPA 最终仍失败且携带短 `Retry-After` 时，等待后切渠道 | 是 | 最终失败后是 |
| 429：weekly hard wall | 整个 credential 冷却到 reset | CPA 池仍无可用账号时才切映射渠道 | 是，但不可再次选择该 credential | 是 |
| 429：普通无结构 exhausted | 设置 `RetryAfter=1s` 和 `CredentialFailoverDelay=1s`，有界换号 | 保留 `Retry-After`，等待 `[1s,2s)` 后切渠道 | 是 | 最终失败后是 |
| 503：`MODEL_CAPACITY_EXHAUSTED` | request/model scoped，不扫账号池、不冷却账号 | 无响应头时合成 1 秒 delay，等待 `[1s,2s)` 后切映射渠道 | 否 | 是 |
| 503：`No capacity available for model` | 同上 | 同上 | 否 | 是 |
| 503：`temporarily unavailable` + `Retry in 1s` | request/model scoped | 无响应头时合成 1 秒 delay，再切映射渠道 | 否 | 是 |
| 503：new-api 无可用渠道 | CPA 未被调用 | 最终返回；检查能力缓存、渠道状态和分组 | 否 | 已无候选 |
| 503：其他上游真实过载/余额不足 | 按各 provider 语义处理 | 不能伪装成 Antigravity 故障 | 视 provider 而定 | 视配置而定 |

### 2.1 为什么 400 不能统一重试

客户端请求结构、工具参数、内容策略或上下文状态错误不会因为换账号而消失。盲目重试会产生三种后果：

- 相同非法请求重复占用并发。
- 多个健康账号被相同 400 污染或错误冷却。
- 最终错误延迟变长，但成功率不变。

本次修复只消除了一个网关自己制造的 400：OpenAI Chat 转 Gemini 时，空白的 system/developer 消息不再生成空 `parts[0]`。其他真正的请求级 400 仍然终止请求。

### 2.2 为什么 404/容量 503 不在 CPA 扫账号

`requested model or endpoint is unavailable`、`MODEL_CAPACITY_EXHAUSTED` 和 `No capacity available for model` 描述的是当前请求模型或端点，不是某一个 credential 的健康状态。即使在代码测试上允许换多个 credential，生产当前也不应为这些错误扫号池。

CPA 将这些错误标记为 request scoped：停止 credential failover，同时禁止给 credential 写错误冷却。new-api 再通过独立渠道的模型映射，把原模型切到备选模型。

### 2.3 为什么只等待短 `Retry-After`

new-api 仅在以下条件全部满足时等待：

- 状态码为 429 或 503。
- 解析出的 `Retry-After` 大于 0。
- 延迟不超过 5 秒。

支持两种标准格式：整数秒和 HTTP-date。等待采用 equal-jitter：实际延迟为 `[delay, 2*delay)`，并响应请求取消。

5 小时或 7 天 reset 不能阻塞一个 HTTP 请求那么久。这些长期状态由 CPA 冷却和 daemon 管理，new-api 立即寻找其他渠道；如果没有候选，则把最终错误及 `Retry-After` 返回客户端。

---

## 3. CPA 第一层保护

### 3.1 生产配置基线

VPS196 当前语义配置：

```yaml
request-retry: 0
max-retry-credentials: 1
disable-cooling: true
antigravity:
  enforce-short-cooldown: true
```

配置含义：

| 配置 | 当前值 | 实际语义 |
|---|---:|---|
| `request-retry` | 0 | 只有初始 round，不在账号集合耗尽后开启额外 round |
| `max-retry-credentials` | 1 | 每个 round 只选择 1 个 credential；当前只有 round 0，不在同一 project 内连续换号放大 429 |
| `disable-cooling` | true | 关闭旧式长时间通用 cooling；Antigravity 的短冷却仍由下一项强制执行 |
| `enforce-short-cooldown` | true | 即使通用 cooling 被关闭，429 后仍保留 Antigravity 的账号 + 模型短冷却 |

不要把 `request-retry` 理解成“所有重试的总次数”。它控制额外 credential round，不控制 executor 内部的 base URL 尝试，也不控制 new-api 的渠道 fallback。

### 3.2 普通 429 的换号节奏

CPA commit `1d57a590` 为普通无结构 429 增加了两个信号：

- 错误对外携带 `RetryAfter=1s`。
- conductor 读取 `CredentialFailoverDelay=1s`。

代码仍保留连续 credential 失败的 equal-jitter 退避契约，供独立 project 池或未来显式提高上限时使用：

| 连续失败序号 | 等待范围 |
|---:|---|
| 第 1 个失败后 | `[1s, 2s)` |
| 第 2 个失败后 | `[2s, 4s)` |
| 第 3 个及以后 | `[4s, 8s)`，封顶 |

如果已经没有下一个允许的 credential，则不再等待。请求 context 被取消时，等待立即结束。

当前生产 `max-retry-credentials=1`，因此一次 CPA 调用不会在同一 project 内继续尝试第二或第三个 credential；new-api 仍最多执行一次跨模型 fallback。只有在明确接入独立 project 池并重新验证放大上界后，才可提高该值。

### 3.3 10 秒短冷却下限

Antigravity 的 429 即使携带亚秒级 `retryDelay`，当前账号 + 模型也至少隔离 10 秒。这个下限避免多个并发请求在 Google 给出的极短窗口结束前后同时重新选中同一个账号。

较长的上游 reset 时长会被保留，不会被压缩到 10 秒。weekly hard wall 还会通过 credential scoped 错误冷却整个账号，而不是只冷却一个模型。

### 3.4 60 秒模型级 breaker 保持关闭

旧的池级 model breaker 用来阻止无界扫完整个大池。现在 `max-retry-credentials` 已经给单请求设置硬上限，如果两者同时开启，前三个账号碰巧失败就可能让后续请求在 60 秒内完全看不到健康账号。

当前实现中，只要 `max-retry-credentials > 0`，Antigravity model breaker key 就不启用。迁移时不要重新打开这层 breaker，除非 100b 删除了 credential 尝试上限，并重新证明 breaker 的收益大于误伤。

### 3.5 覆盖的执行路径

换号退避必须同时覆盖：

- 普通 Execute。
- ExecuteStream 的 bootstrap 阶段。
- CountTokens。
- Home execution 路径。

流式请求一旦已经向客户端写出响应字节，就不能切换到另一个独立上游响应，否则会把两个流拼接成一个损坏的 HTTP 200/SSE 流。CPA 和 new-api 都必须遵守这个边界。

### 3.6 CPA 参考代码

CPA 是独立仓库 `/Users/danlio/Repositories/CLIProxyAPI`，当前分支与提交：

```text
branch: feat/plugin-quota-slot
commit: 1d57a590 fix(antigravity): pace generic 429 failover
```

关键文件：

```text
internal/runtime/executor/antigravity_executor_credits.go
internal/runtime/executor/antigravity_executor_credits_test.go
internal/runtime/executor/antigravity_model_breaker.go
sdk/cliproxy/auth/conductor_failover_delay.go
sdk/cliproxy/auth/conductor_execution.go
sdk/cliproxy/auth/conductor_home_execution.go
sdk/cliproxy/auth/home_retry_contract_test.go
```

100b 若继续使用同一个外部 CPA 服务，不需要把这些 Go 文件复制进 100b；只需要保留 CPA 镜像、配置和接口契约。若未来 100b 自建 CPA fork，应按目标分支重新实现并跑 CPA 自己的测试。

---

## 4. new-api 第二层保护

### 4.1 已落地代码

当前 tokensheep-newapi 的三个相关提交：

```text
6fe41a40a fix(relay): honor short upstream retry delays
4b770d99b fix(gemini): omit empty system instructions
92e211b5f fix(relay): pace model capacity fallback
```

职责分别是：

| 提交 | 行为 |
|---|---|
| `6fe41a40a` | 解析并保存标准 `Retry-After`；仅对 429/503 的短延迟等待；最终错误继续返回 `Retry-After` |
| `4b770d99b` | OpenAI Chat -> Gemini 时忽略空白 system/developer 消息 |
| `92e211b5f` | 对明确的模型容量 503，在没有响应头时补 1 秒 fallback delay |

关键文件：

```text
controller/relay.go
controller/relay_error_test.go
service/error.go
service/error_test.go
relaykit/types/error.go
relaykit/relayconvert/internal/oai_chat/to_gemini_chat_req.go
relaykit/relayconvert/internal/oai_chat/to_gemini_chat_req_test.go
```

### 4.2 同一请求不重复渠道 ID

`RetryParam` 在每次选择渠道后立即记录该 channel ID，后续缓存路径和数据库路径都会排除已使用 ID，并从剩余渠道中选择最高优先级。

这个机制解决的是“重复 new-api 渠道 ID”，不能识别两个不同 channel ID 背后其实使用相同 CPA URL、相同 key 和相同模型映射。因此配置层仍然禁止创建语义重复的渠道。

### 4.3 retry=3 的准确含义

new-api 的全局 retry 设置为 3，表示循环最多包含初始尝试和 3 次后续选择机会。实际尝试数还受以下因素限制：

- 可用且支持原始模型的不同 channel ID 数量。
- 已使用 channel ID 排除集合。
- 渠道 priority。
- 状态码和错误码是否允许重试。
- 是否已经写出响应字节。
- 请求 context 是否已取消。

当前原模型通常只有“主渠道 + 对应方向的一个映射渠道”，所以通常最多经过 2 个不同的
new-api channel ID。任何路径都不会为了凑满 4 次而重复已使用 channel ID。

### 4.4 当前生产渠道

VPS22 PostgreSQL 当前配置基线：

| ID | 名称 | 状态 | Priority | 用途 |
|---:|---|---:|---:|---|
| 12 | `own-cpa-multi-gemini-mapped-AD9x` | 1（启用） | 0 | 真实模型主渠道 |
| 87 | `own-cpa-gemini-fallback-to-38` | 1（启用） | -1 | 3.7/相关源模型映射到 3.8；3.1 Pro 映射到 3.8 |
| 88 | `own-cpa-gemini-fallback-38-to-37` | 1（启用） | -1 | 3.8 映射到 3.7 |
| 90 | `own-cpa-gemini-fallback-38-to-37` | 2（禁用） | -1 | 与 88 重复，保持禁用 |
| 91 | `own-cpa-gemini-fallback-to-31-lite` | 2（禁用） | -3 | 更深层 3.1-lite 降级，当前不启用 |
| 102 | `own-cpa-gemini-pro-fallback-to-37` | 2（禁用） | -2 | 失败实验：共享 project 限流时继续打 3.7 只会放大请求 |

迁移时 ID 会变化，不能把 12/87/88 当作业务常量。必须按名称、上游目标、模型能力、映射和 priority 重新核对。

### 4.5 为什么 90 必须禁用

渠道 88 和 90 曾经同时具备：

- 相同 CPA URL。
- 相同 key。
- 相同 priority。
- 相同的 3.8 -> 3.7 映射。
- 相同号池。

new-api 会正确排除已经打过的 ID 88，但会把 ID 90 当成另一个渠道，于是同一个客户端请求再次进入相同 CPA 号池和相同模型。这不是冗余容灾，而是重试放大。

100b 迁移后的配置校验必须以“实际故障域”去重，而不只按 ID 去重。以下五项全部相同的渠道只能保留一个启用实例：

```text
base URL + credential/key + provider + source model capability + model mapping
```

### 4.6 模型映射的方向

目标策略是：

```text
请求真实 3.7：先走主渠道的真实 3.7；失败后走渠道 87，把 3.7 映射到 3.8
请求真实 3.8：先走主渠道的真实 3.8；失败后走渠道 88，把 3.8 映射到 3.7
请求 3.1 Pro：先走主渠道的真实 Pro；失败后走渠道 87 映射到 3.8
```

2026-09-22 事故中，旧配置把 `gemini-3.1-pro-high` 从 `gemini-pro-agent` 降级到
`gemini-3.1-pro-low`。两个 Pro 变体在同一个共享 project 上同时返回通用 429：15 分钟内
CPA 产生 293 次真实上游 429，涉及全部 22 个 active credential；93 个失败调用都尝试了
3 个 credential。new-api 的 `12 -> 87` fallback 虽然正常执行，但仍以 503 结束。

同一窗口 Flash 模型仍有稳定成功流量，因此第一级 Pro fallback 改为跨模型族的 3.8。
随后曾短暂启用渠道 102，把 3.8 失败继续降级到 3.7；前三个自然流量样本全部按
`12 -> 87 -> 102` 失败，3.7 同样返回通用 429，没有一次成功，单请求最坏上游尝试反而从
6 次增加到 9 次，因此立即禁用 102。

模型降级只能绕开单模型容量故障，不能制造新的 project 容量；当所有 active credential 都
属于同一个 project 且该 project 整体限流时，继续堆叠 fallback 是错误方向，最终需要独立
project 容量或入口级排队限流。

同一事故窗口还发现 VPS22 曾同时启用渠道 99（Pro -> `gemini-pro-agent`）和渠道 100
（Pro -> 3.8），它们与渠道 87 的 Pro 映射重复，实际路径出现过
`12 -> 99 -> 87 -> 100`。这会让一个共享 project 的 429 被重复放大。2026-09-22
已将 99、100 设为 disabled，只保留 87 作为 Pro 的唯一映射渠道；102 的 3.7 深层
fallback 也保持 disabled。

随后又按 source capability 做了交叉核对：渠道 88 原先还声明了 `gemini-3-flash`、
`gemini-3.6-flash` 等与 87 重叠的 source model，存在异常时形成第三层
`12 -> 87 -> 88` 的可能。现已将 88 收窄为只接受 `gemini-3.8-flash` 和
`gemini-3.8-flash-high`，只承担 3.8 -> 3.7；87 继续承担 3/3.6/3.7/Pro -> 3.8。

两个 fallback 渠道都必须低于主渠道 priority。它们的 source model capability 应保持方向互斥，避免一次请求同时看到无关方向的映射渠道。

价格相同只解决计费一致性，不代表模型语义完全一致。迁移时仍要确认工具调用、长上下文、thinking、结构化输出和流式响应在两个模型上都兼容。

---

## 5. daemon：5h、weekly、自动关闭与恢复

### 5.1 数据源

daemon v4 不从 429 响应猜额度，而是通过 CPA management API 转发到 Antigravity 当前使用的 quota endpoint：

```text
https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary
```

它读取以下桶：

| group | 5 小时桶 | 周桶 | 当前是否控制 Gemini 号池开关 |
|---|---|---|---|
| Gemini Models | `gemini-5h` | `gemini-weekly` | 是 |
| Claude and GPT models | `3p-5h` | `3p-weekly` | 否，只观测 |

当前 new-api 指向 CPA 的生产渠道只承担 Gemini 流量，因此 3p 桶不能用来关闭账号，否则会把 Gemini 仍健康的账号误关。

### 5.2 当前阈值和滞回

当前默认及生产目标值：

```text
CPA_WEEKLY_EXHAUSTED=0.05
CPA_WEEKLY_HEALTHY=0.10
CPA_FIVE_HOUR_EXHAUSTED=0.02
CPA_FIVE_HOUR_HEALTHY=0.10
CPA_MAX_ACTIVE=40
CPA_MAX_ENABLE_CYCLE=5
CPA_CYCLE_SEC=1800
```

完整判断如下：

| 当前状态 | quota 条件 | 动作 |
|---|---|---|
| enabled | weekly `<=5%` | disable，并记入 daemon 自动关闭状态 |
| enabled | weekly `>5%`，5h 有效且 `<=2%` | disable，并记入 daemon 自动关闭状态 |
| enabled | weekly/5h 不触发关闭 | keep |
| disabled，且是 daemon 关闭 | weekly `>=10%` 且 5h `>=10%` | 允许 enable |
| disabled，且是 daemon 关闭 | weekly `<10%`、5h `<10%`，或 5h 缺失/非法 | keep，等待恢复 |
| disabled，且不是 daemon 关闭 | 任意健康 quota | keep，视为管理员手动关闭 |
| 任意 | quota 读取失败或桶数据非法 | keep，不因读取失败改变状态 |

`(5%,10%)` weekly 灰区和 `(2%,10%)` 5h 恢复区构成滞回；边界 5%/2% 触发关闭，10% 才允许恢复。这可以防止账号在临界值附近每 30 分钟反复开关。

### 5.3 手动关闭优先

daemon 把自己因额度关闭的 auth 名称写入持久化状态文件。只有存在于这个集合的 disabled 账号才有资格自动恢复。

管理员在 UI 手动关闭的账号不会被 daemon 打开。管理员手动打开一个 daemon 曾关闭的账号后，下一轮会清除过期的自动关闭标记；后续也不会把它误认为 daemon 管理状态。

这个规则是修复“我刚手动关掉，daemon 又自动打开”的关键。100b 迁移时，状态文件必须与 auth 目录一起持久化，不能放在容器临时文件系统。

### 5.4 开号限流

daemon 每轮先执行全部必要的 disable，再尝试 enable。enable 同时受两层限制：

- 单轮最多 5 个。
- active 总数不能超过 40。

disable 不限数量，因为关闭确定耗尽账号是止血动作。enable 必须渐进，避免大量账号同时重新加入并被并发流量瞬间打空。

`MAX_ACTIVE=40` 是当前运营上限，不是永久容量公式。迁移到 100b 后应按真实单号安全 RPM、单号并发、安全水位和目标流量重新测量；不能把“约多少账号承载多少 RPM”的旧手感直接写进算法。

### 5.5 quota 读取失败与死号

普通读取超时、quota API 短暂限流、blue 实例暂时不可达时，daemon 保持账号当前状态，不会关闭一个可能健康的账号。

明确的 `auth token refresh failed` 会进入死号计数。连续 3 轮确认后，daemon 最多每轮隔离 5 个文件到 `auths/dead/`，只移动、不删除，以便重新 OAuth。这个路径与普通 quota unreadable 分开，不能因为一次探测失败就隔离账号。

### 5.6 daemon 和 CPA 请求时冷却的边界

两者解决不同时间尺度的问题：

- daemon 每 30 分钟读取真实桶，负责池成员资格和长期恢复。
- CPA 在请求发生时立即处理 429，负责秒级到 reset 时刻的冷却和换号。

daemon 无法阻止两个扫描周期之间突然发生的 429；CPA 也不应该长期替代 quota 控制面。两者必须同时保留。

### 5.7 当前服务状态

生产服务：

```text
cpa-daemon-v4.service: active
cpa-sidecar-sync.service: disabled/inactive
```

迁移时不能同时启用两个会修改 auth `disabled` 状态的控制器，否则手动关闭权、自动关闭集合和开号上限都会失去单一事实来源。

---

## 6. 高并发与低并发下的行为

### 6.1 低并发

在 10-20 RPM 下，普通健康流量应大部分由主渠道和第一个 CPA credential 完成。偶发普通 429 会：

1. 当前账号 + 模型进入至少 10 秒冷却。
2. 当前生产上限为 1，CPA 不再在同一渠道内选择第二个 credential。
3. CPA 仍失败后，new-api 根据短 `Retry-After` 再等待，然后只进入一次映射渠道。

如果低并发下仍持续出现大面积 429，优先检查：

- daemon 是否读取正确 project 和 daily endpoint。
- 5h 桶是否已空。
- `max-retry-credentials` 是否意外变成 0（0 表示不限 credential）。
- `request-retry` 是否意外大于 0。
- 是否重新启用了重复 fallback 渠道。
- 是否有多个 daemon/sidecar 同时修改账号状态。

### 6.2 高并发

150-200 并发会增加以下风险：

- 多个请求在账号状态更新传播前同时选中同一 credential。
- 同一秒内大量 429 同步触发下一账号。
- 主模型容量不足时，大量请求同时涌入同一个 fallback 模型。
- 长任务占住连接和账号并发，RPM 看起来不高但 in-flight 很高。

当前 equal-jitter 退避用来打散同步换号，10 秒短冷却阻止新请求立刻回到同一账号，credential 上限和唯一 channel ID 则给放大设置硬边界。

它们不能凭空创造容量。若所有 active 账号的 5h/weekly 都不足，或者 3.7 与 3.8 同时无容量，最终仍会返回 429/503。此时正确动作是降低并发、等待 reset、增加独立健康容量或调整 `MAX_ACTIVE`，而不是继续增加重试层数。

### 6.3 当前放大上界如何理解

当前常见模型有 2 个符合条件的 new-api 渠道：主渠道和一个映射渠道；每次 CPA 调用最多选择 1 个 credential。因此常见硬边界是最多 2 次 CPA 渠道调用、每次最多 1 个 credential 选择。

这不是“最多 6 个 Google HTTP 请求”的保证。CPA executor 内部仍可能因协议路径、base URL 或流式 bootstrap 产生额外 HTTP 动作。可观测和容量规划必须分别统计：

- 客户端请求数。
- new-api channel 尝试数。
- CPA credential 选择数。
- 实际 Google HTTP 请求数。

只有四层都能关联到同一个 request ID，才能判断是否再次发生重试放大。

---

## 7. 100b 移植范围

### 7.1 需要在 100b 重新实现

在 100b 当前 `main` 上逐项核对，缺什么补什么：

1. `NewAPIError` 或等价错误对象保存内部 `retryAfter`。
2. relay error handler 解析标准 `Retry-After`。
3. public error 脱敏时保留内部 retry delay。
4. 最终响应写回整数秒 `Retry-After`，亚秒向上取整。
5. 仅 429/503 且 delay `<=5s` 时，在 channel fallback 前 equal-jitter 等待。
6. 等待响应 request context 取消。
7. 对三类明确模型容量 503，在没有 header 时合成 1 秒 delay。
8. 每次选择后排除已使用 channel ID，缓存路径和数据库路径行为一致。
9. 排除后仍优先选择剩余渠道中的最高 priority。
10. OpenAI Chat -> Gemini 转换过滤空白 system/developer 内容。
11. 已经写出流式响应字节后禁止跨渠道重试。
12. 最终错误日志同时保留原始 upstream 状态和公开脱敏状态。

### 7.2 通过配置重建

以下内容不应硬编码进 100b：

- 主渠道和 fallback channel ID。
- CPA URL 和 key。
- 3.7/3.8 的具体公开模型别名。
- retry 状态码策略。
- new-api 全局 retry 次数。
- channel priority、能力列表和 model mapping。

迁移时从生产导出渠道配置，去除 secret 后进行人工差异检查，再通过 100b 管理接口创建。创建后重新查询数据库/管理 API，确认能力缓存中的 enabled 状态与数据库一致。

### 7.3 不移植到 100b 的内容

- 不把 CPA Go 代码复制进 new-api 仓库。
- 不把 daemon 的管理密钥、账号列表或状态文件提交到 Git。
- 不迁移渠道 90 的重复配置。
- 不默认启用渠道 91 的 3.1-lite 深层降级。
- 不恢复 60 秒池级 model breaker。
- 不把历史账号邮箱、request body 或 OAuth 数据写入文档或测试 fixture。
- 不机械 cherry-pick 跨仓库提交。

CPA 属于另一个仓库，100b 的 new-api 基线也会继续演进。即使文件名相同，也应根据目标仓库当时的错误类型、relay loop、channel selector 和测试结构重新实现。

---

## 8. 迁移实施顺序

1. **盘点 100b 基线**：确认是否已经具备 retry-after、渠道排除、优先级 fallback 和空 system 过滤，避免重复实现。
2. **先补测试**：固定错误分类、delay 上限、取消、渠道唯一性、优先级和流式边界。
3. **移植 new-api 错误元数据**：先让 `Retry-After` 能从 upstream 一直传到 relay loop 和最终响应。
4. **移植 channel fallback 等待**：只覆盖 429/503 的短 delay。
5. **移植 channel ID 排除**：缓存与数据库两种选择路径同时完成。
6. **移植 Gemini 空 system 修复**。
7. **接入已有 CPA**：保持 CPA 代码、镜像和生产配置独立。
8. **部署 daemon 状态存储**：自动关闭集合和 dead streak 文件必须持久化。
9. **创建主渠道**：真实 3.7/3.8，priority 0。
10. **创建单向 fallback**：3.7 -> 3.8 和 3.8 -> 3.7，priority -1。
11. **做重复故障域审计**：确认没有第二个相同 URL/key/pool/mapping 的启用渠道。
12. **设置 retry=3**，确认 404/429/503 的重试策略与本文矩阵一致。
13. **先部署 green**，健康与自然流量验证通过后再部署 blue。
14. **观察至少一个 5h 桶变化窗口的趋势**，再决定是否调整 active 数或阈值。

---

## 9. 测试清单

### 9.1 new-api 单元测试

- [ ] `Retry-After: 1` 解析为 1 秒。
- [ ] HTTP-date 解析为相对延迟。
- [ ] 非法、过去时间、0 和超长 header 不触发等待。
- [ ] 429 和 503 的 1 秒 delay 触发 `[1s,2s)` 策略。
- [ ] 400、404 和其他状态码不因 `Retry-After` 在 new-api 内等待。
- [ ] 超过 5 秒的 delay 不阻塞 fallback。
- [ ] context 取消立即停止等待。
- [ ] 最终公开错误仍携带 `Retry-After`。
- [ ] 三种明确容量 503 在无 header 时得到 1 秒内部 delay。
- [ ] 普通 503 文本不会被误判为模型容量。
- [ ] 空白 system/developer 消息不会生成 Gemini `system_instruction` 空 part。
- [ ] 显式非空 system 内容保持原顺序。
- [ ] retry 不会再次选择同一 channel ID。
- [ ] 同 priority 多渠道会逐一耗尽，再进入较低 priority。
- [ ] 缓存关闭时的 DB selector 与缓存 selector 行为一致。
- [ ] response 已写出后不会跨渠道 retry。

### 9.2 CPA 单元测试

- [ ] 普通 exhausted 429 暴露 1 秒 `RetryAfter`。
- [ ] 普通 exhausted 429 暴露 1 秒 `CredentialFailoverDelay`。
- [ ] 连续失败等待范围为 `[1,2)`、`[2,4)`、`[4,8)`。
- [ ] 没有下一 credential 时不等待。
- [ ] context 取消停止等待。
- [ ] Execute、Stream bootstrap、CountTokens、Home 路径契约一致。
- [ ] 404 model/endpoint unavailable 为 request scoped。
- [ ] 503 model capacity 为 request scoped。
- [ ] weekly hard wall 为 credential scoped，并使用真实 reset。
- [ ] 亚秒 5h retry 至少产生 10 秒账号 + 模型冷却。
- [ ] 代码契约：`max-retry-credentials=3` 时单 round 不选择第 4 个 credential。
- [ ] 生产配置：`max-retry-credentials=1`，自然流量中单个 CPA 渠道调用不出现第二个 credential。
- [ ] `request-retry=0` 时不开始额外 round。

### 9.3 daemon 单元测试

- [ ] weekly `5%` 关闭，`>5%` 不因 weekly 关闭。
- [ ] 5h `2%` 关闭。
- [ ] weekly/5h 都达到 `10%` 才恢复。
- [ ] 手动 disabled 即使 100% 也不恢复。
- [ ] quota unreadable 保持当前状态。
- [ ] refresh failure 需连续 3 轮才隔离。
- [ ] 每轮最多隔离 5 个死号。
- [ ] 每轮最多开启 5 个账号。
- [ ] active 达到 40 后不再开启。
- [ ] disable 失败时不能错误释放 active 名额。
- [ ] quota 查询使用账号自己的 project 和 daily endpoint。

### 9.4 必跑命令

new-api：

```bash
go test ./...
cd relaykit && GOWORK=off go test ./...
cd relaykit && GOWORK=off go build ./...
```

CPA：

```bash
go test ./internal/runtime/executor/...
go test ./sdk/cliproxy/auth/...
go build -o /tmp/cli-proxy-api-retry-check ./cmd/server
```

生产环境禁止 build。所有构建必须在本地或 GitHub Actions 完成，服务器只拉取已构建镜像。

---

## 10. 上线清单

### 10.1 new-api（VPS22）

- [ ] GitHub Actions 镜像构建成功，记录不可变 tag/digest。
- [ ] compose 文件为 `/data/new-api/docker-compose.yml`。
- [ ] 不使用会改回浮动标签的旧 roll 脚本。
- [ ] 先更新 green。
- [ ] green 端口 3002 健康检查为 200。
- [ ] 通过负载入口观察 green 自然流量，无转换 400 和重试异常。
- [ ] 再更新 blue。
- [ ] blue 端口 3001 健康检查为 200。
- [ ] 负载入口 3000 为 200。
- [ ] 两实例查询到的 channel 90 都为 disabled。
- [ ] abilities 缓存中 channel 90 全部 `enabled=false`。
- [ ] 主渠道和两个 fallback 的 priority、model mapping、能力列表一致。

当前已验证镜像：

```text
ghcr.io/daniellee2015/tokensheep-newapi:20260921-92e211b
GitHub Actions run: 35609197255 (success)
```

这个 tag 只用于记录当前基线。迁移 100b 时必须使用 100b 自己的 Actions 产物。

### 10.2 CPA（VPS196）

- [ ] 先更新 green，绝不同时重启蓝绿。
- [ ] 从 Caddy 容器内直连 green 健康检查。
- [ ] 必要时单独重建 cursor sidecar。
- [ ] 再更新 blue。
- [ ] 从 Caddy 容器内直连 blue 健康检查。
- [ ] 确认 `request-retry=0`、`max-retry-credentials=1`。
- [ ] 确认 short cooldown 强制开启。
- [ ] 确认 daemon active，旧 sidecar sync inactive。
- [ ] 确认 daemon 只连接一个稳定 CPA 实例完成 auth index 与 quota 查询。

### 10.3 温和验证原则

生产验证优先使用：

- 健康检查。
- 配置和数据库只读查询。
- daemon 下一轮自然 quota 探测。
- 真实自然流量日志。
- 单个、低频、明确有边界的人工请求。

禁止为了验证重试而并发轰击生产号池。429 本身会改变冷却和选择状态，激进测试会污染被观测系统。

---

## 11. 回滚清单

### 11.1 配置止血优先级

1. 如果 fallback 放大，先禁用异常或重复的低优先级渠道，不动主渠道。
2. 如果 daemon 误开账号，停止 `cpa-daemon-v4.service`，保留状态文件并人工审计；不要删除状态文件。
3. 如果 CPA 新版本异常，按 green -> 检查 -> blue 顺序回到上一镜像。
4. 如果 new-api 新版本异常，按 green -> 检查 -> blue 顺序回到上一 Actions 镜像。

### 11.2 回滚后必须确认

- [ ] 主渠道仍可用。
- [ ] 同一个请求的 `use_channel` 列表没有重复 ID。
- [ ] 90/91 未因回滚被重新启用。
- [ ] 手动 disabled 账号保持关闭。
- [ ] daemon 自动关闭集合仍存在。
- [ ] 蓝绿实例版本一致。
- [ ] 没有同时运行两个账号状态控制器。

回滚代码不等于回滚数据库渠道配置。两者必须分别检查。

---

## 12. 可观测性与日志判据

### 12.1 必须能回答的四个问题

对任意一个 request ID，日志应能回答：

1. new-api 选择了哪些 channel ID，顺序和 priority 是什么。
2. 每个 CPA 调用选择了哪些 credential，是否命中 cooldown。
3. 每个 credential 产生了多少次真实 Google HTTP 请求。
4. 最终成功来自真实模型还是映射模型。

### 12.2 核心指标

| 指标 | 用途 |
|---|---|
| 客户端请求数 / 成功率 / p50/p95/p99 | 判断退避对用户延迟的影响 |
| 按 status、channel、origin model 的最终错误数 | 区分请求错误、容量错误和 quota 错误 |
| `use_channel` 长度和唯一 ID 数 | 检测同渠道重复与 fallback 深度 |
| CPA credential attempts / request | 检测扫池放大 |
| Google HTTP calls / client request | 检测 executor 内部放大 |
| 429 分类计数：5h、weekly、generic、capacity | 判断错误形状是否变化 |
| credential/model cooldown 数和剩余时间 | 判断健康容量是否被过度隔离 |
| daemon active/disabled/auto-disabled/manual-disabled | 判断控制面状态 |
| weekly/5h 分布和 reset 时间 | 预测真实容量 |
| daemon quota-unreadable 和 refresh-failed streak | 区分探测故障与死号 |
| fallback 3.7 -> 3.8、3.8 -> 3.7 成功率 | 判断映射是否真正提高成功率 |

### 12.3 正常判据

- 同一请求的 channel ID 不重复。
- 大多数请求只使用主渠道。
- fallback 请求在短 429/503 后存在约 1 秒以上的分散等待，不再集中在几百毫秒内扫完多个账号。
- 404/容量 503 不产生多 credential 扫描。
- `PROHIBITED_CONTENT` 400 没有后续 channel 尝试。
- weekly `<=5%` 或 5h `<=2%` 的 enabled 账号在下一 daemon 周期被关闭。
- 手动 disabled 账号即使额度恢复也不会出现 `ENABLE`。
- channel 90 不出现在 `use_channel`。

### 12.4 异常判据

- 同一 request ID 在 1 秒内命中多个 credential，且错误为普通 generic 429。
- `use_channel` 出现相同 ID 两次。
- 两个不同 ID 实际使用同 URL/key/mapping。
- 404/容量 503 后 CPA 继续扫账号。
- quota unreadable 导致健康 enabled 账号被批量关闭。
- daemon 打开不在 auto-disabled 集合中的账号。
- 5h 接近 0 的账号仍长期 enabled。
- 主渠道失败后 87/88 没有被选择，或映射后仍请求原模型。
- retry 延迟超过 5 秒并阻塞客户端长时间等待。

---

## 13. 当前验证结果

代码验证：

- new-api `go test ./...` 通过。
- relaykit `GOWORK=off go test ./...` 通过。
- relaykit `GOWORK=off go build ./...` 通过。
- CPA executor/auth 相关测试通过。
- CPA 本地 server build 通过。
- CPA 全量测试存在一个未修改基线也会失败的 `internal/home: TestEnsureClientsWaitsForPreviousTargetClose`，与本次改动无关。

部署后自然流量观察：

- 一个完整观察窗口中，CPA 共 404 次模型 POST，全部返回 200。
- 普通 `Resource has been exhausted` 429 为 0。
- 最终 new-api 镜像启动后的首批 27 次自然模型转发全部为 200。
- 空 system instruction 400 为 0。
- 观察窗口内 Antigravity channel 429/503 为 0。
- 剩余 400 为真实 `PROHIBITED_CONTENT`。
- 剩余 503 来自其他 GPT 渠道的无可用渠道、余额不足或真实 concurrency limit，不属于本机制。

这些结果证明修复消除了当时观测到的重试放大与转换错误，不证明上游永远不会再次返回 429/503。后续仍应按第 12 节持续观察。

---

## 14. 已知限制

1. 所有 active 账号真实无额度时，最终 429 无法通过重试消除。
2. 3.7 和 3.8 同时无容量时，最终 503 无法通过映射消除。
3. 非幂等或已经输出部分流式响应的请求不能安全跨渠道重放。
4. daemon 30 分钟扫描存在控制延迟，请求时冷却仍然必需。
5. `MAX_ACTIVE=40` 是当前运营参数，不代表 100b 的容量结论。
6. 两个不同 channel ID 只要落到同一号池，就不是独立故障域；ID 去重无法自动识别这一点。
7. 模型映射提高可用性，但可能带来模型行为差异，尤其是长任务、工具调用和结构化输出。
8. 真实内容策略 400、认证失效、余额不足和其他 provider 的限流不属于 Antigravity fallback 的修复范围。

---

## 15. 相关归档

- `docs/incidents/20260828-antigravity-quota-and-contamination.md`
- `docs/incidents/20260829-antigravity-429-classification.md`
- `docs/incidents/20260829-retry-amplification-abtest.md`
- `docs/incidents/20260830-cpa-consolidated-state.md`
- `docs/spec/concurrency-porting-to-100b.md`
- `docs/spec/subscription-porting-to-100b.md`

较早事故文档中的阈值和生产配置只代表当时状态。发生冲突时，以本文的 2026-09-21 基线和当前代码为准；迁移实施时，再以目标仓库和目标生产环境的实测配置为准。
