from __future__ import annotations

import json

import pytest

from catacomb_judge.cli import main

PANEL_MEAN_GOLDEN = """\
{"key":"judge.g","value":0.6666666666666666,"run_id":"r1","tool":"panel","tool_version":"0.1.0","panel_size":3}
{"key":"judge.g","value":1,"run_id":"r2","tool":"panel","tool_version":"0.1.0","panel_size":3}
{"key":"judge.g","value":0.3333333333333333,"run_id":"r3","tool":"panel","tool_version":"0.1.0","panel_size":3}
"""

PANEL_VOTE_GOLDEN = """\
{"key":"judge.g","value":1,"run_id":"r1","tool":"panel","tool_version":"0.1.0","panel_size":3}
{"key":"judge.g","value":1,"run_id":"r2","tool":"panel","tool_version":"0.1.0","panel_size":3}
{"key":"judge.g","value":0,"run_id":"r3","tool":"panel","tool_version":"0.1.0","panel_size":3}
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


@pytest.fixture
def three_judge_fixture(tmp_path):
    by_judge = {
        "alpha": [("r1", 1), ("r2", 1), ("r3", 0)],
        "beta": [("r1", 0), ("r2", 1), ("r3", 0)],
        "gamma": [("r1", 1), ("r2", 1), ("r3", 1)],
    }
    return [
        write_jsonl(
            tmp_path / f"{tool}.jsonl",
            [score_line("judge.g", value, run_id, tool) for run_id, value in pairs],
        )
        for tool, pairs in by_judge.items()
    ]


def test_panel_mean_golden_bytes(three_judge_fixture, capsys):
    code, out, err = run_cli(["panel", *three_judge_fixture], capsys)
    assert code == 0
    assert out == PANEL_MEAN_GOLDEN
    assert err == ""


def test_panel_vote_golden_bytes(three_judge_fixture, capsys):
    code, out, err = run_cli(["panel", "--vote", *three_judge_fixture], capsys)
    assert code == 0
    assert out == PANEL_VOTE_GOLDEN
    assert err == ""


def test_panel_tool_version_matches_package(three_judge_fixture, capsys):
    from catacomb_judge import __version__

    code, out, _ = run_cli(["panel", *three_judge_fixture], capsys)
    assert code == 0
    assert {json.loads(line)["tool_version"] for line in out.splitlines()} == {
        __version__
    }


def test_panel_vote_binarizes_at_half_boundary(tmp_path, capsys):
    scores = write_jsonl(
        tmp_path / "scores.jsonl",
        [
            score_line("judge.g", 0.5, "r1", "alpha"),
            score_line("judge.g", 0.4, "r1", "beta"),
            score_line("judge.g", 0.5, "r1", "gamma"),
        ],
    )
    code, out, err = run_cli(["panel", "--vote", scores], capsys)
    assert code == 0
    assert json.loads(out)["value"] == 1


def test_panel_vote_even_group_exit_2(tmp_path, capsys):
    scores = write_jsonl(
        tmp_path / "scores.jsonl",
        [
            score_line("judge.g", 1, "r1", "alpha"),
            score_line("judge.g", 0, "r1", "beta"),
        ],
    )
    code, out, err = run_cli(["panel", "--vote", scores], capsys)
    assert code == 2
    assert out == ""
    assert (
        'error: --vote requires an odd panel: 2 judges '
        'for run_id="r1" key="judge.g"' in err
    )


def test_panel_vote_even_in_any_group_fails_whole_command(tmp_path, capsys):
    scores = write_jsonl(
        tmp_path / "scores.jsonl",
        [
            score_line("judge.g", 1, "r1", "alpha"),
            score_line("judge.g", 1, "r1", "beta"),
            score_line("judge.g", 1, "r1", "gamma"),
            score_line("judge.g", 1, "r2", "alpha"),
            score_line("judge.g", 0, "r2", "beta"),
        ],
    )
    code, out, err = run_cli(["panel", "--vote", scores], capsys)
    assert code == 2
    assert out == ""
    assert 'run_id="r2" key="judge.g"' in err


def test_panel_min_judges_skip_note(tmp_path, capsys):
    scores = write_jsonl(
        tmp_path / "scores.jsonl",
        [
            score_line("judge.g", 1, "r1", "alpha"),
            score_line("judge.g", 0, "r1", "beta"),
            score_line("judge.g", 1, "r2", "alpha"),
        ],
    )
    code, out, err = run_cli(["panel", scores], capsys)
    assert code == 0
    assert [json.loads(line)["run_id"] for line in out.splitlines()] == ["r1"]
    assert err == (
        'note: skipped run_id="r2" key="judge.g": 1 judge(s) < --min-judges 2\n'
    )


def test_panel_min_judges_one_admits_single_judge_groups(tmp_path, capsys):
    scores = write_jsonl(
        tmp_path / "scores.jsonl",
        [
            score_line("judge.g", 1, "r1", "alpha"),
            score_line("judge.g", 0, "r1", "beta"),
            score_line("judge.g", 1, "r2", "alpha"),
        ],
    )
    code, out, err = run_cli(["panel", "--min-judges", "1", scores], capsys)
    assert code == 0
    assert err == ""
    lines = [json.loads(line) for line in out.splitlines()]
    assert [(line["run_id"], line["panel_size"]) for line in lines] == [
        ("r1", 2),
        ("r2", 1),
    ]
    assert lines[1]["value"] == 1


def test_panel_missing_tool_lines_skipped_with_note(tmp_path, capsys):
    scores = write_jsonl(
        tmp_path / "scores.jsonl",
        [
            score_line("judge.g", 1, "r1", "alpha"),
            score_line("judge.g", 0, "r1", "beta"),
            {"key": "judge.g", "value": 1, "run_id": "r1"},
        ],
    )
    code, out, err = run_cli(["panel", scores], capsys)
    assert code == 0
    line = json.loads(out)
    assert (line["value"], line["panel_size"]) == (0.5, 2)
    assert err == "note: skipped 1 score line(s) without tool provenance\n"


def test_panel_duplicate_tool_in_group_exit_2(tmp_path, capsys):
    scores = write_jsonl(
        tmp_path / "scores.jsonl",
        [
            score_line("judge.g", 1, "r1", "alpha"),
            score_line("judge.g", 0, "r1", "alpha"),
            score_line("judge.g", 1, "r1", "beta"),
        ],
    )
    code, out, err = run_cli(["panel", scores], capsys)
    assert code == 2
    assert out == ""
    assert (
        'error: ambiguous panel input: tool "alpha" emitted 2 lines '
        'for run_id="r1" key="judge.g"' in err
    )


def test_panel_duplicate_tool_detected_even_below_min_judges(tmp_path, capsys):
    scores = write_jsonl(
        tmp_path / "scores.jsonl",
        [
            score_line("judge.g", 1, "r1", "alpha"),
            score_line("judge.g", 0, "r1", "alpha"),
        ],
    )
    code, out, err = run_cli(["panel", scores], capsys)
    assert code == 2
    assert 'ambiguous panel input: tool "alpha"' in err


def test_panel_step_key_groups_separately_and_orders_field(tmp_path, capsys):
    scores = write_jsonl(
        tmp_path / "scores.jsonl",
        [
            score_line("judge.g", 1, "r1", "alpha"),
            score_line("judge.g", 1, "r1", "beta"),
            score_line("judge.g", 1, "r1", "alpha", step_key="s1"),
            score_line("judge.g", 0, "r1", "beta", step_key="s1"),
        ],
    )
    code, out, err = run_cli(["panel", scores], capsys)
    assert code == 0
    assert out == (
        '{"key":"judge.g","value":1,"run_id":"r1",'
        '"tool":"panel","tool_version":"0.1.0","panel_size":2}\n'
        '{"key":"judge.g","value":0.5,"run_id":"r1","step_key":"s1",'
        '"tool":"panel","tool_version":"0.1.0","panel_size":2}\n'
    )


def test_panel_out_writes_same_bytes_and_keeps_stdout_clean(
    three_judge_fixture, tmp_path, capsys
):
    out_file = tmp_path / "panel.jsonl"
    code, out, err = run_cli(
        ["panel", "--out", str(out_file), *three_judge_fixture], capsys
    )
    assert code == 0
    assert out == ""
    assert err == ""
    assert out_file.read_text(encoding="utf-8") == PANEL_MEAN_GOLDEN


def test_panel_out_unwritable_exit_2(three_judge_fixture, tmp_path, capsys):
    code, out, err = run_cli(
        ["panel", "--out", str(tmp_path / "nope" / "panel.jsonl"), *three_judge_fixture],
        capsys,
    )
    assert code == 2
    assert out == ""
    assert err.startswith("error: ")


def test_panel_output_round_trips_into_agreement(three_judge_fixture, tmp_path, capsys):
    out_file = tmp_path / "panel.jsonl"
    code, _, _ = run_cli(["panel", "--out", str(out_file), *three_judge_fixture], capsys)
    assert code == 0
    labels = write_jsonl(
        tmp_path / "labels.jsonl",
        [
            {"run_id": run_id, "key": "judge.g", "label": label}
            for run_id, label in [("r1", 1), ("r2", 1), ("r3", 0)]
        ],
    )
    code, out, err = run_cli(
        ["agreement", "--labels", labels, str(out_file), "--json"], capsys
    )
    assert code == 0
    judges = json.loads(out)["keys"][0]["judges"]
    assert [judge["tool"] for judge in judges] == ["panel"]
    assert judges[0]["n"] == 3
    assert judges[0]["kappa"] == 1.0


def test_panel_key_filter(tmp_path, capsys):
    scores = write_jsonl(
        tmp_path / "scores.jsonl",
        [
            score_line("judge.g", 1, "r1", "alpha"),
            score_line("judge.g", 0, "r1", "beta"),
            score_line("judge.h", 1, "r1", "alpha"),
            score_line("judge.h", 1, "r1", "beta"),
        ],
    )
    code, out, err = run_cli(["panel", "--key", "judge.g", scores], capsys)
    assert code == 0
    assert [json.loads(line)["key"] for line in out.splitlines()] == ["judge.g"]


def test_panel_mean_bytes_invariant_to_input_file_order(tmp_path, capsys):
    values = {"alpha": 0.1, "beta": 0.2, "gamma": 0.3}
    paths = [
        write_jsonl(tmp_path / f"{tool}.jsonl", [score_line("judge.g", value, "r1", tool)])
        for tool, value in values.items()
    ]
    code_fwd, out_fwd, _ = run_cli(["panel", *paths], capsys)
    code_rev, out_rev, _ = run_cli(["panel", *reversed(paths)], capsys)
    assert code_fwd == code_rev == 0
    assert out_fwd == out_rev
    assert json.loads(out_fwd)["value"] == sum((0.1, 0.2, 0.3)) / 3


def test_panel_vote_min_judges_skip_precedes_even_panel_error(tmp_path, capsys):
    scores = write_jsonl(
        tmp_path / "scores.jsonl",
        [
            score_line("judge.g", 1, "r1", "alpha"),
            score_line("judge.g", 0, "r1", "beta"),
        ],
    )
    code, out, err = run_cli(["panel", "--vote", "--min-judges", "3", scores], capsys)
    assert code == 0
    assert out == ""
    assert err == (
        'note: skipped run_id="r1" key="judge.g": 2 judge(s) < --min-judges 3\n'
    )


def test_panel_deterministic_ordering_across_input_orders(tmp_path, capsys):
    first = write_jsonl(
        tmp_path / "first.jsonl",
        [
            score_line("judge.b", 1, "r2", "alpha"),
            score_line("judge.a", 1, "r1", "alpha"),
            score_line("judge.b", 0, "r1", "alpha"),
        ],
    )
    second = write_jsonl(
        tmp_path / "second.jsonl",
        [
            score_line("judge.a", 0, "r1", "beta"),
            score_line("judge.b", 1, "r1", "beta"),
            score_line("judge.b", 1, "r2", "beta"),
        ],
    )
    code_ab, out_ab, _ = run_cli(["panel", first, second], capsys)
    code_ba, out_ba, _ = run_cli(["panel", second, first], capsys)
    assert (code_ab, out_ab) == (code_ba, out_ba)
    coords = [
        (line["run_id"], line["key"])
        for line in map(json.loads, out_ab.splitlines())
    ]
    assert coords == [("r1", "judge.a"), ("r1", "judge.b"), ("r2", "judge.b")]


def test_panel_runs_dir(tmp_path, capsys):
    runs = tmp_path / "runs"
    for name, tool in (("a-run", "alpha"), ("b-run", "beta")):
        (runs / name).mkdir(parents=True)
        write_jsonl(runs / name / "scores.jsonl", [score_line("judge.g", 1, "r1", tool)])
    code, out, err = run_cli(["panel", "--runs-dir", str(runs)], capsys)
    assert code == 0
    line = json.loads(out)
    assert (line["value"], line["panel_size"]) == (1, 2)


def test_panel_requires_a_scores_source(capsys):
    code, out, err = run_cli(["panel"], capsys)
    assert code == 2
    assert "usage: catacomb-judge panel" in err
    assert "at least one scores file or --runs-dir is required" in err


def test_panel_malformed_scores_exit_2(tmp_path, capsys):
    bad = tmp_path / "bad.jsonl"
    bad.write_text("{oops\n")
    code, out, err = run_cli(["panel", str(bad)], capsys)
    assert code == 2
    assert f"error: {bad}:1: invalid JSON" in err
