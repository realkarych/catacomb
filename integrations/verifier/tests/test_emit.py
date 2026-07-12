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


def test_emit_bool_value_rejected(capsys):
    with pytest.raises(ValueError, match="finite number"):
        emit(key="verifier.row_diff", value=True)
    assert capsys.readouterr().out == ""


def test_emit_nan_value_rejected(capsys):
    with pytest.raises(ValueError, match="finite number"):
        emit(key="judge.groundedness", value=float("nan"))
    assert capsys.readouterr().out == ""


def test_emit_positive_inf_value_rejected(capsys):
    with pytest.raises(ValueError, match="finite number"):
        emit(key="judge.groundedness", value=float("inf"))
    assert capsys.readouterr().out == ""


def test_emit_negative_inf_value_rejected(capsys):
    with pytest.raises(ValueError, match="finite number"):
        emit(key="judge.groundedness", value=float("-inf"))
    assert capsys.readouterr().out == ""


def test_emit_string_value_rejected(capsys):
    with pytest.raises(ValueError, match="finite number"):
        emit(key="judge.groundedness", value="1.0")  # type: ignore[arg-type]
    assert capsys.readouterr().out == ""


def test_emit_key_with_too_many_dots_rejected(capsys):
    with pytest.raises(ValueError, match="owner.key"):
        emit(key="a.b.c", value=1)
    assert capsys.readouterr().out == ""


def test_emit_key_without_dot_rejected(capsys):
    with pytest.raises(ValueError, match="owner.key"):
        emit(key="ab", value=1)
    assert capsys.readouterr().out == ""


def test_emit_key_empty_owner_rejected(capsys):
    with pytest.raises(ValueError, match="owner.key"):
        emit(key=".b", value=1)
    assert capsys.readouterr().out == ""


def test_emit_key_empty_rest_rejected(capsys):
    with pytest.raises(ValueError, match="owner.key"):
        emit(key="a.", value=1)
    assert capsys.readouterr().out == ""


def test_emit_valid_owner_key_accepted(capsys):
    line = _emit_line(capsys, key="owner.key", value=2)
    assert json.loads(line) == {"key": "owner.key", "value": 2}


def test_emit_passed_with_value_rejected(capsys):
    with pytest.raises(ValueError):
        emit(passed=True, value=1)
    assert capsys.readouterr().out == ""


def test_emit_passed_false_with_value_rejected(capsys):
    with pytest.raises(ValueError):
        emit(passed=False, value=0)
    assert capsys.readouterr().out == ""
