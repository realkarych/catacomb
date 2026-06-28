from __future__ import annotations

import pytest

pytest.importorskip("deepeval")

from catacomb_deepeval.adapter import make_offline_metric, session_to_test_case
from catacomb_deepeval.expected import load_expected_names
from catacomb_deepeval.reader import load_jsonl, parse_session
from tests.conftest import testdata_path as _testdata_path


def test_tool_correctness_perfect_score():
    lines = load_jsonl(str(_testdata_path("session.jsonl")))
    session = parse_session(lines, "run-001")
    expected = load_expected_names(str(_testdata_path("expected_names.json")))
    tc = session_to_test_case(session, expected_names=expected)

    metric = make_offline_metric()
    metric.measure(tc)
    assert metric.score == pytest.approx(1.0)


def test_tool_correctness_wrong_expected():
    lines = load_jsonl(str(_testdata_path("session.jsonl")))
    session = parse_session(lines, "run-001")
    tc = session_to_test_case(session, expected_names=["NonExistentTool"])

    metric = make_offline_metric()
    metric.measure(tc)
    assert metric.score < 1.0


def test_tool_correctness_no_payload_graceful():
    lines = load_jsonl(str(_testdata_path("session_no_payload.jsonl")))
    session = parse_session(lines, "run-002")
    tc = session_to_test_case(session, expected_names=["Bash"])

    metric = make_offline_metric()
    metric.measure(tc)
    assert metric.score is not None
