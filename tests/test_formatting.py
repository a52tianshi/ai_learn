"""formatting:HTML 转义、重复释义去重(回归护栏)、字段缺失容错。"""
from __future__ import annotations

from wordbot import formatting


def test_word_card_escapes_all_external_text():
    card = formatting.word_card(
        {
            "text": "a<b>&c",
            "phonetic": "/<x>/",
            "senses": [
                {
                    "pos": "n<",
                    "meaning_en": "less than < & greater >",
                    "meaning_cn": "小于 <号> & 符号",
                    "examples": ["1 < 2 & 3 > 2"],
                    "synonyms": ["<tag>", "a&b"],
                }
            ],
        }
    )
    # 原始危险字符只能以实体形式出现
    assert "a&lt;b&gt;&amp;c" in card
    assert "less than &lt; &amp; greater &gt;" in card
    assert "小于 &lt;号&gt; &amp; 符号" in card
    assert "1 &lt; 2 &amp; 3 &gt; 2" in card
    assert "&lt;tag&gt;, a&amp;b" in card
    assert "<x>" not in card and "<tag>" not in card and "<号>" not in card


def test_duplicate_senses_same_pos_and_meaning_are_deduped():
    """2026-10-26 回归:同一词性下相同释义重复出现,只能展示一次。"""
    dup = {"pos": "noun", "meaning_en": "excessive pride", "meaning_cn": "虚荣心"}
    card = formatting.word_card(
        {"text": "vanity", "senses": [dup, dict(dup), dict(dup), {**dup, "pos": "NOUN "}]}
    )
    assert card.count("excessive pride") == 1
    assert card.count("虚荣心") == 1


def test_duplicates_do_not_eat_display_slots():
    dup = {"pos": "noun", "meaning_en": "same", "meaning_cn": "相同"}
    distinct = [
        {"pos": "noun", "meaning_en": f"sense {i}", "meaning_cn": f"义{i}"} for i in range(3)
    ]
    card = formatting.word_card({"text": "w", "senses": [dup, dup, dup, *distinct]})
    # 1 条去重后的 + 3 条不同释义 = 4 条,正好达到上限
    assert card.count("\n• ") == formatting.MAX_SENSES_SHOWN
    assert "sense 2" in card


def test_distinct_meanings_same_pos_are_kept():
    card = formatting.word_card(
        {
            "text": "vanity",
            "senses": [
                {"pos": "noun", "meaning_en": "excessive pride", "meaning_cn": "虚荣心"},
                {"pos": "noun", "meaning_en": "a dressing table", "meaning_cn": "梳妆台"},
            ],
        }
    )
    assert "虚荣心" in card and "梳妆台" in card


def test_missing_english_or_chinese_meaning_does_not_crash():
    card = formatting.word_card(
        {
            "text": "x",
            "senses": [
                {"pos": "verb", "meaning_en": "only english"},
                {"pos": "noun", "meaning_cn": "只有中文"},
                {"meaning_en": None, "meaning_cn": None},  # 全空:跳过
                {"pos": None, "meaning_en": "", "meaning_cn": "无词性"},
            ],
        }
    )
    assert "only english" in card
    assert "只有中文" in card
    assert "无词性" in card
    assert "None" not in card


def test_word_card_with_no_senses_or_fields():
    assert formatting.word_card({}) == "<b></b>"
    assert "<b>hi</b>" in formatting.word_card({"text": "hi", "senses": None})


def test_review_reveal_and_word_list_escape():
    reveal = formatting.review_reveal(
        {"text": "<w>", "senses": [{"meaning_en": "a & b"}]}, 36500
    )
    assert "&lt;w&gt;" in reveal and "a &amp; b" in reveal
    assert "永久记住" in reveal
    assert "&lt;evil&gt;" in formatting.word_list([{"text": "<evil>"}])
