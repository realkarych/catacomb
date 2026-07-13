from __future__ import annotations

import pathlib

import pytest

deepeval = pytest.importorskip("deepeval")

from catacomb_deepeval.model import SessionData
from catacomb_deepeval.reader import load_jsonl, parse_session
from catacomb_deepeval.trace_replay import build_trace_dict, session_to_trace_testcase

TESTDATA = pathlib.Path(__file__).parent / "testdata"


def _load(name: str) -> SessionData:
    lines = load_jsonl(str(TESTDATA / name))
    return parse_session(lines, "run-001")


def test_build_trace_dict_type_agent():
    sd = _load("session.jsonl")
    d = build_trace_dict(sd)
    assert d["type"] == "agent"


def test_build_trace_dict_input_output():
    sd = _load("session.jsonl")
    d = build_trace_dict(sd)
    assert sd.input in d["input"]
    assert sd.actual_output in d["output"]


def test_build_trace_dict_children_ordered():
    sd = _load("session.jsonl")
    d = build_trace_dict(sd)
    children = d["children"]
    assert len(children) == 4
    types = [c["type"] for c in children]
    assert types == ["llm", "tool", "llm", "tool"]


def test_build_trace_dict_tool_names():
    sd = _load("session.jsonl")
    d = build_trace_dict(sd)
    tool_children = [c for c in d["children"] if c["type"] == "tool"]
    names = [c["name"] for c in tool_children]
    assert names == ["Bash", "mcp__fs__read"]


def test_build_trace_dict_tool_io():
    sd = _load("session.jsonl")
    d = build_trace_dict(sd)
    bash = next(c for c in d["children"] if c["name"] == "Bash")
    assert "ls -la" in str(bash["input"])
    assert "file.txt" in str(bash["output"])


def test_build_trace_dict_no_network():
    sd = _load("session.jsonl")
    d = build_trace_dict(sd)
    assert isinstance(d, dict)


def test_session_to_trace_testcase_trace_dict_set():
    sd = _load("session.jsonl")
    tc = session_to_trace_testcase(sd)
    assert isinstance(tc._trace_dict, dict)
    assert tc._trace_dict["type"] == "agent"


def test_session_to_trace_testcase_input_output():
    sd = _load("session.jsonl")
    tc = session_to_trace_testcase(sd)
    assert tc.input == sd.input
    assert tc.actual_output == sd.actual_output


def test_session_to_trace_testcase_rejects_seam_dropping_testcase(monkeypatch):
    import deepeval.test_case as tc_mod

    class DroppingTestCase:
        def __init__(self, input: str, actual_output: str) -> None:
            object.__setattr__(self, "input", input)
            object.__setattr__(self, "actual_output", actual_output)

        def __setattr__(self, name: str, value: object) -> None:
            pass

    monkeypatch.setattr(tc_mod, "LLMTestCase", DroppingTestCase)
    with pytest.raises(RuntimeError, match="unsupported deepeval version"):
        session_to_trace_testcase(_load("session.jsonl"))


def test_session_to_trace_testcase_rejects_seam_raising_testcase(monkeypatch):
    import deepeval.test_case as tc_mod

    class RejectingTestCase:
        __slots__ = ("input", "actual_output")

        def __init__(self, input: str, actual_output: str) -> None:
            self.input = input
            self.actual_output = actual_output

    monkeypatch.setattr(tc_mod, "LLMTestCase", RejectingTestCase)
    with pytest.raises(RuntimeError, match="unsupported deepeval version"):
        session_to_trace_testcase(_load("session.jsonl"))


def test_build_trace_dict_fallback_empty_steps():
    sd = SessionData(
        run_id="r",
        input="do something",
        actual_output="done",
        steps=[],
    )
    d = build_trace_dict(sd)
    assert d["type"] == "agent"
    assert isinstance(d, dict)
