"""Website fetch for the ``fetch_website`` node (SPEC.md §9.7).

Fetches one page, the lead's website URL, and reduces it to readable text.
Every request (the initial URL and each redirect hop) passes the same guard:
http(s) on ports 80/443 only, and the host must resolve exclusively to public
addresses. Errors never escape :meth:`Fetcher.fetch`; they become a
:class:`FetchStatus`.

Not yet implemented (M4): robots.txt handling.
"""

from __future__ import annotations

import asyncio
import ipaddress
import logging
import re
import socket
import time
from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from enum import StrEnum
from html.parser import HTMLParser
from urllib.parse import urljoin, urlsplit

import httpx

log = logging.getLogger(__name__)

USER_AGENT = "LeadIntelBot/0.1 (+https://github.com/zori7/lead-intelligence-platform)"
MAX_BYTES = 1 << 20
MAX_REDIRECTS = 3
MIN_TEXT_CHARS = 200
HTML_TYPES = ("text/html", "application/xhtml+xml")


class FetchStatus(StrEnum):
    FETCHED = "fetched"
    NOT_PROVIDED = "not_provided"
    DISABLED = "disabled"
    BLOCKED = "blocked"
    FAILED = "failed"


@dataclass(frozen=True)
class WebsiteContent:
    status: FetchStatus
    final_url: str | None = None
    title: str = ""
    description: str = ""
    text: str = ""

    @property
    def fetched(self) -> bool:
        return self.status is FetchStatus.FETCHED


Resolver = Callable[[str], Awaitable[list[str]]]
"""Resolves a host name to its IP addresses (injectable for tests)."""


async def system_resolver(host: str) -> list[str]:
    infos = await asyncio.get_running_loop().getaddrinfo(
        host, None, type=socket.SOCK_STREAM, proto=socket.IPPROTO_TCP
    )
    return sorted({str(info[4][0]) for info in infos})


def is_public_address(addr: str) -> bool:
    """True only for globally routable unicast addresses.

    Rejects loopback, private, link-local (incl. cloud metadata 169.254.169.254),
    shared, reserved, unspecified and multicast ranges, for IPv4 and IPv6,
    including IPv4-mapped IPv6 addresses.
    """
    try:
        ip = ipaddress.ip_address(addr.split("%", 1)[0])
    except ValueError:
        return False
    if isinstance(ip, ipaddress.IPv6Address) and ip.ipv4_mapped is not None:
        ip = ip.ipv4_mapped
    return ip.is_global and not ip.is_multicast


class BlockedError(Exception):
    """The URL is not allowed to be fetched."""


class FetchError(Exception):
    """The page could not be fetched or contains no usable text."""


class Fetcher:
    def __init__(
        self,
        client: httpx.AsyncClient,
        *,
        timeout: float,
        max_chars: int,
        resolver: Resolver = system_resolver,
    ) -> None:
        self._client = client
        self._timeout = timeout
        self._max_chars = max_chars
        self._resolve = resolver

    async def fetch(self, url: str) -> WebsiteContent:
        start = time.monotonic()
        try:
            async with asyncio.timeout(self._timeout):
                result = await self._fetch(url)
        except BlockedError as exc:
            result, reason = WebsiteContent(FetchStatus.BLOCKED), str(exc)
        except (FetchError, httpx.HTTPError, TimeoutError, UnicodeError) as exc:
            result, reason = (
                WebsiteContent(FetchStatus.FAILED),
                type(exc).__name__ + ": " + str(exc),
            )
        else:
            reason = ""
        log.info(
            "website fetch",
            extra={
                "status": result.status.value,
                "host": urlsplit(result.final_url or url).hostname,
                "chars": len(result.text),
                "duration_ms": int((time.monotonic() - start) * 1000),
                "reason": reason[:200],
            },
        )
        return result

    async def _fetch(self, url: str) -> WebsiteContent:
        for _ in range(MAX_REDIRECTS + 1):
            await self._check(url)
            request = self._client.build_request(
                "GET",
                url,
                headers={"User-Agent": USER_AGENT, "Accept": "text/html,application/xhtml+xml"},
            )
            response = await self._client.send(request, stream=True, follow_redirects=False)
            try:
                if response.is_redirect:
                    location = response.headers.get("location")
                    if not location:
                        raise FetchError("redirect without location")
                    url = urljoin(url, location)
                    continue
                if not response.is_success:
                    raise FetchError(f"HTTP {response.status_code}")
                content_type = response.headers.get("content-type", "").split(";")[0].strip()
                if content_type.lower() not in HTML_TYPES:
                    raise FetchError(f"content type {content_type or 'missing'}")
                body = await _read_capped(response, MAX_BYTES)
                html = body.decode(response.charset_encoding or "utf-8", errors="replace")
            finally:
                await response.aclose()
            page = extract(html, self._max_chars)
            if len(page.text) < MIN_TEXT_CHARS:
                raise FetchError("page has too little readable text")
            return WebsiteContent(
                FetchStatus.FETCHED,
                final_url=url,
                title=page.title,
                description=page.description,
                text=page.text,
            )
        raise FetchError("too many redirects")

    async def _check(self, url: str) -> None:
        parts = urlsplit(url)
        if parts.scheme not in ("http", "https"):
            raise BlockedError(f"scheme {parts.scheme!r} not allowed")
        if parts.username or parts.password:
            raise BlockedError("credentials in URL not allowed")
        host = parts.hostname
        if not host:
            raise BlockedError("URL has no host")
        try:
            port = parts.port
        except ValueError as exc:
            raise BlockedError("invalid port") from exc
        if port not in (None, 80, 443):
            raise BlockedError(f"port {port} not allowed")
        try:
            ipaddress.ip_address(host)
            addrs = [host]
        except ValueError:
            try:
                addrs = await self._resolve(host)
            except OSError as exc:
                raise FetchError(f"cannot resolve host: {exc}") from exc
        if not addrs:
            raise FetchError("host has no addresses")
        if not all(is_public_address(a) for a in addrs):
            raise BlockedError("host resolves to a non-public address")


async def _read_capped(response: httpx.Response, limit: int) -> bytes:
    """Read at most ``limit`` bytes; a larger body is cut, not rejected."""
    chunks: list[bytes] = []
    size = 0
    async for chunk in response.aiter_bytes():
        chunks.append(chunk)
        size += len(chunk)
        if size >= limit:
            break
    return b"".join(chunks)[:limit]


# ---- HTML → text ------------------------------------------------------------

_SKIP = {"script", "style", "noscript", "svg", "template", "nav", "footer", "iframe"}
_BLOCK = {
    "address", "article", "aside", "blockquote", "br", "dd", "div", "dl", "dt", "figcaption",
    "figure", "form", "h1", "h2", "h3", "h4", "h5", "h6", "header", "hr", "li", "main", "ol",
    "p", "pre", "section", "table", "td", "th", "tr", "ul",
}  # fmt: skip
_VOID = {
    "area",
    "base",
    "br",
    "col",
    "embed",
    "hr",
    "img",
    "input",
    "link",
    "meta",
    "source",
    "wbr",
}
_SPACES = re.compile(r"[ \t\r\f\v\u00a0]+")


@dataclass(frozen=True)
class Page:
    title: str
    description: str
    text: str


class _TextExtractor(HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self._skip_depth = 0
        self._in_title = False
        self.title_parts: list[str] = []
        self.description = ""
        self.og_description = ""
        self.parts: list[str] = []

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        if tag == "title":
            self._in_title = True
        elif tag == "meta":
            a = {k.lower(): (v or "") for k, v in attrs}
            key = (a.get("name") or a.get("property") or "").lower()
            if key == "description" and not self.description:
                self.description = a.get("content", "")
            elif key == "og:description" and not self.og_description:
                self.og_description = a.get("content", "")
        if tag in _SKIP and tag not in _VOID:
            self._skip_depth += 1
        elif tag in _BLOCK:
            self.parts.append("\n")

    def handle_startendtag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        # Self-closing tags (<br/>, <meta/>) never open a skipped region.
        if tag == "meta":
            self.handle_starttag(tag, attrs)
        elif tag in _BLOCK:
            self.parts.append("\n")

    def handle_endtag(self, tag: str) -> None:
        if tag == "title":
            self._in_title = False
        if tag in _SKIP and self._skip_depth > 0:
            self._skip_depth -= 1
        elif tag in _BLOCK:
            self.parts.append("\n")

    def handle_data(self, data: str) -> None:
        if self._in_title:
            self.title_parts.append(data)
        elif self._skip_depth == 0:
            self.parts.append(data)


def _normalize(s: str) -> str:
    lines = (_SPACES.sub(" ", line).strip() for line in s.splitlines())
    return "\n".join(line for line in lines if line)


def extract(html: str, max_chars: int) -> Page:
    """Title, description and visible text of ``html``, text cut to ``max_chars``."""
    parser = _TextExtractor()
    parser.feed(html)
    parser.close()
    text = _normalize("".join(parser.parts))
    if len(text) > max_chars:
        text = text[:max_chars].rsplit(" ", 1)[0]
    return Page(
        title=_normalize(" ".join(parser.title_parts))[:300],
        description=_normalize(parser.description or parser.og_description)[:500],
        text=text,
    )
