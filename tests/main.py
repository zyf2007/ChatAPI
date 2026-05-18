from __future__ import annotations

import argparse
import json
import os
import random
import string
import threading
import time
import uuid
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass, field
from queue import Queue
from typing import Any, Literal

import httpx
from anthropic import Anthropic
from openai import OpenAI


ToolProvider = Literal["openai", "anthropic"]
ScenarioMode = Literal["assistant_text", "tool_call"]

DEFAULT_BASE_URL = "http://127.0.0.1:5000"
DEFAULT_USERNAME = "admin"
DEFAULT_PASSWORD = "change-me"
DEFAULT_MODEL = "mock-gpt-4.1-mini"

WORDS = [
    "alpha",
    "bravo",
    "charlie",
    "delta",
    "echo",
    "foxtrot",
    "golf",
    "hotel",
    "india",
    "juliet",
    "kilo",
    "lima",
    "mike",
    "november",
    "oscar",
    "papa",
    "quebec",
    "romeo",
    "sierra",
    "tango",
    "uniform",
    "victor",
    "whiskey",
    "xray",
    "yankee",
    "zulu",
    "stream",
    "chunk",
    "buffer",
    "final",
    "draft",
    "socket",
    "payload",
    "latency",
    "repeat",
    "suffix",
    "merge",
    "flush",
]

OUTPUT_SEPARATORS = ["-", "_", "/", ".", ",", ":"]

TOOL_NAMES = [
    "search_docs",
    "lookup_weather",
    "query_orders",
    "run_report",
    "fetch_calendar",
]


def load_local_backend_env() -> dict[str, str]:
    values: dict[str, str] = {}
    for candidate in ("backend/.env", ".env"):
        if not os.path.exists(candidate):
            continue
        with open(candidate, encoding="utf-8") as handle:
            for raw_line in handle:
                line = raw_line.strip()
                if not line or line.startswith("#") or "=" not in line:
                    continue
                key, value = line.split("=", 1)
                values.setdefault(key.strip(), value.strip().strip('"').strip("'"))
    return values


@dataclass
class Scenario:
    index: int
    provider: ToolProvider
    mode: ScenarioMode
    prompt: str
    draft_chunks: list[str]
    final_text: str
    tool_name: str
    tool_arguments_text: str
    tool_arguments: dict[str, Any]
    conversation_title: str
    send_completion_delay: float
    chunk_delay_range: tuple[float, float]


@dataclass
class ScenarioResult:
    index: int
    provider: ToolProvider
    mode: ScenarioMode
    conversation_id: str
    ok: bool
    duration_seconds: float
    observed_text: str
    expected_text: str
    observed_tool_name: str = ""
    expected_tool_name: str = ""
    observed_tool_arguments_text: str = ""
    expected_tool_arguments_text: str = ""
    finish_reasons: list[str] = field(default_factory=list)
    notes: list[str] = field(default_factory=list)
    error: str = ""


class ApiSession:
    def __init__(
        self,
        *,
        base_url: str,
        api_key: str,
        username: str,
        password: str,
        timeout_seconds: float,
        verify: bool,
        trust_env: bool,
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self._client = httpx.Client(
            base_url=self.base_url,
            timeout=timeout_seconds,
            verify=verify,
            trust_env=trust_env,
            follow_redirects=True,
        )
        self._auth_headers: dict[str, str] = {}
        if api_key:
            self._auth_headers = {"Authorization": f"Bearer {api_key}"}
        else:
            response = self._client.post(
                "/api/auth/login",
                json={"username": username, "password": password},
            )
            response.raise_for_status()

    @property
    def client(self) -> httpx.Client:
        return self._client

    @property
    def auth_headers(self) -> dict[str, str]:
        return dict(self._auth_headers)

    def post_json(self, path: str, payload: dict[str, Any]) -> dict[str, Any]:
        response = self._client.post(path, json=payload, headers=self._auth_headers)
        response.raise_for_status()
        return response.json()

    def create_conversation(self, title: str) -> str:
        payload = self.post_json("/api/conversations", {"title": title})
        return str(payload["conversation"]["id"])

    def get_conversation(self, conversation_id: str) -> dict[str, Any]:
        response = self._client.get(
            f"/api/conversations/{conversation_id}",
            headers=self._auth_headers,
        )
        response.raise_for_status()
        return response.json()

    def get_automation_rules(self) -> list[dict[str, Any]]:
        response = self._client.get("/api/config/automation-rules", headers=self._auth_headers)
        response.raise_for_status()
        return list(response.json().get("rules", []))

    def set_automation_rules(self, rules: list[dict[str, Any]]) -> list[dict[str, Any]]:
        payload = self.post_json("/api/config/automation-rules", {"rules": rules})
        return list(payload.get("rules", []))

    def add_output_delta(self, conversation_id: str, text: str) -> None:
        self.post_json(
            "/api/chat/output/delta",
            {
                "conversation_id": conversation_id,
                "text": text,
            },
        )

    def complete_output(
        self,
        *,
        conversation_id: str,
        mode: ScenarioMode,
        final_text: str,
        model: str,
        tool_name: str,
        tool_arguments_text: str,
    ) -> None:
        payload: dict[str, Any] = {
            "conversation_id": conversation_id,
            "model": model,
            "mode": "assistant_message" if mode == "assistant_text" else "tool_call",
        }
        if mode == "assistant_text":
            payload["text"] = final_text
        else:
            payload["tool_name"] = tool_name
            payload["text"] = tool_arguments_text
        self.post_json("/api/chat/output/complete", payload)

    def close(self) -> None:
        self._client.close()


def build_parser() -> argparse.ArgumentParser:
    backend_env = load_local_backend_env()
    parser = argparse.ArgumentParser(
        description="并发模拟 OpenAI / Anthropic SDK 流式请求，检测尾部重复与 tool call 组合问题。",
    )
    parser.add_argument("--base-url", default=DEFAULT_BASE_URL, help="后端地址，默认 http://127.0.0.1:5000")
    parser.add_argument(
        "--api-key",
        default=os.environ.get("CHATAPI_API_KEY", backend_env.get("CHATAPI_API_KEY", "")),
        help="如果后端启用了 CHATAPI_API_KEY，优先使用它",
    )
    parser.add_argument(
        "--username",
        default=os.environ.get("CHATAPI_USERNAME", backend_env.get("CHATAPI_USERNAME", DEFAULT_USERNAME)),
        help="未提供 API key 时使用的用户名",
    )
    parser.add_argument(
        "--password",
        default=os.environ.get("CHATAPI_PASSWORD", backend_env.get("CHATAPI_PASSWORD", DEFAULT_PASSWORD)),
        help="未提供 API key 时使用的密码",
    )
    parser.add_argument("--model", default=DEFAULT_MODEL, help="请求里传入的模型名")
    parser.add_argument("--requests", type=int, default=24, help="总请求数")
    parser.add_argument("--concurrency", type=int, default=8, help="并发连接数")
    parser.add_argument(
        "--providers",
        nargs="+",
        choices=["openai", "anthropic"],
        default=["openai", "anthropic"],
        help="要覆盖的 SDK 提供方",
    )
    parser.add_argument("--tool-call-rate", type=float, default=0.35, help="tool call 场景比例")
    parser.add_argument("--max-draft-chunks", type=int, default=7, help="每个请求最多推送多少个流式文本块")
    parser.add_argument("--max-chunk-words", type=int, default=7, help="单个文本块最多多少个单词")
    parser.add_argument("--seed", type=int, default=20260518, help="随机种子，便于复现")
    parser.add_argument("--timeout", type=float, default=45.0, help="单请求超时时间，秒")
    parser.add_argument("--verify", action="store_true", help="启用 TLS 证书校验")
    parser.add_argument("--trust-env", action="store_true", help="允许 httpx / SDK 读取代理等环境变量")
    parser.add_argument(
        "--keep-automation-rules",
        action="store_true",
        help="默认会临时禁用 automation rules；传这个参数则保持原样",
    )
    parser.add_argument("--verbose", action="store_true", help="输出每个请求的详细结果")
    return parser


def make_rng(seed: int, index: int) -> random.Random:
    return random.Random(seed + index * 9973)


def random_words(rng: random.Random, count: int) -> str:
    return " ".join(rng.choice(WORDS) for _ in range(count))


def random_sentence(rng: random.Random, *, min_words: int, max_words: int) -> str:
    count = rng.randint(min_words, max_words)
    text = random_words(rng, count)
    return f"{text}."


def random_output_text(rng: random.Random, *, min_units: int, max_units: int) -> str:
    unit_count = rng.randint(min_units, max_units)
    parts: list[str] = []
    for index in range(unit_count):
        parts.append(rng.choice(WORDS))
        if index < unit_count - 1:
            parts.append(rng.choice(OUTPUT_SEPARATORS))
    if rng.random() < 0.7:
        parts.append(rng.choice([".", "!", "~"]))
    return "".join(parts)


def build_tool_arguments(rng: random.Random) -> dict[str, Any]:
    return {
        "request_id": f"req_{uuid.uuid4().hex[:12]}",
        "query": random_sentence(rng, min_words=3, max_words=8),
        "limit": rng.randint(1, 20),
        "include_archived": rng.choice([True, False]),
        "tags": rng.sample(WORDS, k=rng.randint(1, 4)),
    }


def split_text_into_chunks(rng: random.Random, text: str, max_chunks: int) -> list[str]:
    if not text:
        return []
    if len(text) == 1:
        return [text]
    chunk_count = min(max_chunks, len(text), max(1, rng.randint(1, max_chunks)))
    if chunk_count == 1:
        return [text]

    cut_points = sorted(rng.sample(range(1, len(text)), k=chunk_count - 1))
    parts: list[str] = []
    start = 0
    for cut in cut_points:
        parts.append(text[start:cut])
        start = cut
    parts.append(text[start:])
    return parts


def build_scenario(index: int, providers: list[ToolProvider], args: argparse.Namespace) -> Scenario:
    rng = make_rng(args.seed, index)
    provider = providers[index % len(providers)]
    mode: ScenarioMode = "tool_call" if rng.random() < args.tool_call_rate else "assistant_text"
    prompt = (
        f"scenario {index}: please stream output in chunks, sometimes end with a tool call, "
        f"and never repeat the final suffix. payload={random_sentence(rng, min_words=5, max_words=10)}"
    )

    full_draft_seed = random_output_text(
        rng,
        min_units=max(4, args.max_chunk_words),
        max_units=max(10, args.max_chunk_words * max(2, args.max_draft_chunks // 2)),
    )
    draft_chunks = split_text_into_chunks(rng, full_draft_seed, args.max_draft_chunks)

    extra_suffix = ""
    if mode == "assistant_text" and rng.random() < 0.55:
        extra_suffix = random_output_text(rng, min_units=3, max_units=args.max_chunk_words)
    final_text = "".join(draft_chunks) + extra_suffix

    if mode == "tool_call" and rng.random() < 0.25:
        draft_chunks = []

    tool_name = rng.choice(TOOL_NAMES)
    tool_arguments = build_tool_arguments(rng)
    tool_arguments_text = json.dumps(tool_arguments, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    title_suffix = "".join(rng.choice(string.ascii_lowercase) for _ in range(6))

    return Scenario(
        index=index,
        provider=provider,
        mode=mode,
        prompt=prompt,
        draft_chunks=draft_chunks,
        final_text=final_text,
        tool_name=tool_name,
        tool_arguments_text=tool_arguments_text,
        tool_arguments=tool_arguments,
        conversation_title=f"stream-fuzz-{provider}-{index}-{title_suffix}",
        send_completion_delay=rng.uniform(0.01, 0.12),
        chunk_delay_range=(0.01, 0.08),
    )


def build_openai_tools(scenario: Scenario) -> list[dict[str, Any]]:
    return [
        {
            "type": "function",
            "function": {
                "name": scenario.tool_name,
                "description": "Synthetic tool schema for stream fuzzing",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "request_id": {"type": "string"},
                        "query": {"type": "string"},
                        "limit": {"type": "integer"},
                        "include_archived": {"type": "boolean"},
                        "tags": {"type": "array", "items": {"type": "string"}},
                    },
                    "required": ["request_id", "query"],
                    "additionalProperties": True,
                },
            },
        }
    ]


def build_anthropic_tools(scenario: Scenario) -> list[dict[str, Any]]:
    return [
        {
            "name": scenario.tool_name,
            "description": "Synthetic tool schema for stream fuzzing",
            "input_schema": {
                "type": "object",
                "properties": {
                    "request_id": {"type": "string"},
                    "query": {"type": "string"},
                    "limit": {"type": "integer"},
                    "include_archived": {"type": "boolean"},
                    "tags": {"type": "array", "items": {"type": "string"}},
                },
                "required": ["request_id", "query"],
                "additionalProperties": True,
            },
        }
    ]


def tail_repeat_note(expected: str, observed: str) -> str:
    if not expected or observed == expected:
        return ""
    if observed.startswith(expected):
        extra = observed[len(expected):]
        return f"observed text has extra suffix after expected final text: {extra!r}"
    for tail_len in range(min(len(expected) // 2, 80), 5, -1):
        tail = expected[-tail_len:]
        if observed == expected + tail:
            return f"observed text repeats expected tail once more: {tail!r}"
    return ""


def run_output_driver(
    api_session: ApiSession,
    scenario: Scenario,
    conversation_id: str,
    model: str,
    timeout_seconds: float,
    error_queue: Queue[str],
) -> None:
    rng = make_rng(10_000_000 + scenario.index, scenario.index)
    started_at = time.perf_counter()
    try:
        while True:
            conversation_payload = api_session.get_conversation(conversation_id)
            metadata = conversation_payload.get("conversation", {}).get("metadata", {})
            if metadata.get("realtime_status") == "waiting":
                break
            if time.perf_counter() - started_at > timeout_seconds:
                raise TimeoutError("conversation never entered waiting state")
            time.sleep(0.02)

        for chunk in scenario.draft_chunks:
            api_session.add_output_delta(conversation_id, chunk)
            time.sleep(rng.uniform(*scenario.chunk_delay_range))
        time.sleep(scenario.send_completion_delay)
        api_session.complete_output(
            conversation_id=conversation_id,
            mode=scenario.mode,
            final_text=scenario.final_text,
            model=model,
            tool_name=scenario.tool_name,
            tool_arguments_text=scenario.tool_arguments_text,
        )
    except Exception as exc:
        error_queue.put(f"{type(exc).__name__}: {exc}")


def consume_openai_stream(
    client: OpenAI,
    scenario: Scenario,
    conversation_id: str,
    model: str,
) -> tuple[str, str, str, list[str]]:
    observed_text_parts: list[str] = []
    finish_reasons: list[str] = []
    tool_name = ""
    tool_arguments_parts: list[str] = []
    stream = client.chat.completions.create(
        model=model,
        messages=[{"role": "user", "content": scenario.prompt}],
        tools=build_openai_tools(scenario),
        stream=True,
        extra_body={"conversation_id": conversation_id},
    )
    for chunk in stream:
        for choice in chunk.choices:
            delta = choice.delta
            if getattr(delta, "content", None):
                observed_text_parts.append(str(delta.content))
            for tool_call in getattr(delta, "tool_calls", []) or []:
                function = getattr(tool_call, "function", None)
                if function is not None and getattr(function, "name", None):
                    tool_name = str(function.name)
                if function is not None and getattr(function, "arguments", None):
                    tool_arguments_parts.append(str(function.arguments))
            if choice.finish_reason:
                finish_reasons.append(str(choice.finish_reason))
    return "".join(observed_text_parts), tool_name, "".join(tool_arguments_parts), finish_reasons


def consume_anthropic_stream(
    client: Anthropic,
    scenario: Scenario,
    conversation_id: str,
    model: str,
) -> tuple[str, str, str, list[str]]:
    observed_text_parts: list[str] = []
    finish_reasons: list[str] = []
    observed_tool_name = ""
    observed_tool_arguments_parts: list[str] = []
    stream = client.messages.create(
        model=model,
        max_tokens=512,
        messages=[{"role": "user", "content": scenario.prompt}],
        tools=build_anthropic_tools(scenario),
        stream=True,
        extra_body={"conversation_id": conversation_id},
    )
    for event in stream:
        if event.type == "content_block_delta" and getattr(event.delta, "type", "") == "text_delta":
            observed_text_parts.append(str(event.delta.text))
        elif event.type == "content_block_delta" and getattr(event.delta, "type", "") == "input_json_delta":
            observed_tool_arguments_parts.append(str(event.delta.partial_json))
        elif event.type == "content_block_start" and getattr(event.content_block, "type", "") == "tool_use":
            observed_tool_name = str(event.content_block.name)
        elif event.type == "message_delta" and getattr(event.delta, "stop_reason", None):
            finish_reasons.append(str(event.delta.stop_reason))
    return (
        "".join(observed_text_parts),
        observed_tool_name,
        "".join(observed_tool_arguments_parts),
        finish_reasons,
    )


def run_one_scenario(args: argparse.Namespace, scenario: Scenario, print_lock: threading.Lock) -> ScenarioResult:
    start = time.perf_counter()
    api_session: ApiSession | None = None
    shared_http_client: httpx.Client | None = None
    try:
        api_session = ApiSession(
            base_url=args.base_url,
            api_key=args.api_key,
            username=args.username,
            password=args.password,
            timeout_seconds=args.timeout,
            verify=args.verify,
            trust_env=args.trust_env,
        )
        shared_http_client = httpx.Client(
            timeout=args.timeout,
            verify=args.verify,
            trust_env=args.trust_env,
            follow_redirects=True,
        )
        conversation_id = api_session.create_conversation(scenario.conversation_title)
        if scenario.provider == "openai":
            sdk_client = OpenAI(
                api_key=args.api_key or "local-test-key",
                base_url=f"{args.base_url.rstrip('/')}/v1",
                http_client=shared_http_client,
                default_headers=api_session.auth_headers,
            )
        else:
            sdk_client = Anthropic(
                api_key=args.api_key or "local-test-key",
                base_url=f"{args.base_url.rstrip('/')}/apps/anthropic",
                http_client=shared_http_client,
                default_headers=api_session.auth_headers,
            )

        driver_error_queue: Queue[str] = Queue()
        driver_thread = threading.Thread(
            target=run_output_driver,
            kwargs={
                "api_session": api_session,
                "scenario": scenario,
                "conversation_id": conversation_id,
                "model": args.model,
                "timeout_seconds": args.timeout,
                "error_queue": driver_error_queue,
            },
            daemon=True,
        )
        driver_thread.start()

        if scenario.provider == "openai":
            observed_text, observed_tool_name, observed_tool_arguments, finish_reasons = consume_openai_stream(
                sdk_client,
                scenario,
                conversation_id,
                args.model,
            )
        else:
            observed_text, observed_tool_name, observed_tool_arguments, finish_reasons = consume_anthropic_stream(
                sdk_client,
                scenario,
                conversation_id,
                args.model,
            )

        driver_thread.join(timeout=args.timeout)
        if driver_thread.is_alive():
            raise TimeoutError("output driver did not finish in time")
        if not driver_error_queue.empty():
            raise RuntimeError(driver_error_queue.get())

        notes: list[str] = []
        ok = True
        expected_text = scenario.final_text if scenario.mode == "assistant_text" else "".join(scenario.draft_chunks)
        if observed_text != expected_text:
            ok = False
            notes.append(
                f"text mismatch: expected {expected_text!r}, observed {observed_text!r}"
            )
            repeat_note = tail_repeat_note(expected_text, observed_text)
            if repeat_note:
                notes.append(repeat_note)

        if scenario.mode == "tool_call":
            if observed_tool_name != scenario.tool_name:
                ok = False
                notes.append(
                    f"tool name mismatch: expected {scenario.tool_name!r}, observed {observed_tool_name!r}"
                )
            if observed_tool_arguments != scenario.tool_arguments_text:
                ok = False
                notes.append(
                    "tool arguments mismatch: "
                    f"expected {scenario.tool_arguments_text!r}, observed {observed_tool_arguments!r}"
                )
        else:
            if observed_tool_name or observed_tool_arguments:
                ok = False
                notes.append("assistant_text scenario unexpectedly emitted tool call data")

        return ScenarioResult(
            index=scenario.index,
            provider=scenario.provider,
            mode=scenario.mode,
            conversation_id=conversation_id,
            ok=ok,
            duration_seconds=time.perf_counter() - start,
            observed_text=observed_text,
            expected_text=expected_text,
            observed_tool_name=observed_tool_name,
            expected_tool_name=scenario.tool_name if scenario.mode == "tool_call" else "",
            observed_tool_arguments_text=observed_tool_arguments,
            expected_tool_arguments_text=scenario.tool_arguments_text if scenario.mode == "tool_call" else "",
            finish_reasons=finish_reasons,
            notes=notes,
        )
    except Exception as exc:
        return ScenarioResult(
            index=scenario.index,
            provider=scenario.provider,
            mode=scenario.mode,
            conversation_id="",
            ok=False,
            duration_seconds=time.perf_counter() - start,
            observed_text="",
            expected_text=scenario.final_text if scenario.mode == "assistant_text" else "".join(scenario.draft_chunks),
            error=f"{type(exc).__name__}: {exc}",
            notes=["exception raised while running scenario"],
        )
    finally:
        if shared_http_client is not None:
            shared_http_client.close()
        if api_session is not None:
            api_session.close()
        if args.verbose:
            with print_lock:
                print(f"[debug] finished scenario {scenario.index} provider={scenario.provider} mode={scenario.mode}")


def print_result(result: ScenarioResult) -> None:
    status = "PASS" if result.ok else "FAIL"
    print(
        f"[{status}] #{result.index:03d} provider={result.provider:<9} mode={result.mode:<14} "
        f"duration={result.duration_seconds:>5.2f}s conversation_id={result.conversation_id or '-'}"
    )
    if result.error:
        print(f"  error: {result.error}")
    for note in result.notes:
        print(f"  note: {note}")
    if result.finish_reasons:
        print(f"  finish_reasons: {result.finish_reasons}")


def print_summary(results: list[ScenarioResult], started_at: float) -> int:
    total = len(results)
    failures = [item for item in results if not item.ok]
    duration = time.perf_counter() - started_at
    provider_counts: dict[str, int] = {}
    provider_failures: dict[str, int] = {}
    mode_counts: dict[str, int] = {}
    for item in results:
        provider_counts[item.provider] = provider_counts.get(item.provider, 0) + 1
        mode_counts[item.mode] = mode_counts.get(item.mode, 0) + 1
        if not item.ok:
            provider_failures[item.provider] = provider_failures.get(item.provider, 0) + 1

    print()
    print("Summary")
    print(f"  total={total} passed={total - len(failures)} failed={len(failures)} duration={duration:.2f}s")
    print(f"  provider_counts={provider_counts}")
    print(f"  mode_counts={mode_counts}")
    if provider_failures:
        print(f"  provider_failures={provider_failures}")

    if failures:
        print()
        print("Failures")
        for item in failures[:20]:
            print(
                f"  #{item.index:03d} provider={item.provider} mode={item.mode} "
                f"error={item.error or '; '.join(item.notes)}"
            )
        if len(failures) > 20:
            print(f"  ... and {len(failures) - 20} more")
        return 1
    return 0


def main() -> int:
    args = build_parser().parse_args()
    started_at = time.perf_counter()
    providers = [provider for provider in args.providers]
    scenarios = [build_scenario(index, providers, args) for index in range(args.requests)]
    print_lock = threading.Lock()
    setup_session = ApiSession(
        base_url=args.base_url,
        api_key=args.api_key,
        username=args.username,
        password=args.password,
        timeout_seconds=args.timeout,
        verify=args.verify,
        trust_env=args.trust_env,
    )
    original_rules: list[dict[str, Any]] | None = None

    print(
        f"Running {len(scenarios)} scenarios against {args.base_url} "
        f"with concurrency={args.concurrency}, providers={providers}, seed={args.seed}"
    )

    try:
        if not args.keep_automation_rules:
            original_rules = setup_session.get_automation_rules()
            if original_rules:
                setup_session.set_automation_rules([])
                print(f"Suspended {len(original_rules)} automation rule(s) during fuzz test")

        results: list[ScenarioResult] = []
        with ThreadPoolExecutor(max_workers=args.concurrency) as executor:
            future_map = {
                executor.submit(run_one_scenario, args, scenario, print_lock): scenario
                for scenario in scenarios
            }
            for future in as_completed(future_map):
                result = future.result()
                results.append(result)
                print_result(result)

        results.sort(key=lambda item: item.index)
        return print_summary(results, started_at)
    finally:
        if original_rules is not None:
            setup_session.set_automation_rules(original_rules)
            print(f"Restored {len(original_rules)} automation rule(s)")
        setup_session.close()


if __name__ == "__main__":
    raise SystemExit(main())
