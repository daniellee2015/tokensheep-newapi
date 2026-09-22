#!/usr/bin/env python3
"""cpa-daemon-v4.py — CPA antigravity 号池自动管理 v4

与 v3 的根本区别: v4 读 Google 的真实 quota 桶数据做决策, 不再"试探性 enable"。

数据来源:
    POST /v0/management/api-call  (CPA 转发)
      → POST https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary
      → 必须 User-Agent: antigravity/hub/<ver> darwin/arm64 (aidev_client 会 403)

返回结构:
    groups[].buckets[] 每个 bucket 有:
      bucketId: gemini-weekly | gemini-5h | 3p-weekly | 3p-5h
      remainingFraction: 0.0 ~ 1.0
      resetTime: RFC3339
      window: weekly | 5h

桶层级 (Google 响应体自述, 2026-08-30 实测):
    两个独立 group, 各有自己的 weekly + 5h, 互不影响:
      group "Gemini Models"        (Gemini Flash, Gemini Pro)  → gemini-weekly, gemini-5h
      group "Claude and GPT models" (Claude Opus/Sonnet, GPT-OSS) → 3p-weekly, 3p-5h
    weekly 之上没有更高层级的桶。Google 原文:
      "The 5-hour limit smooths out aggregate demand ... while your weekly
       limit is tied directly to your individual tier."
    即 weekly 是天花板(绑账号 tier), 5h 只是削峰, 且 5h 是从 weekly 里扣的
    —— 反复打空 5h 会把 weekly 抽干, 那之后到 weekly reset 前这个号在该 group
    上就废了。

决策规则:
    gemini_weekly <= WEEKLY_EXHAUSTED_THRESHOLD  → 必须 disable
    gemini_5h <= FIVE_HOUR_EXHAUSTED_THRESHOLD   → 暂时 disable
    gemini_weekly >= WEEKLY_HEALTHY_THRESHOLD 且 gemini_5h >= FIVE_HOUR_HEALTHY
                                                → 仅自动恢复 daemon 自己关闭的账号
    中间区间                                       → 保持现状 (不动)
    quota 查询失败或数据无效                      → 保持当前状态

    management UI 手动关闭的账号不属于 daemon 管理状态, 无论剩余额度多少都保持
    disabled。daemon 只会把自己因 quota 耗尽而关闭的账号写入持久化状态文件,
    并在额度恢复后重新开启它们。

gemini-5h 使用关闭/恢复滞回:
  - 5h 桶真正耗尽时主动关号, 避免 CPA 在并发下继续把请求分配给必然 429 的账号。
  - 桶恢复到 FIVE_HOUR_HEALTHY 后才重新开号, 避免在临界值附近反复开关。
为什么 3p 两个桶只记录不判开关:
  - 3p-weekly / 3p-5h: 走 CPA 的流量只有 Gemini (new-api 那条通道
    own-cpa-multi-gemini-mapped-* 的 models 全是 gemini-*), Claude 侧由
    kiro-rs 兜底。拿 3p 桶关号会把 Gemini 还能跑的号误关。
    反之 "Gemini 空了但 3p 还满, 只让它跑 Claude" 也做不到: CPA 代码里没有
    bucket 概念 (grep bucketId 零命中), 且 management API 不暴露 per-model
    挂起, new-api 侧也没有指向 CPA 的 Claude 通道。所以 Gemini 桶空 = 关号。

死号隔离 (v4.1 新增):
    refresh token 失效的号, CPA 连 access token 都换不出来, api-call 返回
    {"error":"auth token refresh failed"} 且 wrapper 里没有 status_code 字段,
    daemon 侧仅在明确收到该错误时标记 auth-refresh-failed。这种号读不到 quota, 会被
    每轮白查一次。连续 DEAD_STREAK_THRESHOLD 轮判定为死号后, 把 auth 文件移到
    DEAD_DIR (默认 auths/dead/), 不删除 —— 保留原文件便于事后重新 OAuth 授权。
    隔离状态记在 DEAD_STATE_FILE, 重启不丢。单轮隔离数受 MAX_QUARANTINE_CYCLE
    限制, 防误判批量搬走整个号池。

    注: cpa-login-hub 已有 terminal_account 自动删号链路
    (account_lifecycle.go), 但只接了 OpenAI, antigravity 的 refresh 失败
    (invalid_grant → failureReauth) 没有接进去, 删除路径也硬编码 provider
    "openai"。本 daemon 的隔离是过渡方案; 长期应在 login-hub 里补 antigravity
    的重新授权 + 删号, 那才是它的职责。

安全设计:
  - 默认 dry-run, 必须显式 --apply 才真正改号
  - 每轮最多 enable MAX_ENABLE_PER_CYCLE 个 (避免号池突然膨胀触发共享 project 传染)
  - 每轮 disable 不限量 (止血优先)
  - 保底: 若 enable 后 active 数会超过 MAX_ACTIVE 则不再 enable
  - 死号隔离只搬文件不删除, 且需连续多轮确认

用法:
    cpa-daemon-v4.py                    # dry-run 一次, 打印决策表
    cpa-daemon-v4.py --apply --once     # 真正执行一轮
    cpa-daemon-v4.py --apply            # 常驻循环

环境变量:
    CPA_MGMT_KEY (必填)
    CPA_URL                default http://cli-proxy-api-blue:8317
    CPA_CADDY_CONTAINER    default caddy
    CPA_ANTIGRAVITY_UA     default antigravity/hub/2.9.1 darwin/arm64
    CPA_QUOTA_URL          default daily-cloudcode-pa retrieveUserQuotaSummary
    CPA_MAX_ACTIVE         default 40
    CPA_MAX_ENABLE_CYCLE   default 5
    CPA_CYCLE_SEC          default 1800
    CPA_WEEKLY_EXHAUSTED   default 0.0
    CPA_WEEKLY_HEALTHY     default 0.10
    CPA_FIVE_HOUR_EXHAUSTED default 0.0
    CPA_FIVE_HOUR_HEALTHY  default 0.10
    CPA_AUTO_DISABLED_STATE default /var/lib/cpa-daemon-v4/quota-disabled.json
    CPA_AUTH_DIR           default /data/cli-proxy-api/auths
    CPA_DEAD_STREAK        default 3    连续几轮 refresh-failed 才隔离
    CPA_MAX_QUARANTINE     default 5    单轮最多隔离几个
    CPA_QUARANTINE         default 1    设 0 关闭死号隔离
"""

import argparse
import json
import logging
import math
import os
import subprocess
import sys
import time
from datetime import datetime, timezone

def _resolve_cpa_url():
    """Use one stable CPA process for both auth indexes and quota calls.

    CPA_URL wins if set explicitly. The daemon lists auth files and then calls
    the quota endpoint by auth_index, so both operations must hit the same
    process. A transient blue outage is harmless because unreadable quota now
    preserves account state until the next cycle.
    """
    explicit = os.getenv("CPA_URL", "").strip()
    if explicit:
        return explicit
    return "http://cli-proxy-api-blue:8317"


MGMT_KEY = os.getenv("CPA_MGMT_KEY", "")
CPA_URL = _resolve_cpa_url()
CADDY = os.getenv("CPA_CADDY_CONTAINER", "caddy")
ANTIGRAVITY_UA = os.getenv("CPA_ANTIGRAVITY_UA", "antigravity/hub/2.9.1 darwin/arm64")
QUOTA_URL = os.getenv(
    "CPA_QUOTA_URL",
    "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary",
)

MAX_ACTIVE = int(os.getenv("CPA_MAX_ACTIVE", "40"))
MAX_ENABLE_PER_CYCLE = int(os.getenv("CPA_MAX_ENABLE_CYCLE", "5"))
CYCLE_SEC = int(os.getenv("CPA_CYCLE_SEC", "1800"))

# gemini-weekly remainingFraction 阈值
WEEKLY_EXHAUSTED = float(os.getenv("CPA_WEEKLY_EXHAUSTED", "0.0"))
WEEKLY_HEALTHY = float(os.getenv("CPA_WEEKLY_HEALTHY", "0.10"))
FIVE_HOUR_EXHAUSTED = float(os.getenv("CPA_FIVE_HOUR_EXHAUSTED", "0.0"))
FIVE_HOUR_HEALTHY = float(os.getenv("CPA_FIVE_HOUR_HEALTHY", "0.10"))
if not 0 <= WEEKLY_EXHAUSTED < WEEKLY_HEALTHY <= 1:
    raise ValueError("expected 0 <= CPA_WEEKLY_EXHAUSTED < CPA_WEEKLY_HEALTHY <= 1")
if not 0 <= FIVE_HOUR_EXHAUSTED < FIVE_HOUR_HEALTHY <= 1:
    raise ValueError(
        "expected 0 <= CPA_FIVE_HOUR_EXHAUSTED < CPA_FIVE_HOUR_HEALTHY <= 1"
    )

# Weekly and five-hour exhaustion control shutdown; both gate recovery.
DECISION_BUCKET = "gemini-weekly"
FIVE_HOUR_BUCKET = "gemini-5h"
# 日志表里展示的桶顺序 (Google 目前返回这四个)
REPORT_BUCKETS = ("gemini-weekly", "gemini-5h", "3p-weekly", "3p-5h")

# Only accounts recorded here may be automatically re-enabled. This keeps a
# management UI disable operation authoritative across daemon cycles.
AUTO_DISABLED_STATE_FILE = os.getenv(
    "CPA_AUTO_DISABLED_STATE", "/var/lib/cpa-daemon-v4/quota-disabled.json"
)

# 死号隔离
AUTH_DIR = os.getenv("CPA_AUTH_DIR", "/data/cli-proxy-api/auths")
DEAD_DIR = os.path.join(AUTH_DIR, "dead")
DEAD_STATE_FILE = os.path.join(AUTH_DIR, ".dead-streak.json")
DEAD_STREAK_THRESHOLD = int(os.getenv("CPA_DEAD_STREAK", "3"))
MAX_QUARANTINE_PER_CYCLE = int(os.getenv("CPA_MAX_QUARANTINE", "5"))
QUARANTINE_ENABLED = os.getenv("CPA_QUARANTINE", "1") not in ("0", "false", "False")
# api-call 在 refresh token 失效时返回的 quota_err 形状: wrapper 里没有
# status_code 字段, fetch_quota 会保留这个明确的刷新失败标记。
DEAD_QUOTA_ERR_PREFIX = "auth-refresh-failed"

LOG = logging.getLogger("cpa-daemon-v4")


# ---------- CPA management API ----------

def _run(cmd, timeout=40):
    try:
        r = subprocess.run(cmd, capture_output=True, timeout=timeout)
    except subprocess.TimeoutExpired:
        return ""
    return r.stdout.decode("utf-8", errors="replace")


def _parse_json_loose(text):
    """docker exec 输出前后可能混入 shell warning, 从第一个 { 到最后一个 } 截取。"""
    if not text:
        return None
    i = text.find("{")
    j = text.rfind("}")
    if i < 0 or j <= i:
        return None
    try:
        return json.loads(text[i : j + 1])
    except json.JSONDecodeError:
        return None


def list_antigravity_auths():
    out = _run([
        "docker", "exec", CADDY, "wget", "-qO-",
        "--header", f"X-Management-Key: {MGMT_KEY}",
        f"{CPA_URL}/v0/management/auth-files",
    ])
    d = _parse_json_loose(out)
    if not d:
        raise RuntimeError("failed to list auth-files")
    return [f for f in d.get("files", []) if f.get("provider") == "antigravity"]


def fetch_quota(auth_index, project_id):
    """返回 (quota_dict, error_str)。quota_dict 为 None 时 error_str 说明原因。"""
    if not project_id:
        return None, "missing-project-id"
    payload = json.dumps({
        "auth_index": auth_index,
        "method": "POST",
        "url": QUOTA_URL,
        "header": {
            "Authorization": "Bearer $TOKEN$",
            "Content-Type": "application/json",
            "User-Agent": ANTIGRAVITY_UA,
        },
        "data": json.dumps({"project": project_id}),
    })
    out = _run([
        "docker", "exec", CADDY, "curl", "-sS", "--max-time", "25",
        "-X", "POST", f"{CPA_URL}/v0/management/api-call",
        "-H", f"X-Management-Key: {MGMT_KEY}",
        "-H", "Content-Type: application/json",
        "-d", payload,
    ])
    wrap = _parse_json_loose(out)
    if not wrap:
        return None, "api-call-no-response"
    code = wrap.get("status_code")
    if code is None and wrap.get("error") == "auth token refresh failed":
        return None, DEAD_QUOTA_ERR_PREFIX
    inner = _parse_json_loose(wrap.get("body", ""))
    if code != 200:
        msg = ""
        if inner:
            msg = ((inner.get("error") or {}).get("message") or "")[:60]
        return None, f"http{code}:{msg}"
    if not inner:
        return None, "quota-body-unparseable"
    return inner, None


def set_disabled(name, disabled):
    payload = json.dumps({"name": name, "disabled": disabled})
    out = _run([
        "docker", "exec", CADDY, "curl", "-sS", "--max-time", "20",
        "-o", "/dev/null", "-w", "%{http_code}",
        "-X", "PATCH", f"{CPA_URL}/v0/management/auth-files/status",
        "-H", f"X-Management-Key: {MGMT_KEY}",
        "-H", "Content-Type: application/json",
        "-d", payload,
    ])
    return out.strip().endswith("200")


def load_quota_disabled():
    try:
        with open(AUTO_DISABLED_STATE_FILE, encoding="utf-8") as fh:
            data = json.load(fh)
    except (OSError, json.JSONDecodeError):
        return set()
    if not isinstance(data, list):
        return set()
    return {name for name in data if isinstance(name, str) and name}


def save_quota_disabled(names):
    tmp = AUTO_DISABLED_STATE_FILE + ".tmp"
    try:
        state_dir = os.path.dirname(AUTO_DISABLED_STATE_FILE)
        if state_dir:
            os.makedirs(state_dir, exist_ok=True)
        with open(tmp, "w", encoding="utf-8") as fh:
            json.dump(sorted(names), fh, indent=2)
        os.replace(tmp, AUTO_DISABLED_STATE_FILE)
    except OSError as exc:
        LOG.warning("could not persist quota-disabled state: %s", exc)


# ---------- quota 解析 ----------

def extract_buckets(quota):
    """把 groups[].buckets[] 拍平成 {bucketId: {frac, reset}}。"""
    out = {}
    for grp in quota.get("groups", []) or []:
        for b in grp.get("buckets", []) or []:
            bid = b.get("bucketId")
            if not bid:
                continue
            out[bid] = {
                "frac": b.get("remainingFraction"),
                "reset": b.get("resetTime", ""),
            }
    return out


def reset_in_human(reset_iso):
    if not reset_iso:
        return ""
    try:
        t = datetime.fromisoformat(reset_iso.replace("Z", "+00:00"))
    except ValueError:
        return reset_iso
    delta = t - datetime.now(timezone.utc)
    secs = int(delta.total_seconds())
    if secs <= 0:
        return "now"
    d, rem = divmod(secs, 86400)
    h, rem = divmod(rem, 3600)
    m = rem // 60
    if d:
        return f"{d}d{h}h"
    if h:
        return f"{h}h{m}m"
    return f"{m}m"


def quota_fraction_pct(frac):
    """Format a validated quota fraction without rounding tiny capacity to 0."""
    if frac == 0:
        return "0.0%"
    if 0 < frac < 0.001:
        return "<0.1%"
    return f"{frac * 100:.1f}%"


# ---------- 死号隔离 ----------

def looks_dead(quota_err):
    """Only an explicit refresh failure counts toward credential quarantine."""
    return quota_err == DEAD_QUOTA_ERR_PREFIX


def load_dead_streaks():
    try:
        with open(DEAD_STATE_FILE, encoding="utf-8") as fh:
            data = json.load(fh)
        return data if isinstance(data, dict) else {}
    except (OSError, json.JSONDecodeError):
        return {}


def save_dead_streaks(streaks):
    tmp = DEAD_STATE_FILE + ".tmp"
    try:
        with open(tmp, "w", encoding="utf-8") as fh:
            json.dump(streaks, fh, indent=2, sort_keys=True)
        os.replace(tmp, DEAD_STATE_FILE)
    except OSError as exc:
        LOG.warning("could not persist dead-streak state: %s", exc)


def quarantine_auth(email, auth_name):
    """Move a dead auth file into DEAD_DIR. Returns True on success.

    Moves rather than deletes so the credential can be re-authorised later;
    CPA stops loading it either way because it is no longer in AUTH_DIR.
    """
    if not auth_name:
        return False
    src = os.path.join(AUTH_DIR, auth_name)
    if not os.path.isfile(src):
        # Fall back to the conventional antigravity-<email>.json layout.
        if email:
            candidate = os.path.join(AUTH_DIR, f"antigravity-{email}.json")
            if os.path.isfile(candidate):
                src = candidate
            else:
                LOG.warning("quarantine %s: auth file not found (%s)", email, auth_name)
                return False
        else:
            return False
    try:
        os.makedirs(DEAD_DIR, exist_ok=True)
        stamp = datetime.now(timezone.utc).strftime("%Y%m%d-%H%M%S")
        dst = os.path.join(DEAD_DIR, f"{os.path.basename(src)}.dead-{stamp}")
        os.replace(src, dst)
        return True
    except OSError as exc:
        LOG.warning("quarantine %s failed: %s", email, exc)
        return False


def apply_quarantine(rows, apply_changes):
    """Track consecutive refresh-failure rounds and quarantine confirmed dead auths.

    Returns the number of auths moved this cycle.
    """
    streaks = load_dead_streaks()
    seen = set()
    dead_now = []

    for email, auth, _buckets, quota_err, _action, _reason in rows:
        key = email or auth.get("name", "")
        if not key:
            continue
        seen.add(key)
        if looks_dead(quota_err):
            streaks[key] = streaks.get(key, 0) + 1
            if streaks[key] >= DEAD_STREAK_THRESHOLD:
                dead_now.append((key, auth, streaks[key]))
        elif key in streaks:
            # Recovered (or was a transient blip): reset the counter so a
            # credential that comes back never accumulates toward quarantine.
            LOG.info("dead-streak reset for %s (was %d)", key, streaks[key])
            del streaks[key]

    # Drop bookkeeping for auths that no longer exist in the pool.
    for stale in [k for k in streaks if k not in seen]:
        del streaks[stale]

    if not dead_now:
        if apply_changes:
            save_dead_streaks(streaks)
        return 0

    LOG.warning("dead auths confirmed (>=%d consecutive refresh failures): %d",
                DEAD_STREAK_THRESHOLD, len(dead_now))
    for key, _auth, streak in dead_now:
        LOG.warning("  DEAD %s (streak=%d)", key, streak)

    if not QUARANTINE_ENABLED:
        LOG.info("quarantine disabled (CPA_QUARANTINE=0): leaving %d dead auths in place",
                 len(dead_now))
        if apply_changes:
            save_dead_streaks(streaks)
        return 0
    if not apply_changes:
        LOG.info("DRY-RUN: would quarantine %d dead auths into %s",
                 min(len(dead_now), MAX_QUARANTINE_PER_CYCLE), DEAD_DIR)
        return 0

    moved = 0
    for key, auth, _streak in dead_now:
        if moved >= MAX_QUARANTINE_PER_CYCLE:
            LOG.info("skip quarantine %s: per-cycle cap %d reached",
                     key, MAX_QUARANTINE_PER_CYCLE)
            break
        if quarantine_auth(key, auth.get("name", "")):
            LOG.warning("QUARANTINED %s -> %s", key, DEAD_DIR)
            streaks.pop(key, None)
            moved += 1

    save_dead_streaks(streaks)
    if moved:
        LOG.warning("quarantined %d dead auths; CPA will drop them on next auth reload",
                    moved)
    return moved


# ---------- 决策 ----------

def decide(auth, buckets, quota_err, auto_disabled=False):
    """返回 (action, reason)。action ∈ {'disable', 'enable', 'keep'}"""
    disabled = bool(auth.get("disabled"))

    if quota_err:
        # Quota API is itself rate-limited and can fail transiently.  Never
        # turn an enabled account off merely because a read failed; doing so
        # caused accounts with healthy quota to disappear from the pool.
        # Refresh-token failures are handled by the dead-auth quarantine path.
        return "keep", f"quota-unreadable({quota_err})"

    # Weekly and five-hour exhaustion control shutdown. Recovery uses higher
    # thresholds below so an account does not flap near an empty bucket.
    gw = buckets.get(DECISION_BUCKET, {}).get("frac")
    if gw is None:
        return "keep", f"no-{DECISION_BUCKET}-bucket"
    if isinstance(gw, bool) or not isinstance(gw, (int, float)) or not math.isfinite(gw) or not 0 <= gw <= 1:
        return "keep", f"invalid-{DECISION_BUCKET}-fraction"

    if gw <= WEEKLY_EXHAUSTED:
        if not disabled:
            return "disable", f"{DECISION_BUCKET}={quota_fraction_pct(gw)}"
        return "keep", f"{DECISION_BUCKET}={quota_fraction_pct(gw)} (already off)"

    five_hour = buckets.get(FIVE_HOUR_BUCKET, {}).get("frac")
    valid_five_hour = (
        not isinstance(five_hour, bool)
        and isinstance(five_hour, (int, float))
        and math.isfinite(five_hour)
        and 0 <= five_hour <= 1
    )
    if not disabled and valid_five_hour and five_hour <= FIVE_HOUR_EXHAUSTED:
        return "disable", f"{FIVE_HOUR_BUCKET}={quota_fraction_pct(five_hour)}"

    if gw >= WEEKLY_HEALTHY:
        if disabled:
            if not auto_disabled:
                return "keep", f"{DECISION_BUCKET}={quota_fraction_pct(gw)} (manual off)"
            if five_hour is None:
                return "keep", f"no-{FIVE_HOUR_BUCKET}-bucket"
            if not valid_five_hour:
                return "keep", f"invalid-{FIVE_HOUR_BUCKET}-fraction"
            if five_hour < FIVE_HOUR_HEALTHY:
                return "keep", f"{FIVE_HOUR_BUCKET}={quota_fraction_pct(five_hour)} (not ready)"
            return "enable", (
                f"{DECISION_BUCKET}={quota_fraction_pct(gw)}, "
                f"{FIVE_HOUR_BUCKET}={quota_fraction_pct(five_hour)}"
            )
        return "keep", f"{DECISION_BUCKET}={quota_fraction_pct(gw)} (healthy)"

    # 灰区: 不主动改
    return "keep", f"{DECISION_BUCKET}={quota_fraction_pct(gw)} (grey zone)"


def run_cycle(apply_changes):
    global CPA_URL
    CPA_URL = _resolve_cpa_url()
    LOG.info(
        "quota target=%s endpoint=%s thresholds=weekly %.3f/%.3f five-hour %.3f",
        CPA_URL, QUOTA_URL, WEEKLY_EXHAUSTED, WEEKLY_HEALTHY, FIVE_HOUR_HEALTHY,
    )
    auths = list_antigravity_auths()
    quota_disabled = load_quota_disabled()
    auth_by_name = {a.get("name", ""): a for a in auths if a.get("name")}
    stale_quota_disabled = {
        name
        for name in quota_disabled
        if name not in auth_by_name or not auth_by_name[name].get("disabled")
    }
    if stale_quota_disabled:
        quota_disabled.difference_update(stale_quota_disabled)
        if apply_changes:
            save_quota_disabled(quota_disabled)
        LOG.info("cleared %d stale quota-disabled markers", len(stale_quota_disabled))
    active_now = sum(1 for a in auths if not a.get("disabled"))
    LOG.info("antigravity auths=%d active=%d (max_active=%d)", len(auths), active_now, MAX_ACTIVE)

    rows = []
    for a in sorted(auths, key=lambda x: x.get("email", "")):
        email = a.get("email", "")
        idx = a.get("auth_index", "")
        if not idx:
            rows.append((email, a, {}, "no-auth-index", "keep", "no-auth-index"))
            continue
        project_id = a.get("project_id") or a.get("projectId")
        quota, err = fetch_quota(idx, project_id)
        buckets = extract_buckets(quota) if quota else {}
        action, reason = decide(
            a, buckets, err, auto_disabled=a.get("name", "") in quota_disabled
        )
        rows.append((email, a, buckets, err, action, reason))

    # 打印决策表。四个桶全展示 (gemini-weekly 带 reset 倒计时, 它是决策桶),
    # 但只有 DECISION_BUCKET 参与开关 —— 其余三列纯观测, 用于事后判断
    # "5h 反复打空是否在抽干 weekly" 这类问题。
    LOG.info("%-42s %-8s %-8s %-8s %-8s %-8s %-8s %s",
             "EMAIL", "GEM-WK*", "RESET-IN", "GEM-5H*", "3P-WK", "3P-5H", "ACTION", "REASON")
    for email, a, buckets, err, action, reason in rows:
        def pct(bucket_id):
            frac = buckets.get(bucket_id, {}).get("frac")
            if isinstance(frac, bool) or not isinstance(frac, (int, float)) or not math.isfinite(frac) or not 0 <= frac <= 1:
                return "-"
            return quota_fraction_pct(frac)
        LOG.info("%-42s %-8s %-8s %-8s %-8s %-8s %-8s %s",
                 email,
                 pct("gemini-weekly"),
                 reset_in_human(buckets.get("gemini-weekly", {}).get("reset", "")),
                 pct("gemini-5h"),
                 pct("3p-weekly"),
                 pct("3p-5h"),
                 action, reason)

    # 桶名漂移告警: Google 加了新桶而我们没跟上时, 至少在日志里能看见。
    unknown = {bid for _e, _a, b, _err, _act, _r in rows for bid in b} - set(REPORT_BUCKETS)
    if unknown:
        LOG.warning("unreported quota buckets seen (Google added new ones?): %s",
                    ", ".join(sorted(unknown)))

    to_disable = [(e, a, r) for e, a, _b, _err, act, r in rows if act == "disable"]
    to_enable = [(e, a, r) for e, a, _b, _err, act, r in rows if act == "enable"]

    LOG.info("plan: disable=%d enable=%d", len(to_disable), len(to_enable))

    # Dead-auth quarantine runs regardless of the enable/disable plan: a dead
    # credential is already disabled, so it never shows up in to_disable, yet
    # it still costs one wasted quota probe every cycle.
    quarantined = apply_quarantine(rows, apply_changes)

    if not apply_changes:
        LOG.info("DRY-RUN: no changes applied (use --apply to execute)")
        return

    # 先 disable (止血优先, 不限量)
    disabled_count = 0
    for email, a, reason in to_disable:
        ok = set_disabled(a.get("name", ""), True)
        if ok:
            disabled_count += 1
            quota_disabled.add(a.get("name", ""))
            save_quota_disabled(quota_disabled)
        LOG.warning("DISABLE %s (%s) -> %s", email, reason, "ok" if ok else "FAILED")

    # 再 enable, 受 MAX_ENABLE_PER_CYCLE 和 MAX_ACTIVE 限制
    # A failed PATCH must not be counted as a freed slot. Refresh the snapshot
    # before enabling, since operators may also change accounts during the scan.
    current_auths = list_antigravity_auths()
    projected_active = sum(not a.get("disabled") for a in current_auths)
    current_disabled = {a.get("name"): bool(a.get("disabled")) for a in current_auths}
    enabled_count = 0
    for email, a, reason in to_enable:
        if not current_disabled.get(a.get("name"), False):
            continue
        if enabled_count >= MAX_ENABLE_PER_CYCLE:
            LOG.info("skip enable %s: per-cycle cap %d reached", email, MAX_ENABLE_PER_CYCLE)
            break
        if projected_active + enabled_count >= MAX_ACTIVE:
            LOG.info("skip enable %s: would exceed max_active %d", email, MAX_ACTIVE)
            break
        ok = set_disabled(a.get("name", ""), False)
        if ok:
            enabled_count += 1
            quota_disabled.discard(a.get("name", ""))
            save_quota_disabled(quota_disabled)
        LOG.info("ENABLE %s (%s) -> %s", email, reason, "ok" if ok else "FAILED")

    LOG.info("applied: disabled=%d enabled=%d quarantined=%d",
             disabled_count, enabled_count, quarantined)


def main():
    p = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    p.add_argument("--apply", action="store_true", help="真正执行改动 (默认只 dry-run)")
    p.add_argument("--once", action="store_true", help="只跑一轮")
    p.add_argument("--cycle-sec", type=int, default=CYCLE_SEC)
    args = p.parse_args()

    logging.basicConfig(format="%(asctime)s %(levelname)s %(message)s",
                        level=logging.INFO, datefmt="%Y-%m-%dT%H:%M:%S%z")

    if not MGMT_KEY:
        LOG.error("CPA_MGMT_KEY not set")
        sys.exit(1)

    while True:
        try:
            run_cycle(args.apply)
        except Exception as e:
            LOG.exception("cycle failed: %s", e)
        if args.once or not args.apply:
            break
        LOG.info("---- sleeping %ds ----", args.cycle_sec)
        time.sleep(args.cycle_sec)


if __name__ == "__main__":
    main()
