# 2026-08-26 antigravity 号池崩溃事故

## 快速索引

- [`rca.md`](./rca.md) — 完整根因分析：所有已验证的机制、放大链条、代码位置、每个断言的证据
- [`runbook.md`](./runbook.md) — 分 Phase 落地方案：备份 / 执行 / 观察 / 回滚 / 判据
- [`evidence.md`](./evidence.md) — 关键证据快照（CPA log 数字、Redis 状态、配置 diff、DB 查询结果）

## 一句话结论

上游号池被打爆的**放大机制**位于 VPS196 `cli-proxy-api` 的 `quota-exceeded`
fallback + `request-retry` 组合：单次下游请求实测触发 32~64 次上游 API 调用，
横扫 3~5 个 antigravity 账号。加上 caddy `response_header_timeout 0` +
new-api 中继无超时，挂死请求可持续 25~53 分钟，进一步占死号槽。

new-api 侧同时存在**限流交叉污染 bug**（`middleware/model-rate-limit.go`
Redis key 按 userId 建、额度按 token group 取），是「量不进记录」的另一个
独立成因，与号池崩溃并发但根因不同。

## 事故时间线（事发当日）

| 时间 | 事件 |
|---|---|
| 前一天晚上 | 修复探针问题 + 调整 RPM/TPM/session 限流 |
| 白天 | 开放后请求量增大，号池全部 429 |
| 晚 22:47 | CPA `disable-cooling` 已被改为 `false`（备份文件时间戳） |
| 晚 23:00~23:05 | 采集到多条 CPA error log，单次下游请求内含 32~64 次上游调用 |
| 事故排查中 | 定位根因、制定分 Phase 修复方案 |

## 已排除的假设

以下嫌疑经代码或运行时验证**不成立**：

- new-api 内部重试放大 —— `RetryTimes = 0`，不放大
- antigravity-manager OAuth 换号链 —— 废弃项目，与本次无关
- uptime-kuma 探针频率 —— 60s 那批不走 relay，300s 那批快速返回不挂
- 渠道自动探活 —— 会写消费日志且量小
- Status 页面页脚心跳 —— 纯前端 UI，无后端调用
- `disable-cooling` 是当前 429 的直接原因 —— 已确认为 `false`
- caddy `response_header_timeout` 无超时是主因 —— 降级为次因

## 已确认的根因

见 [`rca.md`](./rca.md)，共 12 条，全部附代码位置和运行时证据。

## 修复状态

见 [`runbook.md`](./runbook.md)。

---

**调查负责人**：Claude Code + greenSheep999
**开始时间**：2026-08-26
**当前状态**：文档已完成，进入 Phase 1 执行
