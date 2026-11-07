"""Workflow state (SPEC.md §9.2).

The parallel branches (``analyze_fit``, ``analyze_intent``) write different
keys, but both may append to ``warnings`` and ``usage`` and record their model
in ``node_models``. Those keys need reducers; without them LangGraph raises
``InvalidUpdateError`` when two branches update one key in the same superstep.
"""

from __future__ import annotations

import operator
from dataclasses import dataclass
from typing import Annotated, TypedDict

from lead_ai.errors import Failure
from lead_ai.graph.schemas import (
    AnalysisResult,
    CompanyInsights,
    FitAnalysis,
    IntentAnalysis,
    LeadInput,
    LeadScore,
    Recommendation,
)
from lead_ai.web import WebsiteContent


@dataclass(frozen=True)
class TokenUsage:
    node: str
    input_tokens: int
    output_tokens: int


def merge_dicts(a: dict[str, str], b: dict[str, str]) -> dict[str, str]:
    return {**a, **b}


class AnalysisState(TypedDict, total=False):
    # input
    job_id: str
    lead: LeadInput
    started_at: float  # time.monotonic() when the run started

    # intermediate
    website: WebsiteContent
    company: CompanyInsights | None
    fit: FitAnalysis | None
    intent: IntentAnalysis | None
    score: LeadScore | None
    summary: str
    recommendations: list[Recommendation]

    # accumulated by parallel branches: reducers required
    warnings: Annotated[list[str], operator.add]
    usage: Annotated[list[TokenUsage], operator.add]
    node_models: Annotated[dict[str, str], merge_dicts]

    # control
    failure: Failure | None  # set → route to fail
    result: AnalysisResult | None  # set by aggregate
