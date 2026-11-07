import pytest
from pydantic import ValidationError

from lead_ai.config import DEFAULT_ICP, Settings


def test_icp_description_from_env(monkeypatch):
    monkeypatch.setenv("ICP_DESCRIPTION", "Fintechs in the EU with 20-200 employees.")
    s = Settings(llm_base_url="http://llm.example/v1")
    assert s.icp_description == "Fintechs in the EU with 20-200 employees."


def test_blank_icp_description_uses_default(monkeypatch):
    monkeypatch.setenv("ICP_DESCRIPTION", "  ")
    s = Settings(llm_base_url="http://llm.example/v1")
    assert s.icp_description == DEFAULT_ICP


def test_llm_base_url_is_required(monkeypatch):
    monkeypatch.delenv("LLM_BASE_URL", raising=False)
    with pytest.raises(ValidationError, match="llm_base_url"):
        Settings()


@pytest.mark.parametrize(
    ("var", "value"),
    [
        ("LLM_BASE_URL", "not a url"),
        ("LLM_BASE_URL", "ftp://llm.example/v1"),
        ("LLM_MAX_ATTEMPTS", "0"),
        ("LLM_TIMEOUT_SECONDS", "0"),
        ("LLM_STRUCTURED_OUTPUT_METHOD", "xml"),
        ("WEB_FETCH_TIMEOUT_SECONDS", "0"),
        ("WEB_FETCH_TIMEOUT_SECONDS", "61"),
        ("WEB_FETCH_MAX_CHARS", "100"),
        ("WEB_FETCH_MAX_CHARS", "50001"),
    ],
)
def test_out_of_range_values_are_rejected(monkeypatch, var, value):
    monkeypatch.setenv("LLM_BASE_URL", "http://llm.example/v1")
    monkeypatch.setenv(var, value)
    with pytest.raises(ValidationError):
        Settings()


def test_trailing_slash_is_removed():
    assert Settings(llm_base_url="http://llm.example/v1/").llm_base == "http://llm.example/v1"
