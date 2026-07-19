from __future__ import annotations

import pathlib

from catacomb_deepeval.reader import load_jsonl, parse_session

TESTDATA = pathlib.Path(__file__).parent / "testdata"


def _testdata_path(name: str) -> pathlib.Path:
    return TESTDATA / name


def test_steps_ordered_llm_tool_llm_tool():
    lines = load_jsonl(str(_testdata_path("session.jsonl")))
    sd = parse_session(lines, "run-001")
    kinds = [s.kind for s in sd.steps]
    assert kinds == ["llm", "tool", "llm", "tool"]


def test_steps_tool_names():
    lines = load_jsonl(str(_testdata_path("session.jsonl")))
    sd = parse_session(lines, "run-001")
    tool_steps = [s for s in sd.steps if s.kind == "tool"]
    names = [s.name for s in tool_steps]
    assert names == ["Bash", "mcp__fs__read"]


def test_steps_llm_input_output():
    lines = load_jsonl(str(_testdata_path("session.jsonl")))
    sd = parse_session(lines, "run-001")
    llm_steps = [s for s in sd.steps if s.kind == "llm"]
    assert llm_steps[0].output == "I will list the files for you."
    assert llm_steps[1].output == "Here are the files. Now let me read one."


def test_steps_tool_input_output():
    lines = load_jsonl(str(_testdata_path("session.jsonl")))
    sd = parse_session(lines, "run-001")
    tool_steps = [s for s in sd.steps if s.kind == "tool"]
    assert "ls -la" in (tool_steps[0].input or "")
    assert "file.txt" in (tool_steps[0].output or "")


def test_steps_no_payload_graceful():
    lines = load_jsonl(str(_testdata_path("session_no_payload.jsonl")))
    sd = parse_session(lines, "run-002")
    assert len(sd.steps) >= 1
    for s in sd.steps:
        assert s.kind in ("llm", "tool")


def test_steps_backward_compat_tools_called_unchanged():
    lines = load_jsonl(str(_testdata_path("session.jsonl")))
    sd = parse_session(lines, "run-001")
    names = [t.name for t in sd.tools_called]
    assert names == ["Bash", "mcp__fs__read"]


def test_steps_put_assistant_turn_before_its_tool_call_at_equal_timestamps():
    lines = [
        {"kind": "node", "run_id": "r1", "type": "tool_call", "name": "Bash",
         "id": "aaa", "t_start": "2024-01-01T10:00:02Z",
         "payload": {"input": {"command": "ls"}, "output": "ok"}},
        {"kind": "node", "run_id": "r1", "type": "assistant_turn", "name": "assistant_turn",
         "id": "zzz", "t_start": "2024-01-01T10:00:02Z",
         "payload": {"output": "running ls"}},
    ]
    sd = parse_session(lines, "r1")
    assert [s.kind for s in sd.steps] == ["llm", "tool"]
