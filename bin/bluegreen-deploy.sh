#!/usr/bin/env bash
# bluegreen-deploy.sh — 通用切换式蓝绿部署脚本
#
# 用法:
#   bluegreen-deploy.sh <service-dir> <new-image-tag>
#   bluegreen-deploy.sh <service-dir> rollback
#   bluegreen-deploy.sh <service-dir> <new-image-tag> --dry-run
#
# 依赖 <service-dir>/.env 里的:
#   ACTIVE_COLOR=green
#   IMAGE_TAG_BLUE=<tag>
#   IMAGE_TAG_GREEN=<tag>
#   SERVICE_NAME=cli-proxy-api          # container 名前缀
#   HEALTH_ENDPOINT=http://localhost:8317/   # 探活地址
#   CADDYFILE=/data/caddy/Caddyfile       # caddy 配置路径
#   CADDY_CONTAINER=caddy                  # caddy 容器名
#   HEALTH_TIMEOUT_SEC=60                  # 等健康超时
#
# 详见 docs/ops/bluegreen-pattern.md

set -euo pipefail

SCRIPT_NAME="$(basename "$0")"
SERVICE_DIR="${1:-}"
ACTION="${2:-}"
DRY_RUN="${3:-}"

usage() {
    cat <<EOF
$SCRIPT_NAME — 通用切换式蓝绿部署

Usage:
  $SCRIPT_NAME <service-dir> <new-image-tag>
  $SCRIPT_NAME <service-dir> rollback
  $SCRIPT_NAME <service-dir> <new-image-tag> --dry-run

Examples:
  $SCRIPT_NAME /data/cli-proxy-api feat-plugin-quota-slot
  $SCRIPT_NAME /data/cli-proxy-api rollback
  $SCRIPT_NAME /data/cli-proxy-api v14-be439b044 --dry-run
EOF
    exit 1
}

if [[ -z "$SERVICE_DIR" || -z "$ACTION" ]]; then
    usage
fi

if [[ ! -d "$SERVICE_DIR" ]]; then
    echo "ERROR: service dir does not exist: $SERVICE_DIR" >&2
    exit 1
fi

ENV_FILE="$SERVICE_DIR/.env"
if [[ ! -f "$ENV_FILE" ]]; then
    echo "ERROR: .env not found: $ENV_FILE" >&2
    exit 1
fi

# shellcheck disable=SC1090
source "$ENV_FILE"

: "${ACTIVE_COLOR:?ACTIVE_COLOR not set in $ENV_FILE}"
: "${IMAGE_TAG_BLUE:?IMAGE_TAG_BLUE not set in $ENV_FILE}"
: "${IMAGE_TAG_GREEN:?IMAGE_TAG_GREEN not set in $ENV_FILE}"
: "${SERVICE_NAME:?SERVICE_NAME not set in $ENV_FILE}"
: "${HEALTH_ENDPOINT:?HEALTH_ENDPOINT not set in $ENV_FILE}"
: "${CADDYFILE:?CADDYFILE not set in $ENV_FILE}"
: "${CADDY_CONTAINER:?CADDY_CONTAINER not set in $ENV_FILE}"
: "${HEALTH_TIMEOUT_SEC:=60}"

is_dry_run=0
if [[ "$DRY_RUN" == "--dry-run" ]]; then
    is_dry_run=1
fi

log() { printf '[%s] %s\n' "$(date +'%H:%M:%S')" "$*"; }
run() {
    if [[ "$is_dry_run" -eq 1 ]]; then
        log "DRY-RUN: $*"
    else
        log "EXEC: $*"
        eval "$@"
    fi
}

# 判断 next color
if [[ "$ACTIVE_COLOR" == "green" ]]; then
    NEXT_COLOR="blue"
    OLD_COLOR="green"
else
    NEXT_COLOR="green"
    OLD_COLOR="blue"
fi

NEXT_CONTAINER="${SERVICE_NAME}-${NEXT_COLOR}"
OLD_CONTAINER="${SERVICE_NAME}-${OLD_COLOR}"

# 解析 rollback
if [[ "$ACTION" == "rollback" ]]; then
    # rollback = 反向切一次，用 OLD 的 tag
    NEW_TAG_VAR="IMAGE_TAG_${NEXT_COLOR^^}"
    NEW_TAG="${!NEW_TAG_VAR}"
    log "ROLLBACK mode: swap $OLD_COLOR ($ACTIVE_COLOR) -> $NEXT_COLOR, keep tag $NEW_TAG"
else
    NEW_TAG="$ACTION"
    log "DEPLOY mode: $ACTIVE_COLOR ($OLD_COLOR) -> $NEXT_COLOR with tag $NEW_TAG"
fi

log "Service dir      : $SERVICE_DIR"
log "Active color     : $ACTIVE_COLOR"
log "Next  color      : $NEXT_COLOR"
log "Old   container  : $OLD_CONTAINER"
log "Next  container  : $NEXT_CONTAINER"
log "New   image tag  : $NEW_TAG"
log "Health endpoint  : $HEALTH_ENDPOINT (inside $NEXT_CONTAINER)"
log "Caddyfile        : $CADDYFILE"
log "Caddy container  : $CADDY_CONTAINER"
log "Health timeout   : ${HEALTH_TIMEOUT_SEC}s"
log ""

cd "$SERVICE_DIR"

# Step 3: update .env with new tag
NEW_TAG_VAR="IMAGE_TAG_${NEXT_COLOR^^}"
if [[ "$ACTION" != "rollback" ]]; then
    log "Step 3: update .env $NEW_TAG_VAR=$NEW_TAG"
    run "sed -i.bak-$(date +%Y%m%d-%H%M%S) 's|^${NEW_TAG_VAR}=.*|${NEW_TAG_VAR}=${NEW_TAG}|' '$ENV_FILE'"
fi

# Step 4: pull new image
log "Step 4: pull new image for $NEXT_CONTAINER"
run "docker compose pull $NEXT_CONTAINER"

# Step 5: start standby container
log "Step 5: start $NEXT_CONTAINER"
run "docker compose up -d --force-recreate $NEXT_CONTAINER"

# Step 6: wait for healthy (探活从 caddy 容器外部发 HTTP 请求, 因为 CPA 镜像里没 wget/curl)
log "Step 6: wait for $NEXT_CONTAINER to be responsive (max ${HEALTH_TIMEOUT_SEC}s)"
if [[ "$is_dry_run" -eq 0 ]]; then
    # 从 endpoint 里推 port (host part), 如 http://localhost:8317/ -> 8317
    HEALTH_PORT=$(echo "$HEALTH_ENDPOINT" | sed -nE 's|^https?://[^:]+:([0-9]+)/?.*|\1|p')
    HEALTH_PATH=$(echo "$HEALTH_ENDPOINT" | sed -nE 's|^https?://[^/]+(/.*)$|\1|p')
    HEALTH_PORT=${HEALTH_PORT:-8317}
    HEALTH_PATH=${HEALTH_PATH:-/}
    deadline=$(( $(date +%s) + HEALTH_TIMEOUT_SEC ))
    while :; do
        # 用 caddy 容器 exec wget 访问 <NEXT_CONTAINER>:PORT (station_internal 网络里)
        if docker exec "$CADDY_CONTAINER" wget --timeout=3 --spider -q "http://${NEXT_CONTAINER}:${HEALTH_PORT}${HEALTH_PATH}" 2>/dev/null; then
            log "  -> $NEXT_CONTAINER responsive on ${HEALTH_PORT}${HEALTH_PATH}"
            break
        fi
        if [[ $(date +%s) -ge $deadline ]]; then
            log "ERROR: $NEXT_CONTAINER did not become responsive on port $HEALTH_PORT within ${HEALTH_TIMEOUT_SEC}s"
            log "Aborting. $OLD_CONTAINER still active."
            exit 2
        fi
        printf '.'
        sleep 2
    done
fi

# Step 7: switch Caddy upstream
# NOTE: caddy reload 对 reverse_proxy upstream 变更不敏感 (2026-08-28 实测)。
# 老 upstream 名字保留在内部 pool 里, 新请求继续路由到旧地址返回 503。
# 必须整个 docker restart caddy 才能真正生效。
log "Step 7: switch Caddy upstream $OLD_CONTAINER -> $NEXT_CONTAINER in $CADDYFILE"
run "sed -i.bak-$(date +%Y%m%d-%H%M%S) 's|${OLD_CONTAINER}:|${NEXT_CONTAINER}:|g' '$CADDYFILE'"
run "docker restart $CADDY_CONTAINER"

# Step 8: stop old container
log "Step 8: stop old $OLD_CONTAINER (in-flight requests still complete on their own connection)"
run "docker compose stop $OLD_CONTAINER"

# Step 9: flip ACTIVE_COLOR
log "Step 9: update .env ACTIVE_COLOR=$NEXT_COLOR"
run "sed -i 's|^ACTIVE_COLOR=.*|ACTIVE_COLOR=${NEXT_COLOR}|' '$ENV_FILE'"

log ""
log "DONE. $NEXT_CONTAINER is now active. Old $OLD_CONTAINER stopped (kept for quick rollback)."
log "Rollback: $SCRIPT_NAME $SERVICE_DIR rollback"
