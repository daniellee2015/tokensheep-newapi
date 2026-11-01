# New API 上游错误边界与状态码策略

本文档定义 relay 对上游错误的公开策略。2026-10-22 按用户确认的边界修正：**隐藏上游额度、余额、账号池、分组、渠道、价格与内部基础设施信息；内容安全、上下文超限和普通请求校验原因应保留上游具体提示，返回下游及普通用户日志。502 应保留状态及可公开的上游说明。** 不能仅因为消息来自上游，就统一改成 `Invalid request` 或 `503 Service temporarily unavailable`。

保留的是错误 `message` 中可公开的请求说明，不是把整个上游错误对象、响应头或原始 HTTP body 原封不动转发。HTTP 状态和协议 envelope 仍按下表处理。

## 信任边界

```text
下游客户端
  -> 本站 middleware（本站认证、限流、并发控制）
  -> controller.Relay / RelayTask
  -> 渠道选择与重试
  -> provider adaptor
  -> 渠道 BaseURL
  -> provider 或下一层 API gateway
```

渠道返回的对象必须先解析，以下信息仍按私有运维信息处理：

- 未解析的 HTTP body、HTML、错误对象附带的 metadata；
- 上游账号额度、余额、计费价格、内部账号与凭据；
- 上游自定义 `type`、`code`、`param` 不能直接作为本站错误 envelope；
- request id、账号池状态、并发数、分组、渠道、节点、URL、主机名和代理拓扑；
- 上游响应头中的 request id、版本、Server、Via、缓存节点和 Cookie。

完整诊断原文保留在服务器日志或 `other.admin_info.raw_upstream_error`；可公开的请求错误说明同时进入响应和普通用户日志。普通用户查询日志时必须移除整个 `admin_info`，并清空 `channel`、`channel_name` 和 `upstream_request_id`。

## 层级盘点

本站一次 relay 请求在代码中经过 7 层：

1. middleware：生成本站 request id，并执行认证、限流和本站并发控制；
2. controller：解析请求、预扣费并持有最终 HTTP/WebSocket writer；
3. channel selector：按分组、优先级和权重选择渠道，并排除本次已尝试渠道；
4. relay helper：按 Chat、Responses、Claude、Gemini、Audio、Image 或 Task 组织流程；
5. provider adaptor：转换请求并识别供应商协议中的错误对象；
6. transport：执行 HTTP、SSE 或 WebSocket 调用；
7. 外部渠道：真实供应商或另一层 API gateway。

错误返回时再经过 5 个处理阶段：adaptor 提取原始错误、controller 使用原始错误判断重试和禁用、`PublicUpstreamError` 重建公开错误、协议 writer 输出本站 envelope 与已处理的 message、日志查询层按管理员或普通用户过滤字段。公开响应只经过一次重建，不递归包裹上游 error。

本次生产调用的实际网络拓扑至少有两层 gateway：VPS22 上的本站实例，以及渠道 `#84` 的 `ai.ttapy.com`。响应中的两个 request id 分别来自内层 gateway 和本站追加，不代表执行了两次本地重试。`ai.ttapy.com` 后面是否还有其它内部代理，本站日志无法证明，不能仅凭错误文案继续推断。

## 公开错误映射

公开响应必须重建新的错误对象，不能只替换原对象的 `message`，因为原对象的 `type`、`code` 和 `metadata` 同样由上游控制。

| 上游或渠道映射后的状态 | 下游 HTTP 状态 | 下游 `message` | 下游 `type` / `code` |
| --- | ---: | --- | --- |
| 400–599，包含明确的上下文超限或内容安全拒绝原因，且不含私有运维信息 | 保留原状态 | 保留具体拒绝说明，包括 token 上限、安全原因及修改建议 | 4xx：`invalid_request`；5xx：`service_unavailable` |
| 400、422 | 400 | 请求错误保留上游 message（安全原因码、token 上限、字段与格式说明）；夹带运维信息时返回固定不可用提示；空消息才回退 `Invalid request` | `invalid_request` |
| 404、405、409、410、413、415 | 400 | 按状态返回本站固定的资源、方法、冲突、过期、大小或格式提示 | `invalid_request` |
| 429 | 429 | `Rate limit exceeded. Retry later.` | `rate_limit_exceeded` |
| 502 | 502 | 保留可公开的上游 message；空消息或夹带运维信息时返回固定不可用提示 | `bad_response_status_code` |
| 其余 401、403、408、500–599、无效状态及其它状态 | 503 | `Service temporarily unavailable` | `service_unavailable` |

表中规则按顺序匹配。明确拒绝原因优先于通用状态分类；仅提及 safety 服务不可用不属于内容安全拒绝。

Midjourney 的 `23`、`30` 仅映射为 HTTP 429 和固定限流文案，其它非成功 code 映射为 HTTP 503 和固定不可用文案；不返回原 `description`、`result` 或 `properties`。

最终错误 message 只追加一次本站 request id。上游附带的 `(request id: ...)`、`(request-id: ...)` 及上游响应头 request id 不得进入公开响应。

### 请求错误透传边界（2026-10-22 修正）

`service/public_request_error.go` 对 400/422 不再统一重写成固定分类文案。
处理顺序是：取错误 message、移除嵌套上游 request ID、检查是否夹带私有运维信息，
再保留可公开的原始说明。余额、价格、账号池、分组、渠道、节点或凭据相关内容
优先过滤，即使同一条消息也包含 safety/context 关键字，仍不能借此绕过边界。
既有 URL/IP/域名及渠道配置的脱敏规则继续生效。默认全局规则不再把
“该会话已被网络安全策略屏蔽，请开启新会话”替换为笼统的 `Request rejected`，
也不再把 `Upstream` 一词或 `Upstream temporarily unavailable` 改写成 `service`。
若生产数据库保存了自定义规则，发布时需核对是否仍有这些旧规则；修改源码默认值
不会自动重写已有数据库配置。

| 上游 message | 下游 message 示例 |
| --- | --- |
| `request blocked by Gemini API: PROHIBITED_CONTENT` | 保留该说明及 `PROHIBITED_CONTENT` |
| `The input token count exceeds the maximum number of tokens allowed 131072.` | 原样保留，包括 `131072` |
| `The input token count exceeds the maximum number of tokens allowed (1048576).` | 原样保留，包括 `1048576` |
| `Upstream service temporarily unavailable`（HTTP 502） | 保留原文和 HTTP 502，不再改成 503 |
| 函数名、工具消息顺序、未知字段、图片格式等校验错误 | 保留上游具体说明，不统一改写为 `Invalid request` |
| `Request contains an invalid argument.` | 保留；不编造上游没有提供的详细原因 |
| 无可用账号、分组路由、价格、余额、凭据等内容 | 固定不可用提示，不公开内部细节 |

明确的安全拒绝和上下文超限保留 403、422 或被供应商包装成 5xx 的状态及说明；
不含此类明确原因的认证、限流、资源错误继续使用表中的通用策略。
本次不改变重试、自动禁用、收费或渠道配置的状态码映射。

`PublicUpstreamError` 只取协议 message；OpenRouter 等提供商附加到诊断 message
末尾的 metadata 必须剥离，不随原文保留进入下游。管理员仍保留完整诊断。
公开 envelope 的 type/code/param/metadata 继续由本站构造。
最终响应只追加一次本站 request ID；JSON、SSE 和普通错误日志使用相同说明。
若 SSE 已发送 HTTP 200 和部分数据，HTTP 状态已提交，无法再改为 4xx/5xx；
此时通过流内错误事件返回说明，不重新启动另一个上游响应流。

## 各错误出口

| 出口 | 处理要求 |
| --- | --- |
| OpenAI JSON / Alpha Search | 非 2xx 或 HTTP 200 错误 envelope 先返回 controller，再通过 `PublicUpstreamError` 处理消息并重建 envelope |
| Audio TTS / STT | HTTP 200 JSON 中的 `error` 必须在写音频或转写 body 前识别并返回 controller |
| Claude JSON / SSE | 使用同一公开错误对象转换为 Claude envelope；不得复用上游 Claude error |
| Gemini relay | 原生、兼容和流式响应中的 blocked/empty/error 必须先返回 controller；不得转发 `promptFeedback` 错误内容 |
| WebSocket realtime | 只发送本站生成的 event id 和按本边界处理的公开错误对象 |
| Task submit / fetch | 非 `LocalError` 必须通过 `PublicUpstreamTaskError` 重建 |
| 异步任务结果 | 失败任务的 `fail_reason`、错误 `data`、`properties` 和旧版结果 URL 必须替换或清空；OpenAI Video 失败对象由本站直接构造，不经过供应商 converter |
| Midjourney | 非成功 code 和图片代理非 2xx body 不得直接写出；统一输出固定错误 |
| Chat / Responses / image SSE | 检测错误事件后停止转发原事件，把原文返回 controller 供重试与管理员诊断；已开始的 OpenAI 流只追加本站固定 `data: {"error":...}` 帧 |
| 用户错误日志 | 每个最终失败请求最多一条；内容使用与响应相同的公开错误说明 |
| 管理员日志 | 可在 `admin_info` 中保存有限长度的原始错误、渠道及上游 request id |
| 渠道测试接口 | 属于管理员诊断接口，可以显示原始渠道错误；它不是 relay 下游接口 |

## 重试与日志顺序

```text
收到渠道错误
  -> 保存原始错误
  -> 使用原始状态/message/code 判断重试和自动禁用
  -> 记录服务器诊断
  -> 如可重试，切换到下一个可用渠道
  -> 如最终失败，处理公开 message、重建 envelope 并写一条用户错误日志
  -> 如后续渠道成功，不写前一次失败的用户错误日志
```

脱敏不能改变重试判断。账号池耗尽或并发已满可以切换到其它渠道；渠道选择器必须避免在没有其它候选时反复选择同一渠道。任何重试都不能在已经向客户端写出响应字节后开始，否则会拼接两个独立响应流。

普通 relay 在至少调用过一个上游后，如果重试找不到剩余渠道，必须保留最后一次实际上游错误的公开状态和说明，不能用“无可用渠道”的 503 覆盖它。此规则同时适用于普通路由和独立号池 fallback；没有实际调用过上游时，才返回渠道选择错误。

## 响应头策略

渠道响应头采用 allowlist。relay 只允许复制响应语义必需的头：

- `Content-Type`
- `Content-Length`（仅媒体代理成功响应）
- `Content-Disposition`
- `Accept-Ranges`
- `Content-Range`
- `X-Reasoning-Included`
- `X-Codex-Turn-State`

所有 request-id 变体只捕获到管理员诊断上下文，不写回客户端。其它上游头默认丢弃。
如果成功响应在写 body 前转为错误响应，前面暂存的内容类型、文件名、range 和 Codex 状态头也必须清除，再写本站 JSON 错误头。

## 开发检查清单

新增或修改 relay 路径时必须确认：

1. 非 2xx、HTTP 200 错误对象和 SSE 错误事件都返回 controller，而不是直接写出原 body。
2. 重试和自动禁用读取原始错误，公开响应读取重建后的错误。
3. OpenAI、Claude、Gemini、Task 和 WebSocket envelope 不透传上游 type/code/metadata；message 按公开请求说明与私有运维信息区分处理。
4. 失败后重试成功时，普通用户日志中没有中间失败记录。
5. 最终失败时，普通用户日志保留可公开的请求说明；完整诊断及私有运维信息只位于 `admin_info`。
6. 上游 request id 和基础设施响应头没有覆盖本站响应头。

## 本次生产问题判定

`服务并发已达上限（49）` 来自渠道 `#84` 指向的嵌套 new-api，而不是 VPS22 的系统并发值。本站随后从渠道 `#84` 重试到 `#89` 并成功，因此 `49` 也不是本站重试次数。旧实现把第一次失败立即写成用户可见错误日志，并保留嵌套 gateway 的两个 request id，造成“下游失败”的假象。

VPS22 当时没有 OOM、容器重启或高系统负载证据。另有 Caddy 只代理 `127.0.0.1:3002` 的单 upstream 配置风险，它可能独立产生 `no upstreams available`，但与上述 `49` 文案来源不同。
