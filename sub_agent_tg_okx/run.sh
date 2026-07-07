#!/bin/bash

# Get the directory where this script is located
DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$DIR"

PID_FILE="monitor.pid"
LOG_FILE="monitor.log"

# Determine Python path (local .venv, parent .venv, or system python3)
if [ -d "./.venv" ]; then
    PYTHON_BIN="./.venv/bin/python"
elif [ -d "../.venv" ]; then
    PYTHON_BIN="../.venv/bin/python"
else
    PYTHON_BIN="python3"
fi

# Check if already running
if [ -f "$PID_FILE" ]; then
    PID=$(cat "$PID_FILE")
    if ps -p $PID > /dev/null 2>&1; then
        echo "预警程序已经在运行中，PID: $PID"
        exit 1
    else
        # Stale PID file, clean it up
        rm "$PID_FILE"
    fi
fi

# Run in background redirecting output to monitor.log
echo "正在后台启动 OKX Telegram 价格监控程序..."
nohup $PYTHON_BIN main.py > "$LOG_FILE" 2>&1 &
NEW_PID=$!

# Save PID to file
echo $NEW_PID > "$PID_FILE"

echo "程序启动成功！"
echo "  • PID: $NEW_PID"
echo "  • 日志写入: $LOG_FILE"
echo "  • 提示: 使用 './stop.sh' 可停止运行，使用 'tail -f $LOG_FILE' 可查看实时日志。"
