from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path
from typing import Any


def _repo_root() -> Path:
    return Path(__file__).resolve().parents[1]


def _parse_env_file(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    if not path.exists():
        return values
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        key = key.strip()
        value = value.strip().strip('"').strip("'")
        if key and key not in os.environ:
            values[key] = value
    return values


def _load_env_files() -> None:
    candidates = [
        _repo_root() / ".env",
        _repo_root() / "backend" / ".env",
    ]
    merged: dict[str, str] = {}
    for candidate in candidates:
        merged.update(_parse_env_file(candidate))
    for key, value in merged.items():
        os.environ.setdefault(key, value)


_load_env_files()


def _first_non_empty(*keys: str, default: str = "") -> str:
    for key in keys:
        value = os.environ.get(key, "").strip()
        if value:
            return value
    return default


def _split_csv(raw: str) -> list[str]:
    return [item.strip() for item in raw.split(",") if item.strip()]


@dataclass(frozen=True)
class Settings:
    username: str
    password: str
    session_secret: str
    api_key: str
    db_path: Path
    cors_origins: list[str]
    host: str
    port: int
    tls_cert_file: Path | None
    tls_key_file: Path | None
    title: str
    ntfy_url: str
    messages_per_minute_limit: int

    @classmethod
    def from_env(cls) -> "Settings":
        repo_root = _repo_root()
        data_dir = Path(_first_non_empty("CHATAPI_DATA_DIR", "DATA_DIR", default=str(repo_root / "data")))
        if not data_dir.is_absolute():
            data_dir = (repo_root / data_dir).resolve()
        db_path = Path(_first_non_empty("CHATAPI_DB_PATH", default=str(data_dir / "chatapi.sqlite3")))
        if not db_path.is_absolute():
            db_path = (repo_root / db_path).resolve()
        cors_raw = _first_non_empty(
            "CHATAPI_CORS_ORIGINS",
            "CORS_ORIGINS",
            default="http://localhost:5173,http://127.0.0.1:5173",
        )
        tls_cert_raw = _first_non_empty(
            "CHATAPI_TLS_CERT_FILE",
            "TLS_CERT_FILE",
            default="",
        )
        tls_key_raw = _first_non_empty(
            "CHATAPI_TLS_KEY_FILE",
            "TLS_KEY_FILE",
            default="",
        )

        def _resolve_optional_path(raw: str) -> Path | None:
            if not raw:
                return None
            path = Path(raw)
            if not path.is_absolute():
                path = (repo_root / path).resolve()
            return path

        return cls(
            username=_first_non_empty("CHATAPI_USERNAME", "ADMIN_USERNAME", default="admin"),
            password=_first_non_empty("CHATAPI_PASSWORD", "ADMIN_PASSWORD", default="change-me"),
            session_secret=_first_non_empty(
                "CHATAPI_SESSION_SECRET",
                "ADMIN_SESSION_SECRET",
                default="change-this-session-secret",
            ),
            api_key=_first_non_empty("CHATAPI_API_KEY", default=""),
            db_path=db_path,
            cors_origins=_split_csv(cors_raw),
            host=_first_non_empty("CHATAPI_HOST", "BACKEND_HOST", default="0.0.0.0"),
            port=int(_first_non_empty("CHATAPI_PORT", "BACKEND_PORT", default="5000")),
            tls_cert_file=_resolve_optional_path(tls_cert_raw),
            tls_key_file=_resolve_optional_path(tls_key_raw),
            title=_first_non_empty("CHATAPI_TITLE", default="ChatAPI"),
            ntfy_url=_first_non_empty("CHATAPI_NTFY_URL", "NTFY_URL", default=""),
            messages_per_minute_limit=int(
                _first_non_empty(
                    "CHATAPI_MESSAGES_PER_MINUTE_LIMIT",
                    "MESSAGES_PER_MINUTE_LIMIT",
                    default="0",
                )
            ),
        )


settings = Settings.from_env()
