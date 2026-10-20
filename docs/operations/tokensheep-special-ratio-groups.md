# 特殊倍率规则新增用户组修复（2026-10-20）

## 能力核对

当前生产基线 `011dd5423e31e7009745c3df7ad9fc397770597c` 没有按用户 ID 存储独立消费倍率的字段或用户编辑入口。现有机制是 `GroupGroupRatio[userGroup][billingGroup]`：`relay/helper/price.go:HandleGroupRatio` 先读取特殊倍率，未命中时使用计费分组的 `GroupRatio`。

特殊倍率替代基础分组倍率，不是再叠乘一次。例如模型基础价为 1、计费组基础倍率为 2、某用户组的特殊倍率为 0.8，该用户组费用按 `1 × 0.8` 计算，而非 `1 × 2 × 0.8`。

仅一个用户属于某专属组时，可以通过分组机制实现单独定价。该修复发布时，用户编辑页仍从 `/api/group/tiers` 获取可选用户组；它只读取 `tokensheep_economy.tier_thresholds`，导致新建特殊倍率组不能自动出现在用户编辑框。此流程缺口已由后续[用户组目录修复](tokensheep-user-group-catalog.md)解决：自定义用户组会自动进入可选目录，管理员仍需主动将目标用户分配到该组。创建规则不会自动改变用户归属、贡献等级或可用模型权限。

TokenSheep 还包含贡献等级、商业组和订阅的用户组变更逻辑；如果未来增加不改变用户组的个人价格，应作为独立计费需求设计，覆盖预扣、结算、异步任务、Realtime、价格展示和缓存。

## 原因

提交 `fc26b88fd`（2026-07-06，`feat(group): enhance group ratio editor with improved visibility rules and JSON parsing`）在旧 `web/default/` 路径下将“添加用户组”的文本输入改成了 `GroupNameSelect`，候选值只来自已有价格组目录。之后文件随前端目录迁移继续沿用该行为。

因此新的用户组名称无法输入。2026-09-01 隐藏用户档位价格行的修改保留了目录数据；最近的日志筛选修改没有触及此控件。

## 修复

- 使用可输入的组合框，支持选择已有组或直接输入新组名。
- 输入框绑定可访问标签，建议列表使用浮层，避免被移动端弹窗滚动区域裁切。
- 关闭建议或点击添加时保留输入；支持键盘选择和直接 Enter 提交新名称。
- 去除首尾空白，空名称禁止添加；重复组明确提示并保护已有倍率。
- 新增、展开和删除规则按钮补充可访问名称。
- 只更新 `GroupGroupRatio` 的表单值，不自动修改用户、分组基础价或已有生产倍率数据。
- 两条新增提示通过翻译脚本补齐 en/zh/zh-TW/fr/ja/ru/vi。

## 验证

新增测试先在原版复现失败：输入 `customer-alice` 后组合框没有该值。修复后覆盖自定义组、已有组键盘选择、空目录、空白、Enter、重复保护、取消后重新打开、配置计费组倍率和删除后保留其他规则。

相关三份测试共 11 项通过。TypeScript、受影响文件 lint、翻译同步和生产构建用于发布验收。

浏览器使用本地完整页面和模拟 API 数据验证：

- 桌面 1280×900 新增 `customer-alice` 并配置 `gemini-ultra: 0.8`，页面正确显示替代基础倍率 2。
- 点击“保存分组倍率”仅提交 `GroupGroupRatio`，新规则正确，原有 `vip` 倍率 1.5 保留。
- 手机 390×844 输入 `customer-bob` 后可直接点击添加，按钮在视口内，新组显示正常。

浏览器测试没有修改生产价格或用户归属。

## 发布与备份

发布版本：`20261020-special-ratio-groups`，工作分支：`codex/group-ratio-custom-users`。

从当前线上 `011dd5423` 构建，保留已有 Gemini Ultra 重试、日志多选和设备时间修复。逐个切换蓝绿实例，验证健康、版本、Caddy 恢复及 `.env` 字节不变。失败回滚到 `tokensheep-newapi:20261019-gemini-ultra-retry`，不回写旧环境文件或数据库。

- 项目备份：`.private/production-config/2026-10-20-special-ratio-groups/`。
- 项目外备份：`/Users/danlio/.config/tokensheep/production-backups/2026-10-20-special-ratio-groups/`。
- 服务器发行目录：`/data/new-api/releases/special-ratio-groups-20261020/`。
- 服务器配置备份：`/data/new-api/backups/special-ratio-groups-20261020/`。

提交、镜像、二进制哈希、公网静态资源校验、测试和浏览器截图保存在私有发布记录中。主仓库同步前比对基线并备份，翻译仅添加本次两条键，保留其他本地修改。
