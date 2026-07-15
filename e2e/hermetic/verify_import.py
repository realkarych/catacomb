import os
from catacomb_verifier import Cell, emit

cell = Cell.from_env()
transcript = os.path.join(cell.evidence_dir, "session.jsonl")
emit(passed=os.path.isfile(transcript) and os.path.getsize(transcript) > 0, tool="verify_import", tool_version="1")
