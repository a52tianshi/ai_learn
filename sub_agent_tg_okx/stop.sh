#!/bin/bash

# Get the directory where this script is located
DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$DIR"

PID_FILE="monitor.pid"

if [ -f "$PID_FILE" ]; then
    PID=$(cat "$PID_FILE")
    if ps -p $PID > /dev/null 2>&1; then
        echo "正在停止 PID 为 $PID 的监控程序..."
        
        # Send SIGTERM (graceful shutdown)
        kill -15 $PID
        
        # Wait up to 10 seconds for graceful exit
        for i in {1..10}; do
            if ! ps -p $PID > /dev/null 2>&1; then
                break
            fi
            sleep 1
        done
        
        # Force kill if still running
        if ps -p $PID > /dev/null 2>&1; then
            echo "程序未能在 10 秒内优雅退出，正在强制结束进程 (SIGKILL)..."
            kill -9 $PID
        fi
        
        echo "监控程序已成功停止。"
    else
        echo "发现 PID 文件，但进程 $PID 未运行。"
    fi
    rm "$PID_FILE"
else
    echo "未发现 monitor.pid 文件，程序可能未在后台运行。"
fi
