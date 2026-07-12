import os
from catacomb_verifier import Cell, emit, compare_tables

cell = Cell.from_env()
res = compare_tables(cell.artifact("out/result.csv"), os.environ["GOLDEN"], float_tol=1e-4, ordered=False)
emit(passed=res.equal, tool="verify_sql", tool_version="1")
emit(key="verifier.row_diff", value=float(res.row_diff))
