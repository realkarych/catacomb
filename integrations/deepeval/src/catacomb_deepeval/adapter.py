from __future__ import annotations

from typing import Any, Dict, List, Optional

from catacomb_deepeval.model import SessionData


def session_to_dicts(session: SessionData) -> dict:
    tools: List[Dict[str, Any]] = [
        {
            "name": t.name,
            "input_parameters": t.input_parameters,
            "output": t.output,
        }
        for t in session.tools_called
    ]
    return {
        "input": session.input,
        "actual_output": session.actual_output,
        "tools_called": tools,
        "expected_tools": [],
    }


def session_to_test_case(
    session: SessionData,
    expected_names: Optional[List[str]] = None,
) -> Any:
    from deepeval.test_case import LLMTestCase, ToolCall

    tools_called = [
        ToolCall(
            name=t.name,
            input_parameters=t.input_parameters,
            output=t.output,
        )
        for t in session.tools_called
    ]

    expected_tools: Optional[List[ToolCall]] = None
    if expected_names is not None:
        expected_tools = [ToolCall(name=name) for name in expected_names]

    return LLMTestCase(
        input=session.input,
        actual_output=session.actual_output,
        tools_called=tools_called,
        expected_tools=expected_tools,
    )


def make_offline_metric(**kwargs: Any) -> Any:
    from deepeval.metrics import ToolCorrectnessMetric
    from deepeval.models import DeepEvalBaseLLM

    class _OfflineStub(DeepEvalBaseLLM):
        def load_model(self) -> "_OfflineStub":
            return self

        def generate(self, prompt: str, schema: Any = None) -> str:
            raise RuntimeError("LLM unavailable in offline mode")

        async def a_generate(self, prompt: str, schema: Any = None) -> str:
            raise RuntimeError("LLM unavailable in offline mode")

        def get_model_name(self) -> str:
            return "offline-stub"

    return ToolCorrectnessMetric(model=_OfflineStub(), **kwargs)
