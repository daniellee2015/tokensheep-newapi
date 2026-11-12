# URL 图片兼容：Gemini fileUri 内联与 Claude URL 图转换

## 背景

图片以 URL 形式进入网关时，不同上游对「自己去下载 URL」的支持程度不一样：

1. **Gemini（Google）**：原生 Gemini 请求里的 `fileData.fileUri` 是给 Google 托管文件用的（File API、GCS、YouTube）。普通 `http(s)` 图片 URL 交给 Google 时经常无法识别/抓取，表现为图片丢失或上游报错；
2. **Claude 协议转换**：Claude 原生图片块的 `source.type = "url"` 在 Claude → OpenAI(Chat) 转换时被错误地当成 base64 处理，生成了 `data:;base64,%!s(<nil>)` 这种垃圾 data URI（已复现），再转去 Gemini/Claude 时图片彻底损坏；
3. Claude 协议上游（Anthropic 官方）本身支持 URL 图片源，不需要内联；AWS Bedrock 早已有 URL→base64 的内联逻辑，Vertex 没有（本次不动 Vertex，单独评估）。

## 目标

1. 原生 Gemini 请求中，指向普通公网图片/PDF 的 `fileData.fileUri` 在发往上游前下载并转成 `inlineData`（base64），让 Google 一定能读到字节；
2. Claude → OpenAI(Chat) 转换保留 URL 图片源（`image_url.url` 直接放 URL），交给下一跳转换器决定下载/内联，不再伪造空 data URI；
3. 下载失败、URL 属于 Google 托管引用/YouTube、文件过大或 MIME 不在 Gemini 内联支持范围时，保留原 `fileData` 原样透传（退化为原有行为，不把请求打挂）。

## 设计

### Gemini fileUri 内联

位置：`relay/channel/gemini/url_file_inline.go`，在 `adaptor.ConvertGeminiRequest` 开头调用。

只处理同时满足以下条件的 part：

- `fileData.fileUri` 是 `http://` / `https://`；
- 不是 Google 托管地址（`generativelanguage.googleapis.com`、`aiplatform.googleapis.com`、`gs://`）；
- 不是 YouTube 地址（视频 URL Gemini 自己支持）；
- 声明的 `mimeType` 不是 `video/*` / `audio/*`（体积大，保持透传）。

下载复用 `service.GetBase64Data`（带 SSRF 防护、大小上限、磁盘缓存），成功后仅在以下 MIME 时内联：

- `image/png`、`image/jpeg`、`image/jpg`、`image/webp`、`image/heic`、`image/heif`
- `application/pdf`

并且受两级预算约束：

- 单张 base64 ≤ 1MB（约 750KB 原始字节；Gemini 反正会降采样，内联更大的图只会白烧内存和额度）；
- 整个请求内联 base64 总量 ≤ 2MB，请求里已有的 `inlineData` 会先占用预算。

两个常量集中在 `defaultGeminiInlineLimits`，需要放宽时改这一处即可。

任一预算不足时的兜底策略：

- 能解码的图片格式（JPEG/PNG/GIF/WebP/BMP/TIFF）**自动缩小并重编码为 JPEG**：长边依次尝试 1536/1024/768/512/384，质量依次 85/75/65，直到 base64 装进预算；透明通道压白底；
- 无法解码的图片格式（HEIC/AVIF）和 PDF 保持 `fileData` 原样透传，并记录 debug 日志。

这样超大图片不会直接丢图，也不会让多图请求叠加成几十 MB 的内存和请求体。

### Claude URL 图片转换修复

位置：`relaykit/relayconvert/internal/claude_messages/to_oai_chat_req.go`。

- `source.type == "url"`（或 `url` 非空）→ 直接产出 `image_url.url = source.url`；
- base64 源保留 `data:<media_type>;base64,<data>`，`data` 为空时跳过该块而不是生成 `data:;base64,` 垃圾；
- `source == nil` 防御性跳过（原实现会空指针 panic）。

## 非目标

- 不内联视频/音频 URL（Gemini 对公网视频 URL 有原生支持，且内联会显著放大请求体）；
- 不修改 Vertex / AWS 的 Claude URL 图片路径（AWS 已有内联，Vertex 另行评估）；
- 不处理 PassThrough 模式（不转换就不处理）。

## 验收

1. `cd relaykit && GOWORK=off go build ./... && go test ./...` 通过；
2. 根模块 `go build ./...`、`go test ./...` 通过；
3. 单测：
   - Gemini `fileData.fileUri` 指向本地图片 → 变成 `inlineData`（mime/base64 正确）；
   - Google File API URI → 保持 `fileData`；
   - 下载失败 → 保持 `fileData`；
   - Claude URL 图片 → OpenAI `image_url.url` 原样保留；
   - Claude base64 空数据 → 该块被跳过，不生成垃圾 data URI。
