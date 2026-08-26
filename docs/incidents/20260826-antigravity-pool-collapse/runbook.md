# 修复 Runbook

**执行原则**：
- 每 Phase 独立完成并观察验证后再进下一个
- 每一步先备份，改动前记录基线，改动后对比
- 每一步都有明确回滚路径
- 生产改动前需人工确认

---

## Phase 0 — 前置准备（10 分钟）

**目的**：建立可回滚基线 + 抓取修改前的关键指标基线。

```bash
mkdir -p ~/incident-20260826 && cd ~/incident-20260826

# 1. 备份 CPA 配置
ssh vps196 'docker exec cli-proxy-api cat /CLIProxyAPI/config.yaml' > cpa-config.baseline.yaml

# 2. 备份 new-api options
ssh vps22 'docker exec newapi-postgres psql -U newapi -d newapi -c \
  "SELECT key, value FROM options WHERE key ILIKE '"'"'%RateLimit%'"'"' OR key='"'"'RetryTimes'"'"';"' \
  > newapi-options.baseline.txt

# 3. 基线错误率
ssh vps22 'docker exec newapi-postgres psql -U newapi -d newapi -c \
  "SELECT model_name, count(*) FROM logs WHERE type=5 \
   AND created_at > extract(epoch from now())::bigint - 3600 \
   GROUP BY model_name ORDER BY 2 DESC LIMIT 10;"' \
  > baseline-errors.txt

# 4. CPA 侧基线放大倍数
ssh vps196 'docker exec cli-proxy-api sh -c "ls /CLIProxyAPI/logs/*.log 2>/dev/null | tail -3 | \
  xargs -I{} grep -c \"=== API REQUEST\" {}"' > baseline-amplification.txt

# 5. 号池当前可用状态
ssh vps196 'docker exec cli-proxy-api ls /root/.cli-proxy-api/ | grep -c "antigravity-.*json"' \
  > baseline-account-count.txt
```

**判据**：5 个基线文件都非空且内容合理。

---

## Phase 1 — CPA config 修复（今晚，30 分钟内见效）

### 1.1 备份

```bash
ssh vps196 'docker exec cli-proxy-api cp /CLIProxyAPI/config.yaml \
  /CLIProxyAPI/config.yaml.bak-$(date +%Y%m%d-%H%M%S)'
ssh vps196 'docker exec cli-proxy-api ls -la /CLIProxyAPI/config.yaml*'
```

### 1.2 逐步改动（每一步 reload + 观察 5-10 分钟后再进下一步）

**步骤 1 — 关闭 `switch-preview-model`（最大杠杆）**

```bash
ssh vps196 'docker exec cli-proxy-api sed -i \
  "s/switch-preview-model: true/switch-preview-model: false/" \
  /CLIProxyAPI/config.yaml'
ssh vps196 'docker exec cli-proxy-api grep "switch-preview-model" /CLIProxyAPI/config.yaml'
ssh vps196 'docker restart cli-proxy-api'
```

**判据**：观察 10 分钟内，新 CPA error log 中每账号被打次数从 8 的倍数
（8/16/24）变为 4 的倍数（4/8/12）。

**步骤 2 — 关闭 `switch-project`**

```bash
ssh vps196 'docker exec cli-proxy-api sed -i \
  "s/switch-project: true/switch-project: false/" \
  /CLIProxyAPI/config.yaml'
ssh vps196 'docker restart cli-proxy-api'
```

**判据**：每账号次数从 4 的倍数变为 2 的倍数。

**步骤 3 — `request-retry: 3 → 1`**

```bash
ssh vps196 'docker exec cli-proxy-api sed -i \
  "s/^request-retry: 3/request-retry: 1/" \
  /CLIProxyAPI/config.yaml'
ssh vps196 'docker restart cli-proxy-api'
```

**判据**：单次下游请求上游调用总数 ≤ 4 次（比 32-64 降 8-16 倍）。

**步骤 4 — `antigravity-credits`（需要决策）**

- 号池是**免费号**（无 credit） → 关闭：
  ```bash
  ssh vps196 'docker exec cli-proxy-api sed -i \
    "s/antigravity-credits: true/antigravity-credits: false/" \
    /CLIProxyAPI/config.yaml'
  ssh vps196 'docker restart cli-proxy-api'
  ```
- 号池是**付费号**（有独立 credit 池）→ 保留 `true`

### 1.3 Phase 1 完成判据

- 单次下游请求 → 上游调用 ≤ 4 次
- 每小时 429 错误从 147 降到 <50
- kuma 上 Gemini Pro / GPT 通道探针间歇性恢复 200 OK
- CPA `/CLIProxyAPI/logs/` 新生成 error log 速率下降 ≥ 50%

### 1.4 回滚

任何一步 5 分钟内成功率暴跌超过基线的 50% → 立即回滚：

```bash
LATEST_BAK=$(ssh vps196 'docker exec cli-proxy-api ls -t /CLIProxyAPI/config.yaml.bak-* | head -1')
ssh vps196 "docker exec cli-proxy-api cp $LATEST_BAK /CLIProxyAPI/config.yaml && docker restart cli-proxy-api"
```

---

## Phase 2 — new-api relay context + timeout（明天，2-4 小时）

**前提**：Phase 1 已完成且稳定运行 ≥ 2 小时。

### 2.1 代码改动

**A. `service/http_client.go`** — 加 `ResponseHeaderTimeout`

定位：
```bash
grep -n "http.Client\|Transport:" service/http_client.go
```

在 `&http.Transport{...}` 中加入：
```go
ResponseHeaderTimeout: 120 * time.Second,
```

**B. `relay/channel/api_request.go`** — context 传播

定位所有 `http.NewRequest` 调用：
```bash
grep -n "http.NewRequest\b" relay/channel/api_request.go
```

每一处改为：
```go
req, err := http.NewRequestWithContext(c.Request.Context(), method, url, body)
```

如函数签名未持有 `c *gin.Context`，向上追溯调用链把 context 传下来。

**C. 覆盖检查**：
```bash
grep -rn "http.NewRequest\b" relay/ --include='*.go' | grep -v _test
```
`relay/channel/` 下所有 `http.NewRequest` 都应改为带 context 的版本。

### 2.2 编译与测试

```bash
cd ~/Repositories/tokensheep-newapi
go build ./... 2>&1 | head -30

# relaykit 独立模块验证（AGENTS.md 要求）
cd relaykit && GOWORK=off go build ./... 2>&1 | head
cd ..

# 相关子模块测试
go test ./relay/... ./service/... ./middleware/... 2>&1 | tail -20
```

**判据**：build 通过，测试无新增失败。

### 2.3 打镜像

```bash
NEW_SHA=$(git rev-parse --short HEAD)
docker build -t tokensheep-newapi:v12-${NEW_SHA} .
docker save tokensheep-newapi:v12-${NEW_SHA} | ssh vps22 'docker load'
```

### 2.4 蓝绿分流部署

**只升级 blue，green 保留旧版本作为回滚池**：

```bash
ssh vps22 'docker stop new-api-blue'
# 修改 docker-compose 里 new-api-blue 的 image tag 为 v12-<sha>
ssh vps22 'cd <compose-dir> && docker compose up -d new-api-blue'
ssh vps22 'docker ps --filter name=new-api-blue --format "{{.Status}}"'
# 等待 healthy
```

### 2.5 观察 30 分钟（blue vs green 对比）

```bash
# blue 侧 context canceled 数量（应该上升 — 说明主动切断上游）
ssh vps22 'docker logs --since 30m new-api-blue 2>&1 | grep -cE "context canceled|context deadline exceeded"'

# green 侧对比
ssh vps22 'docker logs --since 30m new-api-green 2>&1 | grep -cE "context canceled|context deadline exceeded"'

# caddy 侧挂死请求数量（应该骤降）
ssh vps196 'docker logs --since 30m caddy 2>&1 | grep -c "aborting with incomplete response"'
```

**成功判据**：
- blue 的 `context canceled` **上升**（好事，说明主动切断上游）
- caddy 侧 duration > 180 秒的请求消失
- blue 的 5xx 率 ≤ green
- 客户端投诉不增加

### 2.6 全量升级

blue 稳定 2 小时后，同样方式升级 green。

### 2.7 回滚

```bash
ssh vps22 'docker stop new-api-blue'
# 恢复 image tag 到 v11-2f83c646
ssh vps22 'cd <compose-dir> && docker compose up -d new-api-blue'
```

---

## Phase 3 — new-api 限流 bug 修复（本周内）

**前提**：Phase 1+2 已稳定运行 ≥ 24 小时。

### 3.1 探针 token 迁走（DB-only，零代码）

```sql
-- 建探针专用用户
INSERT INTO users (username, password, role, "group", quota)
VALUES ('probe-monitor', '<bcrypt-hash>', 1, 'monitor', 999999999);

-- 记录新 user_id 后迁移 token
UPDATE tokens SET user_id = <新id> WHERE name LIKE 'monitor-%';
```

**判据**：kuma 上 `monitor-*` 探针不再被生产流量影响。

### 3.2 Redis key 加 tokenId 维度

**文件**：`middleware/model-rate-limit.go:86, 100`

```go
tokenId := strconv.Itoa(c.GetInt("token_id"))
successKey := fmt.Sprintf("rateLimit:%s:%s:%s",
    ModelRequestRateLimitSuccessCountMark, userId, tokenId)
totalKey := fmt.Sprintf("rateLimit:%s:%s", userId, tokenId)
```

现有 Redis key 作废，自然过期即可。

### 3.3 限流拒绝写日志（type=6）

在 `middleware/model-rate-limit.go:94, 118` 的 `abortWithOpenAiMessage`
调用前加：

```go
gopool.Go(func() {
    model.RecordRateLimitedLog(
        c.GetInt("id"),
        c.GetInt("token_id"),
        c.GetString("token_name"),
        c.Request.URL.Path,
    )
})
```

同时在 `model/log.go` 新增 `RecordRateLimitedLog` 函数，并新增日志类型
常量（如 `LogTypeRateLimited = 6`）。

### 3.4 修复 `setting/rate_limit.go` 并发 bug

**位置**：`setting/rate_limit.go:30-36`

```go
func UpdateModelRequestRateLimitGroupByJSONString(jsonStr string) error {
    newMap := make(map[string][2]int)
    if err := json.Unmarshal([]byte(jsonStr), &newMap); err != nil {
        return err
    }
    ModelRequestRateLimitMutex.Lock()
    defer ModelRequestRateLimitMutex.Unlock()
    ModelRequestRateLimitGroup = newMap
    return nil
}
```

先 unmarshal 到临时 map 再原子替换，写锁改为 `Lock()`。

### 3.5 部署

3.1 独立执行 → 3.2/3.3/3.4 同一次发版，蓝绿分流部署（同 Phase 2）。

### 3.6 Phase 3 完成判据

- kuma 上 `monitor-*` 探针 100% 稳定
- 同一用户不同 token 的限流互不影响（手动验证）
- Redis 内出现 `rateLimit:MRRLS:<user>:<token>` 新格式 key
- logs 表出现 type=6 记录

---

## Phase 4 — 结构性优化（下周开始）

### 4.1 Gemini 官方 API backup channel

在 new-api 后台配置多优先级 fallback：

```
channel: cpa-antigravity-official     priority: 110
channel: gemini-official-fallback     priority: 100   # 新建
```

Priority 110 命中失败/超时后自动 fallback 到 100。

**成本估算**（决策依据）：Gemini flash $0.075/1M input + $0.30/1M output。
按日流量 100k 请求 × 平均 5k tokens = 500M tokens/天 ≈ $37/天，$1100/月。

### 4.2 单模型限流（防单用户滥用）

**方案 A**（快，DB-only）：给嫌疑 token 加 `rpm_limit`

```sql
UPDATE tokens SET rpm_limit = 60 WHERE id IN (...);
```

**方案 B**（结构性）：`middleware/model-rate-limit.go` 增加按
`(userId, tokenId, model)` 三维限流。需要新版发布。

### 4.3 请求体大小差异化配额

在 new-api relay 入口检查 `Content-Length`：
- \> 5MB → 计入 5× 配额
- \> 10MB → 计入 10× 配额，且只允许 priority ≤ 100 的号池处理

避免误杀合法长上下文用户，同时让越狱大 prompt 承担应有成本。

### 4.4 加账号

见 [`account-sizing.md`](./account-sizing.md)（Phase 1 观察后补写）
计算理想号池规模。

---

## 全 Phase 完成后的预期状态

| 指标 | 事故期间 | Phase 1 后 | Phase 2 后 | Phase 3 后 |
|---|---|---|---|---|
| 单下游 → 上游放大倍数 | 32~64× | 2~4× | 2~4× | 2~4× |
| 单请求最大挂时长 | 25~53min | 25~53min | ≤2min | ≤2min |
| 号池扫穿速度 | 12s | 3-5min | 3-5min | 5-10min |
| new-api 429/小时 | 147 | <50 | <30 | <20 |
| kuma 探针稳定性 | 40% | 70% | 90% | 100% |
| 「量不进记录」现象 | 存在 | 存在 | 存在 | 消失 |
