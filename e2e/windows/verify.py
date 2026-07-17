"""SDK-free verifier for the Windows artifact smoke.

Reads the captured artifact and emits one scores-JSONL line on stdout
(the verifier contract; catacomb injects the run_id). Prefers the redacted
evidence copy so the same bytes verify in bench mode and in offline
re-verification (`catacomb verify`, where CATACOMB_WORKDIR is empty);
falls back to the live workdir only in bench mode.

Environment: CATACOMB_EVIDENCE_DIR + CATACOMB_WORKDIR (exec contract, set by
catacomb) and WANT_ANSWER (the expected artifact content, set in the basket's
verify env — the anti-gaming layout keeps the expectation with the verifier,
not the agent).
"""

import json
import os


def artifact_path():
    rel = os.path.join("out", "result.txt")
    evidence = os.path.join(os.environ["CATACOMB_EVIDENCE_DIR"], "artifacts", rel)
    if os.path.exists(evidence):
        return evidence
    return os.path.join(os.environ["CATACOMB_WORKDIR"], rel)


def main():
    with open(artifact_path(), encoding="utf-8") as f:
        answer = f.read().strip()
    passed = 1 if answer == os.environ["WANT_ANSWER"] else 0
    line = {"key": "verifier.pass", "value": passed, "tool": "verify_windows", "tool_version": "1"}
    print(json.dumps(line, separators=(",", ":")))


if __name__ == "__main__":
    main()
