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
class SiteFixture:
    name: str
    url: str
    final_url: str
    status: int
    headers: dict[str, str]
    body: bytes
    redirects: list[dict[str, Any]]
    addresses: dict[str, list[str]]


def load_fixture(path: Path) -> SiteFixture:
    meta = json.loads((path / "response.json").read_text())
    return SiteFixture(
        name=path.name,
        url=meta["url"],
        final_url=meta["final_url"],
        status=meta["status"],
        headers=meta["headers"],
        body=(path / "body.html").read_bytes(),
        redirects=meta.get("redirects", []),
        addresses=meta["addresses"],
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
    addresses: dict[str, list[str]] = field(default_factory=dict)
    requested: list[str] = field(default_factory=list)

    def add(self, site: SiteFixture) -> FakeWeb:
        for hop in site.redirects:
            self.routes[hop["url"]] = httpx.Response(
                hop["status"], headers={"location": hop["location"]}
            )
        self.routes[site.final_url] = httpx.Response(
            site.status, headers=site.headers, content=site.body
        )
        self.addresses.update(site.addresses)
        return self

    def route(self, url: str, response: httpx.Response, addresses: Iterable[str] = ()) -> FakeWeb:
        self.routes[url] = response
        host = httpx.URL(url).host
        if addresses:
            self.addresses[host] = list(addresses)
        return self

    def _handle(self, request: httpx.Request) -> httpx.Response:
        url = str(request.url)
        self.requested.append(url)
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
