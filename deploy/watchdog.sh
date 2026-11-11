#!/usr/bin/env bash
# 没有 systemd 时的自愈看门狗：健康检查失败就重启一次，并记录原因。
# 由 cron 每 5 分钟调用；健康时完全静默，不产生日志噪音。
set -uo pipefail

APP_DIR="${APP_DIR:-/home/chenquan/apps/ai_learn}"
LOG_FILE="${LOG_FILE:-$APP_DIR/logs/watchdog.log}"
LOG_SOURCE="${LOG_SOURCE:-file}"        # file（nohup）| journal（systemd）
PROCESS_MODE="${PROCESS_MODE:-pgrep}"   # pgrep（nohup）| systemd | auto

mkdir -p "$(dirname "$LOG_FILE")"

log() { printf '%s %s\n' "$(date -Is)" "$*" >> "$LOG_FILE"; }

check() {
    cd "$APP_DIR" || return 1
    LOG_SOURCE="$LOG_SOURCE" PROCESS_MODE="$PROCESS_MODE" ./deploy/healthcheck.sh 2>&1
}

if reason=$(check); then
    exit 0   # 健康：静默退出
fi

log "不健康，准备重启：$reason"

if ! cd "$APP_DIR"; then
    log "无法进入 $APP_DIR，放弃"
    exit 1
fi

./stop.sh  >> "$LOG_FILE" 2>&1
./run.sh   >> "$LOG_FILE" 2>&1
sleep 8

if reason=$(check); then
    log "重启后恢复正常"
    exit 0
fi

log "重启后仍不健康：$reason"
exit 1
