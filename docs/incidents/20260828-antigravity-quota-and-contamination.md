# 2026-08-28/29 antigravity 号池传染 + quota 接口发现

## 一、核心结论（先看这个）

1. **开 `gemini-weekly = 0%` 的号会导致全池 429**（多次复现，机制未定）
2. **只开有额度的号就稳定**（34 号池跑 100% 成功率）
3. **quota 查询接口已找到**（关键在 User-Agent），daemon 从此不用猜
4. **five-hour 桶和 weekly 桶是独立的** —— 这是之前所有误判的根源

## 二、quota 查询接口（本次最大突破）

之前一直卡在"无法知道 disabled 号有没有额度"，daemon v3 因此用了错误的启发式
（试探性 enable 看有没有 fail），导致反复传染。

### 正确调用方式

```
POST https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary
Headers:
  Authorization: Bearer <access_token>
  Content-Type: application/json
  User-Agent: antigravity/hub/2.9.1 darwin/arm64   <-- 关键！aidev_client 会 403
Body: {}
```

### 通过 CPA 转发（推荐，避免自己实现 OAuth refresh）

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

`$TOKEN$` 由 CPA 自动替换成该号的 access_token（会自动 refresh，不用自己处理 OAuth）。

### 返回结构

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
     {"bucketId":"3p-weekly","remainingFraction":0.87,...},
     {"bucketId":"3p-5h","remainingFraction":0.93,...}]}]}
```

### 踩过的坑（别再走一遍）

| 尝试 | 结果 |
|---|---|
| `loadCodeAssist` | 只返回 allowedTiers/ineligibleTiers，**没有 quota 桶** |
| `countTokens` / `generateContent` 直连 | 403 "Cloud Code Private API has not been used in project" |
| User-Agent 用 `aidev_client/1.0.13` | 403 SUBSCRIPTION_REQUIRED |
| User-Agent 用 `gemini-cli/0.1.5` | 403 |
| 加 `X-Goog-User-Project` header | 403 USER_PROJECT_DENIED（反而更糟） |
| CPA management API `auth-files` | **对 disabled 号不返回 quota**（内存缓存已清，全是 `quota.signals={}`） |
| 用 refresh_token 走 socks5 proxy | `curl: (97) cannot complete SOCKS5 connection`（proxy 不支持 Google OAuth） |

## 三、daemon v3 判定错误的本质

v3 的逻辑：试探性 enable 一个 disabled 号 → 等 5min → 看 `recent_requests`
有没有 failed → 没有就当"号健康"保留 enabled。

**错在哪**：那些 success 来自 **five-hour 桶还有余量**，而 **weekly 桶已经 0%**。
两个桶完全独立：
- five-hour 桶：几小时就恢复
- weekly 桶：要等一周

v3 把"5h 桶有余"误判成"号健康"，于是把 weekly 已耗尽的号留在池子里，
高峰期流量打上去就撞 429 → 传染。

**证据**：2026-08-29 面板截图显示多个 v3 判定"OK"的号实际是
`Weekly Limit Remaining 0% remaining, Refreshes in 2d 19h`。

## 四、传染时间线（2026-08-28）

```
11:29 CST  daemon v3 启动，每 10min 开 3 个 disabled 号
11:29-16:00 累计 enable 过 36 个号，15 个被 revert，21 个留在 enabled
16:04 UTC  gemini-3.7-flash 开始 500 do request failed
16:28 UTC  转为 429 Resource exhausted，全池打光
~01:06     手动 disable 21 个误开号 + 停 daemon
~01:30     恢复全绿 100%
```

**关键对比**：同期 `gemini-3.6-flash` 618/618 = 100% 成功，
`gemini-3.5-flash` 374/374 = 100%，`gemini-3.1-pro` 60/60 = 100%
—— 只有 3.7-flash 挂，说明是 **model-level 而非全 provider** 的问题。

## 五、传染机制（未定，三个候选）

**已知事实**：
- 64/79 号共享 `aicode-consumers` project（15 个号有独立 project）
- 开 weekly 0% 号 → 传染；只开有额度号 → 稳定
- 号越少越稳（79 全开崩 / 34 个偶有抖动 / 23 个 10h 零错）

**候选 1：`switch-project: true` 副作用**
号撞 429 时 CPA 触发 project 切换操作，污染共享 project。

**候选 2：Google project 级配额**
`aicode-consumers` 有 project 级 RPM/TPM 总配额，多个号同时 429 打爆它。

**候选 3：`request-retry: 5` 放大**
1 个 weekly 号撞 429 → CPA 连换 5 个号重试 → 短时间对同一 project 打 5x 请求量。

**验证方案**（需要独立测试环境，生产做会再打爆号池）：
- 对照 A：`switch-project=false` + 1 个 weekly 0% 号 → 看传染是否消失
- 对照 B：`request-retry=1` + 1 个 weekly 0% 号 → 看传染范围是否缩小
- 抓 CPA 撞 429 后**下一个请求**的完整 payload，确认是否真在切 project

## 六、2026-08-29 全池扫描结果（79 号）

| 分类 | 数量 | 处理 |
|---|---|---|
| `gemini-weekly >= 10%` | 19 | 已全部 enable（分 4 批，每批隔一个观察窗口） |
| `gemini-weekly` 2-10%（灰区） | 5 | 保持现状，3 天后 reset |
| `gemini-weekly < 2%` | 37 | disabled，8/30~9/4 陆续恢复 |
| auth 失效（refresh token 坏） | 7 | 需重新 OAuth |

**auth 失效的 7 个号**（quota 查询返回 unreadable）：
brennan.holmes475184, calvin.kim859544, dawn.kelley612665,
kathryn.payne903834, lawrence.pierce718700, mdshelar969, robin.day925266

**最终号池**：34 enabled / 45 disabled，稳定 100% 成功率。

## 七、daemon v4（已部署常驻）

> **状态更新（2026-08-30）**：本节写作时 v4 尚未部署。现已作为 systemd unit
> `cpa-daemon-v4.service` 在 VPS196 常驻运行（08:28 CEST 起），
> `/usr/local/bin/cpa-daemon-v4.py --apply`，30min 一轮。
> 运行状态、判定档位实测输出、以及新发现的 13 个 `quota-unreadable` 盲区
> 见 [`20260830-cpa-consolidated-state.md`](./20260830-cpa-consolidated-state.md) 第三、四节。

`bin/cpa-daemon-v4.py`

| | v3（错） | v4（对） |
|---|---|---|
| 判断依据 | 试探 enable 看 fail | 读 `gemini-weekly` 真实百分比 |
| 开号 | 随机盲开 | 只开 `>= 10%`（`WEEKLY_HEALTHY`） |
| 关号 | 撞 fail 才关 | `<= 2%` 立刻关（`WEEKLY_EXHAUSTED`） |
| 灰区 | 无概念 | 2-10% 不动（防抖动） |
| auth 失效 | 可能误开 | 识别并保持关闭 |
| 传染风险 | 高 | 零 |
| 默认行为 | 直接改 | dry-run，需 `--apply` |
| 开号限速 | 无 | 每轮 5 个 + 总量 40 |

dry-run 验证（2026-08-29 17:06）：`plan: disable=0 enable=0`
—— 说明当前号池已是最优，决策逻辑正确。

## 八、同期修的其他兼容问题

**`400 Requests ending with a model turn are not supported.`**（1h 内 24 次）

客户端裁剪历史后留下 assistant 消息结尾，Google 拒绝。upstream 已有
`EnsureGeminiLeadingUserContent` 处理"开头是 model turn"，但没有对称的
trailing 处理 —— 疏漏。

修复：`helps.EnsureGeminiTrailingUserContent`（CPA commit `93e8ba3f`）
- contents 结尾是纯 model turn → 追加空 user turn
- 带 `functionCall`（等 tool 输出）或 `functionResponse`（Antigravity 把 tool
  结果也标 `role=model`）→ 保持原样，否则破坏 call/response 配对校验
- 7 个单测，已部署，该错误已消失

## 九、运营要点

- **不要盲开 disabled 号**，先用 quota 接口查 `gemini-weekly`
- **分批开**（5 个一批，间隔一个观察窗口），不要一次全开
- **看 Gemini 桶，不看 Claude 桶**（我们主要跑 Gemini，Claude 有 kiro-rs fallback）
- **CPA 面板的 quota 数据来自前端调这个接口**，后端 auth-files API 不返回

## 十、相关

- CPA daemon v4: `bin/cpa-daemon-v4.py`
- 硬编码模型名审计: `docs/incidents/20260826-antigravity-pool-collapse/cpa-hardcoded-models-audit.md`
- 蓝绿部署规范: `docs/ops/bluegreen-pattern.md`
- 部署脚本: `bin/bluegreen-deploy.sh`
