# 全局每日免费额度池（饥饿机制）

## 状态：待实现

## 背景

TokenSheep 贡献等级的核心区分点是"薅羊毛有限度"——贡献档用户每天能白嫖的量有上限，Standard 用户按量付费不受限。
当前每个用户有独立的每日赠送（quota_gift 签到），但缺少一个**全局总池子**来制造稀缺感。

## 目标

增加一个**全站共享的每日免费额度池**：
- 每天定时（如 0 点）重置到固定总额（运营可配）
- 用户请求扣费时，如果走的是 quota_gift（赠送余额），从全局池里扣减
- 池子耗尽 → 当天所有贡献档的 gift 消费暂停，只能用 quota_paid（自付）
- Standard 用户不受此池影响（只走 quota_paid）

## 用户体验

- 前端展示"今日社区免费池剩余：$XX / $总额"
- 池子快耗尽时制造紧迫感（进度条变色/提示）
- 每天定时重置 → 大家来"抢"当天免费额度

## 效果

- **稀缺感**：免费不是无限的，先到先得
- **区分 Standard**：Standard 永远不受池子限制 = 不用抢
- **控制成本**：运营可通过调总额精确控制每日免费放出量

## 技术方案（待设计）

### 后端
- 新增 `daily_global_pool` 配置项（后台可改总额）
- Redis atomic counter：`DECR tokensheep:daily_pool:{date}` 每次 gift 扣费时减
- 定时任务（0 点）重置 counter = 配置总额
- 请求中间件/扣费逻辑：gift 扣费前检查池子余量，不够则拒绝 gift 消费（返回提示）
- 不影响 quota_paid 扣费路径

### 前端
- `/api/status` 或独立接口暴露 `daily_pool_remaining` / `daily_pool_total`
- Overview / 钱包页展示进度条
- 池子为 0 时提示"今日免费额度已用完，明天再来或使用自付余额"

### 配置
- `tokensheep_setting.DailyGlobalPoolTotal`（运营后台可调）
- `tokensheep_setting.DailyGlobalPoolResetHour`（重置时间，默认 0 点）

## 关联

- economy-model.md §2/§4
- 注册引导领取兑换券（独立功能）
- Standard 通道开通（已就绪，待配倍率）
