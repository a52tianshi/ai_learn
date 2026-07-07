#!/bin/bash

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$DIR"

# 1. Stop Python Bot
PID_FILE="wordbot.pid"
if [ -f "$PID_FILE" ]; then
    PID=$(cat "$PID_FILE")
    if ps -p $PID > /dev/null 2>&1; then
        echo "正在停止 Python 机器人 (PID: $PID)..."
        kill -15 $PID
        for i in {1..5}; do
            if ! ps -p $PID > /dev/null 2>&1; then break; fi
            sleep 1
        done
        if ps -p $PID > /dev/null 2>&1; then
            echo "  • 进程未响应，正在强制杀死..."
            kill -9 $PID
        fi
        echo "  • Python 机器人已停止。"
    else
        echo "  • Python 机器人 (PID: $PID) 未运行。"
    fi
    rm "$PID_FILE"
else
    echo "  • 未发现 Python 机器人 PID 记录。"
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
