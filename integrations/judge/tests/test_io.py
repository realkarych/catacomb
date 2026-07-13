from __future__ import annotations

import json
import re

import pytest

from catacomb_judge._io import (
    FormatError,
    JoinedKey,
    Label,
    Pair,
    ScoreLine,
    join,
    load_labels,
    load_scores,
    scan_runs_dir,
)


def write_jsonl(path, lines):
    rendered = [json.dumps(line) if isinstance(line, dict) else line for line in lines]
    path.write_text("".join(line + "\n" for line in rendered))
    return str(path)


def test_load_scores_full_line_and_defaults(tmp_path):
    path = write_jsonl(
        tmp_path / "scores.jsonl",
        [
            {"key": "judge.g", "value": 1, "run_id": "r1"},
            {
                "key": "judge.g",
                "value": 0.5,
                "run_id": "r2",
                "step_key": "s1",
                "tool": "alpha",
                "tool_version": "9",
                "prompt_hash": "abc",
            },
        ],
    )
    assert load_scores(path) == [
        ScoreLine(key="judge.g", value=1.0, run_id="r1", step_key=None, tool="unknown"),
        ScoreLine(key="judge.g", value=0.5, run_id="r2", step_key="s1", tool="alpha"),
    ]


def test_load_scores_empty_tool_and_step_key_normalize(tmp_path):
    path = write_jsonl(
        tmp_path / "scores.jsonl",
        [{"key": "judge.g", "value": 1, "run_id": "r1", "step_key": "", "tool": ""}],
    )
    assert load_scores(path) == [
        ScoreLine(key="judge.g", value=1.0, run_id="r1", step_key=None, tool="unknown")
    ]


def test_load_scores_skips_lines_without_run_id_one_note_per_file(tmp_path, capsys):
    path = write_jsonl(
        tmp_path / "scores.jsonl",
        [
            {"key": "judge.g", "value": 1},
            {"key": "judge.g", "value": 0, "run_id": ""},
            {"key": "judge.g", "value": 1, "run_id": "r1"},
        ],
    )
    scores = load_scores(path)
    assert [s.run_id for s in scores] == ["r1"]
    err = capsys.readouterr().err
    assert err == f"note: {path}: skipped 2 score line(s) without run_id\n"


def test_load_scores_no_note_when_nothing_skipped(tmp_path, capsys):
    path = write_jsonl(
        tmp_path / "scores.jsonl", [{"key": "judge.g", "value": 1, "run_id": "r1"}]
    )
    load_scores(path)
    assert capsys.readouterr().err == ""


def test_load_scores_blank_lines_skipped(tmp_path):
    path = write_jsonl(
        tmp_path / "scores.jsonl",
        ["", {"key": "judge.g", "value": 1, "run_id": "r1"}, "   "],
    )
    assert len(load_scores(path)) == 1


def test_load_scores_malformed_json_names_file_and_line(tmp_path):
    path = write_jsonl(
        tmp_path / "scores.jsonl",
        [{"key": "judge.g", "value": 1, "run_id": "r1"}, "{not json"],
    )
    with pytest.raises(FormatError, match=re.escape(f"{path}:2: invalid JSON")):
        load_scores(path)


def test_load_scores_non_object_line_rejected(tmp_path):
    path = write_jsonl(tmp_path / "scores.jsonl", ["[1, 2]"])
    with pytest.raises(FormatError, match=re.escape(f"{path}:1: not a JSON object")):
        load_scores(path)


@pytest.mark.parametrize(
    "line",
    [
        {"value": 1, "run_id": "r1"},
        {"key": "nodot", "value": 1, "run_id": "r1"},
        {"key": "a.b.c", "value": 1, "run_id": "r1"},
        {"key": ".b", "value": 1, "run_id": "r1"},
        {"key": "a.", "value": 1, "run_id": "r1"},
        {"key": 7, "value": 1, "run_id": "r1"},
    ],
)
def test_load_scores_rejects_bad_keys(tmp_path, line):
    path = write_jsonl(tmp_path / "scores.jsonl", [line])
    with pytest.raises(FormatError, match=re.escape(f"{path}:1:")):
        load_scores(path)


@pytest.mark.parametrize(
    "line",
    [
        {"key": "judge.g", "run_id": "r1"},
        {"key": "judge.g", "value": True, "run_id": "r1"},
        {"key": "judge.g", "value": "x", "run_id": "r1"},
        '{"key": "judge.g", "value": Infinity, "run_id": "r1"}',
    ],
)
def test_load_scores_rejects_bad_values(tmp_path, line):
    path = write_jsonl(tmp_path / "scores.jsonl", [line])
    with pytest.raises(FormatError, match=re.escape(f"{path}:1:")):
        load_scores(path)


@pytest.mark.parametrize(
    "line",
    [
        {"key": "judge.g", "value": 1, "run_id": 5},
        {"key": "judge.g", "value": 1, "run_id": "r1", "step_key": 5},
        {"key": "judge.g", "value": 1, "run_id": "r1", "tool": 5},
    ],
)
def test_load_scores_rejects_non_string_fields(tmp_path, line):
    path = write_jsonl(tmp_path / "scores.jsonl", [line])
    with pytest.raises(FormatError, match=re.escape(f"{path}:1:")):
        load_scores(path)


def test_load_labels_basic_step_key_and_tolerance(tmp_path):
    path = write_jsonl(
        tmp_path / "labels.jsonl",
        [
            {"run_id": "r1", "key": "judge.g", "label": 1, "note": "extra ignored"},
            {"run_id": "r1", "key": "judge.g", "label": 0.5, "step_key": "s1"},
            {"run_id": "r2", "key": "judge.g", "label": 0, "step_key": ""},
        ],
    )
    assert load_labels(path) == [
        Label(run_id="r1", key="judge.g", label=1.0, step_key=None),
        Label(run_id="r1", key="judge.g", label=0.5, step_key="s1"),
        Label(run_id="r2", key="judge.g", label=0.0, step_key=None),
    ]


def test_load_labels_duplicate_coordinates_error(tmp_path):
    path = write_jsonl(
        tmp_path / "labels.jsonl",
        [
            {"run_id": "r1", "key": "judge.g", "label": 1},
            {"run_id": "r1", "key": "judge.g", "label": 0},
        ],
    )
    with pytest.raises(
        FormatError,
        match=re.escape(f'{path}:2: duplicate label for run_id="r1" key="judge.g"'),
    ):
        load_labels(path)


def test_load_labels_duplicate_with_step_key_error_names_step(tmp_path):
    path = write_jsonl(
        tmp_path / "labels.jsonl",
        [
            {"run_id": "r1", "key": "judge.g", "label": 1, "step_key": "s1"},
            {"run_id": "r1", "key": "judge.g", "label": 0, "step_key": "s1"},
        ],
    )
    with pytest.raises(
        FormatError,
        match=re.escape(
            f'{path}:2: duplicate label for run_id="r1" key="judge.g" step_key="s1"'
        ),
    ):
        load_labels(path)


def test_load_labels_same_run_key_different_step_not_duplicate(tmp_path):
    path = write_jsonl(
        tmp_path / "labels.jsonl",
        [
            {"run_id": "r1", "key": "judge.g", "label": 1},
            {"run_id": "r1", "key": "judge.g", "label": 0, "step_key": "s1"},
            {"run_id": "r1", "key": "judge.g", "label": 1, "step_key": "s2"},
        ],
    )
    assert len(load_labels(path)) == 3


@pytest.mark.parametrize(
    "line",
    [
        {"key": "judge.g", "label": 1},
        {"run_id": "", "key": "judge.g", "label": 1},
        {"run_id": 5, "key": "judge.g", "label": 1},
        {"run_id": "r1", "label": 1},
        {"run_id": "r1", "key": "", "label": 1},
        {"run_id": "r1", "key": 5, "label": 1},
        {"run_id": "r1", "key": "judge.g"},
        {"run_id": "r1", "key": "judge.g", "label": True},
        {"run_id": "r1", "key": "judge.g", "label": "x"},
        {"run_id": "r1", "key": "judge.g", "label": 1, "step_key": 5},
    ],
)
def test_load_labels_rejects_bad_lines(tmp_path, line):
    path = write_jsonl(tmp_path / "labels.jsonl", [line])
    with pytest.raises(FormatError, match=re.escape(f"{path}:1:")):
        load_labels(path)


@pytest.mark.parametrize("key", ["verifierpass", "a.b.c", ".b", "a."])
def test_load_labels_rejects_bad_key_grammar(tmp_path, key):
    path = write_jsonl(
        tmp_path / "labels.jsonl", [{"run_id": "r1", "key": key, "label": 1}]
    )
    with pytest.raises(
        FormatError, match=re.escape(f"{path}:1: key {key!r} must be owner.key")
    ):
        load_labels(path)


def test_load_labels_malformed_json_names_file_and_line(tmp_path):
    path = write_jsonl(tmp_path / "labels.jsonl", ["{oops"])
    with pytest.raises(FormatError, match=re.escape(f"{path}:1: invalid JSON")):
        load_labels(path)


def test_scan_runs_dir_sorted_and_filtered(tmp_path):
    runs = tmp_path / "runs"
    for name in ("b-run", "a-run"):
        (runs / name).mkdir(parents=True)
        (runs / name / "scores.jsonl").write_text("")
    (runs / "c-run-empty").mkdir()
    (runs / "stray.txt").write_text("")
    assert scan_runs_dir(str(runs)) == [
        str(runs / "a-run" / "scores.jsonl"),
        str(runs / "b-run" / "scores.jsonl"),
    ]


def test_scan_runs_dir_missing_dir_raises(tmp_path):
    with pytest.raises(FileNotFoundError, match="runs dir not found"):
        scan_runs_dir(str(tmp_path / "nope"))


def score(key, value, run_id, step_key=None, tool="unknown"):
    return ScoreLine(key=key, value=value, run_id=run_id, step_key=step_key, tool=tool)


def label(run_id, key, value, step_key=None):
    return Label(run_id=run_id, key=key, label=value, step_key=step_key)


def test_join_matches_on_run_and_key():
    joined = join(
        [score("judge.g", 1.0, "r1", tool="alpha"), score("judge.g", 0.0, "r2", tool="alpha")],
        [label("r1", "judge.g", 1.0), label("r2", "judge.g", 0.0)],
    )
    assert len(joined) == 1
    jk = joined[0]
    assert jk.key == "judge.g"
    assert jk.pairs == (
        Pair(tool="alpha", label=1.0, score=1.0),
        Pair(tool="alpha", label=0.0, score=0.0),
    )
    assert jk.unmatched_labels == 0
    assert jk.unmatched_scores == 0


def test_join_label_without_step_key_matches_only_run_level():
    joined = join(
        [
            score("judge.g", 0.25, "r1", step_key="s1"),
            score("judge.g", 1.0, "r1"),
        ],
        [label("r1", "judge.g", 1.0)],
    )
    jk = joined[0]
    assert jk.pairs == (Pair(tool="unknown", label=1.0, score=1.0),)
    assert jk.unmatched_scores == 1
    assert jk.unmatched_labels == 0


def test_join_label_with_step_key_matches_only_that_step():
    joined = join(
        [
            score("judge.g", 1.0, "r1"),
            score("judge.g", 0.25, "r1", step_key="s1"),
            score("judge.g", 0.75, "r1", step_key="s2"),
        ],
        [label("r1", "judge.g", 0.0, step_key="s1")],
    )
    jk = joined[0]
    assert jk.pairs == (Pair(tool="unknown", label=0.0, score=0.25),)
    assert jk.unmatched_scores == 2
    assert jk.unmatched_labels == 0


def test_join_counts_unmatched_labels():
    joined = join(
        [score("judge.g", 1.0, "r1")],
        [label("r1", "judge.g", 1.0), label("r9", "judge.g", 0.0)],
    )
    assert joined[0].unmatched_labels == 1
    assert joined[0].unmatched_scores == 0


def test_join_ignores_score_keys_without_labels():
    joined = join(
        [score("verifier.pass", 1.0, "r1"), score("judge.g", 1.0, "r1")],
        [label("r1", "judge.g", 1.0)],
    )
    assert [jk.key for jk in joined] == ["judge.g"]
    assert joined[0].unmatched_scores == 0


def test_join_label_only_key_reported_empty():
    joined = join([], [label("r1", "judge.h", 1.0), label("r2", "judge.h", 0.0)])
    assert joined == [
        JoinedKey(key="judge.h", pairs=(), unmatched_labels=2, unmatched_scores=0)
    ]


def test_join_keys_sorted_and_pairs_canonical_order():
    scores = [
        score("judge.g", 0.0, "r2", tool="beta"),
        score("judge.g", 1.0, "r1", tool="beta"),
        score("judge.g", 0.0, "r2", tool="alpha"),
        score("judge.g", 1.0, "r1", tool="alpha"),
        score("judge.a", 1.0, "r1", tool="beta"),
    ]
    labels = [
        label("r1", "judge.g", 1.0),
        label("r2", "judge.g", 0.0),
        label("r1", "judge.a", 1.0),
    ]
    joined = join(scores, labels)
    assert [jk.key for jk in joined] == ["judge.a", "judge.g"]
    assert joined[1].pairs == (
        Pair(tool="alpha", label=1.0, score=1.0),
        Pair(tool="alpha", label=0.0, score=0.0),
        Pair(tool="beta", label=1.0, score=1.0),
        Pair(tool="beta", label=0.0, score=0.0),
    )
    assert joined == join(list(reversed(scores)), labels)


def test_join_multiple_tools_share_one_label():
    joined = join(
        [
            score("judge.g", 1.0, "r1", tool="beta"),
            score("judge.g", 0.0, "r1", tool="alpha"),
        ],
        [label("r1", "judge.g", 1.0)],
    )
    assert joined[0].pairs == (
        Pair(tool="alpha", label=1.0, score=0.0),
        Pair(tool="beta", label=1.0, score=1.0),
    )
