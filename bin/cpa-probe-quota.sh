#!/usr/bin/env bash
# cpa-probe-quota.sh —— 只读探针脚本
# 遍历 auths/*.json, 对每个 (含 disabled) 号做:
#   1. 用 refresh_token 换新 access_token (Google OAuth)
#   2. 用新 access_token 打一个 loadCodeAssist 请求
#   3. 记录响应: 200 (号活着) / 401 (auth 失效) / 429 (quota 满) / 5xx (Google 问题)
# 不改任何 auth 文件, 不改 disabled 字段。只报告。
#
# 用法:
#   bin/cpa-probe-quota.sh [--only-disabled] [--limit N]
#
# 输出 CSV: email,disabled,http_status,quota_reset_hint,note
# 用户自己拿这个 CSV 决定哪些改 disabled: false

set -euo pipefail

AUTHS_DIR="${AUTHS_DIR:-/data/cli-proxy-api/auths}"
CLIENT_ID="${CLIENT_ID:-1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com}"
CLIENT_SECRET="${CLIENT_SECRET:-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}"
API_ENDPOINT="${API_ENDPOINT:-https://cloudcode-pa.googleapis.com}"
API_VERSION="${API_VERSION:-v1internal}"
ONLY_DISABLED=0
LIMIT=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --only-disabled) ONLY_DISABLED=1; shift ;;
        --limit) LIMIT="$2"; shift 2 ;;
        --auths-dir) AUTHS_DIR="$2"; shift 2 ;;
        -h|--help)
            head -20 "$0" | sed 's/^# \?//'
            exit 0 ;;
        *) echo "unknown arg: $1" >&2; exit 1 ;;
    esac
done

if ! command -v jq >/dev/null 2>&1; then
    echo "jq required" >&2
    exit 1
fi

echo "email,disabled,http_status,quota_reset_hint,note"

count=0
for f in "$AUTHS_DIR"/*.json; do
    [[ -f "$f" ]] || continue
    disabled=$(jq -r '.disabled // false' "$f" 2>/dev/null)
    if [[ "$ONLY_DISABLED" -eq 1 && "$disabled" != "true" ]]; then
        continue
    fi
    if [[ "$LIMIT" -gt 0 && "$count" -ge "$LIMIT" ]]; then
        break
    fi
    count=$((count + 1))

    email=$(jq -r '.email // ""' "$f")
    refresh_token=$(jq -r '.refresh_token // ""' "$f")
    if [[ -z "$refresh_token" || "$refresh_token" == "null" ]]; then
        echo "$email,$disabled,,,no-refresh-token"
        continue
    fi

    # Step 1: refresh access token
    refresh_resp=$(curl -sS -X POST "https://oauth2.googleapis.com/token" \
        --max-time 10 \
        -d "client_id=$CLIENT_ID" \
        -d "client_secret=$CLIENT_SECRET" \
        -d "refresh_token=$refresh_token" \
        -d "grant_type=refresh_token" 2>&1) || {
        echo "$email,$disabled,,,refresh-curl-failed"
        continue
    }
    access_token=$(echo "$refresh_resp" | jq -r '.access_token // ""' 2>/dev/null)
    if [[ -z "$access_token" || "$access_token" == "null" ]]; then
        err=$(echo "$refresh_resp" | jq -r '.error // .error_description // "unknown"' 2>/dev/null | head -c 80)
        echo "$email,$disabled,,,refresh-failed:$err"
        continue
    fi

    # Step 2: 真调 generateContent 探当前 quota (loadCodeAssist 只告状态, 不告 quota).
    # 用 minimal payload + 最便宜的 flash. project_id header 必须, 否则 403 license error.
    project_id=$(jq -r '.project_id // ""' "$f")
    if [[ -z "$project_id" || "$project_id" == "null" ]]; then
        echo "$email,$disabled,,,no-project-id"
        continue
    fi
    body='{"model":"models/gemini-3-flash","project":"'"$project_id"'","request":{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}}'
    resp_file=$(mktemp)
    http_status=$(curl -sS -o "$resp_file" -w "%{http_code}" -X POST \
        "$API_ENDPOINT/$API_VERSION:generateContent" \
        --max-time 30 \
        -H "Authorization: Bearer $access_token" \
        -H "Content-Type: application/json" \
        -H "X-Goog-User-Project: $project_id" \
        -H "User-Agent: gemini-cli/0.1.5 (linux; x64)" \
        -d "$body" 2>/dev/null) || http_status=""

    # Step 3: 分类响应
    quota_hint=""
    note=""
    if [[ -f "$resp_file" ]]; then
        body_text=$(cat "$resp_file")
        case "$http_status" in
            200)
                # 成功 = 号 quota 未满, 可以 enable
                quota_hint="OK"
                note="活着-可以enable"
                ;;
            429)
                # quota 满 - 抽 Resets in 提示
                rst=$(echo "$body_text" | jq -r '.error.message // ""' 2>/dev/null | grep -oE "Resets in[^.]*" | head -1)
                quota_hint="429"
                note="${rst:-quota-exhausted}"
                ;;
            401|403)
                err=$(echo "$body_text" | jq -r '.error.message // .error.status // ""' 2>/dev/null | head -c 60)
                quota_hint="auth-fail"
                note="$err"
                ;;
            *)
                err=$(echo "$body_text" | jq -r '.error.message // ""' 2>/dev/null | head -c 80 | tr ',\n' '  ')
                quota_hint="$http_status"
                note="$err"
                ;;
        esac
        rm -f "$resp_file"
    fi

    echo "$email,$disabled,$http_status,$quota_hint,$note"
done
