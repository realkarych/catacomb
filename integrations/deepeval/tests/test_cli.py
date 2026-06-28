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


def test_cli_argument_correctness_no_api_key_errors():
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


def test_cli_trace_metrics_no_api_key_exits_2():
    env = {
        k: v for k, v in os.environ.items()
        if k not in ("ANTHROPIC_API_KEY", "OPENAI_API_KEY")
    }
    env["DEEPEVAL_TELEMETRY_OPT_OUT"] = "YES"
    result = subprocess.run(
        [PYTHON, "-m", "catacomb_deepeval",
         str(_testdata_path("session.jsonl")), "--run", "run-001",
         "--trace-metrics"],
        capture_output=True,
        text=True,
        env=env,
    )
    assert result.returncode == 2
    stderr_lower = result.stderr.lower()
    assert "anthropic_api_key" in stderr_lower or "api" in stderr_lower


def test_cli_trace_metrics_judge_failure_clean_error(monkeypatch, capsys):
    pytest.importorskip("deepeval")
    import catacomb_deepeval.trace_replay as tr_mod
    from catacomb_deepeval.cli import main

    def _raise(*args, **kwargs):
        raise RuntimeError("network error from judge")

    monkeypatch.setattr(tr_mod, "make_anthropic_judge", lambda: object())
    monkeypatch.setattr(tr_mod, "run_task_completion", _raise)
    monkeypatch.setenv("ANTHROPIC_API_KEY", "sk-ant-fake")
    monkeypatch.setattr(sys, "argv", [
        "catacomb-deepeval",
        str(_testdata_path("session.jsonl")),
        "--run", "run-001",
        "--trace-metrics",
    ])

    with pytest.raises(SystemExit) as exc_info:
        main()

    assert exc_info.value.code != 0
    captured = capsys.readouterr()
    assert "error:" in captured.err
    assert "Traceback" not in captured.err


def test_cli_default_path_unchanged_no_key():
    env = {
        k: v for k, v in os.environ.items()
        if k not in ("ANTHROPIC_API_KEY", "OPENAI_API_KEY")
    }
    env["DEEPEVAL_TELEMETRY_OPT_OUT"] = "YES"
    result = subprocess.run(
        [PYTHON, "-m", "catacomb_deepeval",
         str(_testdata_path("session.jsonl")), "--run", "run-001"],
        capture_output=True,
        text=True,
        env=env,
    )
    assert result.returncode == 0
    data = json.loads(result.stdout)
    assert "input" in data
    assert "tools_called" in data
