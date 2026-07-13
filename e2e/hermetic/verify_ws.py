from catacomb_verifier import Cell, emit

cell = Cell.from_env()
with open(cell.artifact("applied.txt"), "rb") as f:
    data = f.read()
emit(passed=(data == b"patched-line-1\n"), tool="verify_ws", tool_version="1")
