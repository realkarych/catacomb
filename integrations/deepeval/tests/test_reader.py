from __future__ import annotations

import pathlib

from catacomb_deepeval.model import SessionData, ToolCallData
from catacomb_deepeval.reader import load_jsonl, list_run_ids, parse_session

TESTDATA = pathlib.Path(__file__).parent / "testdata"


def _testdata_path(name: str) -> pathlib.Path:
    return TESTDATA / name


def test_tool_call_data_fields():
    tc = ToolCallData(name="Bash", input_parameters={"command": "ls"}, output="files")
    assert tc.name == "Bash"
    assert tc.input_parameters == {"command": "ls"}
    assert tc.output == "files"


def test_tool_call_data_none_fields():
    tc = ToolCallData(name="X", input_parameters=None, output=None)
    assert tc.input_parameters is None
    assert tc.output is None


def test_session_data_fields():
    tc = ToolCallData(name="Bash", input_parameters=None, output=None)
    sd = SessionData(run_id="r1", input="q", actual_output="a", tools_called=[tc])
    assert sd.run_id == "r1"
    assert sd.input == "q"
    assert sd.actual_output == "a"
    assert len(sd.tools_called) == 1


def test_load_jsonl_returns_all_lines():
    lines = load_jsonl(str(_testdata_path("session.jsonl")))
    assert len(lines) >= 7
    assert all(isinstance(line, dict) for line in lines)


def test_list_run_ids_session():
    lines = load_jsonl(str(_testdata_path("session.jsonl")))
    ids = list_run_ids(lines)
    assert ids == ["run-001"]


def test_parse_session_input():
    lines = load_jsonl(str(_testdata_path("session.jsonl")))
    sd = parse_session(lines, "run-001")
    assert sd.input == "List files in the current directory"


def test_parse_session_actual_output_is_last_assistant():
    lines = load_jsonl(str(_testdata_path("session.jsonl")))
    sd = parse_session(lines, "run-001")
    assert sd.actual_output == "Here are the files. Now let me read one."


def test_parse_session_tools_names():
    lines = load_jsonl(str(_testdata_path("session.jsonl")))
    sd = parse_session(lines, "run-001")
    names = [t.name for t in sd.tools_called]
    assert names == ["Bash", "mcp__fs__read"]


def test_parse_session_tool_input_parameters():
    lines = load_jsonl(str(_testdata_path("session.jsonl")))
    sd = parse_session(lines, "run-001")
    bash = sd.tools_called[0]
    assert bash.input_parameters == {"command": "ls -la"}


def test_parse_session_tool_output():
    lines = load_jsonl(str(_testdata_path("session.jsonl")))
    sd = parse_session(lines, "run-001")
    bash = sd.tools_called[0]
    assert "file.txt" in bash.output


def test_parse_session_no_payload_graceful():
    lines = load_jsonl(str(_testdata_path("session_no_payload.jsonl")))
    sd = parse_session(lines, "run-002")
    assert sd.input == ""
    assert sd.actual_output == ""
    assert sd.tools_called[0].input_parameters is None
    assert sd.tools_called[0].output is None


def test_parse_session_subagent_inner_tools_included():
    lines = load_jsonl(str(_testdata_path("session_subagent.jsonl")))
    sd = parse_session(lines, "run-003")
    names = [t.name for t in sd.tools_called]
    assert "Read" in names


def test_parse_session_run_id():
    lines = load_jsonl(str(_testdata_path("session.jsonl")))
    sd = parse_session(lines, "run-001")
    assert sd.run_id == "run-001"
