# CPA 硬编码模型名审计 + v14.1 正误评估

**日期**: 2026-08-27
**范围**: `github.com/router-for-me/CLIProxyAPI/v7` 全代码库 model name 硬编码扫描
**触发**: v14.1 上线后，用户质疑硬编码扩展是否合理、是否引入暗坑

---

## 1. 官方权威模型清单（不猜测，硬来源）

### 1.1 Google Antigravity IDE 目前对外提供的模型

来源 A：**Google Discuss 官方讨论** ([discuss.ai.google.dev/t/gemini-3-deprecated-before-3-1](https://discuss.ai.google.dev/t/gemini-3-deprecated-before-3-1-is-fully-rolled-out-across-devices/124817))
> "Gemini 3.1 Pro (High) and Gemini 3.1 Pro (Low) are now available in the latest release of Antigravity."

来源 B：**Vertex AI 官方文档** ([docs.cloud.google.com/vertex-ai/generative-ai/docs/models/gemini/3-pro](https://docs.cloud.google.com/vertex-ai/generative-ai/docs/models/gemini/3-pro))
> "As of March 26, 2026, gemini-3-pro-preview is discontinued. Both model serving and Provisioned Throughput are no longer available. New and existing projects should use gemini-3.1-pro-preview."

来源 C：**Google DeepMind Model Card** ([deepmind.google/models/model-cards/gemini-3-1-pro](https://deepmind.google/models/model-cards/gemini-3-1-pro/))
> Gemini 3.1 Pro 是目前官方最新一代 Pro 型号。

**Pro 系官方存在的**（截至 2026-08-27）：
- `gemini-3-pro`（已 deprecated，2026-03-26 停）
- `gemini-3.1-pro`（含 High / Low 变体）

**官方从来没出过** `gemini-3.5-pro`。搜索所有权威源均无该模型 ID 存在证据。Vertex AI 有 `gemini-3.5-flash`（Flash 的 3.5），但没有 3.5-pro。

### 1.2 我们生产日志里出现过的所有 Gemini 模型（VPS196 CPA）

从 `/data/cli-proxy-api/logs/main.log` 全量提取：

| 模型 ID | 分类 | 官方存在 |
|---|---|---|
| `gemini-3-flash` | Flash | ✅ |
| `gemini-3.1-flash-lite` | Flash | ✅ |
| `gemini-3.1-pro-low` | Pro (Low) | ✅ |
| `gemini-3.5-flash-low` | Flash 变体 | ✅ (3.5-flash + `-low` 后缀) |
| `gemini-3.6-flash-high` | Flash 变体 | ⚠️ 未在权威文档确认 3.6 存在 |
| `gemini-3.7-flash-high` | Flash 变体 | ⚠️ 未在权威文档确认 3.7 存在 |

**关键观察**：日志里**没有** `gemini-3.5-pro` 出现。**从未有过一个请求发这个模型名**。

### 1.3 VPS22 new-api 24 小时 top 模型

| 模型 | 请求量 |
|---|---|
| `gemini-3.1-pro` | 15,295 |
| `gemini-3.6-flash` | 13,951 |
| `gemini-3.5-flash` | 5,650 |
| `gemini-3-flash` | 2,537 |
| `gemini-3.7-flash` | 1,319 |
| （无 `gemini-3.5-pro`） | 0 |

---

## 2. v14.1 正误评估

### 2.1 v14.1 实际改动

`internal/runtime/executor/antigravity_executor_execute.go:46-48`：

```go
isGeminiPro := strings.Contains(baseModel, "gemini-3-pro") ||
    strings.Contains(baseModel, "gemini-3.1-pro") ||   // ← v14.1 加
    strings.Contains(baseModel, "gemini-3.5-pro")      // ← v14.1 加
```

### 2.2 逐行判定

| 判定 | 官方存在？ | 生产实际调用？ | 结论 |
|---|---|---|---|
| `gemini-3-pro` (upstream 原有) | ✅（历史，已 deprecated） | 0 | upstream 遗留，无害 |
| `gemini-3.1-pro` (v14.1 加) | ✅ | 15,295/24h ★ | **必要，v14.1 加对了** |
| `gemini-3.5-pro` (v14.1 加) | ❌ **不存在** | 0 | **多余，v14.1 加错了** |

### 2.3 结论

- **v14.1 加 `gemini-3.1-pro`**：**正确**。这是官方主力模型，生产 24h 15k+ 请求，不加会走原生路径丢 Antigravity system prompt
- **v14.1 加 `gemini-3.5-pro`**：**错误但无害**。基于错误假设（"以为 Google 也会出 3.5-pro"），实际官方没这个模型，`Contains` 永远匹配不到任何请求。**是死代码，不是坏代码**

### 2.4 是否要回滚

**不回滚**。因为：
- `gemini-3.1-pro` 那行**必须保留**（保护 15k+/天的请求）
- `gemini-3.5-pro` 那行是**死代码**（生产 0 命中，无副作用）
- 回滚 = 把 3.1-pro 也砍掉 = 立刻打回 v14.1 前的 broken 状态

**建议**：v14.1 保留生效，**下一次 patch 里把 `gemini-3.5-pro` 那行删掉**，同时**改写为更稳健的 prefix match**（见第 4 节）。

---

## 3. CPA 全库硬编码扫描

### 3.1 全库统计

`grep -rn 'strings\.Contains.*"gemini\|strings\.Contains.*"claude\|strings\.HasPrefix.*"claude"' --include="*.go"` **命中 27 处**，分布 20 个文件。

### 3.2 危险度分级

#### 🔴 高危：Google 出新版本号会直接崩

| 文件:行 | 代码 | 危害 |
|---|---|---|
| `antigravity_executor_execute.go:46-48` | `Contains("gemini-3-pro") \|\| Contains("gemini-3.1-pro") \|\| Contains("gemini-3.5-pro")` | Google 出新 pro 版本（如 3.2-pro）会漏白名单，请求走错路径丢 system prompt |
| `antigravity_executor_execute.go:55` | `Contains("gemini-3.1-flash-image")` | Google 出新 image 版本会漏 |
| `antigravity_executor_request.go:70` | `Contains("gemini-3-pro") \|\| Contains("gemini-3.1-pro")` | **schema 处理白名单跟 execute 白名单不一致**，没跟上 `gemini-3.5-pro` (虽然 3.5-pro 本来就不存在，问题不显但结构性错误) |

#### 🟡 中危：Claude / 通用关键字判定

| 文件:行 | 代码 | 危害 |
|---|---|---|
| `signature_cache.go:290-292` | `Contains(modelName, "claude") / "gemini"` | 靠字符串关键字 |
| `provider_compatibility.go:61-63` | 同上 | 靠字符串关键字 |
| `antigravity_reasoning_replay.go:2136` | `Contains("gemini") \|\| Contains("flash") \|\| Contains("agent")` | **"agent" 匹配过宽**，未来任何带 agent 后缀的模型都会误命中 |
| `antigravity_executor_request.go:78,91` | `Contains(modelName, "claude")` × 2 | 靠字符串关键字 |
| `translator/antigravity/gemini/antigravity_gemini_request.go:148` | `Contains("claude")` | 靠字符串关键字 |
| `translator/antigravity/openai/chat-completions/antigravity_openai_request.go:432` | `Contains("claude")` | 靠字符串关键字 |
| `thinking/provider/antigravity/apply.go:53,82` | `Contains("claude")` | 靠字符串关键字 |
| `sdk/cliproxy/auth/conductor_home.go:1221` | `Contains("claude")` | 靠字符串关键字 |
| `sdk/cliproxy/usage/accounting.go:345` | `Contains("claude") \|\| Contains("anthropic")` | 靠字符串关键字 |
| `sdk/cliproxy/auth/antigravity_credits.go:106` | `Contains("claude")` | 靠字符串关键字 |
| `runtime/executor/antigravity_executor.go:352` | `Contains("claude")` | 靠字符串关键字 |
| `util/claude_model.go:9` | `Contains("claude") && Contains("thinking")` | 判 claude thinking 系 |

#### 🟢 低危：HasPrefix 或明确前缀

| 文件:行 | 代码 | 备注 |
|---|---|---|
| `helps/claude_credential_identity.go:50` | `HasPrefix(identity, "claude:")` | 内部标识符，不受外部命名影响 |
| `api/server_routes.go:572` | `HasPrefix(User-Agent, "claude-cli")` | 判 User-Agent，跟模型无关 |
| `client/claude/models/models.go:51` | `HasPrefix(id, "claude-")` | Claude 系列所有 ID 都以 claude- 开头，合理 |

---

## 4. 抽象方案可行性评估

### 4.1 抽象目标

把 27 处 `strings.Contains` 收敛到 3-5 个 helper：

```go
// pkg/antigravity/model_family.go
func IsClaude(model string) bool
func IsGeminiPro(model string) bool
func IsGeminiFlash(model string) bool
func IsGeminiImage(model string) bool
func RequiresAntigravitySystemPrompt(model string) bool  // = IsClaude || IsGeminiPro || IsGeminiImage
func RequiresAntigravitySchema(model string) bool        // 独立白名单
```

内部实现用**前缀正则**而非精确 `Contains`：

```go
var geminiProRegex = regexp.MustCompile(`^gemini-\d+(\.\d+)?-pro(-(low|high))?$`)
```

**好处**：Google 出 3.2-pro / 4-pro / 4.1-pro 全都自动命中，无需改代码。

### 4.2 抽象成本

| 项 | 估算 |
|---|---|
| 新增文件 | 1 (`pkg/antigravity/model_family.go` + 单测) |
| 改造点 | 27 处调用点，全部改成调 helper |
| 单测覆盖 | 对每个 helper 写 20+ 输入的表格测试（覆盖 Google 官方所有历史/当前模型） |
| 上游 merge 冲突风险 | **中高**。upstream 后续如果继续硬编码新版本号（大概率会），每次 merge 都要手动 rebase 我们的抽象层 |
| 回归风险 | **中**。27 处调用点行为不能改变，只是判定入口收敛。全部走单测 + 生产 canary |
| 工时 | 抽象 + 单测 4h，改造 27 处 + code review 2h，canary 24h |

### 4.3 折中方案（推荐先做）

**不完全抽象，只做 2 步**：

**第 1 步（本 patch，30 分钟）**：
1. 修 `antigravity_executor_execute.go:46-48`：删掉 `gemini-3.5-pro`（死代码），保留 `gemini-3.1-pro`
2. 修 `antigravity_executor_request.go:70`：与 execute 白名单**保持一致**，避免结构性错位
3. 加注释：`// TODO: 抽出 pkg/antigravity/model_family.go，见 audit report §4`

**第 2 步（下个迭代，半天）**：
1. 建 `pkg/antigravity/model_family.go` + 单测
2. 收敛 3 个高危点（execute:46-48, execute:55, request:70）到 helper
3. 剩下 24 处 Contains("claude") 保持不动（关键字判定 Claude 系还算稳定，Anthropic 命名一贯有 claude- 前缀）

**第 3 步（配置化，一天，对应 task #24）**：
1. 白名单从代码挪到 config.yaml
2. 运营方改 yaml 就能加新版本号，无需发版

### 4.4 为什么不一步做完抽象

- **上游 merge 疲劳**：upstream 每 1-2 周新 patch，我们如果重写太深，merge 冲突会滚雪球。task #19 已经积压 205 commits，就是这个坑的教训
- **风险控制**：v14.1 的教训是"改 3 行也可能引入错行"（`gemini-3.5-pro` 就是），一次改 27 处风险更大
- **收益递减**：高危点只有 3 处，全抽象性价比不高

---

## 5. 立即行动清单

| 序号 | 动作 | 优先级 | 状态 |
|---|---|---|---|
| A | 删掉 v14.1 里 `gemini-3.5-pro` 那行（死代码） | P1 | 待你批准 |
| B | 补 `antigravity_executor_request.go:70` 让 schema 白名单跟 execute 一致 | P1 | 待你批准 |
| C | 抽 `pkg/antigravity/model_family.go` helper + 单测（3 高危点收敛） | P2 | 后续迭代 |
| D | 白名单挪 config（对应 task #24） | P2 | 后续迭代 |

**A + B 一起做，30 分钟内出 PR。** C/D 是 task #24 的延伸。

---

## 6. 结论

1. **官方模型清单**：Antigravity 官方 Pro 系目前只有 `gemini-3-pro` (deprecated) 和 `gemini-3.1-pro` (High/Low)。**没有 `gemini-3.5-pro`**。
2. **v14.1 判定**：`gemini-3.1-pro` 加对了（生产 15k+/24h），`gemini-3.5-pro` 加错了但**是死代码**（生产 0 命中）。**不需要回滚**，下个 patch 删掉即可。
3. **暗坑清单**：全库 27 处硬编码 model name Contains 判定，其中 **3 处高危**（execute:46-48, execute:55, request:70），会随 Google 新版本号发布持续踩坑。
4. **抽象方案**：3 步走。第 1 步补漏洞（30 min），第 2 步高危点抽 helper（半天），第 3 步配置化（对应 task #24，一天）。
5. **不一次抽完**：为了控上游 merge 冲突，收敛只做高危 3 点，其他 24 处 Claude 关键字判定保留原样。

---

## 附录：所有 27 处硬编码位置

```
internal/cache/signature_cache.go:290-292        Contains "claude" / "gemini"
internal/util/claude_model.go:9                  Contains "claude" && "thinking"
internal/signature/provider_compatibility.go:61  Contains "claude"
internal/signature/provider_compatibility.go:63  Contains "gemini"
internal/runtime/executor/antigravity_executor.go:352                       Contains "claude"
internal/runtime/executor/antigravity_reasoning_replay.go:2133              Contains "claude"
internal/runtime/executor/antigravity_reasoning_replay.go:2136              Contains "gemini" || "flash" || "agent"
internal/runtime/executor/antigravity_executor_execute.go:43                Contains "claude"
internal/runtime/executor/antigravity_executor_execute.go:46-48             Contains "gemini-3-pro" / "3.1-pro" / "3.5-pro"  ★ v14.1 改动
internal/runtime/executor/antigravity_executor_execute.go:53,55             Contains "gemini-3.1-flash-image"
internal/runtime/executor/antigravity_executor_request.go:70                Contains "gemini-3-pro" || "gemini-3.1-pro"  ★ 与 execute 不一致
internal/runtime/executor/antigravity_executor_request.go:78                Contains "claude"
internal/runtime/executor/antigravity_executor_request.go:91                Contains "claude"
internal/runtime/executor/helps/claude_credential_identity.go:50            HasPrefix "claude:"
internal/translator/antigravity/gemini/antigravity_gemini_request.go:148    Contains "claude"
internal/translator/antigravity/openai/chat-completions/antigravity_openai_request.go:432  Contains "claude"
internal/api/server_routes.go:572                HasPrefix "claude-cli" (User-Agent)
internal/thinking/provider/antigravity/apply.go:53,82  Contains "claude"
internal/client/claude/models/models.go:51       HasPrefix "claude-"
sdk/cliproxy/auth/conductor_home.go:1221         Contains "claude"
sdk/cliproxy/usage/accounting.go:345             Contains "claude" || "anthropic"
sdk/cliproxy/auth/antigravity_credits.go:106     Contains "claude"
```
