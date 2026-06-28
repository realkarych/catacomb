from __future__ import annotations

from catacomb_deepeval.model import SessionData, StepData, ToolCallData


def test_step_data_llm():
    s = StepData(kind="llm", name="claude-3", input="what files?", output="let me check")
    assert s.kind == "llm"
    assert s.name == "claude-3"
    assert s.input == "what files?"
    assert s.output == "let me check"


def test_step_data_tool():
    s = StepData(kind="tool", name="Bash", input="ls -la", output="file.txt")
    assert s.kind == "tool"
    assert s.name == "Bash"


def test_step_data_none_io():
    s = StepData(kind="tool", name="X", input=None, output=None)
    assert s.input is None
    assert s.output is None


def test_session_data_steps_defaults_empty():
    sd = SessionData(run_id="r1", input="q", actual_output="a")
    assert sd.steps == []


def test_session_data_steps_set():
    steps = [
        StepData(kind="llm", name="assistant", input="q", output="a"),
        StepData(kind="tool", name="Bash", input="ls", output="files"),
    ]
    sd = SessionData(run_id="r1", input="q", actual_output="a", steps=steps)
    assert len(sd.steps) == 2
    assert sd.steps[0].kind == "llm"
    assert sd.steps[1].kind == "tool"


def test_session_data_backward_compat():
    tc = ToolCallData(name="Bash", input_parameters=None, output=None)
    sd = SessionData(run_id="r1", input="q", actual_output="a", tools_called=[tc])
    assert len(sd.tools_called) == 1
    assert sd.steps == []
