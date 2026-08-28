#!/usr/bin/env python3
"""cpa-daemon-v3.py — CPA 号池自动化 daemon v3

策略:
1. 通过 CPA management API 拉取 auth-files 状态
2. 每一轮:
   a. 拿到当前所有 disabled 号
   b. 每次随机启用 BATCH_SIZE 个号
   c. 等 OBSERVE_SEC 秒, 让流量自然打到这些号
   d. 拉新的 auth-files, 检查每个刚启用的号:
      - 如果 recent_requests 里最近 2 个 slot 有 failed > 0 且 success == 0
        (说明撞 429 或其他错误, 号没恢复) -> 立即 disable
      - 如果 success > 0 -> 保留 enable (号已恢复)
      - 如果两者都是 0 (没被选中) -> 保留 enable, 让下一轮再看
3. 死循环, 每 CYCLE_SEC 秒一轮

用法:
  cpa-daemon-v3.py --dry-run          # 只报告, 不改任何号
  cpa-daemon-v3.py --once             # 只跑一轮
  cpa-daemon-v3.py                    # 死循环

环境变量:
  CPA_URL           default http://cli-proxy-api-blue:8317
  CPA_MGMT_KEY      required (management secret key)
  CPA_BATCH_SIZE    default 3   每轮批量启用几个 disabled 号
  CPA_OBSERVE_SEC   default 300 观察窗口 (5min = quota 冷却窗口)
  CPA_CYCLE_SEC     default 600 相邻两轮间隔
"""

import argparse
import json
import logging
import os
import random
import subprocess
import sys
import time
import urllib.request
import urllib.error
import urllib.parse

CPA_URL = os.getenv("CPA_URL", "http://cli-proxy-api-blue:8317")
CPA_MGMT_KEY = os.getenv("CPA_MGMT_KEY", "")
BATCH_SIZE = int(os.getenv("CPA_BATCH_SIZE", "3"))
OBSERVE_SEC = int(os.getenv("CPA_OBSERVE_SEC", "300"))
CYCLE_SEC = int(os.getenv("CPA_CYCLE_SEC", "600"))

LOG = logging.getLogger("cpa-daemon-v3")


def http_json(method, path, body=None, mgmt_key=None):
    """通过 docker exec caddy wget 调 CPA management API (从 station_internal 网络里)。"""
    url = f"{CPA_URL}{path}"
    headers = {"X-Management-Key": mgmt_key or CPA_MGMT_KEY}
    if body is not None:
        headers["Content-Type"] = "application/json"

    # 用 curl 从 caddy 容器发 (station_internal 网络内)
    cmd = ["docker", "exec", "caddy", "wget", "-qO-", "-t", "1", "--timeout=15"]
    for k, v in headers.items():
        cmd += ["--header", f"{k}: {v}"]
    if method == "POST":
        cmd += ["--post-data", json.dumps(body) if body else ""]
    elif method == "PATCH":
        # wget 不支持 PATCH, 用 curl 代替
        cmd = ["docker", "exec", "caddy", "curl", "-sS", "-X", "PATCH", "--max-time", "15"]
        for k, v in headers.items():
            cmd += ["-H", f"{k}: {v}"]
        if body:
            cmd += ["-d", json.dumps(body)]
        cmd += [url]
    else:
        cmd += [url]

    if method == "GET" or method == "POST":
        cmd += [url]

    try:
        result = subprocess.run(cmd, capture_output=True, timeout=20, check=False)
    except subprocess.TimeoutExpired:
        raise RuntimeError(f"HTTP {method} {path} timed out")

    if result.returncode != 0:
        stderr = result.stderr.decode("utf-8", errors="replace")[:200]
        raise RuntimeError(f"HTTP {method} {path} failed rc={result.returncode}: {stderr}")

    stdout = result.stdout.decode("utf-8", errors="replace").strip()
    if not stdout:
        return None
    # 用 raw_decode 从头解析, 允许后面有多余字符 (docker exec 有时追加尾字节)
    try:
        obj, _ = json.JSONDecoder().raw_decode(stdout)
        return obj
    except json.JSONDecodeError as e:
        # 尝试从第一个 { 开始
        first_brace = stdout.find("{")
        if first_brace > 0:
            try:
                obj, _ = json.JSONDecoder().raw_decode(stdout[first_brace:])
                return obj
            except json.JSONDecodeError:
                pass
        raise RuntimeError(f"HTTP {method} {path} returned non-JSON: {stdout[:200]}...{stdout[-100:]}") from e


def list_auth_files():
    return http_json("GET", "/v0/management/auth-files").get("files", [])


def patch_auth_status(name, disabled):
    return http_json(
        "PATCH",
        "/v0/management/auth-files/status",
        body={"name": name, "disabled": disabled},
    )


def is_antigravity(auth_file):
    return auth_file.get("provider") == "antigravity" or auth_file.get("type") == "antigravity"


def recent_activity(auth_file, slots=2):
    """返回 (success, failed) 最近 N 个 10-min slot 之和。"""
    slots_data = auth_file.get("recent_requests") or []
    if not slots_data:
        return 0, 0
    tail = slots_data[-slots:]
    success = sum(s.get("success", 0) for s in tail)
    failed = sum(s.get("failed", 0) for s in tail)
    return success, failed


def dry_run_report():
    """只报告不改任何号。"""
    files = list_auth_files()
    ag = [f for f in files if is_antigravity(f)]
    active = [f for f in ag if not f.get("disabled")]
    disabled = [f for f in ag if f.get("disabled")]

    LOG.info(
        "antigravity 号池: total=%d active=%d disabled=%d",
        len(ag),
        len(active),
        len(disabled),
    )

    # active 里最近有 failed 的
    problem_active = []
    for f in active:
        succ, fail = recent_activity(f, slots=2)
        if fail > 0 and succ == 0:
            problem_active.append((f.get("email"), succ, fail))

    if problem_active:
        LOG.warning("active but recent all-failed (candidates to disable): %d 个", len(problem_active))
        for email, s, f in problem_active[:10]:
            LOG.warning("  %s success=%d failed=%d", email, s, f)

    # disabled 号里全部都是 signals={} 因为不接流量; 这里只列 email 帮你人肉决策
    LOG.info("disabled 号 (需要人工/daemon 试探性 enable): %d 个", len(disabled))
    for f in disabled[:20]:
        LOG.info("  %s", f.get("email"))
    if len(disabled) > 20:
        LOG.info("  ... 还有 %d 个", len(disabled) - 20)


def run_one_cycle(dry_run=False, batch_size=BATCH_SIZE, observe_sec=OBSERVE_SEC):
    """跑一轮: 批量启用 batch_size 个 disabled 号, 观察, 决定保留还是回退。"""
    files = list_auth_files()
    ag = [f for f in files if is_antigravity(f)]
    disabled = [f for f in ag if f.get("disabled")]
    if not disabled:
        LOG.info("没有 disabled 号可试探, 跳过本轮")
        return

    # 随机挑 batch
    batch = random.sample(disabled, min(batch_size, len(disabled)))
    LOG.info("本轮试探启用 %d/%d 个 disabled 号:", len(batch), len(disabled))
    for f in batch:
        LOG.info("  -> %s", f.get("email"))

    if dry_run:
        LOG.info("DRY-RUN: 不实际改动, 跳过启用/观察阶段")
        return

    # 阶段 1: 启用
    for f in batch:
        try:
            patch_auth_status(f["name"], disabled=False)
            LOG.info("enabled: %s", f.get("email"))
        except Exception as e:
            LOG.error("enable %s failed: %s", f.get("email"), e)

    # 阶段 2: 观察
    LOG.info("等 %ds 观察流量", observe_sec)
    time.sleep(observe_sec)

    # 阶段 3: 决策
    fresh = list_auth_files()
    fresh_by_name = {f["name"]: f for f in fresh}
    kept = []
    reverted = []
    idle = []
    for f in batch:
        fnew = fresh_by_name.get(f["name"])
        if not fnew:
            LOG.warning("post-observe: 找不到 %s", f.get("email"))
            continue
        succ, fail = recent_activity(fnew, slots=2)
        if succ > 0:
            kept.append((f, succ, fail))
        elif fail > 0 and succ == 0:
            # 撞失败, 回退
            try:
                patch_auth_status(f["name"], disabled=True)
                reverted.append((f, succ, fail))
                LOG.warning("reverted (all failed): %s failed=%d", f.get("email"), fail)
            except Exception as e:
                LOG.error("revert %s failed: %s", f.get("email"), e)
        else:
            # 没被选中, 保留看下一轮
            idle.append((f, succ, fail))

    LOG.info(
        "本轮结果: kept=%d reverted=%d idle=%d (idle 号已 enabled, 等下轮再评估)",
        len(kept),
        len(reverted),
        len(idle),
    )


def main():
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("--dry-run", action="store_true", help="只报告不改")
    parser.add_argument("--once", action="store_true", help="只跑一轮就退出")
    parser.add_argument("--batch-size", type=int, default=BATCH_SIZE)
    parser.add_argument("--observe-sec", type=int, default=OBSERVE_SEC)
    parser.add_argument("--cycle-sec", type=int, default=CYCLE_SEC)
    args = parser.parse_args()

    logging.basicConfig(
        format="%(asctime)s %(levelname)s %(message)s",
        level=logging.INFO,
        datefmt="%H:%M:%S",
    )

    if not CPA_MGMT_KEY:
        LOG.error("CPA_MGMT_KEY 未设置")
        sys.exit(1)

    if args.dry_run:
        dry_run_report()
        return

    while True:
        try:
            run_one_cycle(dry_run=False, batch_size=args.batch_size, observe_sec=args.observe_sec)
        except Exception as e:
            LOG.exception("cycle failed: %s", e)
        if args.once:
            break
        LOG.info("---- sleeping %ds until next cycle ----", args.cycle_sec)
        time.sleep(args.cycle_sec)


if __name__ == "__main__":
    main()
