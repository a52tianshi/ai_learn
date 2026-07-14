"""把数据服务返回的结构格式化成 Telegram HTML 消息。"""
from __future__ import annotations

from html import escape

# 单个卡片最多展示几条释义,避免消息过长
MAX_SENSES_SHOWN = 4


def word_card(word: dict) -> str:
    """完整单词卡片:词 + 音标 + 释义/例句/同义词。"""
    text = escape(word.get("text", ""))
    phonetic = word.get("phonetic") or ""
    head = f"<b>{text}</b>"
    if phonetic:
        head += f"  <code>{escape(phonetic)}</code>"

    lines = [head]
    for sense in (word.get("senses") or [])[:MAX_SENSES_SHOWN]:
        pos = sense.get("pos") or ""
        meaning_en = escape(sense.get("meaning_en", ""))
        meaning_cn = escape(sense.get("meaning_cn", ""))
        prefix = f"<i>{escape(pos)}.</i> " if pos else ""
        if meaning_en and meaning_cn:
            lines.append(f"\n• {prefix}{meaning_en}\n  {meaning_cn}")
        else:
            meaning = meaning_en or meaning_cn
            lines.append(f"\n• {prefix}{meaning}")
        for ex in (sense.get("examples") or [])[:1]:
            lines.append(f"   <i>“{escape(ex)}”</i>")
        syn = sense.get("synonyms") or []
        if syn:
            lines.append(f"   ≈ {escape(', '.join(syn[:5]))}")
    return "\n".join(lines)


def review_prompt(card: dict) -> str:
    """复习提问面(不显示释义)。"""
    text = escape(card.get("text", ""))
    phonetic = card.get("phonetic") or ""
    head = f"🧠 <b>{text}</b>"
    if phonetic:
        head += f"  <code>{escape(phonetic)}</code>"
    return f"{head}\n\n还记得它的意思吗?"


def review_reveal(card: dict, next_interval_days: int) -> str:
    """打分后揭晓释义 + 下次复习时间。"""
    body = word_card(
        {
            "text": card.get("text", ""),
            "phonetic": card.get("phonetic", ""),
            "senses": card.get("senses", []),
        }
    )
    if next_interval_days >= 30000:
        when = "永久记住 (已移出复习库)"
    else:
        when = "明天" if next_interval_days <= 1 else f"{next_interval_days} 天后"
    return f"{body}\n\n📅 下次复习:<b>{when}</b>"


def stats_text(st: dict) -> str:
    return (
        "📊 <b>学习统计</b>\n"
        f"• 单词本总数:{st.get('total_words', 0)}\n"
        f"• 今日待复习:{st.get('due_today', 0)}\n"
        f"• 已掌握:{st.get('mastered', 0)}\n"
        f"• 累计复习次数:{st.get('reviews_total', 0)}"
    )


def word_list(words: list[dict]) -> str:
    if not words:
        return "单词本还是空的。直接发我一个英文单词就能收藏 📖"
    items = "\n".join(f"• {escape(w.get('text', ''))}" for w in words)
    return f"📖 <b>最近的单词</b>\n{items}"
