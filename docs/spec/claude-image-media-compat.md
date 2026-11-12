# Claude 图片 media_type 兼容层

## 背景

Claude（Anthropic Messages 协议）的图片块只接受四种 `source.media_type`：

```text
image/jpeg, image/png, image/gif, image/webp
```

不在这四个值里的图片块会被上游以 400 拒绝，错误形如：

```text
status_code=400 ... source.base64.media_type: Input should be 'image/jpeg',
'image/png', 'image/gif' or 'image/webp'
```

这个报错来自 `source.type == "base64"` 分支，也就是说：失败请求到达上游时已经是 base64 图片块，只是我们填的 media_type 上游不认。

## 根因

我们自己的转换链路会把图片（尤其是 URL 图片）下载后转成 base64，再按 `content-type` 头、`Content-Disposition`、URL 扩展名、字节嗅探的顺序猜 MIME：

- `service.smartDetectMimeType` / `service.GetFileTypeFromUrl` 返回的是「服务器说的类型」，不是 Claude 白名单里的规范值；
- 常见真实取值：`image/jpg`（大量 CDN 的非标准写法）、`image/avif`、`image/heic`、`image/bmp`、`image/tiff`、`image/svg+xml`、`application/octet-stream`；
- 这些值此前被原样写进 `ClaudeMessageSource.MediaType` 发给上游。

这是「客户端给 URL，我们下载后转 base64」场景的必然结果，与上游是否支持 URL 图片无关。

## 目标

在**发往 Claude 协议上游之前**统一做一次图片归一化，让请求不会因为 media_type 被 400：

1. 别名校准：`image/jpg` 等写法统一成 `image/jpeg` 等规范值，忽略大小写、`; charset=` 参数和 `data:` 前缀；
2. 类型/内容不一致修复：按字节魔数嗅探真实格式，声明类型与真实内容不一致时以真实格式为准（例如 URL 说 `image/png`、实际是 AVIF）；
3. 可解码的非常规格式转码：`image/bmp`、`image/tiff` 用 Go 解码后转成 `image/png`；
4. 无法转码的格式降级：`image/avif`、`image/heic`、`image/svg+xml`、`application/octet-stream`、空 data 等 Go 无法解码的格式，不把请求打挂，而是把该图片块替换成一段文本占位（`[image omitted: ...]`），保证对话继续可用，同时写一条 WARN 日志说明被替换的原因；
5. 只对 base64 内联图片生效，不改变 URL 图片源（`source.type == "url"` 原样透传，是否有 URL 下载能力由上游决定）；
6. 安全边界：BMP/TIFF 转码前先读图片头校验尺寸（上限 25,000,000 像素），避免一个几十字节的伪造头让解码器分配几十 GB 内存；客户端可控的 media_type 进日志前会去掉控制字符并截断；每个请求最多记录 5 条兼容层日志，防止刷屏。

## 设计

新增 relaykit 独立包 `relaykit/media`（纯函数，不依赖宿主 service，也不依赖 Gin：

- 输入已经是解析好的 base64 图片数据，因此无论来源是 URL 下载还是客户端内联 base64，都走同一套逻辑；
- relaykit 主模块和 new-api 主模块都能直接引用，避免两套实现。

对外 API：

```go
func CanonicalClaudeImageMediaType(mimeType string) (string, bool)
func SniffImageMediaType(raw []byte) string
func PrepareClaudeImageData(raw []byte, declaredMediaType string) (data []byte, mediaType string, dropped bool, note string)
func PrepareClaudeImageBase64(base64Data, declaredMediaType string) (data string, mediaType string, dropped bool, note string)
func NormalizeClaudeRequestImages(request *dto.ClaudeRequest) []string
```

`NormalizeClaudeRequestImages` 负责遍历 Claude 请求里的所有内联图片块，包括：

- `messages[i].content` 为 `[]ClaudeMediaMessage`（我们构造的请求）或 `[]any`（客户端原生 JSON）；
- `tool_result.content` 里嵌套的图片块；
- 纯字符串 / nil 内容保持不变。

`[]any` 分支直接原地改 map，只动 `source` 字段，避免结构体 round-trip 丢掉 `cache_control`、`citations` 等未建模字段。

## 接入点

所有会生成 Claude 协议请求的位置统一调用归一化：

| 层 | 文件 | 说明 |
| --- | --- | --- |
| 原生 Claude 透传 | `relay/claude_handler.go` | `adaptor.ConvertClaudeRequest` 之后，覆盖 claude/aws/newapi 等返回 `*dto.ClaudeRequest` 的渠道 |
| Vertex 原生 | `relay/channel/vertex/adaptor.go` | 返回自定义 `VertexAIClaudeRequest`，需在拷贝前归一化 |
| OpenAI → Claude（宿主实现） | `relay/channel/claude/relay-claude.go` | `RequestOpenAI2ClaudeMessage` 返回前，覆盖 claude/aws/vertex 的 OpenAI 请求转换 |
| OpenAI Chat → Claude（kit） | `relaykit/relayconvert/internal/oai_chat/to_claude_messages_req.go` | 转换函数返回前 |
| OpenAI Responses → Claude（kit） | `relaykit/relayconvert/internal/oai_responses/to_claude_messages_req.go` | 转换函数返回前 |

宿主的 3 个接入点（claude_handler / relay-claude / vertex）通过 `service.NormalizeClaudeRequestImages(c, request)` 统一负责打 WARN 日志；kit 内部两个转换器通过 `kitutil.LogInfo` 记录。

## 行为矩阵

| 声明 media_type | 真实内容 | 处理 |
| --- | --- | --- |
| `image/jpeg/png/gif/webp` | 一致 | 原样发送（不重新编码） |
| `image/jpg`、`image/pjpeg`、`image/x-png` 等别名 | 对应格式 | 只改 media_type 字段，payload 不动 |
| 合法值 | 嗅探为空 | 信任声明，原样发送（避免误伤特殊编码） |
| 空白/缺失 | 可识别 | 用嗅探结果补齐 |
| 与服务端声明不符 | 另一种合法图片 | 以真实格式为准重写 media_type，payload 不动 |
| `image/bmp`、`image/tiff` | 对应格式 | 解码后转 PNG，media_type 改 `image/png` |
| `image/avif`、`image/heic`、`image/svg+xml`、未知二进制 | 无法解码 | 图片块替换为文本占位，写 WARN，请求继续 |
| 空 data / 非法 base64 且声明类型不受支持 | - | 图片块替换为文本占位 |
| 非法 base64 但声明类型合法 | - | 原样透传（不额外花一次全量解码去校验上游本该报错的内容） |
| `binary/octet-stream`、`application/unknown` 等通用二进制 | 可识别 | 按嗅探结果修正 |
| 声明类型是 audio/video/pdf 等非图片类型 | - | 不动，保留上游原本的校验错误 |

## 非目标 / 已知限制

- 不改 URL 图片源（上游自己下载的图片不在这里处理）；AWS 渠道只对顶层消息里的 URL 图片做内联，`tool_result` 内嵌的 URL 图片不下载（既有缺口）；
- 不引入 AVIF/HEIC 解码依赖（cgo/libheif 不进入主仓构建链），这类图片只能降级成占位；
- 开启请求体透传（`PassThroughBodyEnabled` / 全局 pass-through）时请求体不经转换，兼容层不生效；
- 不修改非 Claude 协议渠道的图片处理（Gemini 等支持更多格式）；
- 不改变 token 计数与计费路径（仍按原文件大小/尺寸计算）；图片被替换成占位时，预估 token 仍按原图统计，最终结算以响应 usage 为准；
- Claude 的 `system` 字段本身只允许文本块，Responses→Claude 转换器会把 system 角色的图片也塞进 system（既有转换缺口）；兼容层会照常归一化，但这类请求仍会因协议非法被上游拒绝，需要后续单独修转换器；
- 不处理 JSON `RawMessage` / `[]byte` 形态的 content，调用方需先转成 `[]dto.ClaudeMediaMessage` 或 `[]any`（包文档已注明）。

## 验收

1. `cd relaykit && GOWORK=off go build ./...` 通过；
2. 根模块 `go build ./...` 通过；
3. `relaykit/media` 单测覆盖别名、嗅探、bmp/tiff 转码、AVIF 降级、嵌套 tool_result、未知字段保留；
4. 回归测试：`RequestOpenAI2ClaudeMessage` 收到 `data:image/jpg;base64,...` 时产出的 media_type 为 `image/jpeg`；
5. 不涉及部署：本地验证通过后由人工确认再走发布链路。
