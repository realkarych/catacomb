from __future__ import annotations

import json
import pathlib
import tempfile

import pytest

from catacomb_deepeval.expected import ExpectedLoadError, expected_carries_field, load_expected_names

_TESTDATA = pathlib.Path(__file__).parent / "testdata"


def test_load_name_array():
    names = load_expected_names(str(_TESTDATA / "expected_names.json"))
    assert names == ["Bash", "mcp__fs__read"]


def test_load_object_array():
    names = load_expected_names(str(_TESTDATA / "expected_objects.json"))
    assert names == ["Bash", "mcp__fs__read"]


def test_load_envelope_form():
    data = {"tools": ["Bash", "mcp__fs__read"]}
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
        json.dump(data, f)
        fname = f.name
    names = load_expected_names(fname)
    assert names == ["Bash", "mcp__fs__read"]


def test_load_envelope_object_array():
    data = {"tools": [{"name": "Bash"}, {"name": "mcp__fs__read"}]}
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
        json.dump(data, f)
        fname = f.name
    names = load_expected_names(fname)
    assert names == ["Bash", "mcp__fs__read"]


def test_load_unrecognized_raises():
    data = {"unexpected": 42}
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
        json.dump(data, f)
        fname = f.name
    with pytest.raises(ExpectedLoadError):
        load_expected_names(fname)


def test_load_string_in_object_array_raises():
    data = [{"no_name_key": "X"}]
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
        json.dump(data, f)
        fname = f.name
    with pytest.raises(ExpectedLoadError):
        load_expected_names(fname)


def _write_json(data) -> str:
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
        json.dump(data, f)
        return f.name


def test_carries_field_name_array_is_names_only():
    assert expected_carries_field(str(_TESTDATA / "expected_names.json"), "input_parameters") is False


def test_carries_field_object_array_without_field_is_names_only():
    assert expected_carries_field(str(_TESTDATA / "expected_objects.json"), "output") is False


def test_carries_field_all_entries_carry_field():
    fname = _write_json([{"name": "Bash", "input_parameters": {"command": "ls"}}])
    assert expected_carries_field(fname, "input_parameters") is True


def test_carries_field_mixed_entries_is_names_only():
    fname = _write_json([{"name": "Bash", "input_parameters": {"command": "ls"}}, {"name": "Read"}])
    assert expected_carries_field(fname, "input_parameters") is False


def test_carries_field_envelope_form():
    fname = _write_json({"tools": [{"name": "Bash", "output": "total 8"}]})
    assert expected_carries_field(fname, "output") is True


def test_carries_field_empty_list_is_names_only():
    fname = _write_json([])
    assert expected_carries_field(fname, "input_parameters") is False
