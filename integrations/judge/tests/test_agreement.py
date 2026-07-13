from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path

import pytest

from catacomb_judge import spearman
from catacomb_judge.cli import main

SRC_DIR = Path(__file__).resolve().parent.parent / "src"

GOLDEN_TABLE = """\
KEY      JUDGE    N  SPEARMAN  KAPPA  TPR    TNR
judge.g  alpha    4  1.000     1.000  1.000  1.000
judge.g  beta     4  -         0.000  1.000  0.000
judge.g  overall  8  0.577     0.500  1.000  0.500
"""


def run_cli(args, capsys):
    with pytest.raises(SystemExit) as exc:
        main(args)
    out, err = capsys.readouterr()
    return exc.value.code, out, err


def write_jsonl(path, lines):
    rendered = [json.dumps(line) if isinstance(line, dict) else line for line in lines]
    path.write_text("".join(line + "\n" for line in rendered))
    return str(path)


def score_line(key, value, run_id, tool, **extra):
    return {"key": key, "value": value, "run_id": run_id, "tool": tool, **extra}


def label_line(run_id, key, value, **extra):
    return {"run_id": run_id, "key": key, "label": value, **extra}


@pytest.fixture
def two_judge_fixture(tmp_path):
    labels = write_jsonl(
        tmp_path / "labels.jsonl",
        [label_line(f"r{i}", "judge.g", v) for i, v in enumerate([1, 0, 1, 0], start=1)],
    )
    alpha = write_jsonl(
        tmp_path / "alpha.jsonl",
        [
            score_line("judge.g", v, f"r{i}", "alpha")
            for i, v in enumerate([1, 0, 1, 0], start=1)
        ],
    )
    beta = write_jsonl(
        tmp_path / "beta.jsonl",
        [score_line("judge.g", 1, f"r{i}", "beta") for i in range(1, 5)],
    )
    return labels, alpha, beta


def test_agreement_golden_table(two_judge_fixture, capsys):
    labels, alpha, beta = two_judge_fixture
    code, out, err = run_cli(["agreement", "--labels", labels, alpha, beta], capsys)
    assert code == 0
    assert out == GOLDEN_TABLE


def test_agreement_json_exact_document(two_judge_fixture, tmp_path, capsys):
    labels, alpha, beta = two_judge_fixture
    extra = write_jsonl(
        tmp_path / "extra.jsonl",
        [score_line("judge.g", 1, "r0", "alpha")],
    )
    with open(labels, "a", encoding="utf-8") as handle:
        handle.write(json.dumps(label_line("r5", "judge.g", 1)) + "\n")
    code, out, err = run_cli(
        ["agreement", "--labels", labels, alpha, beta, extra, "--json"], capsys
    )
    assert code == 0
    pooled_spearman = spearman(
        [1.0, 0.0, 1.0, 0.0, 1.0, 0.0, 1.0, 0.0],
        [1.0, 0.0, 1.0, 0.0, 1.0, 1.0, 1.0, 1.0],
    )
    parsed = json.loads(out)
    assert parsed == {
        "keys": [
            {
                "key": "judge.g",
                "unmatched_labels": 1,
                "unmatched_scores": 1,
                "judges": [
                    {
                        "tool": "alpha",
                        "n": 4,
                        "spearman": 1.0,
                        "kappa": 1.0,
                        "tpr": 1.0,
                        "tnr": 1.0,
                    },
                    {"tool": "beta", "n": 4, "kappa": 0.0, "tpr": 1.0, "tnr": 0.0},
                ],
                "overall": {
                    "n": 8,
                    "spearman": pooled_spearman,
                    "kappa": 0.5,
                    "tpr": 1.0,
                    "tnr": 0.5,
                },
            }
        ]
    }
    beta_row = parsed["keys"][0]["judges"][1]
    assert "spearman" not in beta_row
    assert set(beta_row) == {"tool", "n", "kappa", "tpr", "tnr"}


def test_agreement_min_kappa_pass(two_judge_fixture, capsys):
    labels, alpha, _ = two_judge_fixture
    code, out, err = run_cli(
        ["agreement", "--labels", labels, alpha, "--min-kappa", "0.8"], capsys
    )
    assert code == 0
    assert err == ""


def test_agreement_min_kappa_fail_below_threshold(two_judge_fixture, capsys):
    labels, alpha, beta = two_judge_fixture
    code, out, err = run_cli(
        ["agreement", "--labels", labels, alpha, beta, "--min-kappa", "0.8"], capsys
    )
    assert code == 1
    assert out == GOLDEN_TABLE
    assert "judge.g/beta: kappa 0.000 < 0.8" in err


def test_agreement_min_kappa_fail_when_kappa_omitted(tmp_path, capsys):
    labels = write_jsonl(
        tmp_path / "labels.jsonl",
        [label_line("r1", "judge.g", 1), label_line("r2", "judge.g", 1)],
    )
    scores = write_jsonl(
        tmp_path / "scores.jsonl",
        [score_line("judge.g", 1, r, "alpha") for r in ("r1", "r2")],
    )
    code, out, err = run_cli(
        ["agreement", "--labels", labels, scores, "--min-kappa", "0.8"], capsys
    )
    assert code == 1
    assert "judge.g/alpha: kappa omitted" in err


def test_agreement_without_min_kappa_never_exits_one(tmp_path, capsys):
    labels = write_jsonl(
        tmp_path / "labels.jsonl",
        [label_line("r1", "judge.g", 1), label_line("r2", "judge.g", 1)],
    )
    scores = write_jsonl(
        tmp_path / "scores.jsonl",
        [score_line("judge.g", 1, r, "alpha") for r in ("r1", "r2")],
    )
    code, out, err = run_cli(["agreement", "--labels", labels, scores], capsys)
    assert code == 0


def test_agreement_min_kappa_ignores_overall_and_label_only_keys(
    two_judge_fixture, tmp_path, capsys
):
    labels, alpha, _ = two_judge_fixture
    with open(labels, "a", encoding="utf-8") as handle:
        handle.write(json.dumps(label_line("r1", "judge.h", 1)) + "\n")
    code, out, err = run_cli(
        ["agreement", "--labels", labels, alpha, "--min-kappa", "0.8"], capsys
    )
    assert code == 0
    assert "judge.h  overall  0  -" in out


def test_agreement_key_filter(two_judge_fixture, tmp_path, capsys):
    labels, alpha, _ = two_judge_fixture
    with open(labels, "a", encoding="utf-8") as handle:
        handle.write(json.dumps(label_line("r1", "judge.h", 1)) + "\n")
    other = write_jsonl(
        tmp_path / "other.jsonl", [score_line("judge.h", 1, "r1", "alpha")]
    )
    code, out, err = run_cli(
        ["agreement", "--labels", labels, alpha, other, "--key", "judge.g", "--json"],
        capsys,
    )
    assert code == 0
    parsed = json.loads(out)
    assert [entry["key"] for entry in parsed["keys"]] == ["judge.g"]


def test_agreement_key_filter_unknown_key_empty_report(two_judge_fixture, capsys):
    labels, alpha, _ = two_judge_fixture
    code, out, err = run_cli(
        ["agreement", "--labels", labels, alpha, "--key", "judge.zzz", "--json"], capsys
    )
    assert code == 0
    assert json.loads(out) == {"keys": []}


def test_agreement_threshold_rebinarizes_both_sides(tmp_path, capsys):
    labels = write_jsonl(
        tmp_path / "labels.jsonl",
        [label_line("r1", "judge.g", 1), label_line("r2", "judge.g", 0)],
    )
    scores = write_jsonl(
        tmp_path / "scores.jsonl",
        [
            score_line("judge.g", 0.7, "r1", "alpha"),
            score_line("judge.g", 0.2, "r2", "alpha"),
        ],
    )
    code, out, _ = run_cli(["agreement", "--labels", labels, scores, "--json"], capsys)
    assert code == 0
    judge = json.loads(out)["keys"][0]["judges"][0]
    assert judge["kappa"] == 1.0
    assert judge["spearman"] == 1.0
    code, out, _ = run_cli(
        ["agreement", "--labels", labels, scores, "--threshold", "0.9", "--json"],
        capsys,
    )
    assert code == 0
    judge = json.loads(out)["keys"][0]["judges"][0]
    assert judge["kappa"] == 0.0
    assert judge["spearman"] == 1.0


def test_agreement_non_binary_label_binarizes_at_threshold(tmp_path, capsys):
    labels = write_jsonl(
        tmp_path / "labels.jsonl",
        [label_line("r1", "judge.g", 0.6), label_line("r2", "judge.g", 1)],
    )
    scores = write_jsonl(
        tmp_path / "scores.jsonl",
        [
            score_line("judge.g", 0.95, "r1", "alpha"),
            score_line("judge.g", 0.95, "r2", "alpha"),
        ],
    )
    code, out, _ = run_cli(["agreement", "--labels", labels, scores, "--json"], capsys)
    assert code == 0
    judge = json.loads(out)["keys"][0]["judges"][0]
    assert "tnr" not in judge
    assert judge["tpr"] == 1.0
    code, out, _ = run_cli(
        ["agreement", "--labels", labels, scores, "--threshold", "0.9", "--json"],
        capsys,
    )
    assert code == 0
    judge = json.loads(out)["keys"][0]["judges"][0]
    assert judge["tnr"] == 0.0
    assert judge["tpr"] == 1.0


def test_agreement_label_only_key_omits_every_metric(two_judge_fixture, capsys):
    labels, alpha, _ = two_judge_fixture
    with open(labels, "a", encoding="utf-8") as handle:
        handle.write(json.dumps(label_line("r1", "judge.h", 1)) + "\n")
    code, out, err = run_cli(["agreement", "--labels", labels, alpha, "--json"], capsys)
    assert code == 0
    assert json.loads(out)["keys"][1] == {
        "key": "judge.h",
        "unmatched_labels": 1,
        "unmatched_scores": 0,
        "judges": [],
        "overall": {"n": 0},
    }
    code, table, err = run_cli(["agreement", "--labels", labels, alpha], capsys)
    assert code == 0
    row = next(line for line in table.splitlines() if line.startswith("judge.h"))
    assert row.split() == ["judge.h", "overall", "0", "-", "-", "-", "-"]


def test_agreement_runs_dir_scanned_sorted(tmp_path, capsys):
    runs = tmp_path / "runs"
    for name, run_id in (("b-run", "r2"), ("a-run", "r1")):
        (runs / name).mkdir(parents=True)
        write_jsonl(
            runs / name / "scores.jsonl",
            [
                score_line("judge.g", 1, run_id, "alpha"),
                {"key": "judge.g", "value": 0},
            ],
        )
    labels = write_jsonl(
        tmp_path / "labels.jsonl",
        [label_line("r1", "judge.g", 1), label_line("r2", "judge.g", 1)],
    )
    code, out, err = run_cli(
        ["agreement", "--labels", labels, "--runs-dir", str(runs)], capsys
    )
    assert code == 0
    assert "  2  " in out
    assert err.splitlines() == [
        f"note: {runs / 'a-run' / 'scores.jsonl'}: skipped 1 score line(s) without run_id",
        f"note: {runs / 'b-run' / 'scores.jsonl'}: skipped 1 score line(s) without run_id",
    ]


def test_agreement_combines_positional_and_runs_dir(two_judge_fixture, tmp_path, capsys):
    labels, alpha, _ = two_judge_fixture
    runs = tmp_path / "runs"
    (runs / "x-run").mkdir(parents=True)
    write_jsonl(
        runs / "x-run" / "scores.jsonl",
        [score_line("judge.g", v, f"r{i}", "beta") for i, v in enumerate([1, 0, 1, 0], start=1)],
    )
    code, out, err = run_cli(
        ["agreement", "--labels", labels, alpha, "--runs-dir", str(runs), "--json"],
        capsys,
    )
    assert code == 0
    assert json.loads(out)["keys"][0]["overall"]["n"] == 8


def test_agreement_runs_dir_missing_exit_2(two_judge_fixture, tmp_path, capsys):
    labels, _, _ = two_judge_fixture
    code, out, err = run_cli(
        ["agreement", "--labels", labels, "--runs-dir", str(tmp_path / "nope")], capsys
    )
    assert code == 2
    assert "runs dir not found" in err


def test_agreement_requires_a_scores_source(two_judge_fixture, capsys):
    labels, _, _ = two_judge_fixture
    code, out, err = run_cli(["agreement", "--labels", labels], capsys)
    assert code == 2
    assert "usage: catacomb-judge agreement" in err
    assert "at least one scores file or --runs-dir is required" in err


def test_agreement_malformed_scores_line_exit_2(two_judge_fixture, tmp_path, capsys):
    labels, _, _ = two_judge_fixture
    bad = tmp_path / "bad.jsonl"
    bad.write_text('{"key": "judge.g", "value": 1, "run_id": "r1"}\n{oops\n')
    code, out, err = run_cli(["agreement", "--labels", labels, str(bad)], capsys)
    assert code == 2
    assert f"{bad}:2: invalid JSON" in err


def test_agreement_malformed_labels_line_exit_2(two_judge_fixture, tmp_path, capsys):
    _, alpha, _ = two_judge_fixture
    bad = tmp_path / "bad-labels.jsonl"
    bad.write_text("{oops\n")
    code, out, err = run_cli(["agreement", "--labels", str(bad), alpha], capsys)
    assert code == 2
    assert f"{bad}:1: invalid JSON" in err


def test_agreement_duplicate_label_exit_2(two_judge_fixture, tmp_path, capsys):
    _, alpha, _ = two_judge_fixture
    dup = write_jsonl(
        tmp_path / "dup.jsonl",
        [label_line("r1", "judge.g", 1), label_line("r1", "judge.g", 0)],
    )
    code, out, err = run_cli(["agreement", "--labels", dup, alpha], capsys)
    assert code == 2
    assert f'{dup}:2: duplicate label for run_id="r1" key="judge.g"' in err


def test_agreement_missing_labels_file_exit_2(two_judge_fixture, tmp_path, capsys):
    _, alpha, _ = two_judge_fixture
    missing = str(tmp_path / "absent.jsonl")
    code, out, err = run_cli(["agreement", "--labels", missing, alpha], capsys)
    assert code == 2
    assert missing in err


def test_agreement_deterministic_under_shuffled_inputs(two_judge_fixture, capsys):
    labels, alpha, beta = two_judge_fixture
    code_ab, out_ab, _ = run_cli(
        ["agreement", "--labels", labels, alpha, beta, "--json"], capsys
    )
    code_ba, out_ba, _ = run_cli(
        ["agreement", "--labels", labels, beta, alpha, "--json"], capsys
    )
    assert (code_ab, out_ab) == (code_ba, out_ba)
    _, table_ab, _ = run_cli(["agreement", "--labels", labels, alpha, beta], capsys)
    _, table_ba, _ = run_cli(["agreement", "--labels", labels, beta, alpha], capsys)
    assert table_ab == table_ba


def test_agreement_unmatched_disclosure_lines(two_judge_fixture, tmp_path, capsys):
    labels, alpha, beta = two_judge_fixture
    with open(labels, "a", encoding="utf-8") as handle:
        handle.write(json.dumps(label_line("r5", "judge.g", 1)) + "\n")
    extra = write_jsonl(
        tmp_path / "extra.jsonl", [score_line("judge.g", 1, "r0", "alpha")]
    )
    code, out, err = run_cli(
        ["agreement", "--labels", labels, alpha, beta, extra], capsys
    )
    assert code == 0
    assert out == GOLDEN_TABLE + "\njudge.g: 1 unmatched labels, 1 unmatched scores\n"


def test_no_command_is_usage_error(capsys):
    code, out, err = run_cli([], capsys)
    assert code == 2
    assert "usage: catacomb-judge" in err


def test_module_entrypoint_runs_off_pythonpath(two_judge_fixture):
    labels, alpha, beta = two_judge_fixture
    env = {**os.environ, "PYTHONPATH": str(SRC_DIR)}
    result = subprocess.run(
        [sys.executable, "-m", "catacomb_judge", "agreement", "--labels", labels, alpha, beta],
        capture_output=True,
        text=True,
        env=env,
    )
    assert result.returncode == 0
    assert result.stdout == GOLDEN_TABLE
