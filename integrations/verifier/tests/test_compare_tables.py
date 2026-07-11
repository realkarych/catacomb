from __future__ import annotations

import dataclasses

import pytest

from catacomb_verifier import CompareResult, compare_tables


def _write(tmp_path, name, text):
    path = tmp_path / name
    path.write_text(text, encoding="utf-8")
    return str(path)


def test_reordered_rows_equal_when_unordered(tmp_path):
    got = _write(tmp_path, "got.csv", "region,total\nnorth,10\nsouth,20\n")
    want = _write(tmp_path, "want.csv", "region,total\nsouth,20\nnorth,10\n")
    res = compare_tables(got, want)
    assert res.equal is True
    assert res.row_diff == 0
    assert res.mismatches == []


def test_reordered_rows_unequal_when_ordered(tmp_path):
    got = _write(tmp_path, "got.csv", "region,total\nnorth,10\nsouth,20\n")
    want = _write(tmp_path, "want.csv", "region,total\nsouth,20\nnorth,10\n")
    res = compare_tables(got, want, ordered=True)
    assert res.equal is False
    assert res.mismatches  # positional pairing surfaces the swap


def test_float_within_tolerance_equal(tmp_path):
    got = _write(tmp_path, "got.csv", "region,total\nnorth,100.00005\n")
    want = _write(tmp_path, "want.csv", "region,total\nnorth,100.0\n")
    res = compare_tables(got, want, float_tol=1e-4)
    assert res.equal is True


def test_float_outside_tolerance_unequal(tmp_path):
    got = _write(tmp_path, "got.csv", "region,total\nnorth,100.0\n")
    want = _write(tmp_path, "want.csv", "region,total\nnorth,101.5\n")
    res = compare_tables(got, want, float_tol=1e-4)
    assert res.equal is False
    assert res.mismatches == ["row 0 col total: 100.0 != 101.5"]


def test_header_variance_equal_when_normalized(tmp_path):
    got = _write(tmp_path, "got.csv", "Region,Total Sales\nnorth,10\n")
    want = _write(tmp_path, "want.csv", "region,total_sales\nnorth,10\n")
    res = compare_tables(got, want, normalize_headers=True)
    assert res.equal is True


def test_header_variance_unequal_when_not_normalized(tmp_path):
    got = _write(tmp_path, "got.csv", "Region,total\nnorth,10\n")
    want = _write(tmp_path, "want.csv", "region,total\nnorth,10\n")
    res = compare_tables(got, want, normalize_headers=False)
    assert res.equal is False
    assert any("Region" in m or "region" in m for m in res.mismatches)


def test_extra_column_unequal_under_strict_names_column(tmp_path):
    got = _write(tmp_path, "got.csv", "region,total,extra\nnorth,10,x\n")
    want = _write(tmp_path, "want.csv", "region,total\nnorth,10\n")
    res = compare_tables(got, want, strict=True)
    assert res.equal is False
    assert any("extra" in m for m in res.mismatches)


def test_extra_column_tolerated_when_not_strict(tmp_path):
    got = _write(tmp_path, "got.csv", "region,total,extra\nnorth,10,x\n")
    want = _write(tmp_path, "want.csv", "region,total\nnorth,10\n")
    res = compare_tables(got, want, strict=False)
    assert res.equal is True
    assert res.mismatches == []


def test_missing_column_under_strict_names_column(tmp_path):
    got = _write(tmp_path, "got.csv", "region\nnorth\n")
    want = _write(tmp_path, "want.csv", "region,total\nnorth,10\n")
    res = compare_tables(got, want, strict=True)
    assert res.equal is False
    assert any("column total" in m for m in res.mismatches)


def test_row_count_mismatch_sets_row_diff(tmp_path):
    got = _write(tmp_path, "got.csv", "region,total\nnorth,10\nsouth,20\neast,30\n")
    want = _write(tmp_path, "want.csv", "region,total\nnorth,10\nsouth,20\n")
    res = compare_tables(got, want, strict=True)
    assert res.equal is False
    assert res.row_diff == 1
    assert any("row count" in m for m in res.mismatches)


def test_extra_rows_tolerated_when_not_strict_keeps_row_diff(tmp_path):
    got = _write(tmp_path, "got.csv", "region,total\nnorth,10\nsouth,20\neast,30\n")
    want = _write(tmp_path, "want.csv", "region,total\nnorth,10\nsouth,20\n")
    res = compare_tables(got, want, strict=False)
    assert res.equal is True
    assert res.row_diff == 1


def test_jsonl_vs_csv_cross_format_equal(tmp_path):
    got = _write(
        tmp_path,
        "got.jsonl",
        '{"region":"north","total":10}\n{"region":"south","total":20}\n',
    )
    want = _write(tmp_path, "want.csv", "region,total\nnorth,10\nsouth,20\n")
    res = compare_tables(got, want)
    assert res.equal is True
    assert res.row_diff == 0


def test_int_float_coercion_compares_numerically(tmp_path):
    got = _write(tmp_path, "got.jsonl", '{"region":"north","total":100.0}\n')
    want = _write(tmp_path, "want.csv", "region,total\nnorth,100\n")
    res = compare_tables(got, want)
    assert res.equal is True


def test_string_cells_are_stripped(tmp_path):
    got = _write(tmp_path, "got.csv", "region,total\n  north  ,10\n")
    want = _write(tmp_path, "want.csv", "region,total\nnorth,10\n")
    res = compare_tables(got, want)
    assert res.equal is True


def test_empty_files_equal(tmp_path):
    got = _write(tmp_path, "got.csv", "")
    want = _write(tmp_path, "want.csv", "")
    res = compare_tables(got, want)
    assert res.equal is True
    assert res.row_diff == 0
    assert res.mismatches == []


def test_empty_cross_format_equal(tmp_path):
    got = _write(tmp_path, "got.jsonl", "")
    want = _write(tmp_path, "want.csv", "")
    res = compare_tables(got, want)
    assert res.equal is True


def test_mismatches_capped_at_ten(tmp_path):
    got_rows = "".join(f"r{i},{i}\n" for i in range(20))
    want_rows = "".join(f"r{i},{i + 1000}\n" for i in range(20))
    got = _write(tmp_path, "got.csv", "region,total\n" + got_rows)
    want = _write(tmp_path, "want.csv", "region,total\n" + want_rows)
    res = compare_tables(got, want, ordered=True)
    assert res.equal is False
    assert len(res.mismatches) == 10


def test_missing_row_under_non_strict_is_reported(tmp_path):
    got = _write(tmp_path, "got.csv", "region,total\nnorth,10\n")
    want = _write(tmp_path, "want.csv", "region,total\nnorth,10\nsouth,20\n")
    res = compare_tables(got, want, strict=False)
    assert res.equal is False
    assert any(m.startswith("missing row:") and "south" in m for m in res.mismatches)


def test_non_strict_mismatches_capped_at_ten(tmp_path):
    got = _write(tmp_path, "got.csv", "region,total\n")
    want_rows = "".join(f"r{i},{i}\n" for i in range(15))
    want = _write(tmp_path, "want.csv", "region,total\n" + want_rows)
    res = compare_tables(got, want, strict=False)
    assert res.equal is False
    assert len(res.mismatches) == 10


def test_short_csv_row_pads_missing_cells_with_none(tmp_path):
    got = _write(tmp_path, "got.csv", "region,total\nnorth\n")
    want = _write(tmp_path, "want.csv", "region,total\nnorth,10\n")
    res = compare_tables(got, want)
    assert res.equal is False
    assert res.mismatches == ["row 0 col total: None != 10"]


def test_blank_jsonl_lines_are_skipped(tmp_path):
    got = _write(
        tmp_path,
        "got.jsonl",
        '{"region":"north","total":10}\n\n{"region":"south","total":20}\n',
    )
    want = _write(tmp_path, "want.csv", "region,total\nnorth,10\nsouth,20\n")
    res = compare_tables(got, want)
    assert res.equal is True
    assert res.row_diff == 0


def test_bool_and_null_cells_compare_by_equality(tmp_path):
    got = _write(tmp_path, "got.jsonl", '{"region":"north","active":true,"note":null}\n')
    want = _write(tmp_path, "want.jsonl", '{"region":"north","active":true,"note":null}\n')
    res = compare_tables(got, want)
    assert res.equal is True


def test_bool_mismatch_is_reported(tmp_path):
    got = _write(tmp_path, "got.jsonl", '{"region":"north","active":true}\n')
    want = _write(tmp_path, "want.jsonl", '{"region":"north","active":false}\n')
    res = compare_tables(got, want)
    assert res.equal is False
    assert res.mismatches == ["row 0 col active: True != False"]


def test_unsupported_extension_raises(tmp_path):
    got = _write(tmp_path, "got.txt", "region,total\nnorth,10\n")
    want = _write(tmp_path, "want.csv", "region,total\nnorth,10\n")
    with pytest.raises(ValueError):
        compare_tables(got, want)


def test_result_is_frozen_dataclass():
    res = CompareResult(equal=True, row_diff=0, mismatches=[])
    assert dataclasses.is_dataclass(res)
    with pytest.raises(dataclasses.FrozenInstanceError):
        res.equal = False  # type: ignore[misc]
