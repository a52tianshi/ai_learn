"""Go 数据服务的 HTTP 客户端(见 TD/01-总览.md §4.3)。"""
from __future__ import annotations

from typing import Any

import httpx

from . import config


class WordNotFound(Exception):
    """DictionaryAPI.dev 未收录该词(Go 返回 404)。"""


class DataAPIError(Exception):
    """数据服务返回非预期错误。"""


class DataClient:
    def __init__(self, base: str | None = None, timeout: float = 10.0):
        self._client = httpx.AsyncClient(
            base_url=base or config.DATA_API_BASE, timeout=timeout
        )

    async def aclose(self) -> None:
        await self._client.aclose()

    async def _json(self, resp: httpx.Response) -> Any:
        if resp.status_code == 404:
            raise WordNotFound()
        if resp.status_code >= 400:
            detail = ""
            try:
                detail = resp.json().get("error", "")
            except Exception:
                detail = resp.text
            raise DataAPIError(f"{resp.status_code}: {detail}")
        return resp.json()

    # --- 查询 ---
    async def lookup_word(self, text: str) -> dict:
        resp = await self._client.get(f"/words/{text}")
        return await self._json(resp)

    async def add_to_notebook(self, tg_user_id: int, word_id: int) -> dict:
        resp = await self._client.post(
            "/notebook", json={"tg_user_id": tg_user_id, "word_id": word_id}
        )
        return await self._json(resp)

    # --- 复习 ---
    async def due_cards(self, tg_user_id: int, limit: int = 20) -> list[dict]:
        resp = await self._client.get(
            "/reviews/due", params={"tg_user_id": tg_user_id, "limit": limit}
        )
        data = await self._json(resp)
        return data.get("cards") or []

    async def submit_review(self, user_word_id: int, quality: int) -> dict:
        resp = await self._client.post(
            "/reviews", json={"user_word_id": user_word_id, "quality": quality}
        )
        return await self._json(resp)

    # --- 推荐阅读 ---
    async def recent_words(self, tg_user_id: int, n: int = 10) -> list[dict]:
        resp = await self._client.get(
            "/words/recent", params={"tg_user_id": tg_user_id, "n": n}
        )
        data = await self._json(resp)
        return data.get("words") or []

    async def save_reading(
        self, tg_user_id: int, content: str, target_words: list[str], model: str
    ) -> dict:
        resp = await self._client.post(
            "/readings",
            json={
                "tg_user_id": tg_user_id,
                "content": content,
                "target_words": target_words,
                "model": model,
            },
        )
        return await self._json(resp)

    # --- 统计 ---
    async def stats(self, tg_user_id: int) -> dict:
        resp = await self._client.get("/stats", params={"tg_user_id": tg_user_id})
        return await self._json(resp)
