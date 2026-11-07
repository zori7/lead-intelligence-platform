from __future__ import annotations

import pytest

from lead_ai.graph.scoring import compute_score, tier_for


@pytest.mark.parametrize(
    ("value", "tier"),
    [(0, "cold"), (39, "cold"), (40, "warm"), (69, "warm"), (70, "hot"), (100, "hot")],
)
def test_tier_boundaries(value: int, tier: str) -> None:
    assert tier_for(value) == tier


@pytest.mark.parametrize(
    ("fit", "intent", "conf", "value", "tier", "confidence", "warnings"),
    [
        (80, 50, 0.9, 68, "warm", 0.9, []),  # 0.6*80 + 0.4*50 = 68
        (100, 100, 1.0, 100, "hot", 1.0, []),
        (0, 0, 0.5, 0, "cold", 0.5, []),
        (70, 70, 0.8, 70, "hot", 0.8, []),
        (75, None, 0.5, 75, "hot", 0.3, ["intent unavailable"]),
        (None, 39, 1.0, 39, "cold", 0.6, ["fit unavailable"]),
        (50, 50, 1.7, 50, "warm", 1.0, []),  # confidence clamped
    ],
)
def test_compute_score(fit, intent, conf, value, tier, confidence, warnings) -> None:
    out = compute_score(fit, intent, conf)
    assert out.score is not None
    assert out.score.value == value
    assert out.score.tier == tier
    assert out.score.confidence == pytest.approx(confidence)
    assert (out.score.fit, out.score.intent) == (fit, intent)
    assert out.warnings == warnings


def test_rounding_uses_weighted_average() -> None:
    out = compute_score(41, 40, 1.0)  # 24.6 + 16.0 = 40.6 → 41
    assert out.score is not None and out.score.value == 41


def test_no_branches_gives_no_score() -> None:
    out = compute_score(None, None, 0.9)
    assert out.score is None
