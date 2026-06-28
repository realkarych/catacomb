from __future__ import annotations

import pytest

deepeval = pytest.importorskip("deepeval")

from deepeval.metrics import ToolCorrectnessMetric

from catacomb_deepeval.adapter import session_to_test_case
from catacomb_deepeval.expected import load_expected_names
from catacomb_deepeval.reader import load_jsonl, parse_session
from tests.conftest import testdata_path


def test_tool_correctness_perfect_score():
    """Fixture has exactly Bash + mcp__fs__read; expected matches → score=1.0."""
    lines = load_jsonl(str(testdata_path("session.jsonl")))
    session = parse_session(lines, "run-001")
    expected = load_expected_names(str(testdata_path("expected_names.json")))
    tc = session_to_test_case(session, expected_names=expected)

    metric = ToolCorrectnessMetric()
    metric.measure(tc)
    assert metric.score == pytest.approx(1.0)


def test_tool_correctness_wrong_expected():
    """Expected tool that was never called → score < 1.0."""
    lines = load_jsonl(str(testdata_path("session.jsonl")))
    session = parse_session(lines, "run-001")
    tc = session_to_test_case(session, expected_names=["NonExistentTool"])

    metric = ToolCorrectnessMetric()
    metric.measure(tc)
    assert metric.score < 1.0


def test_tool_correctness_no_payload_graceful():
    """Session without payload → empty input/output → metric runs without error."""
    lines = load_jsonl(str(testdata_path("session_no_payload.jsonl")))
    session = parse_session(lines, "run-002")
    tc = session_to_test_case(session, expected_names=["Bash"])

    metric = ToolCorrectnessMetric()
    metric.measure(tc)
    assert metric.score is not None
