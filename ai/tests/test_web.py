from __future__ import annotations

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
    assert fake.requested == ["https://redirector.example/"]


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
    assert len(fake.requested) == 4  # initial request + 3 redirects


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


@pytest.mark.parametrize("path", synthetic_fixtures(), ids=lambda p: p.name)
async def test_synthetic_fixtures_extract(path: Path) -> None:
    site = load_fixture(path)
    fake = FakeWeb().add(site)
    result = await fetcher(fake).fetch(site.url)

    assert result.status is FetchStatus.FETCHED
    assert result.final_url == site.final_url
    assert result.title
    assert 200 <= len(result.text) <= MAX_CHARS
    _assert_clean_text(result.text)
    assert fake.requested == [hop["url"] for hop in site.redirects] + [site.final_url]


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
