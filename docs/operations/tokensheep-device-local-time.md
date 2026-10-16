# 使用日志设备时间偏差排查（2026-10-16）

## 当前结论

用户设备约 19:40，日志列表显示约 11:40；“今天”显示前一天 16:00 至当天 15:59。生产日志原始 Unix 时间戳正确，截图中的记录能逐条对应；错误位于浏览器日期转换与显示。

实际出错日志页的管理员一次性诊断报告显示：

- `Intl` 返回 `UTC`，`getTimezoneOffset()` 返回 `0`。
- `Date.toString()` 却包含 `GMT+0800 (null)`，本地构造函数生成的当天零点为 `1792080000000`，对应上海 2026-10-16 00:00。
- 同一时间戳 `1792150830` 被原格式化器显示为 `2026-10-16 11:40:30`；设备 +08:00 下应为 `2026-10-16 19:40:30`。
- `Date` 和 `Date.prototype.getHours` 的函数文本不再包含原生函数标识。

后续 v3 报告拿到了实际 `Date` 构造函数前缀与 `getHours` 函数文本，两者均与本机 Chrome 已安装扩展 **Change Timezone for Google Chrome™ 3.0.0** 的 `data/inject-content.js` 逐字匹配。

- 扩展 ID：`cejckpjfigehbeecbbfhangbknpidcnh`。
- Chrome 的 `Secure Preferences` 记录首次安装及最后更新时间均为 **2026-10-16 02:04:33.973537 +08:00**。
- 扩展在所有网站 `document_start` 注入，将 `globalThis.Date` 替换为 `class extends ODate`，按 `Date.prefs` 偏移本地 getter，并覆盖 `Intl.DateTimeFormat`。
- 实际回传中的 `getHours` 返回 `getHours.apply(this.nd, args)`，与扩展源码一致；原生时间戳与部分本地构造语义仍被保留。

因此本次八小时时差的直接来源已定位为该时区扩展造成的不一致日期接口。安装记录与故障出现时间相符，能够解释昨天正常、今天偏移。没有修改、停用或删除扩展。

## 昨天到今天的改动

- `f5356ae2e`：日期选择立即查询、重置可重复执行、导航完成后刷新；默认截止时间由打开页面时加一小时改为当天结束。
- `3820739d4`：将 Today/Reset 等保存为相对范围，每次按设备日期解析，避免 URL 固定昨天。
- 上述两个提交没有修改 `formatTimestampToDate`、Day.js 全局配置或日期库依赖。日志格式化逻辑沿用 8 月已存在的实现。
- 筛选状态问题能定位到具体前端逻辑；八小时时差则已通过实际运行函数与本地扩展源码匹配定位到时区扩展，两者需要分开处理。

## 修复与验证

`52cc0b838103b86986f5800b000b29a6c7911e9f` 统一筛选日期字段和日志显示：读取该日期字符串保留的 GMT 偏移，再用 UTC 字段解析偏移后的日历值，避免混用被覆盖的本地 getters 与原生本地构造语义。没有固定 UTC+8，也没有改变日志的原始时间戳。

回归测试模拟诊断中观察到的行为：本地 getters 和 Intl 对应 UTC，但本地构造及字符串 GMT 偏移仍对应设备时区。修复前稳定复现 `11:40:30` 和前一天 `16:00`；修复后为 `19:40:30` 和当天 `00:00`。

- 上海日志模块 38 项通过。
- 洛杉矶、UTC 各 10 项日期回归通过；包含夏令时边界。
- 印度 UTC+5:30 的设备时间回归 2 项通过。
- TypeScript、受影响文件 lint、差异检查、生产前端构建和 Linux amd64 编译通过。
- 已发布最终版本 `20261016-device-local-time-final`，blue/green 均 healthy。公网日志模块 JS 的 SHA-256 与本地构建完全一致；入口 JS 仅有既存 Caddy 配置对系统名称字面量的 10 处替换，其余逐字相同。
- 生产 `.env` 发布前后字节一致。

**实际出错浏览器已验证：** 用户刷新后，v3 报告的最新日志 `1792152357` 显示为 `2026-10-16 20:05:57`，当天起点为 `2026-10-16 00:00:00`，查询起点仍为正确的 `1792080000000`。与此同时，被扩展替换的 `Intl` 仍返回 `12:05:57`，证明修复在扩展仍生效的环境里正确恢复设备时间。

独立 `/__clock-check-20261016` 页面已下线，其 Caddy 路由已删除，完整 Caddyfile 已精确恢复到诊断前版本。原始诊断页面保留在私有服务器备份中。

## 发布与备份

- 运行镜像：`tokensheep-newapi:20261016-721612d-device-local-time-final`
- 功能修复提交：`52cc0b838103b86986f5800b000b29a6c7911e9f`；诊断清理提交：`721612d650c49dd81c977d5dd7f4a87706fb8fc1`。
- 二进制 SHA-256：`82f0f5378d4a637b4d8ba6ff0cf708471a58e934279ac3d9f3b204bbd1b0c4c4`
- 隔离工作区：`/Users/danlio/.codex/worktrees/shared-verification-codes/tokensheep-newapi`
- 分支：`codex/log-time-range-reset`
- 项目备份：`.private/production-config/2026-10-16-device-local-time/`
- 项目外备份：`/Users/danlio/.config/tokensheep/production-backups/2026-10-16-device-local-time/`
- 服务器备份：`/data/new-api/backups/device-local-time-20261016/` 和 `/data/new-api/backups/device-local-time-final-20261016/`
- 发布材料：`/data/new-api/releases/device-local-time-final-721612d/`；验证版保存在 `/data/new-api/releases/device-local-time-52cc0b8/`。

主日志页一次性回传与独立诊断页均已清理；类型检查、lint、前端构建及 Go 编译通过，用户实际页验证与清理版发布已完成。最终版本为 `20261016-device-local-time-final`，完整发布文件保存在项目备份的 `final/`。实际功能修复文件与移除诊断后的日志表文件已逐一同步回主仓库，覆盖前确认与先前版本一致并备份，其他本地修改保留。

回滚仅回滚应用镜像，不恢复旧 `.env` 或数据库选项。重新发布继续采用蓝绿切换并检查环境文件不变。
