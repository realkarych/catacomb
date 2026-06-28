from __future__ import annotations

import json
import pathlib
import tempfile

import pytest

from catacomb_deepeval.expected import ExpectedLoadError, load_expected_names

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
