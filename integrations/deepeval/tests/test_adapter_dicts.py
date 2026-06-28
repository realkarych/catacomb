from __future__ import annotations

from catacomb_deepeval.adapter import session_to_dicts
from catacomb_deepeval.model import SessionData, ToolCallData


def _make_session() -> SessionData:
    return SessionData(
        run_id="run-001",
        input="List files",
        actual_output="Here are the files.",
        tools_called=[
            ToolCallData(name="Bash", input_parameters={"command": "ls"}, output="file.txt"),
            ToolCallData(name="mcp__fs__read", input_parameters={"path": "x"}, output="content"),
        ],
    )


def test_session_to_dicts_keys():
    d = session_to_dicts(_make_session())
    assert set(d.keys()) == {"input", "actual_output", "tools_called", "expected_tools"}


def test_session_to_dicts_input():
    d = session_to_dicts(_make_session())
    assert d["input"] == "List files"


def test_session_to_dicts_actual_output():
    d = session_to_dicts(_make_session())
    assert d["actual_output"] == "Here are the files."


def test_session_to_dicts_tools_called():
    d = session_to_dicts(_make_session())
    assert len(d["tools_called"]) == 2
    assert d["tools_called"][0] == {
        "name": "Bash",
        "input_parameters": {"command": "ls"},
        "output": "file.txt",
    }


def test_session_to_dicts_expected_tools_default_empty():
    d = session_to_dicts(_make_session())
    assert d["expected_tools"] == []


def test_session_to_dicts_no_tools():
    sd = SessionData(run_id="r", input="q", actual_output="a", tools_called=[])
    d = session_to_dicts(sd)
    assert d["tools_called"] == []
