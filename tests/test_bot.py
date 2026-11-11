"""bot 层:输入校验、统一异常处理、消息分片、无状态复习流程。"""
from __future__ import annotations

from types import SimpleNamespace
from unittest.mock import AsyncMock, MagicMock

import httpx
import pytest
from telegram import Update
from telegram.error import BadRequest

from wordbot import bot
from wordbot.data_client import DataAPIError, WordNotFound


# --- 测试替身 ---
def make_message_update(text: str = "", user_id: int = 1) -> MagicMock:
    update = MagicMock(spec=Update)
    update.callback_query = None
    update.effective_user = SimpleNamespace(id=user_id)
    msg = MagicMock()
    msg.text = text
    msg.reply_text = AsyncMock()
    update.effective_message = msg
    update.message = msg
    chat = MagicMock()
    chat.send_message = AsyncMock()
    chat.send_action = AsyncMock()
    update.effective_chat = chat
    return update


def make_callback_update(data: str, user_id: int = 1) -> MagicMock:
    update = make_message_update(user_id=user_id)
    query = MagicMock()
    query.data = data
    query.answer = AsyncMock()
    query.edit_message_text = AsyncMock()
    query.edit_message_reply_markup = AsyncMock()
    update.callback_query = query
    return update


def make_context(data_client) -> SimpleNamespace:
    return SimpleNamespace(
        application=SimpleNamespace(bot_data={"data": data_client}), user_data={}
    )


def replies(update) -> list[str]:
    return [c.args[0] for c in update.effective_message.reply_text.await_args_list]


# --- 输入校验 ---
@pytest.mark.parametrize(
    "raw",
    [
        "hello world",
        "你好",
        "hello你好",
        "https://example.com/word",
        "www.example.com",
        "a" * 41,
        "This is a long paragraph of English text that someone pasted.",
        "123",
        "hello!",
        "-hello",
        "hello-",
        "don''t",
        "e.g.",
        "foo/bar",
        "naïve",
    ],
)
def test_validate_word_rejects(raw):
    word, problem = bot.validate_word(raw)
    assert word is None
    assert problem  # 必须给出友好提示


def test_validate_word_multiple_words_message():
    _, problem = bot.validate_word("hello world")
    assert "一次只能查一个单词" in problem


@pytest.mark.parametrize(
    "raw,expected",
    [
        ("hello", "hello"),
        ("  Hello  ", "Hello"),
        ("WORLD", "WORLD"),
        ("a", "a"),
        ("well-being", "well-being"),
        ("don't", "don't"),
        ("don’t", "don't"),  # iOS 弯撇号
        ("a" * 40, "a" * 40),
    ],
)
def test_validate_word_accepts(raw, expected):
    assert bot.validate_word(raw) == (expected, None)


def test_validate_word_empty_is_silently_ignored():
    assert bot.validate_word("   ") == (None, None)


async def test_on_text_invalid_input_never_calls_data_service():
    data = MagicMock()
    data.lookup_word = AsyncMock()
    update = make_message_update("hello world")
    await bot.on_text(update, make_context(data))
    data.lookup_word.assert_not_awaited()
    assert "一次只能查一个单词" in replies(update)[0]


# --- 统一异常处理 ---
@pytest.mark.parametrize(
    "exc",
    [
        DataAPIError("500: boom"),
        WordNotFound(),
        httpx.ReadTimeout("slow"),
        httpx.ConnectError("refused"),
    ],
)
async def test_handle_errors_turns_backend_errors_into_user_message(exc):
    @bot.handle_errors
    async def handler(update, context):
        raise exc

    update = make_message_update()
    await handler(update, make_context(None))  # 不抛异常
    assert replies(update) == [bot.MSG_SERVICE_DOWN]


async def test_handle_errors_unexpected_exception_is_logged_and_reported(caplog):
    @bot.handle_errors
    async def handler(update, context):
        raise KeyError("oops")

    update = make_message_update()
    with caplog.at_level("ERROR", logger="wordbot.bot"):
        await handler(update, make_context(None))
    assert replies(update) == [bot.MSG_GENERIC_FAIL]
    assert any("unexpected error" in r.message for r in caplog.records)


async def test_handle_errors_callback_uses_alert():
    @bot.handle_errors
    async def handler(update, context):
        raise DataAPIError("503: down")

    update = make_callback_update("g:1:5")
    await handler(update, make_context(None))
    update.callback_query.answer.assert_awaited_once_with(bot.MSG_SERVICE_DOWN, show_alert=True)


async def test_handle_errors_survives_failing_notification():
    @bot.handle_errors
    async def handler(update, context):
        raise DataAPIError("500")

    update = make_message_update()
    update.effective_message.reply_text.side_effect = RuntimeError("telegram down")
    await handler(update, make_context(None))  # 提示失败也不能再抛


@pytest.mark.parametrize("name", ["cmd_review", "cmd_due", "cmd_read", "cmd_list", "cmd_stats"])
async def test_all_commands_report_data_service_failure(name):
    data = MagicMock()
    for m in ("due_cards", "recent_words", "stats"):
        setattr(data, m, AsyncMock(side_effect=DataAPIError("500: db down")))
    update = make_message_update()
    await getattr(bot, name)(update, make_context(data))
    assert replies(update) == [bot.MSG_SERVICE_DOWN]


async def test_on_text_timeout_keeps_specific_message():
    data = MagicMock()
    data.lookup_word = AsyncMock(side_effect=httpx.ReadTimeout("slow"))
    update = make_message_update("hello")
    await bot.on_text(update, make_context(data))
    assert "超时" in replies(update)[0]


async def test_on_text_data_api_error_does_not_leak_details():
    data = MagicMock()
    data.lookup_word = AsyncMock(side_effect=DataAPIError("500: sql: connection refused"))
    update = make_message_update("hello")
    await bot.on_text(update, make_context(data))
    assert replies(update) == [bot.MSG_SERVICE_DOWN]


# --- 分片 ---
def _u16(s: str) -> int:
    return len(s.encode("utf-16-le")) // 2


def test_split_message_short_text_unchanged():
    assert bot.split_message("hello") == ["hello"]


def test_split_message_lines_each_chunk_within_limit():
    text = "\n".join(f"• <b>word{i}</b> <i>n.</i> 释义 &amp; 说明 {i}" for i in range(800))
    chunks = bot.split_message(text)
    assert len(chunks) > 1
    assert all(_u16(c) <= 4096 for c in chunks)
    # 按行切:不丢内容、不切断行
    assert "\n".join(chunks) == text


def test_split_message_single_huge_line_and_emoji():
    text = ("This is a sentence with <b>bold</b> and &amp; entity 😀. " * 400).strip()
    chunks = bot.split_message(text)
    assert len(chunks) > 1
    for c in chunks:
        assert _u16(c) <= 4096
        assert c.count("<b>") == c.count("</b>")  # 标签未被切半
        assert c.rfind("<") <= c.rfind(">")
        assert not c.endswith("&amp")


def test_split_message_no_spaces_hard_cut():
    chunks = bot.split_message("x" * 10000)
    assert all(_u16(c) <= 4096 for c in chunks)
    assert "".join(chunks) == "x" * 10000


async def test_reply_html_chunks_and_falls_back_per_chunk():
    update = make_message_update()
    calls = []

    async def reply_text(text, **kwargs):
        calls.append(kwargs.get("parse_mode"))
        if kwargs.get("parse_mode") and len(calls) == 1:
            raise BadRequest("Can't parse entities")

    update.effective_message.reply_text = AsyncMock(side_effect=reply_text)
    await bot._reply_html(update, "\n".join(["line " * 50] * 100))
    # 第一片 HTML 失败 → 该片纯文本重发;其余片仍按 HTML 发送
    assert calls[0] == "HTML" and calls[1] is None
    assert all(p == "HTML" for p in calls[2:])
    assert len(calls) >= 3


# --- 复习:无状态 + 单消息 edit ---
def _card(uwid: int, text: str) -> dict:
    return {
        "user_word_id": uwid,
        "text": text,
        "senses": [{"pos": "n", "meaning_en": f"meaning of {text}", "meaning_cn": "释义"}],
    }


async def test_cmd_review_sends_one_prompt_and_stores_no_queue():
    data = MagicMock()
    data.due_cards = AsyncMock(return_value=[_card(1, "apple"), _card(2, "pear")])
    update = make_message_update()
    ctx = make_context(data)
    await bot.cmd_review(update, ctx)
    assert ctx.user_data == {}
    call = update.effective_message.reply_text.await_args
    assert "apple" in call.args[0] and "还剩 <b>2</b>" in call.args[0]
    buttons = call.kwargs["reply_markup"].inline_keyboard[0]
    assert [b.callback_data for b in buttons] == ["g:1:2", "g:1:3", "g:1:5", "g:1:99"]


async def test_on_grade_reveals_and_edits_same_message_with_next_card():
    data = MagicMock()
    data.due_cards = AsyncMock(return_value=[_card(1, "apple"), _card(2, "pear")])
    data.submit_review = AsyncMock(return_value={"interval_days": 3})
    update = make_callback_update("g:1:5")
    await bot.on_grade(update, make_context(data))

    data.submit_review.assert_awaited_once_with(1, 5, tg_user_id=1)
    q = update.callback_query
    q.edit_message_text.assert_awaited_once()
    text = q.edit_message_text.await_args.args[0]
    assert "meaning of apple" in text and "3 天后" in text and "pear" in text
    kb = q.edit_message_text.await_args.kwargs["reply_markup"]
    assert kb.inline_keyboard[0][0].callback_data == "g:2:2"
    update.effective_chat.send_message.assert_not_awaited()  # 不刷新消息


async def test_on_grade_last_card_shows_done():
    data = MagicMock()
    data.due_cards = AsyncMock(return_value=[_card(1, "apple")])
    data.submit_review = AsyncMock(return_value={"interval_days": 1})
    update = make_callback_update("g:1:2")
    await bot.on_grade(update, make_context(data))
    text = update.callback_query.edit_message_text.await_args.args[0]
    assert "复习完成" in text and "meaning of apple" in text


async def test_on_grade_stale_card_is_not_resubmitted():
    data = MagicMock()
    data.due_cards = AsyncMock(return_value=[_card(2, "pear")])
    data.submit_review = AsyncMock()
    update = make_callback_update("g:1:5")
    await bot.on_grade(update, make_context(data))
    data.submit_review.assert_not_awaited()
    assert "已经复习过" in update.callback_query.answer.await_args.args[0]


async def test_on_grade_passes_current_user_id_end_to_end():
    """防「忘了传 tg_user_id」回归:走真实 DataClient + MockTransport,
    直接检查发到服务端的 POST /reviews body,不依赖调用方式(位置/关键字参数)。"""
    import json

    from wordbot.data_client import DataClient

    uid = 424242
    review_bodies: list[dict] = []

    def handler(req: httpx.Request) -> httpx.Response:
        if req.url.path == "/reviews/due":
            assert req.url.params["tg_user_id"] == str(uid)
            return httpx.Response(200, json={"cards": [_card(7, "apple")]})
        if req.url.path == "/reviews":
            review_bodies.append(json.loads(req.content))
            return httpx.Response(200, json={"interval_days": 2})
        return httpx.Response(500)

    client = DataClient(base="http://data.test")
    client._client = httpx.AsyncClient(
        base_url="http://data.test", transport=httpx.MockTransport(handler)
    )
    update = make_callback_update("g:7:3", user_id=uid)
    await bot.on_grade(update, make_context(client))
    await client.aclose()

    assert review_bodies == [{"user_word_id": 7, "quality": 3, "tg_user_id": uid}]
    assert "复习完成" in update.callback_query.edit_message_text.await_args.args[0]


async def test_on_grade_card_of_other_user_is_never_submitted():
    """伪造 callback_data 评别人的卡:本人 due 列表里没有 → 不提交。"""
    data = MagicMock()
    data.due_cards = AsyncMock(return_value=[_card(2, "pear")])  # 本人的卡
    data.submit_review = AsyncMock()
    update = make_callback_update("g:999:5", user_id=1)  # 999 属于别人
    await bot.on_grade(update, make_context(data))
    data.due_cards.assert_awaited_once_with(1, bot.REVIEW_FETCH_LIMIT)
    data.submit_review.assert_not_awaited()
    update.callback_query.edit_message_text.assert_not_awaited()


async def test_on_grade_server_ownership_rejection_is_handled():
    """服务端归属校验返回 404(WordNotFound):提示卡片失效,不揭晓、不报「服务不可用」。"""
    data = MagicMock()
    data.due_cards = AsyncMock(return_value=[_card(1, "apple")])
    data.submit_review = AsyncMock(side_effect=WordNotFound())
    update = make_callback_update("g:1:5", user_id=1)
    await bot.on_grade(update, make_context(data))
    q = update.callback_query
    q.answer.assert_awaited_once()
    assert "失效" in q.answer.await_args.args[0]
    q.edit_message_text.assert_not_awaited()


async def test_on_grade_submit_failure_alerts_user():
    data = MagicMock()
    data.due_cards = AsyncMock(return_value=[_card(1, "apple")])
    data.submit_review = AsyncMock(side_effect=httpx.ReadTimeout("slow"))
    update = make_callback_update("g:1:5")
    await bot.on_grade(update, make_context(data))
    update.callback_query.answer.assert_awaited_once_with(bot.MSG_SERVICE_DOWN, show_alert=True)
    update.callback_query.edit_message_text.assert_not_awaited()  # 卡片保留,可重试
