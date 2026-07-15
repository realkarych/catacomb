from catacomb_verifier import Cell, emit

cell = Cell.from_env()
try:
    got = open(cell.artifact("out/result.csv")).read().strip()
except OSError:
    got = ""
emit(passed=(got == "CATACOMB-SKILL-OK"), tool="verify_emit", tool_version="1")
