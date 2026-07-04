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
