"""Telegram Bot:命令与消息处理,串起查询 / 复习 / 推荐阅读。"""
from __future__ import annotations

import functools
import logging
import re
from typing import Awaitable, Callable

import httpx

from telegram import (
    InlineKeyboardButton,
    InlineKeyboardMarkup,
    Update,
)
from telegram.constants import ChatAction
from telegram.request import HTTPXRequest
from telegram.error import BadRequest, TelegramError
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

# 用户可见的统一错误文案
MSG_SERVICE_DOWN = "服务暂时不可用,稍后再试 🙏"
MSG_GENERIC_FAIL = "出了点问题,没能完成这次操作,稍后再试 🙏"

# 数据服务相关的「可预期」故障:业务错误 / 超时 / 连不上
BACKEND_ERRORS = (DataAPIError, WordNotFound, httpx.TransportError)

# Telegram 单条消息上限(按 UTF-16 码元计)
TG_MAX_LEN = 4096

# 复习时一次从服务端取多少张 due 卡(只用于找当前卡 + 下一张 + 估算剩余数)
REVIEW_FETCH_LIMIT = 20


def _data(context: ContextTypes.DEFAULT_TYPE) -> DataClient:
    return context.application.bot_data["data"]


# --- 统一异常处理 ---
Handler = Callable[[Update, ContextTypes.DEFAULT_TYPE], Awaitable[None]]


async def _notify_failure(update: object, text: str) -> None:
    """尽力把失败提示告诉用户;本身失败只记日志,绝不再抛。"""
    if not isinstance(update, Update):
        return
    try:
        query = update.callback_query
        if query is not None:
            # 按钮回调:优先弹窗提示;若该回调已应答过/已过期,退回发一条消息
            try:
                await query.answer(text, show_alert=True)
                return
            except TelegramError:
                pass
        if update.effective_message is not None and query is None:
            await update.effective_message.reply_text(text)
        elif update.effective_chat is not None:
            await update.effective_chat.send_message(text)
    except Exception:  # noqa: BLE001 - 兜底,提示失败也不能让 handler 崩
        log.exception("failed to notify user about error")


def handle_errors(fn: Handler) -> Handler:
    """handler 装饰器:任何异常都转成用户能看懂的中文提示,并记录日志,绝不静默。

    - 数据服务错误 / 超时 / 连接失败 → MSG_SERVICE_DOWN
    - 其它异常 → 记录完整堆栈 + MSG_GENERIC_FAIL
    """

    @functools.wraps(fn)
    async def wrapper(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
        try:
            await fn(update, context)
        except BACKEND_ERRORS as e:
            log.warning("%s: data service error: %r", fn.__name__, e)
            await _notify_failure(update, MSG_SERVICE_DOWN)
        except Exception:  # noqa: BLE001
            log.exception("%s: unexpected error", fn.__name__)
            await _notify_failure(update, MSG_GENERIC_FAIL)

    return wrapper


# --- 消息分片与发送 ---
def _u16len(s: str) -> int:
    """Telegram 按 UTF-16 码元计长度(emoji 等占 2)。"""
    return len(s.encode("utf-16-le")) // 2


def _fit_prefix(s: str, limit: int) -> int:
    """返回最大的 i,使 s[:i] 的 UTF-16 长度 <= limit。"""
    total = 0
    for i, ch in enumerate(s):
        total += 2 if ord(ch) > 0xFFFF else 1
        if total > limit:
            return i
    return len(s)


_SENTENCE_END = re.compile(r"[.!?。!?;;…](?:\s|$)")


def _safe_cut(window: str) -> int:
    """在 window 内找切点:不落在 HTML 标签 <...> 或实体 &...; 中间。"""
    cut = len(window)
    lt, gt = window.rfind("<"), window.rfind(">")
    if lt > gt and lt > 0:
        cut = lt
    amp = window.rfind("&", 0, cut)
    if amp > window.rfind(";", 0, cut) and cut - amp <= 10 and amp > 0:
        cut = amp
    return cut


def split_message(text: str, limit: int = TG_MAX_LEN) -> list[str]:
    """把长文本按 Telegram 上限切片,每片 UTF-16 长度 <= limit。

    切点优先级:换行 > 句末标点 > 空格 > 硬切;任何切点都避开 HTML 标签与实体内部。
    (标签跨片的极端情况由发送方按片降级纯文本兜底。)
    """
    chunks: list[str] = []
    rest = text
    while rest:
        if _u16len(rest) <= limit:
            chunks.append(rest)
            break
        end = max(_fit_prefix(rest, limit), 1)  # 保证每轮都有进展
        window = rest[:end]

        nl = window.rfind("\n")
        if nl > 0:
            chunk, rest = window[:nl], rest[nl + 1:]
        else:
            cut = 0
            for m in _SENTENCE_END.finditer(window):
                cut = m.start() + 1
            if cut <= 0:
                cut = window.rfind(" ")
            if cut <= 0:
                cut = end
            cut = _safe_cut(window[:cut]) or cut
            chunk, rest = window[:cut].rstrip(), rest[cut:].lstrip(" ")
        if chunk.strip():
            chunks.append(chunk)
    return chunks or [text]


async def _reply_html(update: Update, text: str, **kwargs) -> None:
    """按 4096 上限分片后逐片以 HTML 发送;某片 HTML 解析失败则仅该片降级为纯文本。

    reply_markup 等附加参数只挂在最后一片上。
    """
    msg = update.effective_message
    chunks = split_message(text)
    for i, chunk in enumerate(chunks):
        extra = kwargs if i == len(chunks) - 1 else {}
        try:
            await msg.reply_text(chunk, parse_mode=HTML, **extra)
        except BadRequest as e:
            log.info("HTML send failed (%s), falling back to plain text", e)
            await msg.reply_text(chunk, **extra)


# --- 输入校验 ---
WORD_MAX_LEN = 40
_WORD_RE = re.compile(r"^[A-Za-z]+(?:['-][A-Za-z]+)*$")
_CJK_RE = re.compile(r"[\u3400-\u9fff\uf900-\ufaff]")
_URL_RE = re.compile(r"(?i)^(?:https?://|www\.)|://")


def validate_word(raw: str) -> tuple[str | None, str | None]:
    """校验用户输入是否为单个英文单词。

    合法:字母组成,允许内部连字符 / 撇号(弯撇号 ’ 视同 '),长度 1-40,不区分大小写。
    返回 (单词, None) 或 (None, 给用户的提示)。空输入返回 (None, None) 表示静默忽略。
    """
    text = (raw or "").strip().replace("\u2019", "'")
    if not text:
        return None, None
    if _URL_RE.search(text):
        return None, "这看起来是个链接 🔗 我只能查英文单词哦"
    if len(text.split()) > 1:
        return None, "一次只能查一个单词哦 ✍️ 换个单词单独发我吧"
    if _CJK_RE.search(text):
        return None, "目前只支持查英文单词 🙂 直接发英文单词给我吧"
    if len(text) > WORD_MAX_LEN:
        return None, f"这个太长了,单词最多 {WORD_MAX_LEN} 个字母哦"
    if not _WORD_RE.match(text):
        return None, "这看起来不是一个英文单词 🤔(只支持字母,以及词中的连字符 - 和撇号 ')"
    return text, None


# --- 基础命令 ---
@handle_errors
async def cmd_start(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    await _reply_html(update, HELP)


# --- 查询 ---
@handle_errors
async def on_text(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    msg = update.effective_message
    text, problem = validate_word(msg.text or "")
    if problem:
        await msg.reply_text(problem)
        return
    if not text:
        return
    uid = update.effective_user.id
    try:
        word = await _data(context).lookup_word(text)
    except WordNotFound:
        await msg.reply_text(f"没找到 “{text}” 🤔(词典未收录,检查下拼写?)")
        return
    except httpx.TimeoutException:
        await msg.reply_text(
            f"查询 “{text}” 超时了,词典服务可能比较慢,稍后再试一下?"
        )
        return
    # 其它数据服务错误(DataAPIError / 连接失败)交给 handle_errors 统一提示

    try:
        await _data(context).add_to_notebook(uid, word["id"])
    except BACKEND_ERRORS as e:
        # 收藏失败不影响展示查询结果
        log.warning("add_to_notebook failed: %r", e)

    await _reply_html(update, formatting.word_card(word))


# --- 复习 ---
# 无状态:不在内存里存队列,每次都从服务端取当前 due 卡;
# 整个复习过程只用一条消息,反复 edit 展示「上一词揭晓 + 下一词提问」。
def _review_keyboard(uwid: int) -> InlineKeyboardMarkup:
    return InlineKeyboardMarkup(
        [[
            InlineKeyboardButton("😵 忘了", callback_data=f"g:{uwid}:2"),
            InlineKeyboardButton("🤔 模糊", callback_data=f"g:{uwid}:3"),
            InlineKeyboardButton("😎 记得", callback_data=f"g:{uwid}:5"),
            InlineKeyboardButton("👌 太简单", callback_data=f"g:{uwid}:99"),
        ]]
    )


def _progress_line(remaining: int, capped: bool) -> str:
    n = f"{remaining}+" if capped else str(remaining)
    return f"📚 今日待复习还剩 <b>{n}</b> 个"


def _prompt_block(card: dict, remaining: int, capped: bool) -> str:
    return f"{_progress_line(remaining, capped)}\n\n{formatting.review_prompt(card)}"


async def _edit_html(query, text: str, reply_markup=None) -> None:
    """编辑回调所在消息;HTML 失败降级纯文本,内容未变化则忽略。"""
    try:
        await query.edit_message_text(text, parse_mode=HTML, reply_markup=reply_markup)
    except BadRequest as e:
        if "not modified" in str(e).lower():
            return
        log.info("HTML edit failed (%s), falling back to plain text", e)
        await query.edit_message_text(text, reply_markup=reply_markup)


@handle_errors
async def cmd_review(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    uid = update.effective_user.id
    cards = await _data(context).due_cards(uid, REVIEW_FETCH_LIMIT)
    if not cards:
        await update.effective_message.reply_text("🎉 今天没有要复习的词啦!")
        return
    card = cards[0]
    await _reply_html(
        update,
        _prompt_block(card, len(cards), len(cards) >= REVIEW_FETCH_LIMIT),
        reply_markup=_review_keyboard(card["user_word_id"]),
    )


@handle_errors
async def on_grade(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    query = update.callback_query
    try:
        _, uwid_s, q_s = query.data.split(":")
        uwid, quality = int(uwid_s), int(q_s)
    except ValueError:
        await query.answer()
        return

    uid = update.effective_user.id
    data = _data(context)
    # 提交前先取该用户当前 due 列表:既拿到卡片内容用于揭晓,
    # 也顺带校验这张卡确实属于该用户且仍待复习(防重复点击 / 旧消息上的按钮)。
    cards = await data.due_cards(uid, REVIEW_FETCH_LIMIT)
    card = next((c for c in cards if c.get("user_word_id") == uwid), None)
    if card is None:
        await query.answer("这个词已经复习过了 👍 输入 /review 继续")
        try:
            await query.edit_message_reply_markup(reply_markup=None)
        except BadRequest:
            pass
        return

    try:
        # 传当前用户 id,服务端据此做归属校验(防 IDOR:伪造 callback_data 评别人的卡)
        uw = await data.submit_review(uwid, quality, tg_user_id=uid)
    except WordNotFound:
        # 服务端判定该卡不属于此用户 / 已不存在:不揭晓、不继续
        log.warning("submit_review rejected: uwid=%s uid=%s", uwid, uid)
        await query.answer("这张卡片已失效 🤔 输入 /review 重新开始", show_alert=True)
        return
    await query.answer()

    reveal = formatting.review_reveal(card, uw.get("interval_days", 1))
    rest = [c for c in cards if c.get("user_word_id") != uwid]
    if not rest:
        await _edit_html(query, f"{reveal}\n\n✅ 复习完成,干得漂亮!")
        return

    nxt = rest[0]
    prompt = _prompt_block(nxt, len(rest), len(cards) >= REVIEW_FETCH_LIMIT)
    sep = "\n\n──────────\n"
    budget = TG_MAX_LEN - _u16len(sep + prompt)
    if _u16len(reveal) > budget:  # 极端长释义:截断揭晓部分,保证能 edit 成功
        reveal = split_message(reveal, max(budget, 1))[0]
    await _edit_html(
        query, f"{reveal}{sep}{prompt}", reply_markup=_review_keyboard(nxt["user_word_id"])
    )


@handle_errors
async def cmd_due(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    cards = await _data(context).due_cards(update.effective_user.id)
    if cards:
        await update.effective_message.reply_text(
            f"今日待复习:{len(cards)} 个。输入 /review 开始 👇"
        )
    else:
        await update.effective_message.reply_text("今天没有待复习的词 🎉")


# --- 推荐阅读(唯一 LLM 环节) ---
@handle_errors
async def cmd_read(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    uid = update.effective_user.id
    words = await _data(context).recent_words(uid, 8)
    if not words:
        await update.effective_message.reply_text("先发我几个单词收藏一下,再来读文章吧 📖")
        return
    targets = [w["text"] for w in words]
    await update.effective_chat.send_action(ChatAction.TYPING)
    try:
        passage = await generate_reading(targets)
    except Exception:  # 模型/密钥/网络等;细节只进日志,不暴露给用户
        log.exception("generate_reading failed")
        await update.effective_message.reply_text("生成阅读失败了,稍后再试 🙏")
        return

    try:
        await _data(context).save_reading(uid, passage, targets, config.MODEL)
    except BACKEND_ERRORS as e:
        # 存档失败不影响把文章发给用户
        log.warning("save_reading failed: %r", e)

    footer = "\n\n🔑 " + ", ".join(targets)
    await _reply_html(update, passage + footer)


# --- 其它 ---
@handle_errors
async def cmd_list(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    words = await _data(context).recent_words(update.effective_user.id, 20)
    await _reply_html(update, formatting.word_list(words))


@handle_errors
async def cmd_stats(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    st = await _data(context).stats(update.effective_user.id)
    await _reply_html(update, formatting.stats_text(st))


async def on_error(update: object, context: ContextTypes.DEFAULT_TYPE) -> None:
    """最后一道兜底:handler 已统一处理,能到这里的多是框架层错误。"""
    log.error("handler error", exc_info=context.error)
    await _notify_failure(update, MSG_GENERIC_FAIL)


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
    custom_transport = httpx.AsyncHTTPTransport(local_address="0.0.0.0")
    request_class = HTTPXRequest(
        httpx_kwargs={"transport": custom_transport}
    )

    app = (
        Application.builder()
        .token(token)
        .request(request_class)
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
