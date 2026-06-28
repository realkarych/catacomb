from __future__ import annotations

from typing import Any, Dict, List, Optional

from catacomb_deepeval.model import SessionData, ToolCallData


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
