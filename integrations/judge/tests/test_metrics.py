from __future__ import annotations

import pytest

from catacomb_judge import binarize, kappa, spearman, tnr, tpr


def test_spearman_perfect_monotone_is_one():
    assert spearman([1.0, 2.0, 3.0, 4.0], [10.0, 20.0, 30.0, 40.0]) == 1.0


def test_spearman_nonlinear_monotone_is_one():
    assert spearman([1.0, 2.0, 3.0, 4.0], [1.0, 4.0, 9.0, 100.0]) == 1.0


def test_spearman_perfect_inverse_is_minus_one():
    assert spearman([1.0, 2.0, 3.0], [3.0, 2.0, 1.0]) == -1.0


def test_spearman_ties_use_average_ranks():
    # xs = [1, 2, 2, 3] -> ranks [1, 2.5, 2.5, 4] (the tied 2s share (2+3)/2);
    # ys = [1, 2, 3, 4] -> ranks [1, 2, 3, 4]. Pearson on the ranks:
    # deviations dx = [-1.5, 0, 0, 1.5], dy = [-1.5, -0.5, 0.5, 1.5];
    # cov = 4.5, var_x = 4.5, var_y = 5.0; rho = 4.5/sqrt(22.5) = sqrt(0.9).
    assert spearman([1.0, 2.0, 2.0, 3.0], [1.0, 2.0, 3.0, 4.0]) == pytest.approx(
        0.9486832980505138
    )


def test_spearman_ties_on_both_sides_perfect_agreement():
    # xs = [1, 1, 2] and ys = [3, 3, 5] both rank to [1.5, 1.5, 3] -> rho = 1.
    assert spearman([1.0, 1.0, 2.0], [3.0, 3.0, 5.0]) == pytest.approx(1.0)


def test_spearman_fewer_than_two_pairs_is_none():
    assert spearman([], []) is None
    assert spearman([1.0], [2.0]) is None


def test_spearman_constant_side_is_none():
    assert spearman([1.0, 2.0, 3.0], [5.0, 5.0, 5.0]) is None
    assert spearman([7.0, 7.0, 7.0], [1.0, 2.0, 3.0]) is None


def test_spearman_length_mismatch_raises():
    with pytest.raises(ValueError, match="spearman requires equal-length inputs"):
        spearman([1.0, 2.0], [1.0])


def test_binarize_boundary_maps_to_one():
    assert binarize([0.2, 0.5, 0.7, 0.49999], 0.5) == [0, 1, 1, 0]


def test_binarize_exactly_threshold_single_value():
    assert binarize([0.5], 0.5) == [1]


def test_binarize_zero_threshold_with_negatives():
    assert binarize([-1.0, 0.0, 1.0], 0.0) == [0, 1, 1]


def test_binarize_empty_is_empty():
    assert binarize([], 0.5) == []


def test_kappa_hand_computed_two_by_two_table():
    # TP=4, TN=3, FP=2, FN=1 over n=10:
    # p_o = (4+3)/10 = 0.7;
    # p_e = P(a=1)P(b=1) + P(a=0)P(b=0) = 0.5*0.6 + 0.5*0.4 = 0.5;
    # kappa = (0.7 - 0.5)/(1 - 0.5) = 0.4.
    a = [1, 1, 1, 1, 0, 0, 0, 0, 0, 1]
    b = [1, 1, 1, 1, 0, 0, 0, 1, 1, 0]
    assert kappa(a, b) == pytest.approx(0.4)


def test_kappa_perfect_agreement_is_one():
    assert kappa([1, 0, 1, 0], [1, 0, 1, 0]) == pytest.approx(1.0)


def test_kappa_chance_level_is_zero():
    # p_o = 0.5 agreement, p_e = 0.5*0.5 + 0.5*0.5 = 0.5 -> kappa = 0.
    assert kappa([1, 1, 0, 0], [1, 0, 1, 0]) == pytest.approx(0.0)


def test_kappa_both_marginals_constant_is_none():
    assert kappa([1, 1, 1], [1, 1, 1]) is None
    assert kappa([0, 0], [0, 0]) is None


def test_kappa_opposite_constants_is_zero():
    # p_e = 1*0 + 0*1 = 0 and p_o = 0 -> kappa = 0, not None.
    assert kappa([1, 1], [0, 0]) == pytest.approx(0.0)


def test_kappa_empty_is_none():
    assert kappa([], []) is None


def test_kappa_length_mismatch_raises():
    with pytest.raises(ValueError, match="kappa requires equal-length inputs"):
        kappa([1, 0], [1])


def test_tpr_hand_computed_table():
    # labels [1,1,1,0,0] vs preds [1,0,1,1,0]: TP=2, FN=1 -> TPR = 2/3.
    assert tpr([1, 1, 1, 0, 0], [1, 0, 1, 1, 0]) == pytest.approx(2 / 3)


def test_tnr_hand_computed_table():
    # labels [1,1,1,0,0] vs preds [1,0,1,1,0]: TN=1, FP=1 -> TNR = 1/2.
    assert tnr([1, 1, 1, 0, 0], [1, 0, 1, 1, 0]) == pytest.approx(0.5)


def test_tpr_perfect_and_zero():
    assert tpr([1, 0, 1], [1, 0, 1]) == pytest.approx(1.0)
    assert tpr([1, 1], [0, 0]) == pytest.approx(0.0)


def test_tnr_perfect_and_zero():
    assert tnr([1, 0, 1], [1, 0, 1]) == pytest.approx(1.0)
    assert tnr([0, 0], [1, 1]) == pytest.approx(0.0)


def test_tpr_no_positive_labels_is_none():
    assert tpr([0, 0], [0, 1]) is None
    assert tnr([0, 0], [0, 1]) == pytest.approx(0.5)


def test_tnr_no_negative_labels_is_none():
    assert tnr([1, 1], [1, 0]) is None
    assert tpr([1, 1], [1, 0]) == pytest.approx(0.5)


def test_tpr_tnr_empty_is_none():
    assert tpr([], []) is None
    assert tnr([], []) is None


def test_tpr_length_mismatch_raises():
    with pytest.raises(ValueError, match="tpr requires equal-length inputs"):
        tpr([1], [1, 0])


def test_tnr_length_mismatch_raises():
    with pytest.raises(ValueError, match="tnr requires equal-length inputs"):
        tnr([0, 1], [0])


def test_package_version_and_exports():
    import catacomb_judge

    assert catacomb_judge.__version__ == "0.1.0"
    assert catacomb_judge.__all__ == ["binarize", "kappa", "spearman", "tnr", "tpr"]
