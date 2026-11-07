"""LLM client construction and model discovery (SPEC.md §9.6).

Each LLM node asks :func:`for_node` for its chat model. The model and
temperature come from the request (``configurable["models"]``), never from
defaults in this service. Tests inject fakes through
``configurable["llm_factory"]``; there is no runtime fake mode.
"""

from __future__ import annotations

import threading
from collections.abc import Callable, Mapping
from dataclasses import dataclass
from typing import Any

import httpx
from langchain_core.language_models import BaseChatModel
from langchain_core.messages import AIMessage
from langchain_core.runnables import Runnable, RunnableConfig
from langchain_openai import ChatOpenAI
from pydantic import BaseModel, SecretStr

from lead_ai.config import Settings, get_settings
from lead_ai.errors import StructuredOutputError

LLM_NODES: tuple[str, ...] = (
    "extract_company",
    "analyze_fit",
    "analyze_intent",
    "generate_recommendations",
)


@dataclass(frozen=True)
class ModelChoice:
    model: str
    temperature: float


LlmFactory = Callable[[str, ModelChoice], BaseChatModel]
"""Builds the chat model for ``(node, choice)``. Used by tests only."""

_cache: dict[tuple[str, float], ChatOpenAI] = {}
_cache_lock = threading.Lock()


def _client(choice: ModelChoice, settings: Settings) -> ChatOpenAI:
    """Return a cached client per (model, temperature) so connection pools are reused."""
    key = (choice.model, choice.temperature)
    with _cache_lock:
        llm = _cache.get(key)
        if llm is None:
            llm = ChatOpenAI(
                base_url=settings.llm_base,
                api_key=SecretStr(settings.llm_api_key),
                model=choice.model,
                temperature=choice.temperature,
                timeout=settings.llm_timeout_seconds,
                # Retries live in one place (with_retry, M4); SDK retries would multiply them.
                max_retries=0,
            )
            _cache[key] = llm
        return llm


def model_choice(config: RunnableConfig, node: str) -> ModelChoice:
    models: Mapping[str, ModelChoice] = config.get("configurable", {})["models"]
    return models[node]


def for_node(config: RunnableConfig, node: str) -> BaseChatModel:
    """Chat model for ``node`` as configured in this run."""
    choice = model_choice(config, node)
    factory: LlmFactory | None = config.get("configurable", {}).get("llm_factory")
    if factory is not None:
        return factory(node, choice)
    return _client(choice, get_settings())


@dataclass
class StructuredReply[T: BaseModel]:
    value: T
    input_tokens: int
    output_tokens: int


async def invoke_structured[T: BaseModel](
    llm: BaseChatModel, schema: type[T], messages: list[Any], config: RunnableConfig
) -> StructuredReply[T]:
    """Call ``llm`` and parse the reply into ``schema``.

    ``include_raw=True`` keeps the raw message so token usage can be reported
    and a parse failure surfaces as :class:`StructuredOutputError` instead of a
    bare exception from deep inside the parser.
    """
    method = get_settings().llm_structured_output_method
    runnable: Runnable[Any, dict[str, Any]] = llm.with_structured_output(
        schema, method=method, include_raw=True
    )
    out = await runnable.ainvoke(messages, config=config)
    if out.get("parsing_error") is not None or out.get("parsed") is None:
        raise StructuredOutputError(str(out.get("parsing_error") or "empty structured output"))
    raw = out.get("raw")
    usage = raw.usage_metadata if isinstance(raw, AIMessage) and raw.usage_metadata else None
    return StructuredReply(
        value=out["parsed"],
        input_tokens=usage["input_tokens"] if usage else 0,
        output_tokens=usage["output_tokens"] if usage else 0,
    )


class LlmUnavailableError(Exception):
    """The LLM server could not be reached or returned an unusable model list."""


async def list_models(settings: Settings | None = None) -> list[str]:
    """Model IDs reported by ``GET {LLM_BASE_URL}/models``, sorted."""
    settings = settings or get_settings()
    try:
        async with httpx.AsyncClient(timeout=5.0) as client:
            resp = await client.get(
                f"{settings.llm_base}/models",
                headers={"Authorization": f"Bearer {settings.llm_api_key}"},
            )
            resp.raise_for_status()
            data = resp.json().get("data", [])
            return sorted({str(m["id"]) for m in data if isinstance(m, dict) and m.get("id")})
    except (httpx.HTTPError, ValueError, AttributeError) as exc:
        raise LlmUnavailableError(str(exc)) from exc
