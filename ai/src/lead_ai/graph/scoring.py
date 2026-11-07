"""Deterministic lead scoring (SPEC.md §9.4).

The LLM judges the sub-dimensions; code combines them, so the final score is
explainable, testable and stable across runs.
"""

from __future__ import annotations

from dataclasses import dataclass

from lead_ai.graph.schemas import LeadScore, Tier

FIT_WEIGHT = 0.6
INTENT_WEIGHT = 0.4
SINGLE_BRANCH_CONFIDENCE_FACTOR = 0.6
HOT_THRESHOLD = 70
WARM_THRESHOLD = 40


@dataclass(frozen=True)
class ScoreOutcome:
    score: LeadScore | None  # None: both branches missing
    warnings: list[str]


def tier_for(value: int) -> Tier:
    if value >= HOT_THRESHOLD:
        return "hot"
    if value >= WARM_THRESHOLD:
        return "warm"
    return "cold"


def compute_score(fit: int | None, intent: int | None, company_confidence: float) -> ScoreOutcome:
    """Combine sub-scores into the final score.

    Both present: weighted average. One present: that score, with reduced
    confidence. Neither: no score (the workflow fails).
    """
    confidence = max(0.0, min(1.0, company_confidence))
    if fit is not None and intent is not None:
        value = round(FIT_WEIGHT * fit + INTENT_WEIGHT * intent)
        warnings: list[str] = []
    elif fit is not None:
        value, warnings = fit, ["intent unavailable"]
        confidence *= SINGLE_BRANCH_CONFIDENCE_FACTOR
    elif intent is not None:
        value, warnings = intent, ["fit unavailable"]
        confidence *= SINGLE_BRANCH_CONFIDENCE_FACTOR
    else:
        return ScoreOutcome(None, [])
    value = max(0, min(100, value))
    return ScoreOutcome(
        LeadScore(
            value=value,
            tier=tier_for(value),
            confidence=round(confidence, 3),
            fit=fit,
            intent=intent,
        ),
        warnings,
    )
