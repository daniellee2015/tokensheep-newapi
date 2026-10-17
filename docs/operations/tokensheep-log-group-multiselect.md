# 使用日志分组搜索与多选（2026-10-17）

## 问题与行为

原筛选框只在完整翻译名称精确匹配时转换为分组 ID；大小写不同、部分名称和多个分组均无法正常查询。

分组筛选现在提供可搜索的多选下拉框，选中项以可移除标签显示：

- 搜索忽略大小写，统一全角字符与多余空白，支持原始分组 ID 和七种界面语言的分组名称。
- 输入 `gemini ultra` 后点击搜索可同时查询 `gemini-ultra` 和 `gemini-ultra-per-call`；也可在列表中分别选择“Gemini Ultra 按量”和“Gemini Ultra 按次”。
- 输入多个名称时支持英文逗号、中文逗号、顿号和换行分隔。
- 精确原始 ID 优先，未找到的 ID 保留用于历史记录查询。
- 单组继续使用原 `group` 参数，多组页面链接使用 JSON 数组 `groups`，API 使用重复的 `groups` 查询参数。
- 列表、总数、额度和 RPM/TPM 应用同一组选项；普通用户继续限定为自己的记录。
- URL 刷新恢复选择、删除单个标签、重置和手机端键盘操作均受回归测试保护。

名称解析在前端将输入映射为规范分组 ID，后端使用参数化 `IN` 条件；数据库不需要修改排序规则或增加迁移。API 的分组参数仍为原始 ID。

## 验证

先添加能复现旧行为的失败用例，再实现修复：前端旧实现有六个新增用例失败；后端旧实现忽略多组选项，导致列表总数和统计额度包含不相关分组。

- 上海时区使用日志模块：47 项通过，包含既有日期、重置与设备时区回归。
- 随后新增的完整日志表集成测试：1 项通过，验证列表与统计同时携带多组条件，并在移除一组后恢复单组参数。
- Go controller/model 定向测试通过，使用真实控制器和 SQLite 测试数据库，覆盖列表、分页总数、额度、RPM/TPM、重复选项、未知分组、旧参数及用户隔离。
- TypeScript、受影响文件 lint、翻译同步、前端生产构建与 Linux amd64 编译用于发布验收。
- 七种语言各增加 `Select multiple groups` 一个翻译键。

## 发布与恢复

发布版本：`20261017-log-group-multiselect`。

以线上 `02370aa636828ac06b1f41ae27626e45aecb92d6` 为基线，保留此前的上游错误信息和独立备用渠道修复。工作分支为 `codex/log-group-multiselect`，隔离目录为 `/Users/danlio/.codex/worktrees/shared-verification-codes/tokensheep-newapi`。

发布通过逐个更新蓝绿容器完成，另一实例继续承接请求。发布脚本验证二进制校验和、两个实例健康与版本、Caddy 路由恢复和生产 `.env` 字节不变；失败自动恢复之前的应用镜像。版本、提交、二进制哈希及最终检查结果保存在私有发布清单中。

- 项目备份：`.private/production-config/2026-10-17-log-group-multiselect/`。
- 项目外备份：`/Users/danlio/.config/tokensheep/production-backups/2026-10-17-log-group-multiselect/`。
- 服务器发行目录：`/data/new-api/releases/log-group-multiselect-20261017/`。
- 服务器配置与验证备份：`/data/new-api/backups/log-group-multiselect-20261017/`。

回滚镜像为 `tokensheep-newapi:20261017-gemini-reserve-fallback`。回滚仅替换应用镜像并通过蓝绿健康检查，不回写旧 `.env`、数据库或认证配置。主仓库文件同步前逐一比对生产基线并备份，翻译只增补本次键，保留其他本地修改。
