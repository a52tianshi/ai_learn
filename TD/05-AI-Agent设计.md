# AI Agent / Python 侧设计(文件5)

> 深化 [`01-总览.md`](01-总览.md) 第 5 节。对应实现目录 `wordbot/`,入口 `main.py`。
> 职责:Telegram 收发 + 交互编排 + 调 Gemini 生成阅读。数据一律经 HTTP 调 Go,**不直接连 MySQL**。

## 0. 一个如实说明:当前不是 langgraph Agent

总览最初设想用 langgraph 做「意图路由 + 工具调用」的 Agent。落地时发现:

- 查询/复习/统计都是**确定性命令**,用 Telegram 原生命令 + 按钮体验更好、更省 token;
- 全项目**唯一需要 LLM 的只有「生成阅读短文」**,是一次单轮调用,不需要工具编排。

所以 MVP 采用**命令式编排**:`python-telegram-bot` 的 handler 直接调 Go REST,`/read` 触发一次 Gemini 调用。langgraph/工具调用留作后续「自然语言对话路由」时再引入(见 §7)。这样更简单、可靠、可控。

## 1. 模块结构

```
wordbot/
├── config.py         # 读 .env,日志,token 校验
├── data_client.py    # Go 数据服务 httpx 异步客户端
├── reading.py        # Gemini 生成阅读(唯一 LLM 环节,懒加载)
├── formatting.py     # 把数据结构格式化成 Telegram HTML
└── bot.py            # handlers + 生命周期 + main()
main.py               # 入口:from wordbot.bot import main
```

依赖方向:`bot → (config, data_client, reading, formatting)`;`reading → config`;`data_client → config`。

## 2. 配置(.env)

| 变量 | 用途 | 必填 |
|---|---|---|
| `TG_BOT_TOKEN` | Telegram Bot Token(@BotFather) | ✅ |
| `GOOGLE_API_KEY` | Gemini,`langchain-google-genai` 读取 | 仅 `/read` 需要 |
| `DATA_API_BASE` | Go 服务地址(默认 `http://127.0.0.1:8080`) | |
| `MODEL` | Gemini 模型(默认 `gemini-2.5-flash`) | |
| `LOG_LEVEL` | 日志级别 | |

`config.require_bot_token()` 在启动时校验 token,缺失直接 `SystemExit` 并给出中文提示。

## 3. 命令 → 处理器映射(bot.py)

| 输入 | Handler | 动作 |
|---|---|---|
| `/start`、`/help` | `cmd_start` | 发帮助 |
| 纯文本(非命令) | `on_text` | 查词 + 收藏 |
| `/review` | `cmd_review` | 拉今日到期,进入复习流 |
| 复习按钮回调 `g:<uwid>:<q>` | `on_grade` | 提交 SM-2,揭晓,下一张 |
| `/due` | `cmd_due` | 今日待复习数量 |
| `/read` | `cmd_read` | 生成阅读短文 |
| `/list` | `cmd_list` | 近期收藏 |
| `/stats` | `cmd_stats` | 学习统计 |
| 任意异常 | `on_error` | 记日志 |

`CallbackQueryHandler` 用正则 `^g:\d+:\d+$` 精确匹配,避免误吞其它回调。

## 4. 三条链路

### 4.1 查询(on_text)
```
用户发 "resilient"
  → GET /words/resilient
       404 → 回复「没找到,检查拼写?」
       503/其它 → 回复「服务出错了」
       200 → POST /notebook {tg_user_id, word_id}   (失败仅告警,不打断)
  → formatting.word_card() 以 HTML 回复
```
- `tg_user_id = update.effective_user.id`(Telegram 天然身份)。
- 收藏失败不阻断查词结果展示(体验优先)。

### 4.2 复习(cmd_review + on_grade)
```
/review
  → GET /reviews/due?tg_user_id=&limit=20
       空 → 「今天没有要复习的词啦」
       非空 → 存 context.user_data["due"] = cards,发第一张提问面
提问面:review_prompt()(只露单词,遮释义)+ 三个按钮
  😵忘了=2   🤔模糊=3   😎记得=5     (callback_data = g:<uwid>:<quality>)
按钮回调 on_grade:
  → POST /reviews {user_word_id, quality}
  → 从队列移除该卡;edit_message 揭晓 review_reveal()(释义 + 下次复习时间)
  → 发下一张;队列空则「复习完成」
```
- **复习队列**存在 `context.user_data`(每用户会话态),不落库;进度即时反映在 Go 侧 `due_at`。
- 质量映射:`忘了→2`(<3 视作 lapse),`模糊→3`,`记得→5`。
- 「下次复习时间」来自 Go 返回的 `interval_days`(`<=1` 显示「明天」)。

### 4.3 推荐阅读(cmd_read)—— 唯一 LLM 环节
```
/read
  → GET /words/recent?tg_user_id=&n=8
       空 → 「先收藏几个单词再来读」
  → sendChatAction(typing)
  → reading.generate_reading(targets)     # 调 Gemini
       失败(无 key/网络/模型)→ 回复「生成阅读失败: …」
  → POST /readings 留档(失败仅告警)
  → 回复短文 + 「🔑 目标词列表」
```

## 5. 阅读生成(reading.py)

- **懒加载**:首次 `/read` 才实例化 `ChatGoogleGenerativeAI`;查询/复习无需 `GOOGLE_API_KEY`。
- **模型**:`config.MODEL`(默认 `gemini-2.5-flash`),`temperature=0.7`。
- **Prompt**(要点):写一段 90–130 词、B1–B2 难度、自然连贯的短文,必须自然用上全部目标词,并把每个目标词用 `<b>…</b>` 包裹;只输出正文。
- **返回**:去空白的 HTML 文本;兼容返回内容块列表的模型版本(拼接 `text`)。
- **异步**:`await llm.ainvoke(prompt)`,不阻塞事件循环。

## 6. 展示与健壮性

### 6.1 formatting.py(Telegram HTML)
- 所有来自数据的动态文本 `html.escape`,防止破坏 HTML 解析;阅读短文里的 `<b>` 是 LLM 有意产出,单独走文本发送。
- `word_card`:词 + 音标(`<code>`)+ 最多 4 条义项(词性斜体、例句斜体、同义词 `≈`)。
- `review_prompt` / `review_reveal` / `stats_text` / `word_list` 各司其职。

### 6.2 发送降级
`_reply_html()` 先按 HTML 发;若内容含非法标记触发 `BadRequest`,自动降级为纯文本重发。复习揭晓的 `edit_message_text` 同样处理。

### 6.3 错误边界
- `DataClient` 把 404→`WordNotFound`、其它 4xx/5xx→`DataAPIError`。
- handler 内对这两类异常给中文提示;非致命的写操作(收藏、留档)失败只 `log.warning`,不打断主流程。
- 全局 `add_error_handler` 兜底记录未捕获异常。

## 7. 演进:升级为 langgraph Agent

当需要「用自然语言对话式操作」(如「把上次那个词的例句再给我一个」「我这周学得怎么样」)时:
- 把现有 REST 调用封装为 **langgraph 工具**:`lookup_word` / `start_review` / `grade_review` / `recommend_reading` / `get_stats`。
- 加一层意图路由节点(LLM 决定调哪个工具),会话态用 langgraph checkpoint 按 `tg_user_id` 维护 thread(总览 §5.3 的「短期记忆」)。
- 命令与按钮仍保留(高频确定性操作走命令,省 token)。

## 8. 运行

```bash
cp .env.example .env      # 填 TG_BOT_TOKEN、GOOGLE_API_KEY
uv sync
uv run python main.py     # 需 Go 数据服务已在 DATA_API_BASE 运行
```
`main()`:装配 `Application`,注册 handlers,`post_init` 建 `DataClient`,`post_shutdown` 关闭,`run_polling()`。
