from __future__ import annotations

from concurrent.futures import ThreadPoolExecutor
from email.header import Header
from logging import Logger
from urllib import error, request

from ..repositories import SystemConfigStore, UserStore
from .url_safety import validate_public_http_url

_executor = ThreadPoolExecutor(max_workers=2, thread_name_prefix="ntfy")


class _NoRedirectHandler(request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):  # type: ignore[override]
        return None


_opener = request.build_opener(_NoRedirectHandler)


def _encode_header(value: str) -> str:
    try:
        value.encode("latin-1")
        return value
    except UnicodeEncodeError:
        return Header(value, "utf-8").encode()


def notify_new_message(
    system_config_store: SystemConfigStore,
    user_store: UserStore,
    owner_id: str,
    *,
    conversation_title: str,
    message_text: str,
    logger: Logger,
) -> None:
    url = user_store.get_effective_ntfy_url(owner_id)
    text = message_text.strip()
    if not url or not text:
        return
    user = user_store.get_user(owner_id)
    allow_private = system_config_store.is_ntfy_private_url_allowed_for_role(user.role if user else "")
    safety = validate_public_http_url(url, allow_private=allow_private)
    if not safety.ok:
        logger.warning("Skipped unsafe ntfy notification URL: %s", safety.reason or "unsafe URL")
        return
    title_fallback = system_config_store.get_effective_title("ChatAPI")

    def send() -> None:
        body = text.encode("utf-8")
        req = request.Request(
            url,
            data=body,
            method="POST",
            headers={
                "Content-Type": "text/plain; charset=utf-8",
                "Title": _encode_header(conversation_title[:80] or title_fallback),
            },
        )
        try:
            with _opener.open(req, timeout=5) as response:
                response.read(1)
        except error.HTTPError as exc:
            if 300 <= exc.code < 400:
                logger.warning("Skipped ntfy notification redirect to %s", exc.headers.get("Location", ""))
                return
            logger.exception("Failed to send ntfy notification")
        except Exception:
            logger.exception("Failed to send ntfy notification")

    _executor.submit(send)
