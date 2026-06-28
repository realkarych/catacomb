from __future__ import annotations

import pytest

deepeval = pytest.importorskip("deepeval")

from deepeval.test_case import LLMTestCase, ToolCall
from catacomb_deepeval.adapter import session_to_test_case
from catacomb_deepeval.model import SessionData, ToolCallData


def _make_session() -> SessionData:
    return SessionData(
        run_id="run-001",
        input="List files",
        actual_output="Here are the files.",
        tools_called=[
            ToolCallData(name="Bash", input_parameters={"command": "ls"}, output="file.txt"),
        ],
    )


def test_session_to_test_case_type():
    tc = session_to_test_case(_make_session())
    assert isinstance(tc, LLMTestCase)


def test_session_to_test_case_input():
    tc = session_to_test_case(_make_session())
    assert tc.input == "List files"


def test_session_to_test_case_actual_output():
    tc = session_to_test_case(_make_session())
    assert tc.actual_output == "Here are the files."


def test_session_to_test_case_tools_called():
    tc = session_to_test_case(_make_session())
    assert len(tc.tools_called) == 1
    tool = tc.tools_called[0]
    assert isinstance(tool, ToolCall)
    assert tool.name == "Bash"
    assert tool.input_parameters == {"command": "ls"}


def test_session_to_test_case_expected_tools():
    tc = session_to_test_case(_make_session(), expected_names=["Bash"])
    assert len(tc.expected_tools) == 1
    assert tc.expected_tools[0].name == "Bash"


def test_session_to_test_case_no_expected():
    tc = session_to_test_case(_make_session())
    assert tc.expected_tools is None or tc.expected_tools == []
