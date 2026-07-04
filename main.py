"""单词本记忆助手 · 启动入口。运行 Telegram Bot。

先启动 Go 数据服务(service-data),再运行本文件。
配置见 .env(TG_BOT_TOKEN / DATA_API_BASE / GOOGLE_API_KEY / MODEL)。
"""
from wordbot.bot import main

if __name__ == "__main__":
    main()
