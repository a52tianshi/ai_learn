# 单词本记忆助手

一个带**记忆(间隔复习 SM-2)系统**的英语单词本,通过 **Telegram Bot** 使用。
三大功能:**查询单词** · **复习** · **推荐相关阅读**。

设计文档见 [`TD/01-总览.md`](TD/01-总览.md)。

## 架构

```
Telegram ──► Python(Bot + Agent)──HTTP/JSON──► Go 数据服务 ──► MySQL
                       │
                       └─ Gemini(仅用于生成阅读短文)
```

- **Python(`wordbot/`)**:Telegram 收发、查询/复习交互、调 Gemini 生成阅读。不直接连 MySQL。
- **Go(`service-data/`)**:词典抓取(DictionaryAPI.dev)、缓存、SM-2 复习调度、MySQL 持久化。对外暴露 REST。
- **用户识别**:直接用 Telegram `tg_user_id`,无注册/登录。
- **释义**:DictionaryAPI.dev(英英,免费无 Key),结果缓存入库。

## 运行准备

- Go 1.22+、Python 3.11+(用 [uv](https://docs.astral.sh/uv/))、MySQL 8
- 一个 Telegram Bot Token(找 [@BotFather](https://t.me/BotFather) 创建)
- 一个 Google AI(Gemini)API Key

## 启动步骤

### 1. 建库建表
```bash
mysql -u root -p -e "CREATE DATABASE wordbot CHARACTER SET utf8mb4;"
mysql -u root -p wordbot < service-data/migrations/schema.sql
```

### 2. 启动 Go 数据服务
```bash
cd service-data
MYSQL_DSN='root:你的密码@tcp(127.0.0.1:3306)/wordbot?charset=utf8mb4' \
  go run ./cmd/server
# 监听 :8080;健康检查 curl localhost:8080/healthz
```

### 3. 配置并启动 Bot
```bash
cp .env.example .env      # 填入 GOOGLE_API_KEY 和 TG_BOT_TOKEN
uv sync
uv run python main.py
```

### 4. 快捷脚本一键后台启停（推荐）
为了方便您在后台同时运行 Go 服务和 Python 机器人，项目根目录下提供了快捷脚本：

*   **一键后台启动**（自动检测 Go 服务和 Python 机器人，在后台启动它们并重定向日志）：
    ```bash
    ./run.sh
    ```
*   **查看运行日志**：
    - 查看 Go 数据服务日志：`tail -f service-data.log`
    - 查看 Python 机器人日志：`tail -f wordbot.log`
*   **一键关闭所有服务**（优雅停用后台进程并清理 PID 文件）：
    ```bash
    ./stop.sh
    ```

在 Telegram 里对你的 bot:直接发单词即可查询收藏。

## Bot 命令

| 输入 | 作用 |
|---|---|
| 直接发英文单词 | 查询并收藏 |
| `/review` | 开始今日复习(😵忘了 / 🤔模糊 / 😎记得) |
| `/due` | 今日待复习数量 |
| `/read` | 生成一篇含近期单词的短文 |
| `/list` | 最近收藏的单词 |
| `/stats` | 学习统计 |
| `/help` | 帮助 |

## 测试

```bash
cd service-data && go test ./...     # 含 SM-2 算法单测
```

> 生产代码(`cmd/server/main.go`、`internal/store/store.go`)实际用的是 **SQLite**(`modernc.org/sqlite`,语句里全是 `datetime('now', ...)`),不是 MySQL。但 `internal/store/store_test.go` 是一个没跟着改的历史遗留测试,还在用 `go-sql-driver/mysql` 连 MySQL、写 `NOW()`。如果机器上恰好有 MySQL 在监听 3306(`db.Ping()` 能连上就不会 `t.Skip`),这个测试就会因为 SQL 语法不对(SQLite 写法喂给 MySQL)而报错,属于该测试本身过期、待修/待删,和当前这类改动无关,出现时不用当成新 bug 去排查。

## 部署到服务器(bd 主机)笔记

远端是 `ssh bd`,代码在 `~/apps/ai_learn`,Go 服务和 Python bot 分别由 `run.sh`/`stop.sh` 管理(端口 8080 + PID 文件判断是否已运行)。更新部署的正确顺序:

```bash
ssh bd "cd ~/apps/ai_learn && git pull --ff-only"   # 1. 拉代码,run.sh 不会自动 pull
ssh bd "cd ~/apps/ai_learn && ./stop.sh"             # 2. 停掉旧进程
ssh bd "cd ~/apps/ai_learn && ./run.sh"              # 3. 重新编译 Go 服务 + 启动两个进程
```

几个容易踩的坑:

- **`run.sh` 只负责编译+启动,不会 `git pull`**。先 pull 不然等于重启了旧代码。
- **这台机器编译慢**,`go build` 常常超过 Claude Code Bash 工具默认的 120s 超时,命令会被挪到后台执行——这是正常现象,不代表卡死,等后台任务完成通知或自己 `ps aux | grep server` / 看 `service-data.log` 确认即可,不要反复用短 `sleep` 轮询刷屏。
- **本地 `git push` 可能因为拿不到 keychain 凭证失败**(非交互环境下 `osxkeychain` helper 读不到凭证,报 `could not read Username for 'https://github.com'`)。遇到这种情况应该请人用 `! git push origin main` 在交互终端里手动推送,而不是反复重试。
- 部署前先在本地跑一遍改动涉及的单测(以及方便的话在远端也跑一遍),比光靠 `go build` 过了就当没问题更稳妥。
