from __future__ import annotations

import os

# Settings are read from the environment on first use; tests never talk to a
# real LLM server (fakes are injected via configurable["llm_factory"]).
os.environ.setdefault("LLM_BASE_URL", "http://llm.invalid/v1")
os.environ["WEB_FETCH_ENABLED"] = "true"

import pytest

from lead_ai import llm
from lead_ai.config import get_settings


@pytest.fixture(autouse=True)
def _fresh_settings():
    get_settings.cache_clear()
    yield
    get_settings.cache_clear()


@pytest.fixture(autouse=True)
def _no_retry_wait(monkeypatch):
    # Keep retry behavior, drop the backoff sleeps.
    monkeypatch.setattr(llm, "RETRY_WAIT", {"multiplier": 0, "exp_base": 3, "max": 0, "jitter": 0})
