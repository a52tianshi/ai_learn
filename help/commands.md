# 单词本记忆助手 · 常用运维与开发命令

本项目的组件（Go 数据服务、MariaDB 数据库、Python Telegram Bot）均部署在用户态沙盒中。以下是管理和运行这些服务的常用命令指南。

---

## 📁 核心环境路径与配置

- **Go 编译器**: `/home/penny/go/bin/go`
- **MariaDB 安装路径**: `/home/penny/mariadb`
- **数据库配置文件**: [my.cnf](file:///home/penny/mariadb/my.cnf)
- **Python uv 管理器**: `/home/penny/.local/bin/uv`
- **Bot 环境变量配置**: [.env](file:///home/penny/ai_learn/.env)

---

## 💾 1. MariaDB 数据库管理

### 启动数据库 (后台)
```bash
/home/penny/mariadb/bin/mariadbd --defaults-file=/home/penny/mariadb/my.cnf --user=penny &
```

### 停止数据库
```bash
kill $(pgrep -f mariadbd)
```

### 检查运行状态与监听端口
```bash
# 检查进程
ps aux | grep mariadbd
# 检查端口 (3306)
ss -tulpn | grep 3306
```

### 连接数据库 CLI (手动调试)
```bash
# 使用本地 Unix 套接字直接免密连接
/home/penny/mariadb/bin/mariadb --defaults-file=/home/penny/mariadb/my.cnf -u penny wordbot
```

### 查看数据库错误日志
```bash
tail -n 100 /home/penny/mariadb-tmp/mariadbd.err
```

---

## 🌐 2. Go 数据服务管理

### 启动 Go 服务 (前台运行)
在 `/home/penny/ai_learn/service-data` 目录下运行：
```bash
MYSQL_DSN='penny@tcp(127.0.0.1:3306)/wordbot?charset=utf8mb4' /home/penny/go/bin/go run ./cmd/server
```

### 启动 Go 服务 (后台运行并重定向日志)
```bash
nohup env MYSQL_DSN='penny@tcp(127.0.0.1:3306)/wordbot?charset=utf8mb4' /home/penny/go/bin/go run ./cmd/server > go-service.log 2>&1 &
```

### 停止 Go 服务
```bash
kill $(pgrep -f 'go run ./cmd/server')
# 或者释放 8080 端口占用
kill $(lsof -t -i:8080)
```

### 运行 Go 单元测试
在 `/home/penny/ai_learn/service-data` 目录下运行：
```bash
/home/penny/go/bin/go test ./...
```

---

## 🤖 3. Python Telegram Bot 管理

### 安装/更新 Python 依赖
在 `/home/penny/ai_learn` 目录下运行：
```bash
/home/penny/.local/bin/uv sync
```

### 启动 Bot 机器人
在 `/home/penny/ai_learn` 目录下运行：
```bash
/home/penny/.local/bin/uv run python main.py
```

### 停止 Bot 机器人
```bash
kill $(pgrep -f 'python main.py')
```
