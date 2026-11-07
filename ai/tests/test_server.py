from __future__ import annotations

import asyncio
from collections.abc import AsyncIterator, Callable
from typing import Any

import grpc
import pytest

from lead_ai import llm
from lead_ai.gen.leadintel.analysis.v1 import analysis_pb2 as pb
from lead_ai.gen.leadintel.analysis.v1 import analysis_pb2_grpc as pb_grpc
from lead_ai.server import AnalysisServicer
from tests.support import FakeLlm, FakeWeb

LLM_STEPS = [
    pb.STEP_EXTRACT_COMPANY,
    pb.STEP_ANALYZE_FIT,
    pb.STEP_ANALYZE_INTENT,
    pb.STEP_GENERATE_RECOMMENDATIONS,
]


def request(job_id: str = "job-1", steps: list[int] | None = None, **lead: str) -> Any:
    steps = LLM_STEPS if steps is None else steps
    lead = lead or {"company_name": "Acme", "industry": "SaaS", "notes": "Wants a demo"}
    return pb.AnalyzeLeadRequest(
        job_id=job_id,
        lead=pb.LeadInput(**lead),
        models=[pb.NodeModelConfig(step=s, model="model-a", temperature=0.2) for s in steps],
    )


@pytest.fixture
async def fake_llm() -> FakeLlm:
    return FakeLlm()


@pytest.fixture
async def servicer(fake_llm: FakeLlm) -> AnalysisServicer:
    web = FakeWeb()
    return AnalysisServicer(
        extra_configurable={
            "llm_factory": fake_llm,
            "http_client": web.client(),
            "resolver": web.resolve,
        }
    )


@pytest.fixture
async def stub(servicer: AnalysisServicer) -> AsyncIterator[Any]:
    server = grpc.aio.server()
    pb_grpc.add_AnalysisServiceServicer_to_server(servicer, server)
    port = server.add_insecure_port("127.0.0.1:0")
    await server.start()
    async with grpc.aio.insecure_channel(f"127.0.0.1:{port}") as channel:
        yield pb_grpc.AnalysisServiceStub(channel)
    await server.stop(None)


async def collect(stub: Any, req: Any) -> list[Any]:
    return [e async for e in stub.AnalyzeLead(req, timeout=10)]


async def test_stream_order_and_single_terminal_event(stub: Any) -> None:
    events = await collect(stub, request())

    assert [e.sequence for e in events] == list(range(1, len(events) + 1))
    assert all(e.job_id == "job-1" for e in events)
    kinds = [e.WhichOneof("payload") for e in events]
    assert kinds[-1] == "completed"
    assert kinds.count("completed") + kinds.count("failed") == 1

    progress = [e.progress for e in events[:-1]]
    assert progress[0].step == pb.STEP_VALIDATE_INPUT
    assert progress[0].state == pb.STEP_STATE_RUNNING
    percents = [p.percent for p in progress]
    assert percents == sorted(percents) and percents[-1] == 100

    result = events[-1].completed.result
    assert result.score.breakdown.HasField("fit") and result.score.breakdown.HasField("intent")
    assert result.meta.website.status == pb.WEBSITE_FETCH_STATUS_NOT_PROVIDED
    assert dict(result.meta.node_models) == {
        "extract_company": "model-a",
        "analyze_fit": "model-a",
        "analyze_intent": "model-a",
        "generate_recommendations": "model-a",
    }


async def test_insufficient_data_is_a_failed_event(stub: Any) -> None:
    events = await collect(stub, request(company_name="Acme"))
    last = events[-1]
    assert last.WhichOneof("payload") == "failed"
    assert last.failed.code == pb.ERROR_CODE_INSUFFICIENT_DATA
    assert last.failed.retryable is False


@pytest.mark.parametrize(
    "steps",
    [
        LLM_STEPS[:3],  # missing one
        [*LLM_STEPS, pb.STEP_ANALYZE_FIT],  # duplicate
        [*LLM_STEPS, pb.STEP_SCORE_LEAD],  # not an LLM step
    ],
    ids=["missing", "duplicate", "non-llm"],
)
async def test_invalid_models_give_invalid_input(stub: Any, steps: list[int]) -> None:
    events = await collect(stub, request(steps=steps))
    assert len(events) == 1
    assert events[0].failed.code == pb.ERROR_CODE_INVALID_INPUT
    assert events[0].failed.retryable is False


async def test_duplicate_job_is_rejected(stub: Any, fake_llm: FakeLlm) -> None:
    fake_llm.gate = asyncio.Event()
    first = stub.AnalyzeLead(request("job-dup"), timeout=10)
    # Wait until the first run is inside the graph (blocked on the gate).
    async for e in first:
        if e.progress.step == pb.STEP_EXTRACT_COMPANY:
            break

    with pytest.raises(grpc.aio.AioRpcError) as err:
        await collect(stub, request("job-dup"))
    assert err.value.code() == grpc.StatusCode.ALREADY_EXISTS

    fake_llm.gate.set()
    rest = [e async for e in first]
    assert rest[-1].WhichOneof("payload") == "completed"
    # The job ID is released afterwards.
    again = await collect(stub, request("job-dup"))
    assert again[-1].WhichOneof("payload") == "completed"


async def eventually(condition: Callable[[], bool], what: str) -> None:
    """Poll ``condition`` for up to 2s; the servicer exposes no event to await."""
    for _ in range(200):
        if condition():
            return
        await asyncio.sleep(0.01)
    raise AssertionError(f"timed out waiting for {what}")


async def wait_until_idle(servicer: AnalysisServicer) -> None:
    """Wait for the handler to finish: its ``finally`` releases the job ID."""
    await eventually(lambda: not servicer._in_flight, "the in-flight set to empty")


async def start_blocked(stub: Any, fake_llm: FakeLlm, job_id: str, rpc_timeout: float = 10) -> Any:
    """Start a run and return once it is blocked inside its first LLM call."""
    fake_llm.gate = asyncio.Event()
    call = stub.AnalyzeLead(request(job_id), timeout=rpc_timeout)
    async for e in call:
        if e.progress.step == pb.STEP_EXTRACT_COMPANY:
            break
    await eventually(lambda: bool(fake_llm.calls), "the first LLM call")
    return call


async def test_client_cancel_aborts_the_run(
    stub: Any, servicer: AnalysisServicer, fake_llm: FakeLlm
) -> None:
    call = await start_blocked(stub, fake_llm, "job-cancel")
    calls_before = len(fake_llm.calls)

    call.cancel()
    await wait_until_idle(servicer)

    # Opening the gate would let a still-running graph continue: it must not.
    fake_llm.gate.set()
    await asyncio.sleep(0.1)
    assert len(fake_llm.calls) == calls_before
    assert await call.code() == grpc.StatusCode.CANCELLED


async def test_deadline_aborts_the_run(
    stub: Any, servicer: AnalysisServicer, fake_llm: FakeLlm
) -> None:
    call = await start_blocked(stub, fake_llm, "job-deadline", rpc_timeout=0.5)
    calls_before = len(fake_llm.calls)

    with pytest.raises(grpc.aio.AioRpcError) as err:
        async for _ in call:
            pass
    assert err.value.code() == grpc.StatusCode.DEADLINE_EXCEEDED
    await wait_until_idle(servicer)

    fake_llm.gate.set()
    await asyncio.sleep(0.1)
    assert len(fake_llm.calls) == calls_before


async def test_list_models(stub: Any, monkeypatch: pytest.MonkeyPatch) -> None:
    async def fake_list(settings: Any = None) -> list[str]:
        return ["a-model", "b-model"]

    monkeypatch.setattr(llm, "list_models", fake_list)
    resp = await stub.ListModels(pb.ListModelsRequest(), timeout=5)
    assert [m.id for m in resp.models] == ["a-model", "b-model"]


async def test_list_models_unavailable(stub: Any, monkeypatch: pytest.MonkeyPatch) -> None:
    async def failing(settings: Any = None) -> list[str]:
        raise llm.LlmUnavailableError("connection refused")

    monkeypatch.setattr(llm, "list_models", failing)
    with pytest.raises(grpc.aio.AioRpcError) as err:
        await stub.ListModels(pb.ListModelsRequest(), timeout=5)
    assert err.value.code() == grpc.StatusCode.UNAVAILABLE
