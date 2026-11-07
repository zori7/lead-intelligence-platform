"""Test support: website fixtures served through httpx.MockTransport, and a fake LLM.

Website fixtures live in ``tests/fixtures/websites/<name>/`` as
``response.json`` + ``body.html`` (SPEC.md §9.7). Synthetic fixtures are
committed; recorded ones (``recorded/<name>/``) are git-ignored and optional.
"""

from __future__ import annotations

import asyncio
import json
from collections.abc import Callable, Iterable
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import httpx
from langchain_core.language_models import BaseChatModel
from langchain_core.messages import AIMessage, BaseMessage
from langchain_core.outputs import ChatGeneration, ChatResult
from langchain_core.runnables import RunnableLambda
from pydantic import BaseModel, ConfigDict

from lead_ai.llm import ModelChoice

FIXTURES = Path(__file__).parent / "fixtures" / "websites"
RECORDED = FIXTURES / "recorded"


@dataclass
class Expected:
    """Outcome a synthetic fixture must produce (``expected`` in response.json)."""

    status: str = "fetched"
    reason_contains: str = ""
    never_requested: list[str] = field(default_factory=list)


@dataclass
class SiteFixture:
    name: str
    url: str
    final_url: str | None  # None when the fetch never reaches a page (e.g. redirect loop)
    status: int
    headers: dict[str, str]
    body: bytes
    redirects: list[dict[str, Any]]
    addresses: dict[str, list[str]]
    robots_txt: str | None = None  # served at every origin of the fixture
    delay_seconds: float = 0.0  # delay before the page response
    expected: Expected = field(default_factory=Expected)


def load_fixture(path: Path) -> SiteFixture:
    meta = json.loads((path / "response.json").read_text())
    body = (path / "body.html").read_bytes() if (path / "body.html").exists() else b""
    # Oversized bodies are generated, not committed: the file is repeated.
    body *= int(meta.get("body_repeat", 1))
    return SiteFixture(
        name=path.name,
        url=meta["url"],
        final_url=meta.get("final_url"),
        status=meta.get("status", 200),
        headers=meta.get("headers", {}),
        body=body,
        redirects=meta.get("redirects", []),
        addresses=meta["addresses"],
        robots_txt=meta.get("robots_txt"),
        delay_seconds=float(meta.get("delay_seconds", 0)),
        expected=Expected(**meta.get("expected", {})),
    )


def synthetic_fixtures() -> list[Path]:
    return sorted(p for p in FIXTURES.iterdir() if (p / "response.json").exists())


def recorded_fixtures() -> list[Path]:
    if not RECORDED.is_dir():
        return []
    return sorted(p for p in RECORDED.iterdir() if (p / "response.json").exists())


@dataclass
class FakeWeb:
    """Routes for MockTransport plus a resolver, built from fixtures."""

    routes: dict[str, httpx.Response] = field(default_factory=dict)
    delays: dict[str, float] = field(default_factory=dict)
    addresses: dict[str, list[str]] = field(default_factory=dict)
    requested: list[str] = field(default_factory=list)

    @property
    def page_requests(self) -> list[str]:
        """Requests other than robots.txt."""
        return [u for u in self.requested if not u.endswith("/robots.txt")]

    def add(self, site: SiteFixture) -> FakeWeb:
        urls = [site.url, *(hop["url"] for hop in site.redirects)]
        for hop in site.redirects:
            self.routes[hop["url"]] = httpx.Response(
                hop["status"], headers={"location": hop["location"]}
            )
        if site.final_url is not None:
            urls.append(site.final_url)
            self.routes[site.final_url] = httpx.Response(
                site.status, headers=site.headers, content=site.body
            )
            self.delays[site.final_url] = site.delay_seconds
        if site.robots_txt is not None:
            for url in urls:
                u = httpx.URL(url)
                self.routes[f"{u.scheme}://{u.netloc.decode()}/robots.txt"] = httpx.Response(
                    200, headers={"content-type": "text/plain"}, content=site.robots_txt.encode()
                )
        self.addresses.update(site.addresses)
        return self

    def route(self, url: str, response: httpx.Response, addresses: Iterable[str] = ()) -> FakeWeb:
        self.routes[url] = response
        host = httpx.URL(url).host
        if addresses:
            self.addresses[host] = list(addresses)
        return self

    async def _handle(self, request: httpx.Request) -> httpx.Response:
        url = str(request.url)
        self.requested.append(url)
        if self.delays.get(url):
            await asyncio.sleep(self.delays[url])
        if url not in self.routes:
            return httpx.Response(404, headers={"content-type": "text/html"}, content=b"not found")
        r = self.routes[url]
        return httpx.Response(r.status_code, headers=r.headers, content=r.content)

    def client(self) -> httpx.AsyncClient:
        return httpx.AsyncClient(transport=httpx.MockTransport(self._handle))

    async def resolve(self, host: str) -> list[str]:
        if host not in self.addresses:
            raise OSError(f"unknown host {host}")
        return self.addresses[host]


# ---- Fake LLM ------------------------------------------------------------------


@dataclass
class Call:
    node: str
    model: str
    temperature: float
    messages: list[BaseMessage]


Reply = dict[str, Any] | Exception | Callable[[], Any]

MALFORMED = object()
"""Reply sentinel: the model answered, but the reply does not parse into the schema."""


def sequence(*replies: Any) -> Callable[[], Any]:
    """Reply with each item in turn (one per attempt); the last one repeats."""
    items = list(replies)

    def next_reply() -> Any:
        return items.pop(0) if len(items) > 1 else items[0]

    return next_reply


def api_error(cls: type[Exception], status: int) -> Exception:
    """An ``openai`` HTTP error as the SDK raises it, e.g. ``api_error(NotFoundError, 404)``."""
    response = httpx.Response(status, request=httpx.Request("POST", "http://llm.invalid/v1/x"))
    return cls(f"HTTP {status}", response=response, body=None)  # type: ignore[call-arg]


class FakeStructuredModel(BaseChatModel):
    """Answers ``with_structured_output`` with canned data per schema name."""

    node: str
    choice: ModelChoice
    owner: Any  # FakeLlm; pydantic would copy a list field, so calls go through the owner

    model_config = ConfigDict(arbitrary_types_allowed=True)

    @property
    def _llm_type(self) -> str:
        return "fake-structured"

    def _generate(self, messages: list[BaseMessage], *args: Any, **kwargs: Any) -> ChatResult:
        return ChatResult(generations=[ChatGeneration(message=AIMessage(content="{}"))])

    def with_structured_output(self, schema: Any, **kwargs: Any) -> Any:  # type: ignore[override]
        async def run(messages: list[BaseMessage]) -> dict[str, Any]:
            owner: FakeLlm = self.owner
            owner.calls.append(
                Call(self.node, self.choice.model, self.choice.temperature, list(messages))
            )
            if owner.gate is not None:
                await owner.gate.wait()
            reply = owner.replies[schema.__name__]
            if callable(reply) and not isinstance(reply, type):
                reply = reply()
            if isinstance(reply, Exception):
                raise reply
            if reply is MALFORMED:
                return {
                    "raw": AIMessage(content="not json"),
                    "parsed": None,
                    "parsing_error": ValueError("invalid JSON"),
                }
            raw = AIMessage(
                content=json.dumps(reply),
                usage_metadata={"input_tokens": 100, "output_tokens": 20, "total_tokens": 120},
            )
            parsed: BaseModel = schema.model_validate(reply)
            return {"raw": raw, "parsed": parsed, "parsing_error": None}

        return RunnableLambda(run)


DEFAULT_REPLIES: dict[str, Reply] = {
    "CompanyExtraction": {
        "industry": "B2B software",
        "size_estimate": "51-200",
        "business_model": "Annual per-seat SaaS subscriptions",
        "description": "Revenue intelligence platform for B2B sales teams.",
        "key_needs": ["Pipeline visibility", "Rep coaching"],
        "confidence": 0.8,
    },
    "FitAnalysis": {
        "score": 80,
        "rationale": "B2B SaaS in the target size range.",
        "matched_criteria": ["B2B SaaS", "50-1000 employees"],
        "gaps": [],
    },
    "IntentAnalysis": {
        "score": 55,
        "rationale": "Hiring sales staff suggests investment in growth.",
        "signals": ["Hiring account executives"],
    },
    "RecommendationsOutput": {
        "summary": "Strong-fit SaaS company with moderate intent.",
        "recommendations": [
            {"action": "Email the VP Sales", "rationale": "Owns forecasting", "priority": "high"},
            {"action": "Share a case study", "rationale": "Builds trust", "priority": "medium"},
        ],
    },
}


@dataclass
class FakeLlm:
    """``llm_factory`` for configurable: records every call."""

    replies: dict[str, Reply] = field(default_factory=lambda: dict(DEFAULT_REPLIES))
    calls: list[Call] = field(default_factory=list)
    gate: asyncio.Event | None = None

    def __call__(self, node: str, choice: ModelChoice) -> BaseChatModel:
        return FakeStructuredModel(node=node, choice=choice, owner=self)

    def attempts(self, node: str) -> int:
        return sum(1 for c in self.calls if c.node == node)

    def prompt(self, node: str) -> str:
        call = next(c for c in self.calls if c.node == node)
        return "\n".join(str(m.content) for m in call.messages)


def models(**overrides: str) -> dict[str, ModelChoice]:
    base = {
        "extract_company": ModelChoice("model-default", 0.2),
        "analyze_fit": ModelChoice("model-default", 0.2),
        "analyze_intent": ModelChoice("model-default", 0.2),
        "generate_recommendations": ModelChoice("model-default", 0.2),
    }
    for node, model in overrides.items():
        base[node] = ModelChoice(model, 0.2)
    return base
