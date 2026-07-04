"""用 Gemini 生成一段含目标词的定制短文(全项目唯一的 LLM 环节)。"""
from __future__ import annotations

import logging

from . import config

log = logging.getLogger("wordbot.reading")

_PROMPT = (
    "You are an English tutor. Write ONE short, natural, coherent passage "
    "(about 90-130 words) that a learner can enjoy reading. "
    "You MUST naturally use every one of these target words at least once: {words}. "
    "Wrap each target word in <b>...</b> where it appears. "
    "Keep it at an intermediate (B1-B2) level. Output only the passage, no title, no notes."
)

# 懒加载:仅在真正需要生成阅读时才初始化模型,查询/复习无需 GOOGLE_API_KEY。
_llm = None


def _get_llm():
    global _llm
    if _llm is None:
        from langchain_google_genai import ChatGoogleGenerativeAI

        if not config.GOOGLE_API_KEY:
            raise RuntimeError(
                "缺少 GOOGLE_API_KEY,无法生成阅读。请在 .env 中配置。"
            )
        _llm = ChatGoogleGenerativeAI(model=config.MODEL, temperature=0.7)
    return _llm


async def generate_reading(words: list[str]) -> str:
    """根据目标词生成短文,返回带 <b> 高亮的 HTML 文本。"""
    if not words:
        raise ValueError("no target words")
    prompt = _PROMPT.format(words=", ".join(words))
    log.info("generating reading for %d words", len(words))
    resp = await _get_llm().ainvoke(prompt)
    text = resp.content
    if isinstance(text, list):  # 某些版本返回内容块列表
        text = "".join(part if isinstance(part, str) else part.get("text", "") for part in text)
    return text.strip()
