from lead_ai.config import DEFAULT_ICP, Settings


def test_icp_description_from_env(monkeypatch):
    monkeypatch.setenv("ICP_DESCRIPTION", "Fintechs in the EU with 20-200 employees.")
    s = Settings(llm_base_url="http://llm.example/v1")
    assert s.icp_description == "Fintechs in the EU with 20-200 employees."


def test_blank_icp_description_uses_default(monkeypatch):
    monkeypatch.setenv("ICP_DESCRIPTION", "  ")
    s = Settings(llm_base_url="http://llm.example/v1")
    assert s.icp_description == DEFAULT_ICP
