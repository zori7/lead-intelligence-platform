"""Graph wiring (SPEC.md §9.1).

The graph is compiled once. Models, the HTTP client and test fakes arrive per
run through ``RunnableConfig["configurable"]``, so they are not part of the
graph structure.
"""

from __future__ import annotations

from typing import Any

from langgraph.graph import END, START, StateGraph

from lead_ai.graph import nodes
from lead_ai.graph.state import AnalysisState

STEPS: tuple[str, ...] = (
    "validate_input",
    "fetch_website",
    "extract_company",
    "analyze_fit",
    "analyze_intent",
    "score_lead",
    "generate_recommendations",
    "aggregate",
)


def _after_validate(state: AnalysisState) -> str:
    return "fail" if state.get("failure") else "fetch_website"


def _after_extract(state: AnalysisState) -> str | list[str]:
    return "fail" if state.get("failure") else ["analyze_fit", "analyze_intent"]


def _after_score(state: AnalysisState) -> str:
    return "fail" if state.get("failure") else "generate_recommendations"


def build_graph() -> Any:
    g = StateGraph(AnalysisState)
    g.add_node("validate_input", nodes.validate_input)
    g.add_node("fetch_website", nodes.fetch_website)
    g.add_node("extract_company", nodes.extract_company)
    g.add_node("analyze_fit", nodes.analyze_fit)
    g.add_node("analyze_intent", nodes.analyze_intent)
    g.add_node("score_lead", nodes.score_lead)
    g.add_node("generate_recommendations", nodes.generate_recommendations)
    g.add_node("aggregate", nodes.aggregate)
    g.add_node("fail", nodes.fail)

    g.add_edge(START, "validate_input")
    g.add_conditional_edges("validate_input", _after_validate, ["fail", "fetch_website"])
    g.add_edge("fetch_website", "extract_company")
    g.add_conditional_edges(
        "extract_company", _after_extract, ["fail", "analyze_fit", "analyze_intent"]
    )
    # Fan-in: score_lead waits for both parallel branches.
    g.add_edge(["analyze_fit", "analyze_intent"], "score_lead")
    g.add_conditional_edges("score_lead", _after_score, ["fail", "generate_recommendations"])
    g.add_edge("generate_recommendations", "aggregate")
    g.add_edge("aggregate", END)
    g.add_edge("fail", END)
    return g.compile()
