# 2026-08-30 CPA 号池现状归档（v7.2.143 merge + daemon v4 + 配置基线）

**范围**：只覆盖 CPA（cli-proxy-api）侧。new-api 蓝绿收敛、kiro-rs 蓝绿对齐不在本文档内。

**跨引用**：
- 429 分类修复：[`20260829-antigravity-429-classification.md`](./20260829-antigravity-429-classification.md)
- 重试放大 A/B：[`20260829-retry-amplification-abtest.md`](./20260829-retry-amplification-abtest.md)
- 传染机制 + quota 接口：[`20260828-antigravity-quota-and-contamination.md`](./20260828-antigravity-quota-and-contamination.md)
- 硬编码模型清点：[`20260826-antigravity-pool-collapse/cpa-hardcoded-models-audit.md`](./20260826-antigravity-pool-collapse/cpa-hardcoded-models-audit.md)

本文档补齐前几份事故报告没记的部分，并**修正它们中已经过期的陈述**。

---

## 一、生产配置基线（2026-08-30 实测，以本文档为准）

VPS196 `/data/cli-proxy-api/config.yaml` 实际值：

```yaml
request-retry: 3
disable-cooling: true
quota-exceeded:
  switch-project: true
  switch-preview-model: true
  antigravity-credits: true
routing:
  strategy: "round-robin"
```

### 修正：`request-retry` 到底是 3 还是 5

记忆 `cpa-ops-lessons` 里写的是 `request-retry: 5`（并注明"大池必须 5+ 才够扫号"），`ab-test-baseline` 里写的是 `3`。**生产实际是 3**。

不需要 5 的原因，在 429 分类修复之后成立：`request-retry` 的作用是"多轮扫号以撞到刚出 5min quota 窗口的号"。而 `20260829-retry-amplification-abtest.md` 第 3 节已证实 `request-retry: 1` 对换号轮询无效——换号轮询来自两层**硬编码**重试，不受这个配置控制。所以调高它只放大废调用，不增加扫号覆盖。**结论：保持 3，不要按旧记忆调到 5。**

### 三个反直觉结论（仍然成立，勿动）

| 配置 | 正确值 | 望文生义会以为 | 实测后果 |
|---|---|---|---|
| `disable-cooling` | `true` | 关掉冷却更激进？ | 关掉才 429。CPA 的 cooldown 是 30min+ 长挂起，不是短冷却 |
| `switch-preview-model` | `true` | 关掉能加快扫号 | 号池 79/79 全染色，成功率 94% → 90.5%，可用号 26 → 0 |
| `switch-project` | `true` | 怀疑它污染共享 project | A/B 证伪，`false` 不阻止放大 |

`switch-preview-model` 的完整数据轨迹见 `ab-test-baseline` 记忆。**关键操作纪律：改这三个里任何一个，观察窗口必须 ≥ 20 分钟**，跨过 `quota_exceeded` 5min 窗口的累积扩散期。前 10 分钟"看起来没影响"是号池未被染色的错觉——2026-08-27 就是在 +8min 看到 94.2% 而给出错误的 KEEP 结论，+18min 才跌到 90.5% 并引发投诉。

---

## 二、upstream merge v7.2.143

CPA fork 已合并 upstream v7.2.143，配套两处改动从硬编码转为配置化：

- Execute 白名单配置化
- `modelQuotaExceededWindow` 配置化（默认 5min）

合并过程中发现的兼容问题记录在 `20260828-antigravity-quota-and-contamination.md` 第八节。

### stream 转换白名单

upstream 的 stream 转换走白名单机制。新增 channel 时若 provider 支持 `StreamOptions`，必须把该 channel 加进 `streamSupportedChannels`，否则流式请求的 usage 统计会缺失。这条同时是 `AGENTS.md` 的既有约定，此处记录是因为 v7.2.143 merge 时踩到过。

---

## 三、daemon v4：从手动隔离到自动化

### 修正：旧记忆说"不要自动化"，现已被 v4 取代

`cpa-ops-lessons` 记忆里有一条强约束：

> **不要自动化**——daemon 版本会把秒级 429 也误判 weekly，把 79/79 全 disable 造成 500 洪水

这条**针对的是 v3 及更早版本**，现在已不适用。v3 的误判本质：它从 429 响应推断 weekly 耗尽，而秒级 429 来自 5h 滑动桶，两者响应形状不同（见 `20260829-antigravity-429-classification.md` 第二、三节）。

**v4 换了判定源**：不看 429，直接查 quota 接口读 `gemini-weekly` 百分比。这是确定性数值而非推断，所以自动化是安全的。原来的手动挑号策略（人工找 Resets in 60min+ 的号打 `disabled: true`）已由 v4 接管。

### v4 运行状态（2026-08-30 08:28 CEST 起常驻）

systemd unit `cpa-daemon-v4.service`，`active (running)`：

```
ExecStart=/usr/bin/python3 /usr/local/bin/cpa-daemon-v4.py --apply
Environment="CPA_MAX_ACTIVE=40"
Environment="CPA_MAX_ENABLE_CYCLE=5"
Environment="CPA_CYCLE_SEC=1800"
StandardOutput=append:/var/log/cpa-daemon-v4.log
Restart=on-failure / RestartSec=60
```

判定档位（读 `gemini-weekly` 的 `remainingFraction`，阈值来自脚本第 90-91 行）：

```python
WEEKLY_EXHAUSTED = 0.02   # <= 2%  视为耗尽, 必须关
WEEKLY_HEALTHY   = 0.10   # >= 10% 视为健康, 可以开
```

| `gemini-weekly` | 当前 enabled | 当前 disabled |
|---|---|---|
| `<= 2%` | **disable** | keep（already off） |
| `2% ~ 10%`（灰区） | keep | keep —— 防抖动，两边都不动 |
| `>= 10%` | keep（healthy） | **enable** |
| 读不出来 | **disable**（`quota-unreadable(...)`） | keep |

enable 受 `MAX_ACTIVE=40` 与单轮 `MAX_ENABLE_CYCLE=5` 双重限流；disable 不限量（止血优先）。

**注意读不出来的那一档**：脚本对 quota 查询失败的号，若当前是 enabled 则**主动 disable**（第 226-227 行），不是保守 keep。日志里那 13 个 `quota-unreadable` 全部显示 keep，是因为它们本来就已经是 disabled 状态。

最近一轮实际输出：`plan: disable=1 enable=7` → `applied: disabled=1 enabled=1`。enable 只落地 1 个是因为 `skip enable ...: would exceed max_active 40` —— 限流按设计生效。

号池当前 82 个 auth，41 个 `disabled: true`，即 41 活跃。这个数字贴着 `MAX_ACTIVE=40` 是 daemon 主动限流的结果，不是号池耗尽。

---

## 四、本次巡检新发现：13 个号 quota 读不出来

上一轮 daemon 日志的状态分布：

| 状态 | 数量 |
|---|---|
| healthy | 35 |
| already off（0%，已关） | 15 |
| quota-unreadable | **13** |
| grey zone | 8 |
| enable（100%） | 7 |

13 个号报 `quota-unreadable(httpNone:)`，占 82 号池的 **16%**。

### 根因已查明：refresh token 已失效，这 13 个是死号

拿 CPA management api-call 直接探原始 wrapper，返回的是：

```json
{"error":"auth token refresh failed"}
```

**不是 quota 接口的问题，是 refresh token 死了。** CPA 连 access token 都换不出来，所以 wrapper 里根本没有 `status_code` 字段，daemon 的 `wrap.get("status_code")` 拿到 `None`，打印出来就成了 `httpNone:`。

auth JSON 佐证（抽查三个）：

| 号 | access token 过期 | 文件 mtime |
|---|---|---|
| brennan.holmes475184 | 2026-08-29T05:52:14+08:00 | 08-29 11:55 |
| calvin.kim859544 | 2026-08-29T05:52:05+08:00 | 08-29 11:55 |
| robin.day925266 | 2026-08-29T05:52:22+08:00 | 08-28 20:52 |

`refresh_token` 字段都还在，但 Google 拒绝刷新。过期时间卡在 8-29 早上，文件从那以后再没被写过——CPA 每次尝试刷新都失败，所以不再更新。

**这批号救不回来**，只能重新 OAuth 授权或直接清掉。（早前本节写的「等扩容时再查」是错的，已更正。）

### 复现方法（供后续排查复用）

```
POST {CPA}/v0/management/api-call
  转发到 https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary
关键: 传 auth_index (不是 index —— 传错会让 CPA 收到 null，
      Google 直接回 401 invalid_token，看起来像全池死掉，实为探针 bug)
      Authorization 用 Bearer $TOKEN$ 占位符，由 CPA 服务端替换
判死: 返回 {"error":"auth token refresh failed"} 且无 status_code 字段
```

### 影响面

不构成 429 风险——13/13 全部已是 disabled 状态，不会被轮询。代价是这 16% 的号被排除在号池外。当前 40 活跃已够承载（见第六节），所以不紧急，但**要从 auths 目录清理掉**，否则 daemon 每轮都白查它们一次。

### 两波集中死亡（根因未明）

按「最后一次成功刷新」（`expired` 减 1h）排序，13 个号呈两波集中死亡：

| 波次 | 时间 (UTC) | 数量 |
|---|---|---|
| — | 08-25 10:16 | 1（mdshelar969） |
| **第一波** | **08-28 20:45 ~ 20:52** | **6** |
| **第二波** | **08-29 19:46 ~ 19:47** | **6** |

两波各 6 个、间隔约 23 小时、都落在 UTC 20:00 前后。不是随机老化，是集中事件。

**已证伪的假设：**

- ❌ **disable 导致 token 饿死**：26 个号 disabled 但 token 新鲜（`expired` 指向 1 小时后）；v3 在 08-28 用「盲开→失败→回滚」的方式 disable 过约 15 个号，今天 14 活 1 死；决定性证据是 `mdshelar969` 的 token 08-25 就过期，比 v3 在 08-28 disable 它早三天。**因果是反的：token 先死 → 请求全失败 → v3/人工才把它 disable。** v4 的 `quota-unreadable → disable` 也是同一方向。
- ❌ **蓝绿双活并发刷 refresh token**：曾怀疑 blue/green 挂同一个 auths 目录、各跑 refresh loop，导致 Google 的 refresh token 轮转让一方副本作废。**实测排除**：green 自 06:35 启动至今只有 11 行日志（全是 plugin 加载），`refresh`/`token` 关键词 0 命中，它加载完 82 个 auth 就没动过。CPA 从来没有并发刷 token（new-api 那边才是真双活）。
  - 但仍有一处值得留意（非本次死因）：两个容器**确实**挂同一个 `/data/cli-proxy-api/auths` → `/root/.cli-proxy-api`，blue 是 `restart: no`、green 是 `unless-stopped`。物理上存在并发写的可能，只是 green 当前没在刷。

根因仍未定，需要从 Google 侧 refresh token 的失效条件入手（例如 OAuth app 处于 testing 模式时 refresh token 的有效期策略、或批量吊销事件）。

---

## 五、遗留事项

| 项 | 状态 |
|---|---|
| 13 个死号处理（重新 OAuth 或清掉） | 根因已查明（refresh token 失效），待执行清理。**daemon v4 没有任何自动清理逻辑**——`grep clean/remove/delete/dead/purge` 零命中，它只改 `disabled` 字段从不删文件，所以死号会一直留在 auths 里每轮被白查一次 |
| 两波集中死亡根因 | 未定。已证伪 disable-饿死 与蓝绿双活两个假设（见第四节），需从 Google 侧 refresh token 失效条件查 |
| daemon v4 只判 `gemini-weekly` 单桶 | 待扩展，见第三节覆盖边界 |
| 残余 2x 429 放大（选号层不查 quota-exceeded 窗口） | **已修**（2026-08-30，CPA `7530ce0c`），见下方「已解决」 |
| 429 全池传染根因对照实验 | 需独立测试环境，生产做会再打爆号池。daemon v4 属症状规避不根治 |
| `docs/ops/bluegreen-pattern.md` | **文件不存在**，但 `bluegreen-deploy.sh` 头部注释引用了它。CPA 蓝绿方案本身已定型（Caddy 常驻双 upstream + `lb_policy first` + 2s 健康检查，部署不碰 Caddy），文档待补。归部署 agent 范围 |

### 已解决，可从待办移除

- **残余 2x 429 放大**（CPA commit `7530ce0c`，2026-08-30）：根因是**注册表标记与选号层脱节**。`SetModelQuotaExceeded` 把 `(auth, model)` 写进 `registry.QuotaExceededClients`，但全项目搜索该 map，除 `model_registry.go` 内部的模型级可用性聚合外**无任何读取**——选号过滤 `isAuthBlockedForModel` 只读 per-Auth 内存态。而生产配置 `disable-cooling: true` 会在每次 429 后主动清掉 `state.Unavailable` / `state.Quota.Exceeded`（`conductor_cooldown.go` 893-896 行），于是注册表标记成了唯一记录、却没人读 → 刚撞 429 的号在 5min 窗口内又被 round-robin 选中再撞。
  - 修法：新增 `registry.IsModelQuotaExceeded` 读端 + `QuotaExceededWindow()` 访问器；`isAuthBlockedForModel` 把注册表作为**副次**信号消费（per-Auth `ModelStates` 仍是主信号，只在 per-Auth 未追踪 quota 时回落到注册表）。这个层级同时保住了两个既有语义：手动 rewind 时间戳仍能让号恢复、以及**单模型 429 不污染同 credential 上的兄弟模型**。
  - 另修一个顺序 bug：`MarkResult` 里 `scheduler.upsertAuth` 原本跑在 registry `Set/Clear` **之前**，导致 scheduler 基于过期快照缓存 `scheduledStateCooldown` + 未来 5min 的 `nextRetryAt`，恢复了的号要等满整个窗口才被复查。已把 registry 写入移到 upsert 之前。
  - **注意区分**：这与 08-29 那次修复不是同一个 bug。08-29 修的是 `decideAntigravity429` **分类**错误（周硬墙 429 被误判成 `SoftRetry`，只给 1s 冷却），把放大从 8x 降到 2x，属于「没关号」；本次修的是「关了号但选号层看不见」，消掉剩下的 2x。
  - 覆盖范围：主要吃掉**没有 reset 时间线索的 429**（瞬时限流、上游没给 `retryDelay` 的那种）。带 `retryDelay` 的 5h 桶用上游给的时长；周硬墙走 `CredentialScope=true` 挂整号到真实 reset 时间，都不依赖这个 5min 兜底窗口。

- **channel 35 `INVALID_MODEL_ID`**：不是靠改正 mapping 解决的，而是整个 channel 被手动禁用（`status=2`，`model_mapping` 已清空）。`aws-q-stable` 分组由 channel 49 承接，其 `model_mapping` 为空——**空映射在这里是正确的**，模型 ID 原样透传，不会重演 channel 35 把 opus 系错映射到 `claude-sonnet-4-6-thinking` 的问题。24h 内 `INVALID_MODEL_ID` 零条。不要照搬 channel 32 的映射，那是 `claude-lowprice` 分组走 CPA 的路径，与 aws-q 无关。

---

## 六、号池规模：`MAX_ACTIVE` 不能长期写死

**运营方明确要求（2026-08-30）**：`CPA_MAX_ACTIVE` 暂时保持 40，但**绝对不能作为长期方案**。号池规模应该由下游实际需要的 RPM 和并发推导，而不是拍脑门定一个常数。

### ⚠️ 现有容量数字只是手感，不是基线

运营方口述的粗略感觉：

- 约 12 个号能承载约 20 RPM
- 约 30 个号大致能跑 50-60 RPM

**运营方明确说明这是模糊的感觉，不是准确数字。** 由此换算出的「约 1.7-2 RPM/号」是推算值，**不得作为公式系数，不得写进任何自动化逻辑**。

真要做动态调节，必须先压测拿到三个可信输入：

1. 单号可持续 RPM —— 要分模型，`gemini-2.5-pro` 和 `flash` 的额度消耗速率不同
2. 单号并发上限
3. 撞 429 前的**安全水位**（不是极限值）

### 当前负载参考（2026-08-30 17:40 实测）

| 项 | 值 |
|---|---|
| CPA 通道 `own-cpa-multi-gemini-mapped-AD9x` 过去 1h | 2386 请求 = **39.8 RPM** |
| 全站同期 | 48.8 ~ 69.4 RPM（其余走 codex / aws-q，不占这个池） |
| 当前 active | 40（顶到 `MAX_ACTIVE`） |

4 个满额度号（`gemini-weekly=100%`，reset=6d16h）被卡在门外：`paula.fuller684002`、`roberto.jackson921138`、`tara.mason699610`、`victoria.vargas835939`。按手感估算 40 号对当前 39.8 RPM 仍有余量，所以**暂不放开**，这 4 个号卡着不影响承载。

### 未来动态化的形状

```
MAX_ACTIVE = ceil(目标RPM / 单号安全RPM) × 安全系数
```

三个输入目前全缺可信值，必须先压测。另外 daemon 跑在 VPS196，要读 new-api（VPS22）的实时 RPM 需要跨机开只读口子。

### 保留上限的原始理由（仍然成立）

`MAX_ACTIVE` 当初设 40 是为了**防号池突然膨胀触发共享 project 传染**（64/79 号共享 `aicode-consumers` project）。调高前要权衡这个风险，**不能只看 RPM 够不够**。

改法：改 systemd unit 的 `Environment="CPA_MAX_ACTIVE=40"`，`daemon-reload` + 重启 daemon，不用改代码。

---

## 七、安全提醒

`ab-test-baseline` 记忆里的"稳定生产配置"代码块含一条明文 `proxy-url`，带用户名和密码。建议轮换该凭据并改从环境变量注入，同时把记忆里的明文替换为占位符。本文档不复制该值。
