"""DataClient:状态码映射、响应解析、超时传播(httpx.MockTransport,无网络)。"""
from __future__ import annotations

import json

import httpx
import pytest

from wordbot.data_client import DataAPIError, DataClient, WordNotFound


def make_client(handler) -> DataClient:
    client = DataClient(base="http://data.test")
    client._client = httpx.AsyncClient(
        base_url="http://data.test", transport=httpx.MockTransport(handler)
    )
    return client


async def test_404_raises_word_not_found():
    client = make_client(lambda req: httpx.Response(404, json={"error": "not found"}))
    with pytest.raises(WordNotFound):
        await client.lookup_word("zzzz")
    await client.aclose()


@pytest.mark.parametrize("status", [400, 500, 502, 503])
async def test_error_status_raises_data_api_error_with_error_field(status):
    client = make_client(lambda req: httpx.Response(status, json={"error": "db down"}))
    with pytest.raises(DataAPIError) as exc:
        await client.stats(1)
    assert str(status) in str(exc.value)
    assert "db down" in str(exc.value)
    await client.aclose()


async def test_error_status_with_non_json_body_uses_text():
    client = make_client(lambda req: httpx.Response(502, text="Bad Gateway"))
    with pytest.raises(DataAPIError) as exc:
        await client.due_cards(1)
    assert "Bad Gateway" in str(exc.value)
    await client.aclose()


async def test_lookup_word_parses_json_and_hits_right_path():
    seen = {}

    def handler(req: httpx.Request) -> httpx.Response:
        seen["path"] = req.url.path
        return httpx.Response(200, json={"id": 7, "text": "hello", "senses": []})

    client = make_client(handler)
    word = await client.lookup_word("hello")
    assert word == {"id": 7, "text": "hello", "senses": []}
    assert seen["path"] == "/words/hello"
    await client.aclose()


async def test_due_cards_and_recent_words_unwrap_lists():
    def handler(req: httpx.Request) -> httpx.Response:
        if req.url.path == "/reviews/due":
            assert req.url.params["tg_user_id"] == "42"
            assert req.url.params["limit"] == "5"
            return httpx.Response(200, json={"cards": [{"user_word_id": 1}]})
        if req.url.path == "/words/recent":
            return httpx.Response(200, json={"words": None})
        return httpx.Response(500)

    client = make_client(handler)
    assert await client.due_cards(42, 5) == [{"user_word_id": 1}]
    assert await client.recent_words(42) == []  # null → 空列表
    await client.aclose()


def _capture_reviews():
    """返回 (handler, bodies):记录每次 POST /reviews 的 JSON body。"""
    bodies: list[dict] = []

    def handler(req: httpx.Request) -> httpx.Response:
        assert req.method == "POST" and req.url.path == "/reviews"
        bodies.append(json.loads(req.content))
        return httpx.Response(200, json={"interval_days": 36500})

    return handler, bodies


async def test_submit_review_without_user_id_omits_field():
    """向后兼容:不给 tg_user_id 时 body 里不能出现该字段(也不能是 null)。"""
    handler, bodies = _capture_reviews()
    client = make_client(handler)
    assert (await client.submit_review(3, 99))["interval_days"] == 36500
    assert bodies == [{"user_word_id": 3, "quality": 99}]
    assert "tg_user_id" not in bodies[0]
    await client.aclose()


@pytest.mark.parametrize("uid", [123456789, 0])
async def test_submit_review_with_user_id_sends_field(uid):
    """给了 tg_user_id 就写进 body;0 也要照传(服务端对零值同样做校验)。"""
    handler, bodies = _capture_reviews()
    client = make_client(handler)
    await client.submit_review(3, 5, tg_user_id=uid)
    assert bodies == [{"user_word_id": 3, "quality": 5, "tg_user_id": uid}]
    await client.aclose()


async def test_submit_review_ownership_mismatch_raises_word_not_found():
    """服务端归属校验失败返回 404 → WordNotFound。"""
    client = make_client(
        lambda req: httpx.Response(404, json={"error": "user_word not found"})
    )
    with pytest.raises(WordNotFound):
        await client.submit_review(3, 5, tg_user_id=999)
    await client.aclose()


async def test_timeout_propagates():
    def handler(req: httpx.Request) -> httpx.Response:
        raise httpx.ReadTimeout("slow", request=req)

    client = make_client(handler)
    with pytest.raises(httpx.TimeoutException):
        await client.lookup_word("slow")
    await client.aclose()
