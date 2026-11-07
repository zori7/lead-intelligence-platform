"""Pydantic models: workflow input, LLM structured outputs and the final result.

LLM output schemas are deliberately compact (short descriptions, at most five
list items) so small local models stay reliable (SPEC.md §9.6).
"""

from __future__ import annotations

from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field, StringConstraints

ShortText = Annotated[str, StringConstraints(strip_whitespace=True, max_length=300)]
Item = Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=200)]


class LeadInput(BaseModel):
    """Lead fields sent by Go. ``contact_email`` is intentionally absent."""

    model_config = ConfigDict(frozen=True)

    company_name: str
    website: str = ""
    contact_name: str = ""
    contact_title: str = ""
    industry: str = ""
    company_size: str = ""
    notes: str = ""


# ---- LLM structured outputs ------------------------------------------------


class CompanyExtraction(BaseModel):
    """Output of ``extract_company``."""

    industry: ShortText = Field(description="Industry, a few words")
    size_estimate: ShortText = Field(description="Employee range, e.g. '51-200', or 'unknown'")
    business_model: ShortText = Field(description="How the company makes money, one sentence")
    description: Annotated[str, StringConstraints(strip_whitespace=True, max_length=1000)] = Field(
        description="What the company does, 1-3 sentences"
    )
    key_needs: list[Item] = Field(max_length=5, description="Likely business needs, max 5")
    confidence: float = Field(ge=0, le=1, description="0-1: how well-supported this profile is")


class FitAnalysis(BaseModel):
    score: int = Field(ge=0, le=100, description="ICP fit 0-100")
    rationale: Annotated[str, StringConstraints(strip_whitespace=True, max_length=1000)] = Field(
        description="Why, 1-3 sentences"
    )
    matched_criteria: list[Item] = Field(max_length=5, description="ICP criteria met, max 5")
    gaps: list[Item] = Field(max_length=5, description="ICP criteria missed or unknown, max 5")


class IntentAnalysis(BaseModel):
    score: int = Field(ge=0, le=100, description="Buying intent 0-100")
    rationale: Annotated[str, StringConstraints(strip_whitespace=True, max_length=1000)] = Field(
        description="Why, 1-3 sentences"
    )
    signals: list[Item] = Field(max_length=5, description="Observed intent signals, max 5")


Priority = Literal["high", "medium", "low"]


class Recommendation(BaseModel):
    action: Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=300)]
    rationale: Annotated[str, StringConstraints(strip_whitespace=True, max_length=600)]
    priority: Priority


class RecommendationsOutput(BaseModel):
    """Output of ``generate_recommendations``."""

    summary: Annotated[str, StringConstraints(strip_whitespace=True, max_length=800)] = Field(
        description="2-3 sentence assessment of the lead for a salesperson"
    )
    recommendations: list[Recommendation] = Field(
        min_length=1, max_length=5, description="2-5 prioritized next actions"
    )


# ---- Final result (mirrors proto AnalysisResult) ----------------------------


class CompanyInsights(BaseModel):
    name: str
    industry: str
    size_estimate: str
    business_model: str
    description: str
    key_needs: list[str]
    confidence: float


Tier = Literal["hot", "warm", "cold"]


class LeadScore(BaseModel):
    value: int = Field(ge=0, le=100)
    tier: Tier
    confidence: float = Field(ge=0, le=1)
    fit: int | None
    intent: int | None


class WebsiteMeta(BaseModel):
    status: Literal["fetched", "not_provided", "disabled", "blocked", "failed"]
    final_url: str | None


class ResultMeta(BaseModel):
    node_models: dict[str, str]
    website: WebsiteMeta
    partial: bool
    warnings: list[str]
    duration_ms: int
    input_tokens: int
    output_tokens: int


class AnalysisResult(BaseModel):
    summary: str
    company: CompanyInsights
    fit: FitAnalysis | None
    intent: IntentAnalysis | None
    score: LeadScore
    recommendations: list[Recommendation]
    meta: ResultMeta
