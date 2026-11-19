"""text.* task types."""

import string
from collections import Counter
from typing import Any

from pydantic import Field

from taskpilot_worker.handlers.base import registry
from taskpilot_worker.registry import HandlerContext, Payload


class WordCountV1(Payload):
    """text.word_count v1 payload."""

    text: str = Field(
        min_length=1, max_length=60000, description="The text to analyse, up to 60,000 characters."
    )
    top_n: int = Field(
        default=10, ge=0, le=50, description="How many of the most frequent words to return (0-50)."
    )


@registry.task(
    "text.word_count",
    title="Word count",
    timeout=30,
    max_timeout=120,
    example={"text": "The quick brown fox jumps over the lazy dog. The dog sleeps.", "top_n": 3},
)
def word_count(p: WordCountV1, _ctx: HandlerContext) -> dict[str, Any]:
    """Counts the words, lines, and characters in a text and lists the most frequent words. Words
    are
    compared case-insensitively, ignoring surrounding punctuation.
    """
    words = [w.strip(string.punctuation).lower() for w in p.text.split()]
    words = [w for w in words if w]
    return {
        "words": len(words),
        "lines": len(p.text.splitlines()),
        "characters": len(p.text),
        "top_words": [{"word": w, "count": c} for w, c in Counter(words).most_common(p.top_n)],
    }
