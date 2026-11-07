from __future__ import annotations

import time
from pathlib import Path

import httpx
import pytest

from lead_ai.web import Fetcher, FetchStatus, extract, is_public_address
from tests.support import FakeWeb, load_fixture, recorded_fixtures, synthetic_fixtures

MAX_CHARS = 6000


def fetcher(fake: FakeWeb, max_chars: int = MAX_CHARS) -> Fetcher:
    return Fetcher(fake.client(), timeout=5, max_chars=max_chars, resolver=fake.resolve)


# ---- Address guard ----------------------------------------------------------------


@pytest.mark.parametrize(
    ("addr", "public"),
    [
        ("8.8.8.8", True),
        ("93.184.215.10", True),
        ("2606:4700::1", True),
        ("127.0.0.1", False),
        ("10.1.2.3", False),
        ("172.18.0.3", False),  # docker compose network
        ("192.168.1.20", False),
        ("169.254.169.254", False),  # cloud metadata
        ("100.64.0.1", False),  # carrier-grade NAT
        ("0.0.0.0", False),
        ("224.0.0.1", False),
        ("::1", False),
        ("fe80::1", False),
        ("fc00::1", False),
        ("::ffff:127.0.0.1", False),
        ("::ffff:10.0.0.1", False),
        ("not-an-ip", False),
    ],
)
def test_is_public_address(addr: str, public: bool) -> None:
    assert is_public_address(addr) is public


@pytest.mark.parametrize(
    ("url", "addresses"),
    [
        ("http://localhost:8080/", ["127.0.0.1"]),  # port not allowed
        ("http://localhost/", ["127.0.0.1"]),
        ("http://api/", ["172.18.0.3"]),
        ("http://127.0.0.1/", []),
        ("http://[::1]/", []),
        ("http://169.254.169.254/latest/meta-data/", []),
        ("ftp://files.example/", ["93.184.215.1"]),
        ("https://user:pass@site.example/", ["93.184.215.1"]),
        ("https://mixed.example/", ["93.184.215.1", "10.0.0.7"]),  # any private address blocks
        ("https://site.example:8443/", ["93.184.215.1"]),
    ],
)
async def test_guard_blocks_without_requesting(url: str, addresses: list[str]) -> None:
    fake = FakeWeb()
    if addresses:
        fake.addresses[httpx.URL(url).host] = addresses
    result = await fetcher(fake).fetch(url)
    assert result.status is FetchStatus.BLOCKED
    assert fake.requested == []


async def test_guard_applies_to_redirect_hops() -> None:
    fake = FakeWeb().route(
        "https://redirector.example/",
        httpx.Response(302, headers={"location": "http://169.254.169.254/latest/meta-data/"}),
        addresses=["93.184.215.50"],
    )
    result = await fetcher(fake).fetch("https://redirector.example/")
    assert result.status is FetchStatus.BLOCKED
    assert fake.page_requests == ["https://redirector.example/"]


async def test_redirect_to_internal_hostname_is_blocked() -> None:
    fake = FakeWeb().route(
        "https://redirector.example/",
        httpx.Response(301, headers={"location": "http://postgres/"}),
        addresses=["93.184.215.50"],
    )
    fake.addresses["postgres"] = ["172.18.0.2"]
    result = await fetcher(fake).fetch("https://redirector.example/")
    assert result.status is FetchStatus.BLOCKED
    assert "http://postgres/" not in fake.requested


async def test_redirect_limit() -> None:
    fake = FakeWeb()
    for i in range(5):
        fake.route(
            f"https://loop.example/{i}",
            httpx.Response(302, headers={"location": f"/{i + 1}"}),
            addresses=["93.184.215.60"],
        )
    result = await fetcher(fake).fetch("https://loop.example/0")
    assert result.status is FetchStatus.FAILED
    assert len(fake.page_requests) == 4  # initial request + 3 redirects


@pytest.mark.parametrize(
    "response",
    [
        httpx.Response(500, headers={"content-type": "text/html"}, content=b"<p>error</p>"),
        httpx.Response(200, headers={"content-type": "application/pdf"}, content=b"%PDF"),
        httpx.Response(200, headers={"content-type": "text/html"}, content=b"<p>tiny</p>"),
    ],
    ids=["http-error", "not-html", "too-little-text"],
)
async def test_unusable_pages_fail(response: httpx.Response) -> None:
    fake = FakeWeb().route("https://bad.example/", response, addresses=["93.184.215.70"])
    result = await fetcher(fake).fetch("https://bad.example/")
    assert result.status is FetchStatus.FAILED
    assert result.text == ""


async def test_unresolvable_host_fails() -> None:
    result = await fetcher(FakeWeb()).fetch("https://nowhere.example/")
    assert result.status is FetchStatus.FAILED


# ---- Extraction over fixtures ---------------------------------------------------


def _assert_clean_text(text: str) -> None:
    for leaked in (
        "dataLayer",
        "font-family",
        "decorative svg",
        "hidden template",
        "Cookie settings",
    ):
        assert leaked not in text


FIXTURE_TIMEOUT = 1.0  # seconds; edge-slow responds after 5 s


@pytest.mark.parametrize("path", synthetic_fixtures(), ids=lambda p: p.name)
async def test_synthetic_fixtures(path: Path) -> None:
    """One case per committed fixture: expected status, no disallowed request, bounded time."""
    site = load_fixture(path)
    fake = FakeWeb().add(site)
    f = Fetcher(fake.client(), timeout=FIXTURE_TIMEOUT, max_chars=MAX_CHARS, resolver=fake.resolve)

    start = time.monotonic()
    result = await f.fetch(site.url)
    elapsed = time.monotonic() - start

    assert result.status.value == site.expected.status
    assert elapsed < FIXTURE_TIMEOUT + 0.5
    for url in site.expected.never_requested:
        assert url not in fake.requested
    if result.fetched:
        assert result.final_url == site.final_url
        assert result.title
        assert 200 <= len(result.text) <= MAX_CHARS
        assert result.reason == ""
        _assert_clean_text(result.text)
        assert fake.page_requests == [hop["url"] for hop in site.redirects] + [site.final_url]
    else:
        assert result.text == ""
        assert site.expected.reason_contains in result.reason


# ---- robots.txt ---------------------------------------------------------------------

PAGE = httpx.Response(
    200,
    headers={"content-type": "text/html"},
    content=b"<title>Site</title><p>" + b"Readable homepage text. " * 20 + b"</p>",
)


def _robots(body: str, status: int = 200) -> httpx.Response:
    return httpx.Response(status, headers={"content-type": "text/plain"}, content=body.encode())


@pytest.mark.parametrize(
    ("robots", "path", "expected"),
    [
        (None, "/", FetchStatus.FETCHED),  # missing robots.txt (404) allows
        (_robots("", status=500), "/", FetchStatus.FETCHED),  # unreadable allows
        (_robots("User-agent: *\nDisallow: /\n"), "/", FetchStatus.BLOCKED),
        (_robots("User-agent: *\nDisallow: /private/\n"), "/", FetchStatus.FETCHED),
        (_robots("User-agent: *\nDisallow: /private/\n"), "/private/x", FetchStatus.BLOCKED),
        (_robots("User-agent: OtherBot\nDisallow: /\n"), "/", FetchStatus.FETCHED),
        (_robots("User-agent: leadintelbot\nDisallow: /\n"), "/", FetchStatus.BLOCKED),
    ],
    ids=["missing", "server-error", "disallow-all", "other-path", "disallowed-path",
         "other-agent", "case-insensitive-agent"],
)  # fmt: skip
async def test_robots_rules(
    robots: httpx.Response | None, path: str, expected: FetchStatus
) -> None:
    url = "https://site.example" + path
    fake = FakeWeb().route(url, PAGE, addresses=["93.184.215.90"])
    if robots is not None:
        fake.route("https://site.example/robots.txt", robots)
    result = await fetcher(fake).fetch(url)
    assert result.status is expected
    assert fake.requested[0] == "https://site.example/robots.txt"
    if expected is FetchStatus.BLOCKED:
        assert fake.page_requests == []


async def test_robots_checked_for_each_redirect_origin() -> None:
    fake = (
        FakeWeb()
        .route(
            "https://old.example/",
            httpx.Response(301, headers={"location": "https://new.example/"}),
            addresses=["93.184.215.91"],
        )
        .route("https://new.example/", PAGE, addresses=["93.184.215.92"])
        .route("https://new.example/robots.txt", _robots("User-agent: *\nDisallow: /\n"))
    )
    result = await fetcher(fake).fetch("https://old.example/")
    assert result.status is FetchStatus.BLOCKED
    assert fake.requested == [
        "https://old.example/robots.txt",
        "https://old.example/",
        "https://new.example/robots.txt",
    ]


async def test_robots_redirect_is_guarded() -> None:
    fake = (
        FakeWeb()
        .route("https://site.example/", PAGE, addresses=["93.184.215.93"])
        .route(
            "https://site.example/robots.txt",
            httpx.Response(302, headers={"location": "http://169.254.169.254/robots.txt"}),
        )
    )
    result = await fetcher(fake).fetch("https://site.example/")
    assert result.status is FetchStatus.FETCHED  # unreadable robots.txt allows
    assert all("169.254" not in u for u in fake.requested)


async def test_robots_body_is_capped() -> None:
    # A disallow rule after 64 KB of comments is never read.
    body = "# padding\n" * 7000 + "User-agent: *\nDisallow: /\n"
    fake = (
        FakeWeb()
        .route("https://site.example/", PAGE, addresses=["93.184.215.94"])
        .route("https://site.example/robots.txt", _robots(body))
    )
    result = await fetcher(fake).fetch("https://site.example/")
    assert result.status is FetchStatus.FETCHED


async def test_extraction_details() -> None:
    northwind = load_fixture(Path(__file__).parent / "fixtures/websites/northwind-analytics")
    result = await fetcher(FakeWeb().add(northwind)).fetch(northwind.url)
    assert result.title.startswith("Northwind Analytics")
    assert result.description.startswith("Northwind Analytics helps B2B revenue teams")
    assert "Founded in 2019" in result.text
    assert "Log in" not in result.text  # navigation skipped

    bakery = load_fixture(Path(__file__).parent / "fixtures/websites/brightleaf-bakery")
    result = await fetcher(FakeWeb().add(bakery)).fetch(bakery.url)
    assert result.title == "Brightleaf Bakery & Café"  # charset + entities decoded
    assert "café serves breakfast" in result.text


async def test_text_is_truncated() -> None:
    site = load_fixture(Path(__file__).parent / "fixtures/websites/northwind-analytics")
    result = await fetcher(FakeWeb().add(site), max_chars=500).fetch(site.url)
    assert result.status is FetchStatus.FETCHED
    assert len(result.text) <= 500


def test_unclosed_skip_tags_do_not_leak_text() -> None:
    page = extract("<p>Visible</p><script>var secret = 1;", 1000)
    assert page.text == "Visible"


@pytest.mark.recorded
@pytest.mark.parametrize(
    "path",
    recorded_fixtures()
    or [
        pytest.param(
            None, marks=pytest.mark.skip(reason="no recorded fixtures (make record-websites)")
        )
    ],
    ids=lambda p: p.name if p else "none",
)
async def test_recorded_fixtures_extract(path: Path) -> None:
    site = load_fixture(path)
    result = await fetcher(FakeWeb().add(site)).fetch(site.url)
    assert result.status is FetchStatus.FETCHED, f"{site.name}: {result.status}"
    assert result.title
    assert 200 <= len(result.text) <= MAX_CHARS
