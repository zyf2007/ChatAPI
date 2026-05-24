from __future__ import annotations

import ipaddress
import socket
from dataclasses import dataclass
from urllib.parse import urlsplit

MAX_NTFY_URL_LENGTH = 2048

_RESTRICTED_ADDRESS_NETWORKS = (
    ipaddress.ip_network("0.0.0.0/8"),
    ipaddress.ip_network("10.0.0.0/8"),
    ipaddress.ip_network("100.64.0.0/10"),
    ipaddress.ip_network("127.0.0.0/8"),
    ipaddress.ip_network("169.254.0.0/16"),
    ipaddress.ip_network("172.16.0.0/12"),
    ipaddress.ip_network("192.168.0.0/16"),
    ipaddress.ip_network("224.0.0.0/4"),
    ipaddress.ip_network("::/128"),
    ipaddress.ip_network("::1/128"),
    ipaddress.ip_network("fc00::/7"),
    ipaddress.ip_network("fe80::/10"),
    ipaddress.ip_network("ff00::/8"),
)


@dataclass(frozen=True)
class UrlSafetyResult:
    ok: bool
    is_private: bool = False
    reason: str = ""


def _is_restricted_ip(address: ipaddress.IPv4Address | ipaddress.IPv6Address) -> bool:
    if isinstance(address, ipaddress.IPv6Address) and address.ipv4_mapped is not None:
        address = address.ipv4_mapped
    return any(address in network for network in _RESTRICTED_ADDRESS_NETWORKS)


def _resolve_host_addresses(hostname: str, port: int) -> tuple[list[ipaddress.IPv4Address | ipaddress.IPv6Address], str]:
    try:
        literal = ipaddress.ip_address(hostname)
    except ValueError:
        literal = None
    if literal is not None:
        return [literal], ""

    try:
        infos = socket.getaddrinfo(hostname, port, type=socket.SOCK_STREAM)
    except socket.gaierror as exc:
        return [], f"ntfy 地址域名解析失败：{exc}"

    addresses: list[ipaddress.IPv4Address | ipaddress.IPv6Address] = []
    for info in infos:
        raw_address = info[4][0]
        try:
            address = ipaddress.ip_address(raw_address)
        except ValueError:
            continue
        if address not in addresses:
            addresses.append(address)
    if not addresses:
        return [], "ntfy 地址域名没有可用的 IP 解析结果"
    return addresses, ""


def validate_public_http_url(url: str, *, allow_private: bool = False) -> UrlSafetyResult:
    value = str(url or "").strip()
    if not value:
        return UrlSafetyResult(ok=True)
    if len(value) > MAX_NTFY_URL_LENGTH:
        return UrlSafetyResult(ok=False, reason=f"ntfy 地址过长，最多 {MAX_NTFY_URL_LENGTH} 个字符")
    if any(ord(ch) < 32 or ord(ch) == 127 for ch in value):
        return UrlSafetyResult(ok=False, reason="ntfy 地址不能包含控制字符")

    parsed = urlsplit(value)
    scheme = parsed.scheme.lower()
    if scheme not in {"http", "https"}:
        return UrlSafetyResult(ok=False, reason="ntfy 地址只支持 http 或 https")
    hostname = (parsed.hostname or "").strip()
    if not hostname:
        return UrlSafetyResult(ok=False, reason="ntfy 地址必须包含主机名")

    normalized_hostname = hostname.rstrip(".").lower()
    if normalized_hostname == "localhost" or normalized_hostname.endswith(".localhost"):
        if allow_private:
            return UrlSafetyResult(ok=True, is_private=True)
        return UrlSafetyResult(ok=False, is_private=True, reason="ntfy 地址不能指向本机或内网地址")

    try:
        port = parsed.port or (443 if scheme == "https" else 80)
    except ValueError:
        return UrlSafetyResult(ok=False, reason="ntfy 地址端口无效")

    addresses, error = _resolve_host_addresses(normalized_hostname, port)
    if error:
        return UrlSafetyResult(ok=False, reason=error)

    restricted = any(_is_restricted_ip(address) for address in addresses)
    if restricted and not allow_private:
        return UrlSafetyResult(ok=False, is_private=True, reason="ntfy 地址不能指向本机或内网地址")

    return UrlSafetyResult(ok=True, is_private=restricted)
