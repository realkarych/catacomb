from __future__ import annotations

import json

import pytest

from catacomb_verifier import emit


def _emit_line(capsys, *args, **kwargs):
    emit(*args, **kwargs)
    lines = [ln for ln in capsys.readouterr().out.splitlines() if ln.strip()]
    assert len(lines) == 1
    return lines[0]


def test_emit_passed_true_exact_schema(capsys):
    line = _emit_line(capsys, passed=True)
    assert line == '{"key":"verifier.pass","value":1}'
    assert json.loads(line) == {"key": "verifier.pass", "value": 1}


def test_emit_passed_false_is_zero(capsys):
    line = _emit_line(capsys, passed=False)
    assert json.loads(line) == {"key": "verifier.pass", "value": 0}


def test_emit_key_value_integer_preserved(capsys):
    line = _emit_line(capsys, key="verifier.row_diff", value=3)
    assert json.loads(line) == {"key": "verifier.row_diff", "value": 3}
    assert '"value":3' in line


def test_emit_float_value_preserved(capsys):
    line = _emit_line(capsys, key="judge.groundedness", value=0.5)
    assert json.loads(line) == {"key": "judge.groundedness", "value": 0.5}


def test_emit_run_id_passthrough(capsys):
    line = _emit_line(capsys, passed=True, run_id="run-7")
    assert json.loads(line) == {"key": "verifier.pass", "value": 1, "run_id": "run-7"}


def test_emit_provenance_passthrough(capsys):
    line = _emit_line(
        capsys,
        key="judge.groundedness",
        value=0.8,
        tool="verify_sql",
        tool_version="1.2.3",
        prompt_hash="abc123",
    )
    assert json.loads(line) == {
        "key": "judge.groundedness",
        "value": 0.8,
        "tool": "verify_sql",
        "tool_version": "1.2.3",
        "prompt_hash": "abc123",
    }


def test_emit_field_order(capsys):
    line = _emit_line(capsys, key="k.k", value=1, run_id="r", tool="t")
    assert line == '{"key":"k.k","value":1,"run_id":"r","tool":"t"}'


def test_emit_requires_passed_or_key(capsys):
    with pytest.raises(ValueError):
        emit()
    assert capsys.readouterr().out == ""


def test_emit_rejects_both_passed_and_key(capsys):
    with pytest.raises(ValueError):
        emit(passed=True, key="verifier.row_diff", value=1)
    assert capsys.readouterr().out == ""


def test_emit_key_without_value_raises(capsys):
    with pytest.raises(ValueError):
        emit(key="verifier.row_diff")
    assert capsys.readouterr().out == ""
