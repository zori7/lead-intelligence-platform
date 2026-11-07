from __future__ import annotations

import logging
import time
from pathlib import Path
from typing import Any

import httpx
import openai
import pytest

from lead_ai.errors import ErrorCode
from lead_ai.graph.builder import STEPS, build_graph
from lead_ai.graph.schemas import LeadInput
from tests.support import MALFORMED, FakeLlm, FakeWeb, api_error, load_fixture, models, sequence

FIXTURES = Path(__file__).parent / "fixtures/websites"
NORTHWIND = FIXTURES / "northwind-analytics"
LEAD = LeadInput(company_name="Acme", industry="SaaS", notes="Asked for pricing.")


async def run(
    lead: LeadInput, fake_llm: FakeLlm, fake_web: FakeWeb | None = None, **model_overrides: str
) -> tuple[dict[str, Any], list[tuple[str, str]]]:
    fake_web = fake_web or FakeWeb()
    config = {
        "configurable": {
            "models": models(**model_overrides),
            "llm_factory": fake_llm,
            "http_client": fake_web.client(),
            "resolver": fake_web.resolve,
        }
    }
    state = {"job_id": "job-1", "lead": lead, "started_at": time.monotonic()}
    events: list[tuple[str, str]] = []
    final: dict[str, Any] = {}
    async for mode, chunk in build_graph().astream(state, config, stream_mode=["custom", "values"]):
        if mode == "custom":
            events.append((chunk["step"], chunk["state"]))
        else:
            final = chunk
    return final, events


def final_states(events: list[tuple[str, str]]) -> dict[str, str]:
    return {step: state for step, state in events if state != "running"}


async def test_happy_path_with_website() -> None:
    site = load_fixture(NORTHWIND)
    fake_llm = FakeLlm()
    lead = LeadInput(
        company_name="Northwind Analytics",
        website="https://northwind-analytics.example/",
        contact_title="VP Sales",
        notes="Asked for a demo after the webinar.",
    )
    final, events = await run(lead, fake_llm, FakeWeb().add(site))

    assert final.get("failure") is None
    result = final["result"]
    assert result.meta.website.status == "fetched"
    assert result.meta.website.final_url == site.final_url
    # The website text reaches extract_company inside the delimited block.
    prompt = fake_llm.prompt("extract_company")
    assert "<website_content>" in prompt and "Founded in 2019" in prompt
    assert "dataLayer" not in prompt
    # Deterministic score: 0.6 * 80 + 0.4 * 55 = 70 → hot.
    assert (result.score.value, result.score.tier) == (70, "hot")
    assert result.score.confidence == pytest.approx(0.8)
    assert result.company.name == "Northwind Analytics"
    assert len(result.recommendations) == 2
    assert result.meta.partial is False
    assert result.meta.input_tokens == 400 and result.meta.output_tokens == 80
    assert final_states(events) == dict.fromkeys(STEPS, "done")


async def test_each_node_uses_its_configured_model() -> None:
    fake_llm = FakeLlm()
    lead = LeadInput(company_name="Acme", industry="SaaS")
    final, _ = await run(lead, fake_llm, extract_company="model-big")

    used = {c.node: c.model for c in fake_llm.calls}
    assert used == {
        "extract_company": "model-big",
        "analyze_fit": "model-default",
        "analyze_intent": "model-default",
        "generate_recommendations": "model-default",
    }
    assert final["result"].meta.node_models == used


async def test_insufficient_data_skips_llm_and_fetch() -> None:
    fake_llm, fake_web = FakeLlm(), FakeWeb()
    final, events = await run(LeadInput(company_name="Acme"), fake_llm, fake_web)

    assert final["failure"].code is ErrorCode.INSUFFICIENT_DATA
    assert final["failure"].retryable is False
    assert fake_llm.calls == []
    assert fake_web.requested == []
    assert final_states(events) == {"validate_input": "failed"}


async def test_without_website_fetch_is_skipped() -> None:
    final, events = await run(LeadInput(company_name="Acme", industry="SaaS"), FakeLlm())
    assert final["result"].meta.website.status == "not_provided"
    assert final["result"].meta.website.final_url is None
    assert final_states(events)["fetch_website"] == "skipped"


async def test_blocked_website_continues_from_lead_data() -> None:
    fake_web = FakeWeb()
    fake_llm = FakeLlm()
    lead = LeadInput(company_name="Acme", website="http://localhost:8080/", industry="SaaS")
    final, events = await run(lead, fake_llm, fake_web)

    result = final["result"]
    assert result.meta.website.status == "blocked"
    assert any(
        "address is not allowed" in w and "inferred from lead data" in w
        for w in result.meta.warnings
    )
    assert result.meta.partial is False
    assert fake_web.requested == []
    assert "(not available)" in fake_llm.prompt("extract_company")
    assert final_states(events)["fetch_website"] == "failed"


async def test_website_without_scheme_is_normalized() -> None:
    site = load_fixture(NORTHWIND)
    lead = LeadInput(company_name="Northwind", website="Northwind-Analytics.example")
    final, _ = await run(lead, FakeLlm(), FakeWeb().add(site))
    assert final["result"].meta.website.final_url == "https://northwind-analytics.example/"


async def test_extract_failure_fails_the_run() -> None:
    fake_llm = FakeLlm()
    fake_llm.replies["CompanyExtraction"] = RuntimeError("connection reset")
    final, events = await run(LeadInput(company_name="Acme", industry="SaaS"), fake_llm)

    assert final["failure"].code is ErrorCode.PROVIDER_ERROR
    assert final["failure"].retryable is True
    assert final.get("result") is None
    assert final_states(events)["extract_company"] == "failed"


# ---- Retries and degradation (SPEC.md §8.4, §9.3) ----------------------------------


def _attempt_logs(caplog: pytest.LogCaptureFixture, node: str) -> list[logging.LogRecord]:
    return [
        r
        for r in caplog.records
        if r.getMessage() == "llm attempt failed" and getattr(r, "node", None) == node
    ]


@pytest.mark.parametrize(
    "transient",
    [
        openai.APIConnectionError(request=httpx.Request("POST", "http://llm.invalid/v1/x")),
        openai.APITimeoutError(request=httpx.Request("POST", "http://llm.invalid/v1/x")),
        api_error(openai.RateLimitError, 429),
        api_error(openai.InternalServerError, 503),
        MALFORMED,
    ],
    ids=["connection", "timeout", "rate-limit", "server-error", "malformed-json"],
)
async def test_transient_error_is_retried(transient: object) -> None:
    fake_llm = FakeLlm()
    fake_llm.replies["CompanyExtraction"] = sequence(
        transient, fake_llm.replies["CompanyExtraction"]
    )
    final, events = await run(LEAD, fake_llm)

    assert final.get("failure") is None
    assert fake_llm.attempts("extract_company") == 2
    assert final_states(events)["extract_company"] == "done"


async def test_exhausted_retries_fail_with_provider_error(
    caplog: pytest.LogCaptureFixture,
) -> None:
    fake_llm = FakeLlm()
    fake_llm.replies["CompanyExtraction"] = openai.APIConnectionError(
        request=httpx.Request("POST", "http://llm.invalid/v1/x")
    )
    with caplog.at_level(logging.WARNING):
        final, _ = await run(LEAD, fake_llm)

    assert final["failure"].code is ErrorCode.PROVIDER_ERROR
    assert final["failure"].retryable is True
    assert fake_llm.attempts("extract_company") == 4  # LLM_MAX_ATTEMPTS default
    logs = _attempt_logs(caplog, "extract_company")
    assert [r.attempt for r in logs] == [1, 2, 3, 4]
    assert [r.will_retry for r in logs] == [True, True, True, False]


@pytest.mark.parametrize(
    ("error", "code"),
    [
        (api_error(openai.NotFoundError, 404), ErrorCode.PROVIDER_CONFIG),
        (api_error(openai.AuthenticationError, 401), ErrorCode.PROVIDER_CONFIG),
        (api_error(openai.PermissionDeniedError, 403), ErrorCode.PROVIDER_CONFIG),
        (api_error(openai.BadRequestError, 400), ErrorCode.PROVIDER_ERROR),
    ],
    ids=["model-removed", "bad-key", "forbidden", "bad-request"],
)
async def test_non_transient_error_is_not_retried(
    error: Exception, code: ErrorCode, caplog: pytest.LogCaptureFixture
) -> None:
    fake_llm = FakeLlm()
    fake_llm.replies["CompanyExtraction"] = error
    with caplog.at_level(logging.WARNING):
        final, _ = await run(LEAD, fake_llm)

    assert final["failure"].code is code
    assert final["failure"].retryable is (code is ErrorCode.PROVIDER_ERROR)
    assert fake_llm.attempts("extract_company") == 1
    assert len(_attempt_logs(caplog, "extract_company")) == 1


@pytest.mark.parametrize(
    ("failing", "schema", "present"),
    [
        ("analyze_fit", "FitAnalysis", "intent"),
        ("analyze_intent", "IntentAnalysis", "fit"),
    ],
)
async def test_one_branch_fails_gives_partial_result(
    failing: str, schema: str, present: str
) -> None:
    fake_llm = FakeLlm()
    fake_llm.replies[schema] = api_error(openai.InternalServerError, 500)
    final, events = await run(LEAD, fake_llm)

    assert final.get("failure") is None
    result = final["result"]
    assert result.meta.partial is True
    assert getattr(result, present) is not None
    missing = "fit" if present == "intent" else "intent"
    assert getattr(result, missing) is None
    # The single remaining sub-score is used, with reduced confidence (§9.4).
    assert result.score.value == getattr(result, present).score
    assert result.score.confidence == pytest.approx(0.8 * 0.6)
    assert any("Retrying may help" in w for w in result.meta.warnings)
    assert result.meta.node_models[failing] == "model-default"
    assert final_states(events)[failing] == "failed"
    assert final_states(events)["aggregate"] == "done"


async def test_branch_with_removed_model_points_to_admin() -> None:
    fake_llm = FakeLlm()
    fake_llm.replies["IntentAnalysis"] = api_error(openai.NotFoundError, 404)
    final, _ = await run(LEAD, fake_llm)

    assert fake_llm.attempts("analyze_intent") == 1
    assert any("Check /admin" in w for w in final["result"].meta.warnings)


async def test_both_branches_fail_fails_the_run() -> None:
    fake_llm = FakeLlm()
    fake_llm.replies["FitAnalysis"] = api_error(openai.InternalServerError, 500)
    fake_llm.replies["IntentAnalysis"] = MALFORMED
    final, events = await run(LEAD, fake_llm)

    assert final["failure"].code is ErrorCode.PROVIDER_ERROR
    assert final["failure"].retryable is True
    assert final.get("result") is None
    assert "generate_recommendations" not in final_states(events)


async def test_recommendations_failure_is_non_fatal() -> None:
    fake_llm = FakeLlm()
    fake_llm.replies["RecommendationsOutput"] = MALFORMED
    final, _ = await run(LEAD, fake_llm)

    result = final["result"]
    assert result.recommendations == []
    assert result.meta.partial is True
    assert result.summary == result.company.description
    assert fake_llm.attempts("generate_recommendations") == 4


# ---- Website edge cases through the graph -------------------------------------------


@pytest.mark.parametrize(
    "name",
    [p.name for p in sorted(FIXTURES.iterdir()) if p.name.startswith("edge-")],
)
async def test_website_edge_case_never_fails_the_job(
    name: str, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("WEB_FETCH_TIMEOUT_SECONDS", "1")  # edge-slow responds after 5 s
    site = load_fixture(FIXTURES / name)
    lead = LeadInput(company_name="Example Co", website=site.url, industry="Software")
    fake_llm = FakeLlm()
    final, _ = await run(lead, fake_llm, FakeWeb().add(site))

    assert final.get("failure") is None
    result = final["result"]
    assert result.meta.website.status == site.expected.status
    if site.expected.status != "fetched":
        assert any("inferred from lead data" in w for w in result.meta.warnings)


async def test_injected_text_cannot_close_the_data_block() -> None:
    site = load_fixture(FIXTURES / "edge-prompt-injection")
    lead = LeadInput(company_name="Tidewater CRM", website=site.url)
    fake_llm = FakeLlm()
    await run(lead, fake_llm, FakeWeb().add(site))

    prompt = fake_llm.prompt("extract_company")
    assert "IGNORE ALL PREVIOUS INSTRUCTIONS" in prompt  # present, but as data
    assert prompt.count("</website_content>") == 1
    assert prompt.index("IGNORE ALL") < prompt.index("</website_content>")
