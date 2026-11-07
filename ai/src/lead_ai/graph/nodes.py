"""Workflow nodes (SPEC.md §9.1).

Every node is ``async (state, config) -> update`` and wrapped in
:func:`tracked`, which reports the step's state on LangGraph's custom stream.
A node can report an outcome other than ``done`` by setting
``update[STEP_STATE]``; the wrapper removes the key before LangGraph sees it.
"""

from __future__ import annotations

import functools
import logging
import time
from collections.abc import Awaitable, Callable
from typing import Any
from urllib.parse import urlsplit, urlunsplit

import httpx
from langchain_core.runnables import RunnableConfig
from langgraph.config import get_stream_writer

from lead_ai import llm, web
from lead_ai.config import get_settings
from lead_ai.errors import ErrorCode, Failure, classify, insufficient_data, internal, invalid_input
from lead_ai.graph import prompts
from lead_ai.graph.schemas import (
    AnalysisResult,
    CompanyExtraction,
    CompanyInsights,
    FitAnalysis,
    IntentAnalysis,
    LeadInput,
    RecommendationsOutput,
    ResultMeta,
    WebsiteMeta,
)
from lead_ai.graph.scoring import compute_score
from lead_ai.graph.state import AnalysisState, TokenUsage

log = logging.getLogger(__name__)

STEP_STATE = "__step_state__"

Node = Callable[[AnalysisState, RunnableConfig], Awaitable[dict[str, Any]]]


def tracked(step: str) -> Callable[[Node], Node]:
    """Emit ``{"step", "state"}`` progress events around a node."""

    def decorate(fn: Node) -> Node:
        @functools.wraps(fn)
        async def wrapper(state: AnalysisState, config: RunnableConfig) -> dict[str, Any]:
            write = get_stream_writer()
            write({"step": step, "state": "running"})
            try:
                update = await fn(state, config)
            except BaseException:
                write({"step": step, "state": "failed"})
                raise
            write({"step": step, "state": update.pop(STEP_STATE, "done")})
            return update

        return wrapper

    return decorate


# ---- validate_input ------------------------------------------------------------

# Defense in depth: Go enforces the same limits from the OpenAPI spec.
_MAX_LEN = {
    "company_name": 200,
    "website": 300,
    "contact_name": 200,
    "contact_title": 200,
    "industry": 100,
    "company_size": 20,
    "notes": 5000,
}


def _normalize_website(url: str) -> str:
    if not url:
        return ""
    if "://" not in url:
        url = "https://" + url
    parts = urlsplit(url)
    return urlunsplit(
        (parts.scheme.lower(), parts.netloc.lower(), parts.path or "/", parts.query, "")
    )


@tracked("validate_input")
async def validate_input(state: AnalysisState, config: RunnableConfig) -> dict[str, Any]:
    raw = state["lead"]
    values = {k: (getattr(raw, k) or "").strip() for k in _MAX_LEN}
    too_long = [k for k, n in _MAX_LEN.items() if len(values[k]) > n]
    if too_long:
        return {
            "failure": invalid_input(f"Field too long: {', '.join(too_long)}."),
            STEP_STATE: "failed",
        }
    if not values["company_name"]:
        return {"failure": invalid_input("Company name is required."), STEP_STATE: "failed"}
    if not any(values[k] for k in ("website", "industry", "notes", "contact_title")):
        return {"failure": insufficient_data(), STEP_STATE: "failed"}
    values["website"] = _normalize_website(values["website"])
    return {"lead": LeadInput(**values)}


# ---- fetch_website --------------------------------------------------------------


@tracked("fetch_website")
async def fetch_website(state: AnalysisState, config: RunnableConfig) -> dict[str, Any]:
    url = state["lead"].website
    settings = get_settings()
    if not url:
        return {"website": web.WebsiteContent(web.FetchStatus.NOT_PROVIDED), STEP_STATE: "skipped"}
    if not settings.web_fetch_enabled:
        return {"website": web.WebsiteContent(web.FetchStatus.DISABLED), STEP_STATE: "skipped"}

    conf = config.get("configurable", {})
    resolver: web.Resolver = conf.get("resolver", web.system_resolver)
    client: httpx.AsyncClient | None = conf.get("http_client")

    async def run(c: httpx.AsyncClient) -> web.WebsiteContent:
        fetcher = web.Fetcher(
            c,
            timeout=settings.web_fetch_timeout_seconds,
            max_chars=settings.web_fetch_max_chars,
            resolver=resolver,
        )
        return await fetcher.fetch(url)

    if client is not None:
        site = await run(client)
    else:
        # trust_env=False: proxies from the environment must not bypass the address guard.
        async with httpx.AsyncClient(trust_env=False) as c:
            site = await run(c)

    update: dict[str, Any] = {"website": site}
    if not site.fetched:
        reason = site.reason or "The website could not be read."
        update["warnings"] = [f"{reason} Insights are inferred from lead data."]
        update[STEP_STATE] = "failed"
    return update


# ---- LLM nodes ------------------------------------------------------------------


def _usage(node: str, reply: llm.StructuredReply[Any]) -> list[TokenUsage]:
    return [TokenUsage(node, reply.input_tokens, reply.output_tokens)]


def _log_node_failure(node: str, choice: llm.ModelChoice, failure: Failure, exc: Exception) -> None:
    # The error type and code only: provider responses may echo the prompt.
    log.warning(
        "llm node failed",
        extra={
            "node": node,
            "model": choice.model,
            "code": failure.code.value,
            "error": type(exc).__name__,
        },
    )


def _degraded(node: str, choice: llm.ModelChoice, exc: Exception, what: str) -> dict[str, Any]:
    """State update for a non-critical LLM node that failed after its retries.

    The run continues without this part (§9.3). The warning says whether a
    retry may help or the model settings need attention. The model is still
    recorded, so the result footer shows which model failed.
    """
    failure = classify(exc)
    _log_node_failure(node, choice, failure, exc)
    if failure.code is ErrorCode.PROVIDER_CONFIG:
        warning = f"{what} unavailable: the configured model could not be used. Check /admin."
    else:
        warning = f"{what} unavailable: the AI provider failed. Retrying may help."
    return {
        "warnings": [warning],
        "node_models": {node: choice.model},
        STEP_STATE: "failed",
    }


@tracked("extract_company")
async def extract_company(state: AnalysisState, config: RunnableConfig) -> dict[str, Any]:
    lead = state["lead"]
    choice = llm.model_choice(config, "extract_company")
    try:
        reply = await llm.invoke_structured(
            config,
            "extract_company",
            CompanyExtraction,
            prompts.extract_company(lead, state.get("website")),
        )
    except Exception as exc:
        failure = classify(exc)
        _log_node_failure("extract_company", choice, failure, exc)
        return {"failure": failure, STEP_STATE: "failed"}
    company = CompanyInsights(name=lead.company_name, **reply.value.model_dump())
    return {
        "company": company,
        "usage": _usage("extract_company", reply),
        "node_models": {"extract_company": choice.model},
    }


@tracked("analyze_fit")
async def analyze_fit(state: AnalysisState, config: RunnableConfig) -> dict[str, Any]:
    choice = llm.model_choice(config, "analyze_fit")
    company = state["company"]
    assert company is not None
    try:
        reply = await llm.invoke_structured(
            config,
            "analyze_fit",
            FitAnalysis,
            prompts.analyze_fit(state["lead"], company, get_settings().icp_description),
        )
    except Exception as exc:
        return {"fit": None, **_degraded("analyze_fit", choice, exc, "Fit analysis")}
    return {
        "fit": reply.value,
        "usage": _usage("analyze_fit", reply),
        "node_models": {"analyze_fit": choice.model},
    }


@tracked("analyze_intent")
async def analyze_intent(state: AnalysisState, config: RunnableConfig) -> dict[str, Any]:
    choice = llm.model_choice(config, "analyze_intent")
    company = state["company"]
    assert company is not None
    try:
        reply = await llm.invoke_structured(
            config,
            "analyze_intent",
            IntentAnalysis,
            prompts.analyze_intent(state["lead"], company),
        )
    except Exception as exc:
        return {"intent": None, **_degraded("analyze_intent", choice, exc, "Intent analysis")}
    return {
        "intent": reply.value,
        "usage": _usage("analyze_intent", reply),
        "node_models": {"analyze_intent": choice.model},
    }


@tracked("score_lead")
async def score_lead(state: AnalysisState, config: RunnableConfig) -> dict[str, Any]:
    fit, intent, company = state.get("fit"), state.get("intent"), state["company"]
    assert company is not None
    outcome = compute_score(
        fit.score if fit else None, intent.score if intent else None, company.confidence
    )
    if outcome.score is None:
        failure = Failure(
            ErrorCode.PROVIDER_ERROR,
            "Both fit and intent analysis failed. Please retry.",
            retryable=True,
        )
        return {"score": None, "failure": failure, STEP_STATE: "failed"}
    # outcome.warnings ("fit unavailable") are not added: the failed branch has
    # already recorded a more specific warning.
    return {"score": outcome.score}


@tracked("generate_recommendations")
async def generate_recommendations(state: AnalysisState, config: RunnableConfig) -> dict[str, Any]:
    choice = llm.model_choice(config, "generate_recommendations")
    company, score = state["company"], state["score"]
    assert company is not None and score is not None
    try:
        reply = await llm.invoke_structured(
            config,
            "generate_recommendations",
            RecommendationsOutput,
            prompts.generate_recommendations(
                state["lead"],
                company,
                state.get("fit"),
                state.get("intent"),
                score.value,
                score.tier,
            ),
        )
    except Exception as exc:
        return {
            "recommendations": [],
            **_degraded("generate_recommendations", choice, exc, "Recommendations"),
        }
    return {
        "summary": reply.value.summary,
        "recommendations": reply.value.recommendations,
        "usage": _usage("generate_recommendations", reply),
        "node_models": {"generate_recommendations": choice.model},
    }


@tracked("aggregate")
async def aggregate(state: AnalysisState, config: RunnableConfig) -> dict[str, Any]:
    company, score = state["company"], state["score"]
    assert company is not None and score is not None
    site = state.get("website") or web.WebsiteContent(web.FetchStatus.NOT_PROVIDED)
    usage = state.get("usage", [])
    recommendations = state.get("recommendations", [])
    fit, intent = state.get("fit"), state.get("intent")
    result = AnalysisResult(
        summary=state.get("summary") or company.description,
        company=company,
        fit=fit,
        intent=intent,
        score=score,
        recommendations=recommendations,
        meta=ResultMeta(
            node_models=state.get("node_models", {}),
            website=WebsiteMeta(status=site.status.value, final_url=site.final_url),
            partial=fit is None or intent is None or not recommendations,
            warnings=list(dict.fromkeys(state.get("warnings", []))),
            duration_ms=int((time.monotonic() - state["started_at"]) * 1000),
            input_tokens=sum(u.input_tokens for u in usage),
            output_tokens=sum(u.output_tokens for u in usage),
        ),
    )
    return {"result": result}


async def fail(state: AnalysisState, config: RunnableConfig) -> dict[str, Any]:
    """Terminal failure node: guarantees ``failure`` is set."""
    return {} if state.get("failure") else {"failure": internal()}
