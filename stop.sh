#!/bin/bash

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$DIR"

# 杀 pid 文件里的进程，并把它的子进程一起带走。
# uv run 会 fork 出真正的 python 子进程；只杀 uv 包装进程会留下一个孤儿继续轮询，
# 之后 run.sh 再起一个，两个 poller 抢同一个 token 就会 409 Conflict。
stop_pid_tree() {
    local pid="$1" sig="$2" children
    children=$(pgrep -P "$pid" 2>/dev/null || true)
    kill "-$sig" "$pid" 2>/dev/null || true
    for c in $children; do
        kill "-$sig" "$c" 2>/dev/null || true
    done
}

wait_gone() {
    local pid="$1" i
    for i in {1..5}; do
        ps -p "$pid" > /dev/null 2>&1 || return 0
        sleep 1
    done
    return 1
}

# 1. Stop Python Bot
PID_FILE="wordbot.pid"
if [ -f "$PID_FILE" ]; then
    PID=$(cat "$PID_FILE")
    if ps -p $PID > /dev/null 2>&1; then
        echo "正在停止 Python 机器人 (PID: $PID)..."
        stop_pid_tree "$PID" 15
        if ! wait_gone "$PID"; then
            echo "  • 进程未响应，正在强制杀死..."
            stop_pid_tree "$PID" 9
        fi
        echo "  • Python 机器人已停止。"
    else
        echo "  • Python 机器人 (PID: $PID) 未运行。"
    fi
    rm -f "$PID_FILE"
else
    echo "  • 未发现 Python 机器人 PID 记录。"
fi

# 兜底：按绝对路径精确匹配残留的本项目 python 主进程（不会误伤别的项目）。
LEFTOVER=$(pgrep -f "$DIR/.venv/bin/python main.py" 2>/dev/null || true)
if [ -n "$LEFTOVER" ]; then
    echo "  • 发现孤儿子进程，一并清理: $LEFTOVER"
    for p in $LEFTOVER; do
        kill -15 "$p" 2>/dev/null || true
    done
    sleep 2
    LEFTOVER=$(pgrep -f "$DIR/.venv/bin/python main.py" 2>/dev/null || true)
    for p in $LEFTOVER; do
        kill -9 "$p" 2>/dev/null || true
    done
fi

# 2. Stop Go Service
GO_PID_FILE="service-data.pid"
GO_PID=""
if [ -f "$GO_PID_FILE" ]; then
    GO_PID=$(cat "$GO_PID_FILE")
    rm "$GO_PID_FILE"
fi

# Fallback: check who is listening on port 8080
LSOF_PID=$(lsof -t -i :8080)
if [ -n "$LSOF_PID" ]; then
    GO_PID=$LSOF_PID
fi

if [ -n "$GO_PID" ]; then
    if ps -p $GO_PID > /dev/null 2>&1; then
        echo "正在停止 Go 数据服务 (PID: $GO_PID)..."
        kill -15 $GO_PID
        for i in {1..5}; do
            if ! ps -p $GO_PID > /dev/null 2>&1; then break; fi
            sleep 1
        done
        if ps -p $GO_PID > /dev/null 2>&1; then
            echo "  • 进程未响应，正在强制杀死..."
            kill -9 $GO_PID
        fi
        echo "  • Go 数据服务已停止。"
    else
        echo "  • Go 数据服务 (PID: $GO_PID) 未运行。"
    fi
else
    echo "  • 未发现运行在 8080 端口的 Go 数据服务。"
fi

echo "------------------------------------------------"
echo "所有服务已停止！"
