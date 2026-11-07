from __future__ import annotations

import time
from pathlib import Path
from typing import Any

import pytest

from lead_ai.errors import ErrorCode
from lead_ai.graph.builder import STEPS, build_graph
from lead_ai.graph.schemas import LeadInput
from tests.support import FakeLlm, FakeWeb, load_fixture, models

NORTHWIND = Path(__file__).parent / "fixtures/websites/northwind-analytics"


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
    assert any("Website was not fetched" in w for w in result.meta.warnings)
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
