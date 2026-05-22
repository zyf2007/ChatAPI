from __future__ import annotations

import re
from typing import Literal, TypedDict


class ThinkingPart(TypedDict):
    type: Literal["thinking", "answer"]
    text: str


_THINK_RE = re.compile(r"<think(?:\s[^>]*)?>\s*([\s\S]*?)\s*</think>", re.IGNORECASE)


def split_thinking_parts(text: str) -> list[ThinkingPart]:
    if not text:
        return []

    parts: list[ThinkingPart] = []
    last_index = 0
    for match in _THINK_RE.finditer(text):
        before = text[last_index:match.start()].strip()
        if before:
            parts.append({"type": "answer", "text": before})
        thinking = str(match.group(1) or "").strip()
        if thinking:
            parts.append({"type": "thinking", "text": thinking})
        last_index = match.end()

    after = text[last_index:].strip()
    if after:
        parts.append({"type": "answer", "text": after})

    if not parts and text.strip():
        parts.append({"type": "answer", "text": text.strip()})
    return parts


def has_thinking(text: str) -> bool:
    return any(part["type"] == "thinking" for part in split_thinking_parts(text))


def thinking_text(text: str) -> str:
    return "\n\n".join(
        part["text"] for part in split_thinking_parts(text) if part["type"] == "thinking"
    ).strip()


def answer_text(text: str) -> str:
    return "\n\n".join(
        part["text"] for part in split_thinking_parts(text) if part["type"] == "answer"
    ).strip()


class ThinkingStreamParser:
    def __init__(self) -> None:
        self._buffer = ""
        self._in_thinking = False

    def feed(self, chunk: str) -> list[ThinkingPart]:
        if not chunk:
            return []
        self._buffer += chunk
        return self._drain(allow_incomplete_answer=True)

    def flush(self) -> list[ThinkingPart]:
        return self._drain(allow_incomplete_answer=True, flush=True)

    def _drain(self, *, allow_incomplete_answer: bool, flush: bool = False) -> list[ThinkingPart]:
        emitted: list[ThinkingPart] = []
        while self._buffer:
            lower = self._buffer.lower()
            if self._in_thinking:
                close_index = lower.find("</think>")
                if close_index < 0:
                    if flush:
                        text = self._buffer.strip()
                        if text:
                            emitted.append({"type": "thinking", "text": text})
                        self._buffer = ""
                        self._in_thinking = False
                    break
                thinking = self._buffer[:close_index].strip()
                if thinking:
                    emitted.append({"type": "thinking", "text": thinking})
                self._buffer = self._buffer[close_index + len("</think>"):].lstrip()
                self._in_thinking = False
                continue

            open_index = lower.find("<think>")
            if open_index < 0:
                if allow_incomplete_answer or flush:
                    text = self._buffer
                    if not flush:
                        keep = _partial_think_prefix_len(text)
                        if keep > 0:
                            emit_text = text[:-keep]
                            self._buffer = text[-keep:]
                        else:
                            emit_text = text
                            self._buffer = ""
                    else:
                        emit_text = text
                        self._buffer = ""
                    if emit_text.strip():
                        emitted.append({"type": "answer", "text": emit_text})
                break

            before = self._buffer[:open_index]
            if before.strip():
                emitted.append({"type": "answer", "text": before})
            self._buffer = self._buffer[open_index + len("<think>"):]
            self._in_thinking = True
        return emitted


def _partial_think_prefix_len(text: str) -> int:
    marker = "<think>"
    lower = text.lower()
    max_len = min(len(marker) - 1, len(lower))
    for size in range(max_len, 0, -1):
        if marker.startswith(lower[-size:]):
            return size
    return 0
