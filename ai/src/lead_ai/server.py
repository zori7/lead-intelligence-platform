"""``AnalysisService`` gRPC servicer (SPEC.md §8).

Maps the request into a graph run, streams step progress, and ends every
stream with exactly one terminal event (completed or failed). Domain failures
are events; transport problems are gRPC statuses.
"""

from __future__ import annotations

import asyncio
import logging
import time
from collections.abc import AsyncIterator, Mapping
from typing import Any

import grpc
from langchain_core.runnables import RunnableConfig

from lead_ai import llm
from lead_ai.errors import ErrorCode, Failure, internal, invalid_input
from lead_ai.gen.leadintel.analysis.v1 import analysis_pb2 as pb
from lead_ai.gen.leadintel.analysis.v1 import analysis_pb2_grpc as pb_grpc
from lead_ai.graph.builder import STEPS, build_graph
from lead_ai.graph.schemas import AnalysisResult, LeadInput

log = logging.getLogger(__name__)

SERVICE_NAME = "leadintel.analysis.v1.AnalysisService"

STEP_TO_PB: dict[str, int] = {
    "validate_input": pb.STEP_VALIDATE_INPUT,
    "fetch_website": pb.STEP_FETCH_WEBSITE,
    "extract_company": pb.STEP_EXTRACT_COMPANY,
    "analyze_fit": pb.STEP_ANALYZE_FIT,
    "analyze_intent": pb.STEP_ANALYZE_INTENT,
    "score_lead": pb.STEP_SCORE_LEAD,
    "generate_recommendations": pb.STEP_GENERATE_RECOMMENDATIONS,
    "aggregate": pb.STEP_AGGREGATE,
}
PB_TO_STEP = {v: k for k, v in STEP_TO_PB.items()}

STATE_TO_PB: dict[str, int] = {
    "running": pb.STEP_STATE_RUNNING,
    "done": pb.STEP_STATE_DONE,
    "skipped": pb.STEP_STATE_SKIPPED,
    "failed": pb.STEP_STATE_FAILED,
}

ERROR_TO_PB: dict[ErrorCode, int] = {
    ErrorCode.INVALID_INPUT: pb.ERROR_CODE_INVALID_INPUT,
    ErrorCode.INSUFFICIENT_DATA: pb.ERROR_CODE_INSUFFICIENT_DATA,
    ErrorCode.PROVIDER_ERROR: pb.ERROR_CODE_PROVIDER_ERROR,
    ErrorCode.PROVIDER_CONFIG: pb.ERROR_CODE_PROVIDER_CONFIG,
    ErrorCode.INTERNAL: pb.ERROR_CODE_INTERNAL,
}

TIER_TO_PB = {"hot": pb.TIER_HOT, "warm": pb.TIER_WARM, "cold": pb.TIER_COLD}
PRIORITY_TO_PB = {"high": pb.PRIORITY_HIGH, "medium": pb.PRIORITY_MEDIUM, "low": pb.PRIORITY_LOW}
WEBSITE_TO_PB = {
    "fetched": pb.WEBSITE_FETCH_STATUS_FETCHED,
    "not_provided": pb.WEBSITE_FETCH_STATUS_NOT_PROVIDED,
    "disabled": pb.WEBSITE_FETCH_STATUS_DISABLED,
    "blocked": pb.WEBSITE_FETCH_STATUS_BLOCKED,
    "failed": pb.WEBSITE_FETCH_STATUS_FAILED,
}

TERMINAL_STEP_STATES = {"done", "skipped", "failed"}


def parse_models(models: Any) -> dict[str, llm.ModelChoice] | Failure:
    """Validate ``AnalyzeLeadRequest.models``: each LLM node exactly once."""
    out: dict[str, llm.ModelChoice] = {}
    for m in models:
        node = PB_TO_STEP.get(m.step)
        if node not in llm.LLM_NODES:
            return invalid_input("Model configuration names a step that has no model.")
        if node in out:
            return invalid_input(f"Model configuration lists {node} twice.")
        if not m.model or not 0 <= m.temperature <= 2:
            return invalid_input(f"Model configuration for {node} is invalid.")
        out[node] = llm.ModelChoice(m.model, round(m.temperature, 2))
    missing = [n for n in llm.LLM_NODES if n not in out]
    if missing:
        return invalid_input(f"Model configuration is missing: {', '.join(missing)}.")
    return out


def lead_from_pb(lead: Any) -> LeadInput:
    return LeadInput(
        company_name=lead.company_name,
        website=lead.website,
        contact_name=lead.contact_name,
        contact_title=lead.contact_title,
        industry=lead.industry,
        company_size=lead.company_size,
        notes=lead.notes,
    )


def result_to_pb(r: AnalysisResult) -> Any:
    score = pb.LeadScore(
        value=r.score.value,
        tier=TIER_TO_PB[r.score.tier],
        confidence=r.score.confidence,
        breakdown=pb.ScoreBreakdown(fit=r.score.fit, intent=r.score.intent),
    )
    return pb.AnalysisResult(
        summary=r.summary,
        company=pb.CompanyInsights(**r.company.model_dump()),
        fit=pb.FitAnalysis(**r.fit.model_dump()) if r.fit else None,
        intent=pb.IntentAnalysis(**r.intent.model_dump()) if r.intent else None,
        score=score,
        recommendations=[
            pb.Recommendation(
                action=x.action, rationale=x.rationale, priority=PRIORITY_TO_PB[x.priority]
            )
            for x in r.recommendations
        ],
        meta=pb.ResultMeta(
            node_models=r.meta.node_models,
            partial=r.meta.partial,
            warnings=r.meta.warnings,
            duration_ms=r.meta.duration_ms,
            input_tokens=r.meta.input_tokens,
            output_tokens=r.meta.output_tokens,
            website=pb.WebsiteFetch(
                status=WEBSITE_TO_PB[r.meta.website.status],
                final_url=r.meta.website.final_url or "",
            ),
        ),
    )


class AnalysisServicer(pb_grpc.AnalysisServiceServicer):
    def __init__(self, graph: Any = None, extra_configurable: Mapping[str, Any] | None = None):
        self._graph = graph if graph is not None else build_graph()
        # Test hook: llm_factory, http_client, resolver (SPEC.md §9.6, §9.7).
        self._extra = dict(extra_configurable or {})
        self._in_flight: set[str] = set()

    async def AnalyzeLead(
        self, request: Any, context: grpc.aio.ServicerContext
    ) -> AsyncIterator[Any]:
        job_id = request.job_id
        if not job_id:
            await context.abort(grpc.StatusCode.INVALID_ARGUMENT, "job_id is required")
        if job_id in self._in_flight:
            await context.abort(grpc.StatusCode.ALREADY_EXISTS, "job is already running")
        self._in_flight.add(job_id)
        seq = 0

        def event(**payload: Any) -> Any:
            nonlocal seq
            seq += 1
            return pb.AnalysisEvent(job_id=job_id, sequence=seq, **payload)

        def failed(f: Failure) -> Any:
            return event(
                failed=pb.AnalysisFailed(
                    code=ERROR_TO_PB[f.code], message=f.message, retryable=f.retryable
                )
            )

        try:
            models = parse_models(request.models)
            if isinstance(models, Failure):
                log.error("invalid model configuration", extra={"job_id": job_id})
                yield failed(models)
                return

            config: RunnableConfig = {"configurable": {"models": models, **self._extra}}
            state = {
                "job_id": job_id,
                "lead": lead_from_pb(request.lead),
                "started_at": time.monotonic(),
            }
            finished = 0
            final: dict[str, Any] = {}
            remaining = context.time_remaining()
            async with asyncio.timeout(remaining):
                async for mode, chunk in self._graph.astream(
                    state, config, stream_mode=["custom", "values"]
                ):
                    if mode == "values":
                        final = chunk
                        continue
                    step, step_state = chunk["step"], chunk["state"]
                    if step_state in TERMINAL_STEP_STATES:
                        finished += 1
                    yield event(
                        progress=pb.StepProgress(
                            step=STEP_TO_PB[step],
                            state=STATE_TO_PB[step_state],
                            percent=min(100, finished * 100 // len(STEPS)),
                        )
                    )

            if final.get("failure") is not None:
                f: Failure = final["failure"]
                log.info("analysis failed", extra={"job_id": job_id, "code": f.code.value})
                yield failed(f)
            elif final.get("result") is not None:
                result: AnalysisResult = final["result"]
                log.info(
                    "analysis completed",
                    extra={
                        "job_id": job_id,
                        "duration_ms": result.meta.duration_ms,
                        "input_tokens": result.meta.input_tokens,
                        "output_tokens": result.meta.output_tokens,
                    },
                )
                yield event(completed=pb.AnalysisCompleted(result=result_to_pb(result)))
            else:
                log.error("graph ended without result or failure", extra={"job_id": job_id})
                yield failed(internal())
        except asyncio.CancelledError:
            # Go cancelled the stream (user cancel, lead deleted, shutdown).
            # Cancellation unwinds the graph and aborts in-flight LLM and
            # website requests; re-raising lets gRPC finish the RPC. gRPC also
            # cancels the task when the deadline passes, often before
            # asyncio.timeout fires, so the log names the actual reason.
            left = context.time_remaining()
            if left is not None and left <= 0:
                log.info("analysis deadline exceeded", extra={"job_id": job_id})
            else:
                log.info("analysis cancelled by client", extra={"job_id": job_id})
            raise
        except TimeoutError:
            # The propagated deadline passed. Report it as such, so Go records
            # a timeout even when this side notices first.
            log.info("analysis deadline exceeded", extra={"job_id": job_id})
            await context.abort(grpc.StatusCode.DEADLINE_EXCEEDED, "analysis deadline exceeded")
        except Exception:
            log.exception("analysis crashed", extra={"job_id": job_id})
            yield failed(internal())
        finally:
            self._in_flight.discard(job_id)

    async def ListModels(self, request: Any, context: grpc.aio.ServicerContext) -> Any:
        try:
            ids = await llm.list_models()
        except llm.LlmUnavailableError as exc:
            log.warning("LLM server unreachable", extra={"error": str(exc)[:200]})
            await context.abort(grpc.StatusCode.UNAVAILABLE, "LLM server unreachable")
        return pb.ListModelsResponse(models=[pb.ModelInfo(id=i) for i in ids])
