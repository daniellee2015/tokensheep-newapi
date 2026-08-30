# 2026-08-29 CPA 重试放大 A/B 测试

## 测试环境

| 项 | 值 |
|---|---|
| 容器 | `cpa-abtest` (vps22) |
| 端口 | `127.0.0.1:8419` |
| 目录 | `/opt/cpa-abtest/` |
| 镜像 | `gpt-team-cpa:local` (CPA v7.2.135) |

配置（对照组 A）：
```yaml
quota-exceeded:
  switch-project: false      # 对照点
  switch-preview-model: true
  antigravity-credits: true
request-retry: 1             # 想限制换号轮询
disable-cooling: true
routing:
  strategy: round-robin
debug: true
```

号池（6 个，从生产复制后在生产 disable 避免干扰）：

| 号 | Project | gemini-weekly | gemini-5h |
|---|---|---|---|
| noah.alexander162712 | aicode-consumers | 0% | 100% |
| tara.mason699610 | aicode-consumers | 0% | 100% |
| victoria.vargas835939 | aicode-consumers | 0% | 100% |
| walter.ferguson688240 | aicode-consumers | 0% | 100% |
| cindy.stephens897293 | aicode-consumers | 6.9% | 92% |
| t01062880838 | nodal-tape-z9p3r（独立） | 30.6% | 94% |

## 测试 1：只有 4 个 weekly 0% 号

**1 个客户端请求 → 16 次上游调用，8.99s 后返回 429**

```
号1 noah.alexander:
  daily-cloudcode-pa      -> 429 "Individual quota reached. Resets in 12h40m26s."
  cloudcode-pa (fallback) -> 429 "Resource has been exhausted"
  soft-retry 500ms (attempt 1/2):
  daily-cloudcode-pa      -> 429
  cloudcode-pa            -> 429
号2 tara.mason:       同样 4 次
号3 victoria.vargas:  同样 4 次
号4 walter.ferguson:  同样 4 次
= 4 号 × 4 请求 = 16 次上游 429
```

## 测试 2：加入 2 个有额度号，打 10 个请求

| 项 | 数据 |
|---|---|
| 客户端请求 | 10 |
| 客户端成功 | **10 (100%)** |
| 选号分布 | 6 个号各 5 次（严格 round-robin） |
| **上游 429** | **80 次** |
| **放大倍数** | **8x**（10 请求 → 80 次废调用） |

## 已证实的结论

### 1. `switch-project: false` 不阻止放大 — 排除原候选机制 1

配置成 false，80 次 429 照样打出来。传染跟 switch-project 无关。

### 2. 放大来自两层硬编码重试（都没有配置项）

| 层 | 位置 | 倍数 |
|---|---|---|
| base URL fallback (`daily-cloudcode-pa` -> `cloudcode-pa`) | `antigravity_executor_execute.go:185` | ×2 |
| soft rate limit retry (`attempt 1/2`) | `antigravity_executor_execute.go:213` | ×2 |

单个坏号被选中 = 4 次上游调用。

### 3. `request-retry: 1` 对换号轮询无效

设成 1，CPA 还是把 6 个号全扫了。换号由 conductor 控制，不受这个参数约束。

### 4. 独立 project 号没有豁免

`t01062880838`（独立 project）也被 round-robin 平等选中，跟共享 project 号一样。
传染的关键不是号自己的 project，而是**共享 project 上的废调用洪水**。

## 矛盾已解开（2026-08-30 结案）

**详细见续报**：[`20260829-antigravity-429-classification.md`](./20260829-antigravity-429-classification.md)

**结论**：两个说法都对，覆盖不同的 quota bucket。Google Antigravity 至少有两种 429 语义：

| Bucket | 响应形状 | 重试是否有效 | CPA 修复前行为 | CPA 修复后行为 |
|---|---|---|---|---|
| **5h 滑动** | 结构化 `RATE_LIMIT_EXCEEDED` + `retryDelay` 毫秒级 | ✅ 有意义（滑动窗口可能命中释放的容量） | `InstantRetrySameAuth` / `ShortCooldownSwitchAuth` ✅ | **不变** ✅ |
| **周硬墙** | 纯文本 "Individual quota reached … Resets in Xh Ym Zs." + `details` 缺失 或 `reason=QUOTA_EXHAUSTED` | ❌ 徒劳（必须等 wall-clock reset） | 落到 `SoftRetry` no-op + 1s 冷却 → 8× 放大 ❌ | 新 `WeeklyQuotaHardWall` 分类 + `IsCredentialScoped` → 整个 auth 挂起到 reset ✅ |

本次实验测到的 4 个 `weekly=0%/5h=100%` 号是**纯 Bucket B 场景**，所以 80 次重试全废。5h 桶耗尽的重试有效性本次没有直接实测，但 CPA 现有 `InstantRetrySameAuth` / `ShortCooldownSwitchAuth` 路径本来就是对的，修复未动。

## 已验证的答案（对应"下次要验证的"）

1. **`decideAntigravity429` 靠什么区分？**
   - 结构化路径：读 `error.details[].@type=google.rpc.ErrorInfo` 里的 `reason` (`RATE_LIMIT_EXCEEDED` vs `QUOTA_EXHAUSTED`) + `google.rpc.RetryInfo.retryDelay`，阈值 3s / 5min 切三档。
   - 非结构化路径（修复前）：只有 keyword sweep `quota_exhausted`/`quota exhausted`，命不中 "quota reached" 措辞。
   - 修复后新增 wording branch：`!details.Exists()` gate + 消息含 "resets in" + `{"quota reached","individual quota","upgrade your subscription"}` 任一。

2. **weekly 耗尽为什么还被重试 4 次？**
   - 响应体走 `SoftRetry` 默认分支（`details` 缺失 + `ParseRetryDelay` 正则不匹配 "Resets in"）→ executor no-op → conductor `MarkResult` case 429 施加 `quotaBackoffBase=1s` 冷却 → 号 1 秒后重新入选 → 再撞。
   - Base URL fallback (`daily-cloudcode-pa` → `cloudcode-pa`) 和 soft retry (`attempt 1/2`) 都发生在**分类之后**，是每次分类失败后的 executor 内层动作，各贡献 ×2 → 单号 4x 放大。

3. **5h 滑动场景**：本次未直接实测。但代码路径已确认对 5h 场景走 `InstantRetrySameAuth` / `ShortCooldownSwitchAuth`（`credits_test.go:167` 的 `retryDelay: "479.417207ms"` 就是这条），修复只加了独立的 wording branch，5h 路径 0 改动。

4. **CPA 已区分吗？** 部分区分。对结构化响应体区分正确；对非结构化的**周硬墙**响应体（Google 实际最常见的形状）不区分 —— 这就是要修的地方。修复方向和"下次要验证"里第 4 条完全一致：
   - weekly 耗尽 → 跳过重试，`IsCredentialScoped=true` 让 conductor 把整个 auth 冷却到 reset 时点。
   - 5h 耗尽 → 保留重试（原代码不动）。

## 修复部署 & Live 验证

**详细数据**：[`20260829-antigravity-429-classification.md`](./20260829-antigravity-429-classification.md) 第七节

- 交叉编译 patched CPA v7 服务器二进制（`GOOS=linux GOARCH=amd64 CGO_ENABLED=0`）。
- Baseline 保留在容器内 `/CLIProxyAPI/CLIProxyAPI.baseline`，一条命令回滚。
- **结果**：10 客户端请求 → 20 次上游 429（放大 **8× → 2×**），客户端 10/10 = 100% 200 成功，SoftRetry ladder + base URL fallback 零触发。
- 剩余 2× 放大来自 `pickNextMixedLegacy` 只看内存 `auth.Quota.Exceeded` 不参考 `registry.SetModelQuotaExceeded` 5min 窗口，属于第二阶段修复。

## 本次结论的边界

**证明了**：weekly 耗尽的号留在池子里会产生 4x 徒劳重试，坏号越多废调用越多。

**没有证明**：重试机制本身应该削弱或关掉。5h 桶滑动场景下重试可能是必需的。

**所以修复方向是按桶类型区分重试策略，不是简单关掉重试。**

## 修复方案（按性价比）

| 方案 | 效果 | 难度 | 是否影响滑动额度命中 |
|---|---|---|---|
| **daemon v4 及时关 weekly 0% 号** | 消除废调用源头 | ✅ 已实现待部署 | **不影响** |
| CPA 按桶类型区分重试 | 根治 | 改代码 | 不影响（只跳过 weekly 硬墙） |
| 撞过 weekly 429 的号在 reset 前不再选 | 根治 | 改代码 | 不影响 |
| 关掉 base URL fallback | 放大 ×4 -> ×2 | 改代码 | ⚠️ 可能影响 |
| 关掉 soft retry | 放大 ×4 -> ×2 | 改代码 | ⚠️ **会影响滑动命中** |

**优先 daemon v4** —— 它把坏号及时关掉，废调用归零，且完全不影响重试机制和
滑动额度命中能力。

## 测试环境保留

```bash
# 启动
ssh vps22 'docker start cpa-abtest'
# 停止
ssh vps22 'docker stop cpa-abtest'
# 看日志
ssh vps22 'tail -50 /opt/cpa-abtest/logs/main.log'
# 清日志重测
ssh vps22 '> /opt/cpa-abtest/logs/main.log'
```

## 遗留事项

- `cindy.stephens897293` 和 `t01062880838` **仍在生产 disable 状态**
  （为测试借出），测完要决定是否放回生产
- 测试容器 `cpa-abtest` 保留，下次直接 start 就能用
