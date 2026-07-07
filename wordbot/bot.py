"""Telegram Bot:命令与消息处理,串起查询 / 复习 / 推荐阅读。"""
from __future__ import annotations

import logging
import httpx

from telegram import (
    InlineKeyboardButton,
    InlineKeyboardMarkup,
    Update,
)
from telegram.constants import ChatAction
from telegram.error import BadRequest
from telegram.ext import (
    Application,
    CallbackQueryHandler,
    CommandHandler,
    ContextTypes,
    MessageHandler,
    filters,
)

from . import config, formatting
from .data_client import DataAPIError, DataClient, WordNotFound
from .reading import generate_reading

log = logging.getLogger("wordbot.bot")
HTML = "HTML"

HELP = (
    "📖 <b>单词本记忆助手</b>\n\n"
    "• 直接发我一个英文单词 → 查询并收藏\n"
    "• /review 开始今日复习(间隔重复)\n"
    "• /due 查看今日待复习数量\n"
    "• /read 生成一篇含你近期单词的短文\n"
    "• /list 最近收藏的单词\n"
    "• /stats 学习统计\n"
    "• /help 帮助"
)


def _data(context: ContextTypes.DEFAULT_TYPE) -> DataClient:
    return context.application.bot_data["data"]


async def _reply_html(update: Update, text: str, **kwargs) -> None:
    """优先按 HTML 发送;若 LLM/内容含非法标记导致失败,降级为纯文本。"""
    try:
        await update.effective_message.reply_text(text, parse_mode=HTML, **kwargs)
    except BadRequest:
        await update.effective_message.reply_text(text, **kwargs)


# --- 基础命令 ---
async def cmd_start(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    await _reply_html(update, HELP)


# --- 查询 ---
async def on_text(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    text = (update.message.text or "").strip()
    if not text:
        return
    uid = update.effective_user.id
    try:
        word = await _data(context).lookup_word(text)
    except WordNotFound:
        await update.message.reply_text(f"没找到 “{text}” 🤔(词典未收录,检查下拼写?)")
        return
    except DataAPIError as e:
        await update.message.reply_text(f"服务出错了:{e}")
        return

    try:
        await _data(context).add_to_notebook(uid, word["id"])
    except DataAPIError as e:
        log.warning("add_to_notebook failed: %s", e)

    await _reply_html(update, formatting.word_card(word))


# --- 复习 ---
async def cmd_review(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    uid = update.effective_user.id
    cards = await _data(context).due_cards(uid)
    if not cards:
        await update.message.reply_text("🎉 今天没有要复习的词啦!")
        return
    context.user_data["due"] = cards
    await _send_review_prompt(update, context)


async def _send_review_prompt(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    queue = context.user_data.get("due") or []
    chat = update.effective_chat
    if not queue:
        await chat.send_message("✅ 复习完成,干得漂亮!")
        return
    card = queue[0]
    uwid = card["user_word_id"]
    kb = InlineKeyboardMarkup(
        [[
            InlineKeyboardButton("😵 忘了", callback_data=f"g:{uwid}:2"),
            InlineKeyboardButton("🤔 模糊", callback_data=f"g:{uwid}:3"),
            InlineKeyboardButton("😎 记得", callback_data=f"g:{uwid}:5"),
        ]]
    )
    await chat.send_message(
        formatting.review_prompt(card), parse_mode=HTML, reply_markup=kb
    )


async def on_grade(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    query = update.callback_query
    await query.answer()
    try:
        _, uwid_s, q_s = query.data.split(":")
        uwid, quality = int(uwid_s), int(q_s)
    except ValueError:
        return

    try:
        uw = await _data(context).submit_review(uwid, quality)
    except DataAPIError as e:
        await query.edit_message_text(f"提交失败:{e}")
        return

    queue = context.user_data.get("due") or []
    card = next((c for c in queue if c["user_word_id"] == uwid), None)
    context.user_data["due"] = [c for c in queue if c["user_word_id"] != uwid]

    if card:
        text = formatting.review_reveal(card, uw.get("interval_days", 1))
        try:
            await query.edit_message_text(text, parse_mode=HTML)
        except BadRequest:
            await query.edit_message_text(text)

    await _send_review_prompt(update, context)


async def cmd_due(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    cards = await _data(context).due_cards(update.effective_user.id)
    if cards:
        await update.message.reply_text(f"今日待复习:{len(cards)} 个。输入 /review 开始 👇")
    else:
        await update.message.reply_text("今天没有待复习的词 🎉")


# --- 推荐阅读(唯一 LLM 环节) ---
async def cmd_read(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    uid = update.effective_user.id
    words = await _data(context).recent_words(uid, 8)
    if not words:
        await update.message.reply_text("先发我几个单词收藏一下,再来读文章吧 📖")
        return
    targets = [w["text"] for w in words]
    await update.effective_chat.send_action(ChatAction.TYPING)
    try:
        passage = await generate_reading(targets)
    except Exception as e:  # 模型/密钥/网络等
        log.warning("generate_reading failed: %s", e)
        await update.message.reply_text(f"生成阅读失败:{e}")
        return

    try:
        await _data(context).save_reading(uid, passage, targets, config.MODEL)
    except DataAPIError as e:
        log.warning("save_reading failed: %s", e)

    footer = "\n\n🔑 " + ", ".join(targets)
    await _reply_html(update, passage + footer)


# --- 其它 ---
async def cmd_list(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    words = await _data(context).recent_words(update.effective_user.id, 20)
    await _reply_html(update, formatting.word_list(words))


async def cmd_stats(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    st = await _data(context).stats(update.effective_user.id)
    await _reply_html(update, formatting.stats_text(st))


async def on_error(update: object, context: ContextTypes.DEFAULT_TYPE) -> None:
    log.error("handler error", exc_info=context.error)


# --- 生命周期 ---
async def _post_init(app: Application) -> None:
    app.bot_data["data"] = DataClient()
    log.info("bot ready, data service = %s", config.DATA_API_BASE)


async def _post_shutdown(app: Application) -> None:
    data = app.bot_data.get("data")
    if data:
        await data.aclose()


def main() -> None:
    config.setup_logging()
    token = config.require_bot_token()

    # Force outbound traffic to use IPv4 to avoid broken IPv6 routes on some VPS hosts
    transport = httpx.AsyncHTTPTransport(local_address="0.0.0.0")
    custom_client = httpx.AsyncClient(transport=transport)

    app = (
        Application.builder()
        .token(token)
        .httpx_client(custom_client)
        .post_init(_post_init)
        .post_shutdown(_post_shutdown)
        .build()
    )

    app.add_handler(CommandHandler(["start", "help"], cmd_start))
    app.add_handler(CommandHandler("review", cmd_review))
    app.add_handler(CommandHandler("due", cmd_due))
    app.add_handler(CommandHandler("read", cmd_read))
    app.add_handler(CommandHandler("list", cmd_list))
    app.add_handler(CommandHandler("stats", cmd_stats))
    app.add_handler(CallbackQueryHandler(on_grade, pattern=r"^g:\d+:\d+$"))
    app.add_handler(MessageHandler(filters.TEXT & ~filters.COMMAND, on_text))
    app.add_error_handler(on_error)

    log.info("starting polling ...")
    app.run_polling()


if __name__ == "__main__":
    main()
