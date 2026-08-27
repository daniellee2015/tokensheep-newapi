# 蓝绿部署统一规范

**范围**：VPS196 (cli-proxy-api, kiro-rs)、VPS22 (new-api)

**决策日期**：2026-08-28

**背景**：2026-08-27 CPA 硬重启部署造成 72 次 502（in-flight stream 被打断）。事后确认单机双活对有状态服务不友好（auth token race），而跨机房冗灾已解决整机故障；因此单机内统一采用**切换式蓝绿**。

---

## 一、模式选择

| 服务 | 状态 | 单机蓝绿模式 |
|---|---|---|
| new-api | 无状态（session 走 redis） | 切换式（与 CPA/kiro 对齐） |
| cli-proxy-api | 有状态（auth token/cooldown 内存态） | **切换式** |
| kiro-rs | 有状态（auth） | 切换式（已有） |

**为什么全部切换式**：
- 跨机房已经保障整机可用性
- 切换式无 race，任何时刻只有一个实例写状态
- 单机资源开销减半（相较双活）
- 部署时新版本 pre-warm 好在旁边，切换瞬时
- 三服务同构，运营 mental model 一致

---

## 二、命名约定

### 2.1 容器名

- `<service>-blue` + `<service>-green`（对称）
- 例：`cli-proxy-api-blue` / `cli-proxy-api-green`
- 旧的 `<service>`（无后缀）名字**弃用**，一次性改造

### 2.2 Image tag

- **不用 `:blue` / `:green` 作 image tag**（避免既是"位置"又是"内容"的双语义）
- 只使用**真实版本号 tag**：`v14-be439b044`、`feat-plugin-quota-slot`、`20260827-abc1234`
- 蓝绿容器**只是位置**，具体跑什么版本由 `.env` 文件决定

### 2.3 状态存储

`<service-dir>/.env`（不在 git，写在 VPS 本地）：

```dotenv
ACTIVE_COLOR=green
IMAGE_TAG_BLUE=v14-be439b044
IMAGE_TAG_GREEN=v14-be439b044
```

- `ACTIVE_COLOR` 记录当前 active 的颜色
- 部署脚本读它决定"新版本推到哪个容器"
- caddy Caddyfile 硬编码指向 active 容器名（Caddy 变量支持有限，用固定名字更简单，切换时改 Caddyfile + reload）

---

## 三、Compose 模板

```yaml
services:
  # ===== active（初始 green） =====
  cli-proxy-api-green:
    image: ghcr.io/daniellee2015/cli-proxy-api:${IMAGE_TAG_GREEN}
    container_name: cli-proxy-api-green
    restart: unless-stopped        # active 一直跑
    environment:
      - COLOR=green
      - TZ=Asia/Shanghai
    volumes:
      - ./config.yaml:/CLIProxyAPI/config.yaml:ro
      - ./auths:/root/.cli-proxy-api
      - ./logs/green:/CLIProxyAPI/logs
      - ./plugins:/CLIProxyAPI/plugins:ro
      - ./static:/CLIProxyAPI/static:ro
    networks: [station_internal]
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8317/"]
      interval: 10s
      timeout: 3s
      retries: 3
      start_period: 30s
    deploy:
      resources:
        limits:
          memory: 4G

  # ===== standby（初始 blue） =====
  cli-proxy-api-blue:
    image: ghcr.io/daniellee2015/cli-proxy-api:${IMAGE_TAG_BLUE}
    container_name: cli-proxy-api-blue
    restart: "no"                  # 手动 up，平时不跑
    environment:
      - COLOR=blue
      - TZ=Asia/Shanghai
    volumes:
      - ./config.yaml:/CLIProxyAPI/config.yaml:ro
      - ./auths:/root/.cli-proxy-api
      - ./logs/blue:/CLIProxyAPI/logs
      - ./plugins:/CLIProxyAPI/plugins:ro
      - ./static:/CLIProxyAPI/static:ro
    networks: [station_internal]
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8317/"]
      interval: 10s
      timeout: 3s
      retries: 3
      start_period: 30s
    deploy:
      resources:
        limits:
          memory: 4G

networks:
  station_internal:
    external: true
```

**共享 volumes**：
- `config.yaml`：只读，两边一致
- `auths/`：读写，**但只有 active 会写**（standby 平时不接流量、不 refresh），无 race
- `plugins/`、`static/`：只读
- `logs/`：按颜色分子目录，各自独立

---

## 四、Caddy 配置

```caddyfile
cpa.muxpay.xyz {
    import streaming_proxy cli-proxy-api-green:8317 /
    ...
}
```

**切换时**：改 `cli-proxy-api-green` ↔ `cli-proxy-api-blue`，然后 `caddy reload`。

Caddy 变量方案（未采用理由）：
- `import` 不支持 env 变量
- 用 templates 会让配置复杂
- 手动改一行 + reload 更直白，脚本能做

---

## 五、部署脚本

`bin/bluegreen-deploy.sh`：

```
usage: bluegreen-deploy.sh <service> <new-image-tag>
```

**通用流程**（9 步）：

1. **读取当前 active** 从 `<service-dir>/.env` → `ACTIVE_COLOR`
2. **确定 next**（active 的相反色）
3. **更新 .env**：`IMAGE_TAG_<NEXT>=<new-image-tag>`
4. **docker compose pull `<service>-<next>`**（拉新镜像）
5. **docker compose up -d `<service>-<next>`**（启动 standby）
6. **等健康**：轮询 `docker inspect <service>-<next> --format '{{.State.Health.Status}}'` 直到 healthy（超时 60s）
7. **切 Caddy**：sed 改 Caddyfile + `docker exec caddy caddy reload --config /etc/caddy/Caddyfile`
8. **停止旧 active**：`docker compose stop <service>-<active>`
9. **更新 .env**：`ACTIVE_COLOR=<next>`

**回滚**：`bluegreen-deploy.sh <service> rollback` → 拿到旧 `.env` 里的 tag，再走一次流程

**Dry-run**：`bluegreen-deploy.sh <service> <tag> --dry-run` → 只打印动作，不执行

---

## 六、Sidecar 处理

如果服务有 sidecar（如 `cursor-chromium-sidecar` 绑到 CPA network namespace）：

**当前问题**：sidecar 用 `network_mode: service:cli-proxy-api`，硬绑一个容器名。蓝绿后 sidecar 只能跟一个。

**方案**：
- **A（简单）**：sidecar 只跟 active，切换时 sidecar 也一起换（compose depends_on: active）
- **B（推荐）**：sidecar 独立网络，暴露 `127.0.0.1:18901`，两个 CPA 都通过 host 网访问

选 B。改动：

```yaml
cursor-chromium-sidecar:
  network_mode: bridge          # 不再绑 service
  ports:
    - "127.0.0.1:18901:18901"   # 暴露到 host
  container_name: cursor-chromium-sidecar
  ...

cli-proxy-api-green:
  extra_hosts:
    - "host.docker.internal:host-gateway"
  environment:
    - CURSOR_CHROMIUM_SIDECAR_URL=http://host.docker.internal:18901
```

（CPA blue 同上）

---

## 七、部署 Checklist

**部署前**：
- [ ] 新 image tag 已推到 GHCR 并可拉取
- [ ] 检查 `.env` 里 `ACTIVE_COLOR` 是否正确
- [ ] 通知运营（如有）：可能有短暂 handoff 窗口

**部署中**：
- [ ] 运行 `bin/bluegreen-deploy.sh <service> <tag>`
- [ ] 观察 next 容器 healthy 状态
- [ ] Caddy reload 后立即抓 5-10s 请求日志验证

**部署后**：
- [ ] `docker ps` 确认 active 是新颜色、正常运行
- [ ] 前 5 分钟 monitor 错误率
- [ ] 如异常立即回滚（`bluegreen-deploy.sh <service> rollback`）

---

## 八、回滚 Checklist

**触发条件**（任一）：
- 5 分钟内错误率 > 3%
- new-api / caddy 出现新的 5xx 洪水
- 客户端投诉

**回滚步骤**（脚本化）：
1. `bin/bluegreen-deploy.sh <service> rollback` → 反向再切一次
2. 更新 `.env`：`IMAGE_TAG_<current-active>` 保持旧值
3. 事后 review：为什么新版本失败

---

## 九、每个服务的具体位置

| 服务 | 目录 | Compose 文件 | .env 位置 | Caddyfile 引用 |
|---|---|---|---|---|
| cli-proxy-api | `/data/cli-proxy-api` (VPS196) | `docker-compose.yaml` | `/data/cli-proxy-api/.env` | `cpa.muxpay.xyz` block |
| kiro-rs | `/data/kiro-rs-ultra` (VPS196) | `docker-compose.yaml` | `/data/kiro-rs-ultra/.env` | `k2a.muxpay.xyz` block |
| new-api | `/data/new-api` (VPS22) | `docker-compose.yml` | `/data/new-api/.env` | VPS22 caddy new-api block |

---

## 十、迁移顺序

**2026-08-28 定的迁移优先级**：

1. **CPA (VPS196)** —— 今天做，因为已经出问题
2. **kiro-rs (VPS196)** —— 已经蓝绿，只需按本文档补齐 healthcheck/命名/部署脚本
3. **new-api (VPS22)** —— 从双活收敛到切换式（当前 blue+green 都跑）

跨机房冗灾方案另行规划（`docs/ops/multi-region-failover.md` TBD）。

---

## 十一、已知问题与后续

1. **auth token race**（CPA 特有）：切换的 handoff window（新版本 healthy → caddy reload → 旧版本 stop）大概 10-15s，这段时间两个容器都可能接请求。**风险接受**：CPA 的 refresh 都是短暂原子操作，撞概率极低；真出问题再引入文件锁
2. **cooldown 分裂**（CPA 特有）：handoff window 里冷备容器的内存 cooldown 是空的，切换后新 active 遇到已在 cooldown 的号会重新试一次。**影响很小**：最多多打一次 429，CPA 自己会 mark
3. **caddy 变量支持**：Caddyfile 不支持 env 变量在 `import` 里生效，切换要 sed。**接受**：脚本化后不出错
4. **跨机房 auth token 共享**：两台 VPS 的 CPA 用同一批号还是各半，另开决策
