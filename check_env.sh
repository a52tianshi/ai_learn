#!/bin/bash

# Get directory of the script
DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$DIR"

# Helper function to load env variables
load_env() {
    local env_file="$1"
    if [ -f "$env_file" ]; then
        while IFS= read -r line || [ -n "$line" ]; do
            line=$(echo "$line" | tr -d '\r')
            if [[ ! "$line" =~ ^[[:space:]]*# && -n "$line" && "$line" == *"="* ]]; then
                key=$(echo "$line" | cut -d'=' -f1 | xargs)
                value=$(echo "$line" | cut -d'=' -f2-)
                value=$(echo "$value" | sed -e 's/^[[:space:]]*//')
                if [[ "$value" =~ ^\" ]]; then
                    value=$(echo "$value" | cut -d'"' -f2)
                elif [[ "$value" =~ ^\' ]]; then
                    value=$(echo "$value" | cut -d"'" -f2)
                else
                    value=$(echo "$value" | cut -d'#' -f1 | xargs)
                fi
                if [ -n "$key" ]; then
                    export "$key=$value"
                fi
            fi
        done < "$env_file"
    fi
}

echo "================================================="
echo "       单词本记忆助手 · 环境诊断报告"
echo "================================================="

load_env ".env"

# 1. Check Toolchains
echo -e "\n[1/5] 工具链安装情况:"
if command -v go >/dev/null 2>&1; then
    echo -e "  • Go:      已安装 ($(go version | cut -d' ' -f3))"
else
    echo -e "  • Go:      ❌ 未安装"
fi

if command -v python3 >/dev/null 2>&1; then
    echo -e "  • Python:  已安装 ($(python3 --version))"
else
    echo -e "  • Python:  ❌ 未安装"
fi

if command -v uv >/dev/null 2>&1; then
    echo -e "  • uv:      已安装 ($(uv --version))"
else
    echo -e "  • uv:      ❌ 未安装"
fi

# 2. Check Port Listening
echo -e "\n[2/5] 关键网络端口状态:"
# Check MySQL (3306)
if lsof -i :3306 >/dev/null 2>&1 || nc -z 127.0.0.1 3306 >/dev/null 2>&1; then
    echo -e "  • MySQL (3306): 🟢 正常监听中"
else
    echo -e "  • MySQL (3306): ❌ 未启动 (3306 端口连不通，请检查本地 MySQL 服务)"
fi

# Check Go Data (8080)
if lsof -i :8080 >/dev/null 2>&1 || nc -z 127.0.0.1 8080 >/dev/null 2>&1; then
    echo -e "  • Go 服务 (8080): 🟢 正常监听中"
else
    echo -e "  • Go 服务 (8080): ❌ 未开启 (端口不可达，Go 数据服务可能崩溃或未启动)"
fi

# 3. Check Configurations
echo -e "\n[3/5] 配置文件 .env 检查:"
if [ -f ".env" ]; then
    echo -e "  • .env 文件状态: 🟢 已创建"
    
    if [ -n "$TG_BOT_TOKEN" ]; then
        echo -e "  • TG_BOT_TOKEN:  已配置 (前缀: ${TG_BOT_TOKEN:0:8}...)"
    else
        echo -e "  • TG_BOT_TOKEN:  ❌ 未配置"
    fi
    
    if [ -n "$MYSQL_DSN" ]; then
        echo -e "  • MYSQL_DSN:     已配置"
    else
        echo -e "  • MYSQL_DSN:     ⚠️ 未配置 (将尝试使用默认 DSN: root@tcp(127.0.0.1:3306)/wordbot)"
    fi
else
    echo -e "  • .env 文件状态: ❌ 未找到 .env (请从 .env.example 复制创建)"
fi

# 4. Check Logs for Go Data Service
if [ -f "service-data.log" ]; then
    echo -e "\n[4/5] Go 数据服务最新日志 (service-data.log):"
    echo "-------------------------------------------------"
    tail -n 10 service-data.log
    echo "-------------------------------------------------"
fi

# 5. Check Logs for Python Bot
if [ -f "wordbot.log" ]; then
    echo -e "\n[5/5] Python 机器人最新日志 (wordbot.log):"
    echo "-------------------------------------------------"
    tail -n 10 wordbot.log
    echo "-------------------------------------------------"
fi
echo "================================================="
