# TokenSheep 上游请求错误说明透传发布

2026-10-17 01:31（北京时间）完成蓝绿发布和线上核验。

## 范围

在本站 new-api 的公开错误出口保留 400/422 的安全拒绝、上下文限制和普通参数校验说明；额度、余额、账号池、分组、渠道路由、价格、凭据等运营信息继续隐藏。仅保留可公开的 message，上游 metadata 和嵌套 request ID 不进入下游。

默认规则不再将“该会话已被网络安全策略屏蔽”覆盖为 Request rejected。生产数据库检查：没有持久化 error_mask_setting 选项，也没有渠道自定义 error_mask 规则，因此无需修改数据库配置。

本次以实际生产提交为基线，只移植本次消息边界修复。未包含主工作区中其它尚未发布的状态码分类、重试、SMTP 或前端修改。现有前端资源沿用生产版本。

## 版本

- 代码提交：aea81faff0eccb256de75f9a7272dbcca4ba59f5
- 已推送分支：codex/upstream-request-error-guidance
- 基础提交：721612d650c49dd81c977d5dd7f4a87706fb8fc1
- 生产版本：20261017-aea81fa-request-errors
- 镜像：tokensheep-newapi:20261017-aea81fa-request-errors
- 二进制 SHA-256：96e5c969d3b34591436498f5430e70ee915e4b04524b137298eab28db05e8335

## 验证

- go test ./service ./controller ./setting/operation_setting 通过。覆盖 Gemini 安全原因码、具体 token 上限、参数说明、运营信息过滤和 JSON/SSE 输出。
- Linux amd64 发布构建及 git diff --check 通过。
- blue、green 均 healthy，/api/status 返回本版本；两个容器内 /new-api 的 SHA-256 与构建产物完全一致。
- 公网 free.tokensheep.fun/api/status 返回 200 和本版本。
- api.tokensheep.fun/v1/models 与 direct.tokensheep.fun/v1/models 的无凭据请求均返回预期 401。
- 生产 .env 发布前后逐字一致，Caddyfile 已逐字恢复。
- 发布期间观察到的运营类错误继续使用脱敏提示；未主动构造真实安全违规或超长请求。安全/上下文输出的行为验证来自回归测试，线上检查确认部署的是对应代码构建。

本次影响后续请求。历史日志不会重新生成；下游若另有 new-api 实例把错误改成通用文案，仍须修正其自身规则。

## 发布材料与回滚

- 服务器发布目录：/data/new-api/releases/request-errors-aea81fa/
- 服务器配置备份与核验：/data/new-api/backups/request-errors-20261017/
- 本地项目备份：.private/production-config/2026-10-17-request-errors/
- 项目外备份：/Users/danlio/.config/tokensheep/production-backups/2026-10-17-request-errors/
- 旧镜像：tokensheep-newapi:20261016-721612d-device-local-time-final

发布使用蓝绿切换，并为实例退出配置 130 秒容器等待。回滚只切换应用镜像，按健康实例接流量、更新另一实例、核验后交换的顺序执行。不要恢复旧 .env 或数据库选项。
