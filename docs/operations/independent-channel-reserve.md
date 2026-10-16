# 独立容量渠道的有界备用重试

2026-10-17。配置项 `IndependentChannelFallback` 是 JSON；默认不存在即关闭。

```json
{"enabled":true,"groups":["gemini-lowprice","gemini-sale","gemini-stable"],"models":["gemini-3-flash","gemini-3.8-flash-high","gemini-3.8-flash","gemini-3.1-flash-lite","gemini-3.7-flash","gemini-3.6-flash"],"primary_channel_ids":[12,140],"reserve_channel_id":146}
```

仅当入口已授权的具体分组、原始模型和初选渠道均匹配时启用。保留初选的原有优先级/权重，重试依次选择未尝试的主渠道，最后备用渠道。最多两个主渠道加一个备用；备用失败即终止。单请求不重复渠道，不跨 auto 分组扩张，不新增跨模型替代，不扩大全局 RetryTimes。

明确配置的渠道被运营者声明为具有独立可用容量；因此允许额度 429 或长 Retry-After 后尝试另一池，保留原池退避，不等待其长 reset。短退避仍沿用现有抖动。原有状态重试白名单继续有效；400、取消、499、598、已提交响应、明确 skip-retry、强制指定渠道和实时 WebSocket 不开启这条额外重试链；原有 504/524 排除规则保持。

正常渠道选择器继续验证 enabled、分组、原始模型和请求接口。备用缺少某模型、被停用或不支持该接口时跳过，并保留最后一个真实上游错误。3.1 Pro、3.5 不在本次范围：146 尚无这些原始模型的能力记录。3.7/3.6 在 146 的既存映射指向 3.8-high，此次不改映射和价格。

该功能只提供请求内的备用链，不实现跨请求冷却/熔断或预测池余量。若内部池已耗尽但仍启用，每个新请求仍可能先遇到内部失败，再用备用。也不限制下游号池内部重试；最多三个是 new-api 渠道调用数。

部署前用 controller/service/model/operation_setting 回归测试核对额度、长退避、取消、已输出、预算、渠道去重、内存/数据库选择和模型权限。使用生产提交为基线，保留生产前端，进行蓝绿发布。生产配置启用前备份 option 原值和渠道非凭据字段。

回滚：将 IndependentChannelFallback 改为 `{"enabled":false}` 并等待两个实例配置同步，或按蓝绿流程回滚应用镜像；全局 RetryTimes 不变。不得为回滚此功能恢复旧的完整数据库、.env 或渠道 key。
