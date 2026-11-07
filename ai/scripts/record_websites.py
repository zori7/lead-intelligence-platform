"""Record real company homepages as local test fixtures (SPEC.md §9.7, D12).

Fetches each URL once with the production fetcher's rules (address guard,
manual redirects, size cap) and writes
``tests/fixtures/websites/recorded/<name>/{response.json,body.html}``.
That directory is git-ignored: recordings are third-party content and stay on
this machine. Tests over them skip when it is empty.

Usage:
    uv run python scripts/record_websites.py                  # default list
    uv run python scripts/record_websites.py https://a.example https://b.example
"""

from __future__ import annotations

import asyncio
import datetime as dt
import json
import re
import sys
from pathlib import Path
from urllib.parse import urljoin, urlsplit

import httpx

from lead_ai.web import (
    HTML_TYPES,
    MAX_BYTES,
    MAX_REDIRECTS,
    USER_AGENT,
    BlockedError,
    Fetcher,
    FetchStatus,
    system_resolver,
)

OUT = Path(__file__).resolve().parent.parent / "tests" / "fixtures" / "websites" / "recorded"

# Contrasting companies: strong ICP fit, partial fit, poor fit.
DEFAULT_URLS = [
    "https://www.hubspot.com/",
    "https://www.gong.io/",
    "https://www.maersk.com/",
    "https://www.lego.com/",
]

_STRIP = re.compile(
    r"<script\b.*?</script\s*>|<style\b.*?</style\s*>|<svg\b.*?</svg\s*>|<!--.*?-->",
    re.IGNORECASE | re.DOTALL,
)


def slug(url: str) -> str:
    host = urlsplit(url).hostname or "site"
    return re.sub(r"[^a-z0-9]+", "-", host.removeprefix("www.").lower()).strip("-")


async def replay(url: str, html: str) -> object:
    """Fetch the saved page the way tests will: through MockTransport."""

    def handler(_: httpx.Request) -> httpx.Response:
        return httpx.Response(200, headers={"content-type": "text/html"}, content=html.encode())

    async def resolve(_: str) -> list[str]:
        return ["8.8.8.8"]

    async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as client:
        return await Fetcher(client, timeout=5, max_chars=6000, resolver=resolve).fetch(url)


async def record(client: httpx.AsyncClient, url: str) -> str:
    guard = Fetcher(client, timeout=20, max_chars=6000)
    hops: list[dict[str, object]] = []
    addresses: dict[str, list[str]] = {}
    current = url
    for _ in range(MAX_REDIRECTS + 1):
        try:
            await guard._check(current)  # same guard as production
        except BlockedError as exc:
            return f"blocked: {exc}"
        host = urlsplit(current).hostname or ""
        addresses[host] = await system_resolver(host)
        resp = await client.get(current, headers={"User-Agent": USER_AGENT}, follow_redirects=False)
        if resp.is_redirect:
            nxt = urljoin(current, resp.headers["location"])
            hops.append({"url": current, "status": resp.status_code, "location": nxt})
            current = nxt
            continue
        content_type = resp.headers.get("content-type", "")
        if content_type.split(";")[0].strip().lower() not in HTML_TYPES:
            return f"skipped: content type {content_type!r}"
        body = resp.content[:MAX_BYTES]
        html = _STRIP.sub("", body.decode(resp.charset_encoding or "utf-8", errors="replace"))
        meta = {
            "kind": "recorded",
            "url": url,
            "final_url": current,
            "redirects": hops,
            "status": resp.status_code,
            "headers": {"content-type": "text/html; charset=utf-8"},
            "addresses": addresses,
            "recorded_at": dt.datetime.now(dt.UTC).isoformat(timespec="seconds"),
        }
        target = OUT / slug(url)
        target.mkdir(parents=True, exist_ok=True)
        (target / "response.json").write_text(json.dumps(meta, indent=2) + "\n")
        (target / "body.html").write_text(html, encoding="utf-8")

        check = await replay(current, html)
        status = getattr(check, "status", None)
        note = "" if status is FetchStatus.FETCHED else f" (warning: {status})"
        return f"recorded {len(html)} bytes → {target.relative_to(OUT.parent.parent.parent)}{note}"
    return "failed: too many redirects"


async def main(urls: list[str]) -> None:
    async with httpx.AsyncClient(timeout=20, trust_env=False) as client:
        for url in urls:
            try:
                outcome = await record(client, url)
            except (httpx.HTTPError, OSError) as exc:
                outcome = f"failed: {type(exc).__name__}: {exc}"
            print(f"{url}: {outcome}")


if __name__ == "__main__":
    asyncio.run(main(sys.argv[1:] or DEFAULT_URLS))
