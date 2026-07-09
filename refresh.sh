#!/bin/bash
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

load_env ".env"

echo "正在编译并运行单词库刷新脚本..."
cd service-data
go run cmd/refresh/main.go "$@"
