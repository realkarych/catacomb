from __future__ import annotations

import json
import os
import subprocess
import sys

import pytest

from tests.conftest import testdata_path as _testdata_path

PYTHON = sys.executable


def run_cli(*args: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        [PYTHON, "-m", "catacomb_deepeval", *args],
        capture_output=True,
        text=True,
    )


def test_cli_no_expected_prints_json():
    result = run_cli(str(_testdata_path("session.jsonl")), "--run", "run-001")
    assert result.returncode == 0
    data = json.loads(result.stdout)
    assert "input" in data
    assert "tools_called" in data


def test_cli_no_expected_no_run_single_run_ok():
    result = run_cli(str(_testdata_path("session.jsonl")))
    assert result.returncode == 0
    data = json.loads(result.stdout)
    assert data["input"] == "List files in the current directory"


def test_cli_multi_run_without_run_flag_errors():
    import tempfile
    with tempfile.NamedTemporaryFile(mode="w", suffix=".jsonl", delete=False) as f:
        json.dump({"kind": "node", "id": "n1", "run_id": "r1", "type": "user_prompt", "t_start": "2024-01-01T10:00:00Z"}, f)
        f.write("\n")
        json.dump({"kind": "node", "id": "n2", "run_id": "r2", "type": "user_prompt", "t_start": "2024-01-01T10:00:00Z"}, f)
        f.write("\n")
        fname = f.name
    result = run_cli(fname)
    assert result.returncode == 2
    assert "multiple" in result.stderr.lower() or "run" in result.stderr.lower()


def test_cli_argument_correctness_no_api_key_errors(monkeypatch):
    env = {k: v for k, v in os.environ.items() if k not in ("ANTHROPIC_API_KEY",)}
    result = subprocess.run(
        [PYTHON, "-m", "catacomb_deepeval",
         str(_testdata_path("session.jsonl")), "--run", "run-001",
         "--expected", str(_testdata_path("expected_names.json")),
         "--argument-correctness"],
        capture_output=True,
        text=True,
        env=env,
    )
    assert result.returncode == 2
    assert "api" in result.stderr.lower() or "key" in result.stderr.lower()


def test_cli_with_expected_deepeval_gated():
    pytest.importorskip("deepeval")
    env = {**os.environ, "DEEPEVAL_TELEMETRY_OPT_OUT": "YES"}
    result = subprocess.run(
        [PYTHON, "-m", "catacomb_deepeval",
         str(_testdata_path("session.jsonl")), "--run", "run-001",
         "--expected", str(_testdata_path("expected_names.json"))],
        capture_output=True,
        text=True,
        env=env,
    )
    assert result.returncode == 0
    assert "PASS" in result.stdout or "score" in result.stdout.lower()


def test_cli_with_wrong_expected_deepeval_gated():
    pytest.importorskip("deepeval")
    import tempfile
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
        json.dump(["NonExistentTool"], f)
        fname = f.name
    env = {**os.environ, "DEEPEVAL_TELEMETRY_OPT_OUT": "YES"}
    result = subprocess.run(
        [PYTHON, "-m", "catacomb_deepeval",
         str(_testdata_path("session.jsonl")), "--run", "run-001",
         "--expected", fname],
        capture_output=True,
        text=True,
        env=env,
    )
    assert result.returncode == 1
    assert "FAIL" in result.stdout
