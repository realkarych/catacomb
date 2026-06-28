from __future__ import annotations

from catacomb_deepeval.model import SessionData, ToolCallData


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
