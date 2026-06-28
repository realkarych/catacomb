from __future__ import annotations

import pathlib

TESTDATA = pathlib.Path(__file__).parent / "testdata"


def testdata_path(name: str) -> pathlib.Path:
    """Return absolute path to a testdata file."""
    return TESTDATA / name
