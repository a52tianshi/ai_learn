#!/bin/bash

# Get directory of the script
DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$DIR"

# Helper function to load env variables from file without bash errors on comments/spaces
load_env() {
    local env_file="$1"
    if [ -f "$env_file" ]; then
        while IFS= read -r line || [ -n "$line" ]; do
            # Strip carriage return
            line=$(echo "$line" | tr -d '\r')
            # Skip empty lines, comments
            if [[ ! "$line" =~ ^[[:space:]]*# && -n "$line" && "$line" == *"="* ]]; then
                key=$(echo "$line" | cut -d'=' -f1 | xargs)
                value=$(echo "$line" | cut -d'=' -f2-)
                
                # Trim leading spaces
                value=$(echo "$value" | sed -e 's/^[[:space:]]*//')
                
                # Check for quotes
                if [[ "$value" =~ ^\" ]]; then
                    value=$(echo "$value" | cut -d'"' -f2)
                elif [[ "$value" =~ ^\' ]]; then
                    value=$(echo "$value" | cut -d"'" -f2)
                else
                    # Unquoted: strip any inline comment starting with #
                    value=$(echo "$value" | cut -d'#' -f1 | xargs)
                fi
                
                if [ -n "$key" ]; then
                    export "$key=$value"
                fi
            fi
        done < "$env_file"
    fi
}

# 1. Start Go Data Service
echo "正在检查 Go 数据服务 (端口 8080)..."
if lsof -i :8080 > /dev/null 2>&1; then
    echo "  • Go 数据服务已经在运行中。"
else
    echo "  • 正在后台启动 Go 数据服务..."
    cd service-data
    
    # Check if there is any .env we should load DSN from
    load_env "../.env"
    
    nohup go run cmd/server/main.go > ../service-data.log 2>&1 &
    GO_PID=$!
    cd ..
    echo $GO_PID > service-data.pid
    echo "  • Go 数据服务启动成功，PID: $GO_PID"
fi

# 2. Start Python Telegram Bot
echo "正在检查 Python 单词本机器人..."
PID_FILE="wordbot.pid"
if [ -f "$PID_FILE" ]; then
    PID=$(cat "$PID_FILE")
    if ps -p $PID > /dev/null 2>&1; then
        echo "  • Python 单词本机器人已经在运行中，PID: $PID"
        echo "------------------------------------------------"
        echo "服务已全部就绪！"
        exit 0
    else
        rm "$PID_FILE"
    fi
fi

echo "  • 正在后台启动 Python 单词本机器人..."
# Load environment variables from .env if exists
load_env ".env"

nohup uv run python main.py > wordbot.log 2>&1 &
PY_PID=$!
echo $PY_PID > "$PID_FILE"
echo "  • Python 单词本机器人启动成功，PID: $PY_PID"

echo "------------------------------------------------"
echo "全部服务已在后台启动！"
echo "  • Go 数据服务日志: tail -f service-data.log"
echo "  • Python 机器人日志: tail -f wordbot.log"
echo "  • 停止服务请使用: ./stop.sh"
