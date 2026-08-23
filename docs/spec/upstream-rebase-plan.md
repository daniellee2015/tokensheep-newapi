# Upstream Rebase 计划

**目标**：将 tokensheep fork 与 `QuantumNous/new-api` upstream 合并至同一水平，同时保留 tokensheep 定制。

**产出日期**：2026-08-24
**Upstream 快照**：`upstream/main` @ `2d8e50bf3` (`v1.0.0-rc.25` 之后)
**分歧起点**：`1ae757475` (2026-07-04)

---

## 1. 基本情况

| 指标 | 数值 |
|-----|-----|
| Upstream 领先提交数 | **196** |
| tokensheep 独立提交数 | 64 |
| Upstream 最新 release tag | `v1.0.0-rc.25` |
| 共同祖先 commit | `1ae757475f9e8dad4ffedf89b3e707756fe8ecf9` |

## 2. Upstream 关键变更类别

### 2.1 重大架构变更

**`86ac0f774` — refactor: extract protocol conversion layer into standalone relaykit module (#6369)**

- **规模**：368 files, +7144 / -1594
- **本质**：将 `relay/` 下的协议转换层抽出为独立的 `relaykit` 模块，解除对 `gin.Context`、`RelayInfo`、全局 `settings` 的直接依赖，改用 `convmeta.Meta` 和 `convmeta.Options` 传递
- **影响面**：本地正在修改的 10 个文件全部经过 relaykit 迁移
- **移植策略**：所有 tokensheep 定制的 relay/converter 逻辑需要按新的 API surface（`context.Context`, `convmeta.Meta`, `convmeta.Options`）重写

### 2.2 Claude 相关（与 tokensheep 主要 WIP 重合）

| Commit | 主题 | 与本地 WIP 关系 |
|---|---|---|
| `4442bb302` | stop injecting empty tools into Claude requests | 与本地 `sanitize cursor claude native tool prompts` / `fallback cursor claude native tools to json` 主题重合，需交叉验证 |
| `3dda1d50c` | preserve parameterless tools in Claude conversion | 与本地 tool 转换逻辑冲突，重点审 |
| `8ad159a3b` | ollama: preserve reasoning and tool-call context | Ollama 通道，与本地 Claude 修复不冲突但主题相关 |

### 2.3 稳定性 / 流处理

| Commit | 主题 |
|---|---|
| `d6b5ce99d` | HTTP/2 `Request.GetBody` 修复，允许流重置后重试（影响所有走 DoApiRequest 的通道） |
| `153d7f01a` | 避免客户端断开后写入过期流 |
| `bd585d78e` | Bedrock 请求在客户端断开时取消 |
| `ea4f02101` | replay metadata 移到 request body |

### 2.4 计费 / 配额（生产敏感）

| Commit | 主题 |
|---|---|
| `f11641428` | Responses cached token usage 结算修复 |
| `cfaba1dd6` / `df43f8015` | tiered retry 计费加固 |
| `58d4e9bd3` | 异步任务退款同步减少 `used_quota` |
| `ccd535ef8` | 并发配额与状态更新加固 |
| `50e5377ea` | 充值订单原子结算 |
| `92d3c9d18` | uncached remainder 边界修复 + prompt_cache_key 透传 |
| `48068ce92` | 按 cache-creation 价格计费 cache_write_tokens |

### 2.5 安全 / 认证

| Commit | 主题 |
|---|---|
| `31d70fca3` | 仪表盘会话改为无状态令牌 + 会话控制（新增 3 张表） |
| `df087b022` | HTTP 客户端 SSRF 保护 |
| `56dbaab1d` | Secure session cookies 可选支持 |
| `5fc35e28a` | 用户账号邮箱/密码处理加固 |
| `d7992672a` | OAuth 绑定不覆盖用户 state |
| `4a64b8707` | 自服务密码更新守卫 |
| `0cd9dc85e` | Merge commit from fork（安全修复） |

### 2.6 新功能（选择性合并）

| Commit | 主题 |
|---|---|
| `4add708eb` | channel test 增强 |
| `e99a9bd86` | 分通道 HTTP 传输控制 |
| `2d23cdf29` | 可配置工具计费 + Sub2API 通道 + alpha 搜索计费 |
| `398cdafec` | 新增 New API 通道 |
| `0f9f668c6` | zstd 请求解压支持 |
| `85feb7a34` | 参数覆盖暴露 user/group 上下文 |
| `4823417cf` | playground 参数设置面板 |
| `0ab020206` | auto group 功能 |
| `cb4c8c02f` | OIDC 自定义登录显示名称 |

## 3. Schema 变更分析（AB 兼容性）

### 3.1 新增表（3 个，AB 完全兼容）

| 表名 | 用途 | 迁移入口 |
|---|---|---|
| `user_sessions` | 无状态 dashboard 会话 | `AutoMigrate(&UserSession{})` |
| `auth_flows` | 一次性认证流程 state | `AutoMigrate(&AuthFlow{})` |
| `external_identity_claims` | 外部身份提供商声明 | `AutoMigrate(&ExternalIdentityClaim{})` |

A 版本不感知这些表，B 版本读写自持，**AB 兼容 ✅**

### 3.2 现有表新增列（3 处，AB 兼容）

| 表 | 新增列 | 类型 |
|---|---|---|
| `users` | `auth_version` | `bigint NOT NULL DEFAULT 1` |
| `tokens` | `auto_groups` | `text` |
| `midjourneys` | `token_id`, `billing_channel_id` | `int DEFAULT 0` |

A 版本 INSERT 时列由 DB default 填充，UPDATE 不涉及这些列——**AB 兼容 ✅**

### 3.3 必须保留的 tokensheep 定制列

**`model/user.go` — User 结构体**
```go
QuotaGift         int    `gorm:"type:int;default:0;column:quota_gift"`
TotalDonated      int    `gorm:"type:int;default:0;column:total_donated"`
LastRequestAt     int64  `gorm:"default:0;column:last_request_at"`
GiftQuotaUsed     int    `gorm:"type:int;default:0;column:gift_quota_used"`
GiftQuotaUsedDate string `gorm:"type:varchar(10);default:'';column:gift_quota_used_date"`
```

用途：
- `QuotaGift` / `GiftQuotaUsed*` — 打卡礼包池（`checkin.go`）
- `TotalDonated` — 累计打款额，驱动 tier 计算（`tokensheep_maintenance.go`, `topup.go`）
- `LastRequestAt` — 最近 API 调用，用于 tier 降级判断（`tokensheep_maintenance.go`）

**`model/token.go` — Token 结构体**
```go
OwnerSystem        string  `gorm:"type:varchar(64);default:'';index"`
QuotaUnit          *string `gorm:"type:varchar(64)"`
ConversionRevision *string `gorm:"type:varchar(191)"`
IdempotencyKey     *string `gorm:"type:varchar(191);uniqueIndex:idx_tokens_user_idempotency,priority:2"`
```

用途：Kiro.bus scoped token 生命周期 API（`feat(kirobus): add scoped token lifecycle API`）。

**独有的 model 文件**：`model/tokensheep_maintenance.go` — 保留全文。

### 3.4 未发现的破坏性变更

- ❌ 未发现 upstream 删除任何 tokensheep 独有列
- ❌ 未发现 upstream 修改 tokensheep 独有字段的类型
- ❌ 未发现 upstream 重命名任何共有字段
- ✅ GORM AutoMigrate 只做 ADD，不主动 DROP，天然保护 tokensheep 独有列

## 4. 本地正在修改的文件对照

以下文件在本地有 WIP，且被 upstream 变更覆盖，rebase 时需重点审核：

| 本地文件 | Upstream 相关提交数 | 冲突风险 |
|---|---|---|
| `controller/relay.go` | 5 | 高（relaykit + 计费加固） |
| `middleware/distributor.go` | 4 | 高（relaykit + auto-group + compact suffix） |
| `relay/channel/api_request.go` | 7 | 高（relaykit + HTTP/2 + SSRF + 流断开） |
| `relay/channel/claude/relay-claude.go` | 3 | 极高（relaykit + tool billing） |
| `router/relay-router.go` | 3 | 中（relaykit + Gemini v1/models） |
| `service/error.go` | 2 | 中（relaykit） |
| `types/error.go` | 1 | 中（relaykit） |
| `middleware/utils.go` | 1 | 中（relaykit） |
| `common/utils.go` | 0 | 低 |
| `middleware/request-id.go` | 0 | 低 |

本地新增的文件（upstream 无对应，直接保留）：
- `common/request_id_test.go`
- `controller/claude_count_tokens.go`
- `middleware/anthropic_error_test.go`
- `middleware/request_id_test.go`
- `relay/channel/claude/native_passthrough_test.go`

## 5. 合并策略

采用 **方案 C：完全 rebase 到 upstream/main**，理由：

1. Upstream 领先 196 个提交，继续 cherry-pick 只会让分歧越来越大
2. `relaykit` 重构是根本性架构变更，避不掉，晚合并只会更痛
3. tokensheep 有 AB 镜像兜底，可以承受"新分支跑一段时间才切流"的验证周期

### 5.1 分支模型

- 基线分支：`main`
- 工作分支：`tokensheep/rebase-upstream`
- 合并方式：`git merge upstream/main`（保留 tokensheep 64 个提交的独立历史，不 rebase）

### 5.2 执行步骤

**第 1 步：收尾本地 WIP**（未提交的 11 modified + 5 untracked）
- 决定：commit / stash / drop
- 目标：进 rebase 分支时工作区干净

**第 2 步：创建 rebase 分支**
```bash
git checkout -b tokensheep/rebase-upstream main
```

**第 3 步：合并 upstream**
```bash
git merge upstream/main
```
预期产生大量冲突，按 §4 表格顺序解决。

**第 4 步：冲突解决优先级**

1. `model/user.go`, `model/token.go` — 按 §3.3 清单保留 tokensheep 定制字段
2. `model/main.go` — AutoMigrate 列表合并（tokensheep 独有 + upstream 新表）
3. `relay/channel/claude/*` — 结合 upstream `4442bb302`, `3dda1d50c` 审 tokensheep WIP，去除重复修复
4. `relay/channel/api_request.go` — 采用 upstream 的 relaykit 接口
5. `controller/relay.go`, `middleware/distributor.go` — 迁移到新的 tiered retry / auto group / compact suffix 语义
6. 其余 relaykit 导入路径调整

**第 5 步：验证**

```bash
go build ./...
go test ./...
```

跑一遍 SQLite 空库 AutoMigrate 验证新表结构：
```bash
rm -f /tmp/tokensheep-rebase-test.db
SQL_DSN=local go run ./ --port 0 --exit-after-migrate  # 或等价命令
```

**第 6 步：部署 B 镜像灰度**

- B 镜像跑 3-7 天
- 关注：会话（新的 UserSession 表）、计费（tiered retry, quota_reserve）、Claude 通道
- 无异常后切流

**第 7 步：保留 A 镜像作为回滚点**

至少保留 1 周。

### 5.3 AB 期间的规则

- 不删除 tokensheep 独有列的 model 定义（即使 upstream 没有）
- 不改共有字段的 gorm 标签（尤其是 `type:` / `default:` / 索引名）
- 新表的 migration 只 ADD，不 DROP
- Option 表新增 key 时用可选/默认值语义，A 版本读不到不影响

## 6. 风险与预案

| 风险 | 预案 |
|---|---|
| relaykit 迁移导致 tokensheep 的 Cursor Claude 定制丢失 | 每个 tokensheep claude 提交按 patch 逐一验证，保留 test |
| tiered retry 计费与 tokensheep economy 冲突 | 参考 `docs/spec/economy-model.md`，确认结算路径一致 |
| upstream 新会话表与 A 版本的 legacy session 并存 | 保留 legacy session 中间件，B 版本能读旧 session |
| AutoMigrate 在生产 DB 上加索引卡住 | 预演 SQLite/MySQL/PostgreSQL 三种数据库 |
| Frontend option migration 破坏 A 版本 UI 配置 | 备份 options 表，验证 A 版本仍能读迁移前的 key |

## 7. 追踪

- 本文档：`docs/spec/upstream-rebase-plan.md`
- 相关既有文档：
  - `docs/spec/economy-model.md` — tokensheep 经济模型
  - `docs/spec/daily-global-pool.md` — 每日全局池
  - `pkg/billingexpr/expr.md` — 计费表达式系统
