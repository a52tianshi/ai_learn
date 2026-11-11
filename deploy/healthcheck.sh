#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${APP_DIR:-/home/chenquan/apps/ai_learn}"
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:8080/healthz}"
SERVICE_NAME="${SERVICE_NAME:-wordbot.service}"
RUN_USER="${RUN_USER:-chenquan}"
PROCESS_MODE="${PROCESS_MODE:-auto}" # auto | systemd | pgrep
PROCESS_PATTERN="${PROCESS_PATTERN:-^([^[:space:]]*/)?(uv run python|python([0-9.]+)?) main[.]py([[:space:]]|$)}"
LOG_SOURCE="${LOG_SOURCE:-file}" # file（nohup）| journal（systemd）
LOG_FILE="${LOG_FILE:-${APP_DIR}/wordbot.log}"
MAX_LOG_AGE="${MAX_LOG_AGE:-300}"
CURL_TIMEOUT="${CURL_TIMEOUT:-5}"

fail() {
    printf 'wordbot 健康检查失败：%s\n' "$*" >&2
    exit 1
}

[[ "$MAX_LOG_AGE" =~ ^[1-9][0-9]*$ ]] || fail 'MAX_LOG_AGE 必须是正整数秒数'
[[ "$CURL_TIMEOUT" =~ ^[1-9][0-9]*$ ]] || fail 'CURL_TIMEOUT 必须是正整数秒数'

# --noproxy 防止服务器的代理环境变量干扰回环地址检查；不跟随重定向。
if ! http_code=$(curl --noproxy '*' --silent --show-error --output /dev/null \
    --connect-timeout "$CURL_TIMEOUT" --max-time "$CURL_TIMEOUT" \
    --write-out '%{http_code}' "$HEALTH_URL" 2>/dev/null); then
    fail "HTTP 健康检查连接失败或超时：$HEALTH_URL"
fi
[[ "${http_code}" == 200 ]] || fail "HTTP 健康检查应返回 200，实际为 ${http_code}：$HEALTH_URL"

if [[ "${PROCESS_MODE}" == auto ]]; then
    if command -v systemctl >/dev/null 2>&1 && [[ -d /run/systemd/system ]]; then
        PROCESS_MODE=systemd
    else
        PROCESS_MODE=pgrep
    fi
fi
case "${PROCESS_MODE}" in
    systemd)
        # unit 不活跃时直接失败，不能让遗留 nohup 掩盖 systemd 故障。
        systemctl is-active --quiet "${SERVICE_NAME}" 2>/dev/null \
            || fail "wordbot 进程未运行：${SERVICE_NAME} 不活跃（或无法查询）"
        ;;
    pgrep)
        pgrep -u "${RUN_USER}" -f "$PROCESS_PATTERN" >/dev/null 2>&1 \
            || fail "wordbot 进程未找到：用户 ${RUN_USER}，模式 $PROCESS_PATTERN"
        ;;
    *) fail "PROCESS_MODE 无效：${PROCESS_MODE}（应为 auto/systemd/pgrep）" ;;
esac

case "${LOG_SOURCE}" in
    file)
        [[ -r "$LOG_FILE" && -s "$LOG_FILE" ]] || fail "日志不存在、不可读或为空：$LOG_FILE"
        # GNU stat（Ubuntu）；BSD stat 分支便于本机 macOS 验证。
        if ! modified=$(stat -c %Y -- "$LOG_FILE" 2>/dev/null); then
            modified=$(stat -f %m "$LOG_FILE" 2>/dev/null) || fail "无法读取日志修改时间：$LOG_FILE"
        fi
        [[ "$modified" =~ ^[0-9]+$ ]] || fail "日志修改时间无效：$LOG_FILE"
        now=$(date +%s)
        age=$((now - modified))
        (( age >= 0 && age <= MAX_LOG_AGE )) \
            || fail "日志已有 ${age} 秒未更新（上限 ${MAX_LOG_AGE} 秒，或时钟异常）：$LOG_FILE"
        ;;
    journal)
        if ! recent=$(journalctl --unit="${SERVICE_NAME}" --since="${MAX_LOG_AGE} seconds ago" \
            --lines=1 --output=cat --quiet --no-pager 2>/dev/null); then
            fail "无法读取 journal 日志：${SERVICE_NAME}（检查权限）"
        fi
        [[ -n "$recent" ]] || fail "最近 ${MAX_LOG_AGE} 秒没有 journal 新日志：${SERVICE_NAME}"
        ;;
    *) fail "LOG_SOURCE 无效：${LOG_SOURCE}（应为 file/journal）" ;;
esac
