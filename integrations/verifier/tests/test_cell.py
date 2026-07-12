from __future__ import annotations

import dataclasses

import pytest

from catacomb_verifier import Cell

ALL_VARS = (
    "CATACOMB_EVIDENCE_DIR",
    "CATACOMB_WORKDIR",
    "CATACOMB_RUN_ID",
    "CATACOMB_BASKET",
    "CATACOMB_TASK",
    "CATACOMB_VARIANT",
    "CATACOMB_REP",
    "CATACOMB_AGENT_EXIT_CODE",
)


def _set_full_env(monkeypatch):
    monkeypatch.setenv("CATACOMB_EVIDENCE_DIR", "/ev")
    monkeypatch.setenv("CATACOMB_WORKDIR", "/wd")
    monkeypatch.setenv("CATACOMB_RUN_ID", "run-1")
    monkeypatch.setenv("CATACOMB_BASKET", "sql")
    monkeypatch.setenv("CATACOMB_TASK", "t1")
    monkeypatch.setenv("CATACOMB_VARIANT", "baseline")
    monkeypatch.setenv("CATACOMB_REP", "2")
    monkeypatch.setenv("CATACOMB_AGENT_EXIT_CODE", "0")


def test_from_env_round_trip(monkeypatch):
    _set_full_env(monkeypatch)
    assert Cell.from_env() == Cell(
        evidence_dir="/ev",
        workdir="/wd",
        run_id="run-1",
        basket="sql",
        task="t1",
        variant="baseline",
        rep=2,
        agent_exit_code=0,
    )


def test_from_env_optional_defaults(monkeypatch):
    for var in ALL_VARS:
        monkeypatch.delenv(var, raising=False)
    monkeypatch.setenv("CATACOMB_EVIDENCE_DIR", "/ev")
    monkeypatch.setenv("CATACOMB_RUN_ID", "run-1")
    cell = Cell.from_env()
    assert cell.evidence_dir == "/ev"
    assert cell.run_id == "run-1"
    assert cell.workdir == ""
    assert cell.basket == ""
    assert cell.task == ""
    assert cell.variant == ""
    assert cell.rep == 0
    assert cell.agent_exit_code == 0


def test_from_env_parses_ints(monkeypatch):
    _set_full_env(monkeypatch)
    monkeypatch.setenv("CATACOMB_REP", "5")
    monkeypatch.setenv("CATACOMB_AGENT_EXIT_CODE", "137")
    cell = Cell.from_env()
    assert cell.rep == 5
    assert cell.agent_exit_code == 137


@pytest.mark.parametrize("missing", ["CATACOMB_EVIDENCE_DIR", "CATACOMB_RUN_ID"])
def test_from_env_missing_required_exits_2(monkeypatch, capsys, missing):
    _set_full_env(monkeypatch)
    monkeypatch.delenv(missing, raising=False)
    with pytest.raises(SystemExit) as exc:
        Cell.from_env()
    assert exc.value.code == 2
    assert missing in capsys.readouterr().err


def test_cell_is_frozen():
    cell = Cell(
        evidence_dir="/ev",
        workdir="",
        run_id="r",
        basket="",
        task="",
        variant="",
        rep=0,
        agent_exit_code=0,
    )
    with pytest.raises(dataclasses.FrozenInstanceError):
        cell.run_id = "other"  # type: ignore[misc]


def _make_cell(evidence_dir, workdir):
    return Cell(
        evidence_dir=str(evidence_dir),
        workdir=str(workdir),
        run_id="r",
        basket="",
        task="",
        variant="",
        rep=0,
        agent_exit_code=0,
    )


def test_artifact_prefers_evidence_over_workdir(tmp_path):
    evidence = tmp_path / "ev"
    workdir = tmp_path / "wd"
    art = evidence / "artifacts" / "out" / "result.csv"
    art.parent.mkdir(parents=True)
    art.write_text("evidence")
    wd_copy = workdir / "out" / "result.csv"
    wd_copy.parent.mkdir(parents=True)
    wd_copy.write_text("workdir")
    cell = _make_cell(evidence, workdir)
    assert cell.artifact("out/result.csv") == str(art)


def test_artifact_falls_back_to_workdir(tmp_path):
    evidence = tmp_path / "ev"
    evidence.mkdir()
    workdir = tmp_path / "wd"
    wd_copy = workdir / "result.csv"
    wd_copy.parent.mkdir(parents=True)
    wd_copy.write_text("workdir")
    cell = _make_cell(evidence, workdir)
    assert cell.artifact("result.csv") == str(wd_copy)


def test_artifact_missing_everywhere_raises(tmp_path):
    cell = _make_cell(tmp_path / "ev", tmp_path / "wd")
    with pytest.raises(FileNotFoundError):
        cell.artifact("nope.csv")


def test_artifact_offline_ignores_relative_workdir(tmp_path, monkeypatch):
    evidence = tmp_path / "ev"
    evidence.mkdir()
    monkeypatch.chdir(tmp_path)
    (tmp_path / "result.csv").write_text("cwd file")
    cell = _make_cell(evidence, "")
    with pytest.raises(FileNotFoundError):
        cell.artifact("result.csv")
