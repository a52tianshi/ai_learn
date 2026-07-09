"""集中读取环境变量(来自 .env)。"""
import logging
import os

from dotenv import load_dotenv

load_dotenv()

LOG_LEVEL = os.getenv("LOG_LEVEL", "INFO").upper()

# Telegram
TG_BOT_TOKEN = os.getenv("TG_BOT_TOKEN", "")

# Go 数据服务
DATA_API_BASE = os.getenv("DATA_API_BASE", "http://127.0.0.1:8080").rstrip("/")

# LLM(仅推荐阅读用)
MODEL = os.getenv("MODEL", "gemini-3.1-flash-lite")
# langchain-google-genai 读取 GOOGLE_API_KEY;这里只做存在性检查
GOOGLE_API_KEY = os.getenv("GOOGLE_API_KEY", "")


def setup_logging() -> logging.Logger:
    logging.basicConfig(
        level=getattr(logging, LOG_LEVEL, logging.INFO),
        format="%(asctime)s.%(msecs)03d %(levelname)s %(name)s %(message)s",
        datefmt="%H:%M:%S",
    )
    return logging.getLogger("wordbot")


def require_bot_token() -> str:
    if not TG_BOT_TOKEN:
        raise SystemExit(
            "缺少 TG_BOT_TOKEN。请在 .env 中设置从 @BotFather 获取的 Telegram Bot Token。"
        )
    return TG_BOT_TOKEN
