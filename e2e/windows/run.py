"""Windows artifact smoke — drives the built catacomb binary end-to-end.

The CI Windows leg only ever ran unit tests; this driver executes the shipped
binary shape (catacomb.exe on windows-latest) through the real offline
pipeline with a Windows-idiomatic fixture: no bash, no sqlite3, no claude, no
API, no network. The fake agent and verifier are the python scripts next to
this file, so the identical pipeline also runs on any Unix host for local
validation of everything but the OS.

Steps and asserted exit codes (mirroring the hermetic suite's pipeline, not
its bash):
  1. catacomb version                          -> 0
  2. catacomb bench (15 cells: 3 variants x 5) -> 0, evidence shape checked
  3. catacomb verify (offline re-verify)       -> 0, scores byte-identical,
                                                  verify.json mode -> offline
  4. catacomb regress baseline vs degraded     -> 1, ann:verifier.pass total
                                                  regression named in the JSON
  5. catacomb regress baseline vs baseline2    -> 0, zero regressions (A-vs-A)

The A-vs-A control widens only the continuous band (--metric-rel-delta 1.0):
every axis except wall-clock duration is byte-determined by the fixed
transcript template, and Windows runner jitter on tiny durations is larger
than the ubuntu jitter the hermetic suite absorbs with 0.5. The degraded gate
runs at full defaults; its detection axis is the annotation rate, which the
band flag does not touch.

Run: go build -o bin/catacomb.exe ./cmd/catacomb && python e2e/windows/run.py
Environment: CATACOMB_BIN  catacomb binary to drive (default bin/catacomb.exe
             on Windows, bin/catacomb elsewhere; resolved to an absolute path)
"""

import json
import os
import shutil
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
STEP_TIMEOUT_S = 300
BASKET = "windows-smoke"
BASE1 = "bench-windows-smoke-answer-baseline-r1"
DEGRADED1 = "bench-windows-smoke-answer-degraded-r1"

failures = []


def record(ok, label):
    print("  %s  %s" % ("PASS" if ok else "FAIL", label))
    if not ok:
        failures.append(label)


def run_step(want, label, argv, env, work, log_name):
    try:
        proc = subprocess.run(
            argv, env=env, capture_output=True, encoding="utf-8",
            errors="replace", timeout=STEP_TIMEOUT_S,
        )
    except subprocess.TimeoutExpired:
        record(False, "%s (timed out after %ds)" % (label, STEP_TIMEOUT_S))
        return None
    log = os.path.join(work, log_name)
    with open(log, "w", encoding="utf-8") as f:
        f.write(proc.stdout)
    with open(log + ".stderr", "w", encoding="utf-8") as f:
        f.write(proc.stderr)
    ok = proc.returncode == want
    record(ok, "%s (exit %d, want %d)" % (label, proc.returncode, want))
    if not ok:
        sys.stderr.write(proc.stdout)
        sys.stderr.write(proc.stderr)
    return proc


def stage(work):
    shutil.copy(os.path.join(HERE, "verify.py"), work)
    shutil.copy(os.path.join(HERE, "transcript.jsonl.tmpl"), work)
    cellwork = os.path.join(work, "cellwork")
    os.makedirs(cellwork)
    shutil.copy(os.path.join(HERE, "agent.py"), cellwork)
    os.makedirs(os.path.join(work, "projects"))
    os.makedirs(os.path.join(work, "runs"))
    with open(os.path.join(HERE, "basket.yaml.tmpl"), encoding="utf-8") as f:
        basket = f.read().replace("__PYTHON__", sys.executable.replace("\\", "/"))
    with open(os.path.join(work, "basket.yaml"), "w", encoding="utf-8", newline="\n") as f:
        f.write(basket)


def read_scores(runs):
    scores = {}
    for run_id in sorted(os.listdir(runs)):
        path = os.path.join(runs, run_id, "scores.jsonl")
        with open(path, "rb") as f:
            scores[run_id] = f.read()
    return scores


def verifier_pass(runs, run_id):
    with open(os.path.join(runs, run_id, "scores.jsonl"), encoding="utf-8") as f:
        for line in f:
            entry = json.loads(line)
            if entry.get("key") == "verifier.pass" and entry.get("run_id") == run_id:
                return entry.get("value")
    return None


def verify_mode(runs, run_id):
    with open(os.path.join(runs, run_id, "verify.json"), encoding="utf-8") as f:
        return json.load(f).get("mode")


def check_bench_evidence(runs):
    run_ids = sorted(os.listdir(runs))
    record(len(run_ids) == 15, "runs dir holds 15 evidence dirs (got %d)" % len(run_ids))
    base1 = os.path.join(runs, BASE1)
    for name in ("session.jsonl", "meta.json", "scores.jsonl", "verify.json"):
        record(os.path.isfile(os.path.join(base1, name)), "%s present in %s" % (name, BASE1))
    artifact = os.path.join(base1, "artifacts", "out", "result.txt")
    content = ""
    if os.path.isfile(artifact):
        with open(artifact, encoding="utf-8") as f:
            content = f.read().strip()
    record(content == "42", "captured artifact out/result.txt reads '42' (got %r)" % content)
    record(verify_mode(runs, BASE1) == "bench", "verify.json mode is 'bench' at bench time")
    record(verifier_pass(runs, BASE1) == 1, "baseline r1 scores verifier.pass = 1")
    record(verifier_pass(runs, DEGRADED1) == 0, "degraded r1 scores verifier.pass = 0")


def check_degraded_report(work):
    with open(os.path.join(work, "regress-degraded.json"), encoding="utf-8") as f:
        report = json.load(f)
    hits = [
        finding for finding in report.get("findings", [])
        if finding.get("scope") == "total" and finding.get("metric") == "ann:verifier.pass"
        and finding.get("verdict") == "regression"
    ]
    record(len(hits) == 1, "degraded gate names exactly one ann:verifier.pass total regression")


def check_ava_report(work):
    with open(os.path.join(work, "regress-AvA.json"), encoding="utf-8") as f:
        report = json.load(f)
    clean = report.get("regressions") == 0 and report.get("overall_verdict") != "regression"
    record(clean, "A-vs-A reports zero regressions")


def main():
    default_bin = os.path.join("bin", "catacomb.exe" if os.name == "nt" else "catacomb")
    bin_path = os.path.abspath(os.environ.get("CATACOMB_BIN", default_bin))
    if not os.path.isfile(bin_path):
        sys.exit("windows-smoke: catacomb binary not found at %s (build it or set CATACOMB_BIN)" % bin_path)
    work = tempfile.mkdtemp(prefix="catacomb-windows-smoke-")
    stage(work)
    projects = os.path.join(work, "projects")
    runs = os.path.join(work, "runs")
    basket = os.path.join(work, "basket.yaml")
    env = dict(os.environ, SMOKE_TDIR=work, SMOKE_PROJECTS=projects)

    print("== 1. catacomb version ==")
    run_step(0, "catacomb version", [bin_path, "version"], env, work, "version.out")

    print("== 2. bench windows-smoke basket (15 cells: 3 variants x 5 reps) ==")
    run_step(
        0, "bench windows-smoke basket",
        [bin_path, "bench", basket, "--projects-dir", projects, "--runs-dir", runs,
         "--manifest", os.path.join(work, "m.jsonl")],
        env, work, "bench.out",
    )
    check_bench_evidence(runs)

    print("== 3. offline re-verify (scores byte-identical, mode -> offline) ==")
    snapshot = read_scores(runs)
    run_step(
        0, "catacomb verify (offline re-verify)",
        [bin_path, "verify", basket, "--runs-dir", runs],
        env, work, "verify.out",
    )
    record(read_scores(runs) == snapshot, "offline verify leaves every scores.jsonl byte-identical")
    record(verify_mode(runs, BASE1) == "offline", "verify.json mode flips to offline after re-verify")

    print("== 4. seeded regression: baseline vs degraded must gate (exit 1) ==")
    run_step(
        1, "degraded gate (baseline vs degraded)",
        [bin_path, "regress", "--runs-dir", runs,
         "--baseline", "label:basket=%s,variant=baseline" % BASKET,
         "--candidate", "label:basket=%s,variant=degraded" % BASKET, "--json"],
        env, work, "regress-degraded.json",
    )
    check_degraded_report(work)

    print("== 5. A-vs-A control: baseline vs baseline2 must NOT gate (exit 0) ==")
    run_step(
        0, "A-vs-A control (baseline vs baseline2)",
        [bin_path, "regress", "--runs-dir", runs,
         "--baseline", "label:basket=%s,variant=baseline" % BASKET,
         "--candidate", "label:basket=%s,variant=baseline2" % BASKET,
         "--metric-rel-delta", "1.0", "--json"],
        env, work, "regress-AvA.json",
    )
    check_ava_report(work)

    if failures:
        print("\nwindows-smoke: %d assertion(s) FAILED; work dir kept at %s" % (len(failures), work))
        for label in failures:
            print("  FAIL  %s" % label)
        return 1
    shutil.rmtree(work, ignore_errors=True)
    print("\nwindows-smoke: all assertions passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
