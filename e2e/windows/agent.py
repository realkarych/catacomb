"""Windows-idiomatic fake agent for the artifact smoke: no shell, no API.

Mimics the observable surface `catacomb bench` needs from a Claude Code
child process:
  1. writes a Claude-Code-shaped session transcript (the fixed template next
     to the basket, session id substituted) into the projects dir, where
     bench resolves it as <projects>/<subdir>/<session_id>.jsonl;
  2. writes the task artifact out/result.txt into the cell workdir (content
     comes from AGENT_ANSWER, the per-variant knob: the baseline variants
     answer "42", the degraded one answers "wrong", flipping verifier.pass);
  3. emits stream-json on stdout so bench observes the session id and cost.

Environment (set by run.py before invoking bench):
  SMOKE_TDIR      dir holding transcript.jsonl.tmpl
  SMOKE_PROJECTS  projects dir bench resolves transcripts under
  AGENT_ANSWER    the answer to write into out/result.txt (per-variant)
"""

import os
import sys
import uuid


def main():
    sys.stdout.reconfigure(newline="\n")
    sid = str(uuid.uuid4())
    transcript_tmpl = os.path.join(os.environ["SMOKE_TDIR"], "transcript.jsonl.tmpl")
    projects = os.path.join(os.environ["SMOKE_PROJECTS"], "windows-smoke")
    os.makedirs(projects, exist_ok=True)
    with open(transcript_tmpl, encoding="utf-8") as f:
        transcript = f.read().replace("__SESSION_ID__", sid)
    with open(os.path.join(projects, sid + ".jsonl"), "w", encoding="utf-8", newline="\n") as f:
        f.write(transcript)
    os.makedirs("out", exist_ok=True)
    with open(os.path.join("out", "result.txt"), "w", encoding="utf-8", newline="\n") as f:
        f.write(os.environ["AGENT_ANSWER"] + "\n")
    print('{"type":"system","session_id":"%s"}' % sid)
    print('{"type":"result","session_id":"%s","total_cost_usd":0.0}' % sid)


if __name__ == "__main__":
    main()
