# 管理员渠道探测退役 v1

记录时间：2026-09-29（北京时间）。适用部署：VPS22 TokenSheep new-api。

## 职责边界

模型可用性探测由独立账号 `probe-monitor`（用户 #23）及其 `monitor-*` 令牌发起，通过正常 relay 入口访问。new-api 不再借用 root 身份主动生成模型请求，不保留自动渠道测试、批量测试、单渠道测试或相应设置入口。

本次保留业务请求的路由、重试和失败自动禁用规则；历史任务、消费日志、渠道响应时间字段以及兼容常量保留。历史 `monitor_setting.*` 数据即使存在也没有执行器，`CHANNEL_TEST_ENABLED` / `CHANNEL_TEST_FREQUENCY` 不再有启用探测的代码路径。移除仅供旧探测使用的“成功后恢复”和响应时间阈值表单，避免设置页出现失效开关。

`probe-monitor` 的正常 relay 请求不依赖被移除的 `/api/channel/test`、`/api/channel/test/:id` 管理员接口。此次只核查该账号的既有请求记录，没有触发任何模型测试，也没有修改其令牌、计划或账号池。

## 事故证据

生产配置原值为 `auto_test_channel_enabled=true`、周期 2 分钟、并发 1、模式 `passive_recovery`。#12 CPA 和 #106 g2a 为自动禁用渠道，测试模型均为 `gemini-3-flash`。

2026-09-28 01:38:45 至 2026-09-29 01:38:45 的既有日志包含 568 次测试启动，其中 Gemini 520 次（#106 为 479 次，#12 为 41 次）。另一任务表查询在 01:36:25 截止的 24 小时窗记录 637 个 `channel_test` 任务；空轮次和统计截点不同，因此任务数不等于模型请求数。未发现管理员 `/api/channel/test` 调用；两实例没有同一轮重复执行，任务完成至下一轮启动最短 120 秒。

旧代码用 root 用户 #1 的上下文执行，故消费日志显示 `wholesale-plus` 分组、令牌名“模型测试”，容易被误认为用户调用。

有效模型响应仍可能无法恢复渠道：默认 `ChannelDisableThreshold=5` 秒，而恢复探测外层把超时长响应重新判为失败。`passive_recovery` 只禁止关闭动作，没有跳过此判定。任务 #5530 的两个请求分别耗时约 14 秒、19 秒并写入有效用量，但轮次结果为 `tested=2, failed=2, enabled=0`，随后继续探测。

Git 记录中，`537719229`（上游 PR #5680，2026-06-24，作者 Calcium-Ion）将旧渠道探测纳入系统任务调度；`4add708eb`（上游 PR #6917，2026-08-18，作者 Seefs）增加并发配置。代码作者记录不能证明是谁开启了生产配置。既有选项访问日志缺少足够的键值审计，无法可靠归属启用操作。

## 生产止血与交付

- 2026-09-29 03:00 左右，关闭数据库 `monitor_setting.auto_test_channel_enabled`，顺序重启两个实例，均健康。
- 最后一个旧任务于 02:59:00 创建，02:59:35 完成；03:40 左右复核，最近 30 分钟没有新建旧任务，也没有 `testing channel` 日志。
- `probe-monitor` 用户 #23 状态正常，03:34 左右仍有 `monitor-gpt-stable`、`monitor-aws-q-v2` 的正常用量记录，确认与旧 root 探测是独立入口。
- 代码删除调度 handler、完整模型请求执行器、批量及单渠道管理接口、前端测试操作、自动探测设置及环境变量处理。
- 镜像由 GitHub Actions 构建；VPS 仅拉取成品镜像并逐实例更新。部署结果以本记录对应提交的 Actions 结果及生产镜像 revision 为准，不能把关闭配置等同于代码已部署。

## 验证

本地 `GOWORK=off go test ./controller ./router ./model ./service ./setting/operation_setting`、前端 `bun run typecheck` 和涉及文件的 oxlint 通过。路由回归用例 `TestRetiredChannelProbeEndpointIsUnavailable` 验证单渠道探测 URL 已不可达。全仓库 lint 存在无关历史错误，因此使用涉及文件的独立 lint 结果。

生产验证只读任务表、既有日志、账号令牌元数据以及服务状态接口；不向 Gemini 或其他模型发送试探请求。
