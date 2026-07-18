# export→deepeval seam + codex fixture contract — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the two e2e coverage seams the 2026-07-18 audit found — the untested `catacomb export → catacomb-deepeval` handoff, and the unguarded drift risk in the hermetic codex fake fixtures.

**Architecture:** F2 adds a test-only Go contract test in `ingest/codex` that pins the codex fixtures' record shape to the curated reference and their `cli_version` to `drift.TestedCodexVersion`, forcing a `0.144.4 → 0.144.5` sync. F1 adds one shared Claude-transcript fixture exercised at two levels: a per-PR hermetic author-mode scenario (zero-dep) and a full-metric job in `python-deepeval.yml`.

**Tech Stack:** Go (stdlib `encoding/json`, testify), bash hermetic scenarios, Python `catacomb-deepeval` (DeepEval `ToolCorrectnessMetric`), GitHub Actions.

## Global Constraints

- **No comments in Go code** — none, including in `_test.go`. Enforced by `internal/codepolicy`.
- **100% coverage** — `make cover`. The new Go file is test-only (`contract_test.go`), so it adds no production code and cannot lower coverage.
- **Formatting** — `gofumpt` + `goimports` local prefix `github.com/realkarych/catacomb` (`make fmt`).
- **TDD** — failing test first, minimal change to green.
- **Codex pin** — `drift.TestedCodexVersion = "0.144.5"` (verbatim). The bump target is exactly `0.144.5`.
- **DeepEval** — `deepeval>=4.1,<4.2`; the seam stays offline, name-match, keyless (no `ANTHROPIC_API_KEY`, no `--trace-metrics`).
- **Pinned Action SHAs** — reuse the exact `uses:` SHAs already in `.github/workflows/python-deepeval.yml` and `e2e-live.yml` (`actions/checkout@9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0`, `actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16`, `actions/setup-python@ece7cb06caefa5fff74198d8649806c4678c61a1`).
- **Worktree** — all work in the isolated worktree; never edit the shared checkout.

## Execution order & PRs

F2 and F1 both touch `e2e/hermetic/prod/` and `e2e/hermetic/run.sh`, so they are **serialized: Task 1 (F2) → Tasks 2–3 (F1)**. Recommended PR grouping: **PR-A = Task 1** (codex contract + version sync, carries the spec+plan docs); **PR-B = Tasks 2–3** (deepeval seam). Confirm branch/PR split at the execution-handoff step.

## File structure

| File | New/Mod | Responsibility |
| --- | --- | --- |
| `ingest/codex/contract_test.go` | new | Contract: fixtures' `(type,payload.type)` ⊆ testdata canon; all `session_meta.cli_version` == `drift.TestedCodexVersion`. Test-only. |
| `ingest/codex/testdata/{basic,child,tools,mcp}.jsonl` | mod | Bump `cli_version` `0.144.4→0.144.5`. |
| `e2e/hermetic/prod/fixtures/*-codex*.jsonl.tmpl` (11 files w/ `0.144.4`) | mod | Bump `cli_version` `0.144.4→0.144.5`. |
| `e2e/hermetic/prod/scenarios/{55,56,58}-codex-*.sh` | mod | Bump `agent_version` assertion `0.144.4→0.144.5`. |
| `ingest/codex/codex_test.go` | mod | Bump three `0.144.4` occurrences → `0.144.5`. |
| `integrations/deepeval/tests/testdata/seam_session.jsonl` | new | Shared Claude transcript: `Bash` + `mcp__fs__read` + final text. |
| `integrations/deepeval/tests/testdata/seam_expected_pass.json` | new | Expected tools = both called → PASS. |
| `integrations/deepeval/tests/testdata/seam_expected_fail.json` | new | Expected tool never called → FAIL. |
| `e2e/hermetic/prod/scenarios/85-deepeval-seam.sh` | new | Level A: export → author-mode adapter, assert `tools_called`. |
| `e2e/hermetic/run.sh` (line 190) | mod | Add `integrations/deepeval/src` to `PYTHONPATH`. |
| `.github/workflows/python-deepeval.yml` | mod | Level B: new `seam` job — Go build → export → real metric PASS/FAIL. |

---

## Task 1: Codex fixture contract test + version sync (F2)

**Files:**

- Create: `ingest/codex/contract_test.go`
- Modify: `ingest/codex/testdata/basic.jsonl`, `child.jsonl`, `tools.jsonl`, `mcp.jsonl`
- Modify: `e2e/hermetic/prod/fixtures/55-codex-main.jsonl.tmpl`, `55-codex-degraded.jsonl.tmpl`, `55-codex-child.jsonl.tmpl`, `56-codex-main.jsonl.tmpl`, `56-codex-degraded.jsonl.tmpl`, `57-codex-mcp-baseline.jsonl.tmpl`, `57-codex-mcp-degraded.jsonl.tmpl`, `58-codex-main.jsonl.tmpl`, `58-codex-degraded.jsonl.tmpl`, `58-codex-degraded-noplant.jsonl.tmpl`, `58-codex-child.jsonl.tmpl`
- Modify: `e2e/hermetic/prod/scenarios/55-codex-import.sh`, `56-codex-bench.sh`, `58-codex-subagent.sh`
- Modify: `ingest/codex/codex_test.go`

**Interfaces:**

- Consumes: `github.com/realkarych/catacomb/ingest/drift.TestedCodexVersion` (const `"0.144.5"`).
- Produces: nothing importable (test-only).

- [ ] **Step 1: Write the failing contract test**

Create `ingest/codex/contract_test.go` (no comments):

```go
package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/realkarych/catacomb/ingest/drift"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var contractPlaceholderRE = regexp.MustCompile(`__[A-Z_]+__`)

type contractRecord struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func contractScan(t *testing.T, path string, substitute bool) (map[[2]string]bool, []string) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	pairs := map[[2]string]bool{}
	versions := []string{}
	for _, raw := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		line := raw
		if substitute {
			line = strings.ReplaceAll(line, "__EPOCH__", "0")
			line = contractPlaceholderRE.ReplaceAllString(line, "x")
		}
		var rec contractRecord
		require.NoErrorf(t, json.Unmarshal([]byte(line), &rec), "path=%s line=%s", path, line)
		var payload struct {
			Type       string `json:"type"`
			CliVersion string `json:"cli_version"`
		}
		if len(rec.Payload) > 0 {
			require.NoErrorf(t, json.Unmarshal(rec.Payload, &payload), "path=%s payload=%s", path, rec.Payload)
		}
		pairs[[2]string{rec.Type, payload.Type}] = true
		if rec.Type == "session_meta" && payload.CliVersion != "" {
			versions = append(versions, payload.CliVersion)
		}
	}
	return pairs, versions
}

func TestCodexFixtureContract(t *testing.T) {
	testdata, err := filepath.Glob("testdata/*.jsonl")
	require.NoError(t, err)
	require.NotEmpty(t, testdata)

	canon := map[[2]string]bool{}
	for _, f := range testdata {
		pairs, versions := contractScan(t, f, false)
		for k := range pairs {
			canon[k] = true
		}
		for _, v := range versions {
			assert.Equalf(t, drift.TestedCodexVersion, v, "testdata %s stamps cli_version %q, want pinned %q", f, v, drift.TestedCodexVersion)
		}
	}
	require.NotEmpty(t, canon)

	fixtures, err := filepath.Glob("../../e2e/hermetic/prod/fixtures/*codex*.jsonl.tmpl")
	require.NoError(t, err)
	require.NotEmpty(t, fixtures)

	seen := map[[2]string]bool{}
	for _, f := range fixtures {
		pairs, versions := contractScan(t, f, true)
		require.NotEmptyf(t, pairs, "fixture %s produced no records", f)
		for k := range pairs {
			seen[k] = true
			assert.Truef(t, canon[k], "fixture %s emits record/payload type %v absent from ingest/codex/testdata canon", f, k)
		}
		for _, v := range versions {
			assert.Equalf(t, drift.TestedCodexVersion, v, "fixture %s stamps cli_version %q, want pinned %q", f, v, drift.TestedCodexVersion)
		}
	}
	require.NotEmpty(t, seen)
}
```

- [ ] **Step 2: Run the test to verify it fails on version-sync**

Run: `go test ./ingest/codex/ -run TestCodexFixtureContract -v`
Expected: FAIL — assertions of the form `... stamps cli_version "0.144.4", want pinned "0.144.5"` (15 `session_meta` lines across testdata + fixtures). The subset and non-vacuity assertions pass; only the version-sync ones fail.

- [ ] **Step 3: Bump `0.144.4` → `0.144.5` across the 19 files**

Run (from the worktree root):

```bash
grep -rl '0\.144\.4' \
  ingest/codex/testdata ingest/codex/codex_test.go \
  e2e/hermetic/prod/fixtures e2e/hermetic/prod/scenarios \
  | while IFS= read -r f; do
      LC_ALL=C sed -i '' -e 's/0\.144\.4/0.144.5/g' "$f"
    done
grep -rn '0\.144\.4' ingest/codex e2e/hermetic/prod || echo "no 0.144.4 remaining"
```

Expected: `no 0.144.4 remaining`. (On a Linux runner use `sed -i` without the `''` argument; on macOS keep `sed -i ''`.)

- [ ] **Step 4: Run the contract + existing codex unit tests to verify green**

Run: `go test ./ingest/codex/ -v`
Expected: PASS — `TestCodexFixtureContract` and every existing test in `codex_test.go` (the three `0.144.4`→`0.144.5` bumps keep its assertions aligned).

- [ ] **Step 5: Verify the hermetic codex scenarios still pass after the bump**

Run: `make build && CATACOMB_BIN="$PWD/bin/catacomb" e2e/hermetic/run.sh`
Expected: the run reaches `prod: all scenarios passed` — scenarios `55/56/58` (which assert `agent_version == "0.144.5"` after the bump) and `57/59` stay green.

- [ ] **Step 6: Coverage + format**

Run: `make cover && make fmt`
Expected: coverage gate passes at 100% (the new file is test-only); `gofumpt`/`goimports` report no changes.

- [ ] **Step 7: Commit**

```bash
git add ingest/codex/contract_test.go ingest/codex/codex_test.go ingest/codex/testdata \
        e2e/hermetic/prod/fixtures e2e/hermetic/prod/scenarios
git commit -m "test(codex): pin fake-fixture record shape + cli_version to 0.144.5 (contract test)"
```

---

## Task 2: deepeval seam — Level A hermetic author-mode scenario (F1)

**Files:**

- Create: `integrations/deepeval/tests/testdata/seam_session.jsonl`
- Create: `e2e/hermetic/prod/scenarios/85-deepeval-seam.sh`
- Modify: `e2e/hermetic/run.sh` (line 190 — the `PYTHONPATH` export)

**Interfaces:**

- Consumes: `catacomb export <transcript> --to jsonl --out <file>`; `python3 -m catacomb_deepeval <file>` (author mode: no `--expected`, zero deps, prints `{"input","actual_output","tools_called":[{"name",...}],"expected_tools":[]}`).
- Produces: the shared fixture `integrations/deepeval/tests/testdata/seam_session.jsonl`, reused by Task 3.

- [ ] **Step 1: Create the shared transcript fixture**

Create `integrations/deepeval/tests/testdata/seam_session.jsonl` (one JSON object per line, exactly):

```
{"type":"user","uuid":"u1","sessionId":"seam-session-0001","timestamp":"2026-07-18T10:00:00Z","message":{"role":"user","content":"go"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"seam-session-0001","timestamp":"2026-07-18T10:00:01Z","message":{"role":"assistant","id":"msg_1","model":"claude-opus-4-8","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}]}}
{"type":"user","uuid":"u2","parentUuid":"a1","sessionId":"seam-session-0001","timestamp":"2026-07-18T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"a.txt","is_error":false}]}}
{"type":"assistant","uuid":"a2","parentUuid":"u2","sessionId":"seam-session-0001","timestamp":"2026-07-18T10:00:03Z","message":{"role":"assistant","id":"msg_2","model":"claude-opus-4-8","content":[{"type":"tool_use","id":"toolu_2","name":"mcp__fs__read","input":{"path":"a.txt"}}]}}
{"type":"user","uuid":"u3","parentUuid":"a2","sessionId":"seam-session-0001","timestamp":"2026-07-18T10:00:04Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_2","content":"hello","is_error":false}]}}
{"type":"assistant","uuid":"a3","parentUuid":"u3","sessionId":"seam-session-0001","timestamp":"2026-07-18T10:00:05Z","message":{"role":"assistant","id":"msg_3","model":"claude-opus-4-8","content":[{"type":"text","text":"done"}]}}
```

- [ ] **Step 2: Add `integrations/deepeval/src` to the hermetic `PYTHONPATH`**

In `e2e/hermetic/run.sh`, line 190 currently reads:

```bash
export PYTHONPATH="$repo/integrations/verifier/src:$repo/integrations/judge/src${PYTHONPATH:+:$PYTHONPATH}"
```

Change it to:

```bash
export PYTHONPATH="$repo/integrations/verifier/src:$repo/integrations/judge/src:$repo/integrations/deepeval/src${PYTHONPATH:+:$PYTHONPATH}"
```

- [ ] **Step 3: Write the Level A scenario (the test)**

Create `e2e/hermetic/prod/scenarios/85-deepeval-seam.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
echo "== prod.85 deepeval-seam: catacomb export -> catacomb-deepeval author mode =="
w="$WORK/deepeval-seam"; mkdir -p "$w"
fixture="$REPO/integrations/deepeval/tests/testdata/seam_session.jsonl"
snap="$w/s.jsonl"
run_json 0 "$w/export.out" "export seam transcript -> jsonl snapshot" -- \
  catacomb export "$fixture" --to jsonl --out "$snap"
rc=0
python3 -m catacomb_deepeval "$snap" >"$w/adapter.json" 2>"$w/adapter.err" || rc=$?
record "$rc" "catacomb-deepeval author mode reads the export (exit 0)"
rc=0; python3 - "$w/adapter.json" <<'PY' || rc=$?
import json, sys
d = json.load(open(sys.argv[1]))
names = [t["name"] for t in d.get("tools_called", [])]
errs = []
if names != ["Bash", "mcp__fs__read"]:
    errs.append("tools_called names=%r want [Bash, mcp__fs__read]" % names)
if not d.get("input"):
    errs.append("input empty")
if not d.get("actual_output"):
    errs.append("actual_output empty")
if errs:
    print("\n".join(errs), file=sys.stderr); sys.exit(1)
PY
record "$rc" "export->adapter carries input, actual_output, tools_called [Bash, mcp__fs__read]"
```

- [ ] **Step 4: Run the hermetic suite and confirm scenario 85 passes**

Run: `make build && CATACOMB_BIN="$PWD/bin/catacomb" e2e/hermetic/run.sh`
Expected: `== prod.85 deepeval-seam ...` prints two `PASS` lines and the run ends `prod: all scenarios passed`.

- [ ] **Step 5: Commit**

```bash
git add integrations/deepeval/tests/testdata/seam_session.jsonl \
        e2e/hermetic/prod/scenarios/85-deepeval-seam.sh e2e/hermetic/run.sh
git commit -m "test(e2e): hermetic author-mode export->deepeval seam (scenario 85)"
```

---

## Task 3: deepeval seam — Level B full-metric CI job (F1)

**Files:**

- Create: `integrations/deepeval/tests/testdata/seam_expected_pass.json`
- Create: `integrations/deepeval/tests/testdata/seam_expected_fail.json`
- Modify: `.github/workflows/python-deepeval.yml`

**Interfaces:**

- Consumes: the Task 2 fixture; `catacomb-deepeval <export> --expected <file>` (exit 0 = PASS, exit 1 = FAIL) running the real `ToolCorrectnessMetric` (name-match, keyless).
- Produces: a CI job `seam`.

- [ ] **Step 1: Create the expected-tools files**

Create `integrations/deepeval/tests/testdata/seam_expected_pass.json`:

```json
["Bash", "mcp__fs__read"]
```

Create `integrations/deepeval/tests/testdata/seam_expected_fail.json`:

```json
["mcp__fs__write"]
```

- [ ] **Step 2: Verify the seam locally (the failing→passing check)**

Run (in a scratch venv; installs the heavyweight `deepeval` extra):

```bash
make build
python3 -m venv /tmp/seamvenv && . /tmp/seamvenv/bin/activate
pip install -e 'integrations/deepeval[deepeval]'
bin/catacomb export integrations/deepeval/tests/testdata/seam_session.jsonl --to jsonl --out /tmp/seam.jsonl
catacomb-deepeval /tmp/seam.jsonl --expected integrations/deepeval/tests/testdata/seam_expected_pass.json; echo "pass-exit=$?"
catacomb-deepeval /tmp/seam.jsonl --expected integrations/deepeval/tests/testdata/seam_expected_fail.json; echo "fail-exit=$?"
deactivate
```

Expected: `pass-exit=0` (score 1.000, `PASS`) and `fail-exit=1` (score 0.000, `FAIL`).

- [ ] **Step 3: Add the `seam` job to `python-deepeval.yml`**

Append this job under `jobs:` in `.github/workflows/python-deepeval.yml` (sibling of `test-deepeval`):

```yaml
  seam:
    name: export→deepeval seam (Go build + real metric)
    runs-on: ubuntu-latest
    timeout-minutes: 15
    env:
      DEEPEVAL_TELEMETRY_OPT_OUT: "YES"
    steps:
      - uses: actions/checkout@9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0 # v7.0.0
        with:
          persist-credentials: false
      - name: Set up Go
        uses: actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16 # v6.5.0
        with:
          go-version-file: go.mod
      - name: Set up Python 3.12
        uses: actions/setup-python@ece7cb06caefa5fff74198d8649806c4678c61a1 # v6.3.0
        with:
          python-version: "3.12"
      - name: Build catacomb
        run: make build
      - name: Install catacomb-deepeval with the deepeval extra
        run: pip install -e 'integrations/deepeval[deepeval]'
      - name: Export the seam fixture to a JSONL snapshot
        run: bin/catacomb export integrations/deepeval/tests/testdata/seam_session.jsonl --to jsonl --out seam.jsonl
      - name: PASS — every expected tool was called (exit 0)
        run: catacomb-deepeval seam.jsonl --expected integrations/deepeval/tests/testdata/seam_expected_pass.json
      - name: FAIL — an expected tool was never called (exit 1)
        run: |
          if catacomb-deepeval seam.jsonl --expected integrations/deepeval/tests/testdata/seam_expected_fail.json; then
            echo "::error::expected a FAIL (exit 1) from catacomb-deepeval but it exited 0" >&2
            exit 1
          fi
```

- [ ] **Step 4: Validate the workflow YAML**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/python-deepeval.yml')); print('yaml ok')"`
Expected: `yaml ok`.

- [ ] **Step 5: Commit**

```bash
git add integrations/deepeval/tests/testdata/seam_expected_pass.json \
        integrations/deepeval/tests/testdata/seam_expected_fail.json \
        .github/workflows/python-deepeval.yml
git commit -m "test(ci): full export->deepeval metric seam job (ToolCorrectness PASS/FAIL)"
```

---

## Self-review

**Spec coverage:**

- F1 Level A (per-PR hermetic author-mode) → Task 2. ✓
- F1 Level B (full metric in `python-deepeval.yml`) → Task 3. ✓
- F1 shared fixture + expected files → Task 2 (fixture) + Task 3 (expected). ✓
- F2 Go contract test (subset + version sync + non-vacuity) → Task 1 Step 1. ✓
- F2 version bump `0.144.4→0.144.5` (19 files) → Task 1 Step 3. ✓
- F2 coverage/no-comments/format → Task 1 Steps 6. ✓
- Item 3 (ad-hoc live gate) → out of this plan; handled at execution handoff with budget confirmation.
- Non-goal `--trace-metrics` → never invoked (Task 3 uses only `--expected`). ✓

**Placeholder scan:** No `TBD`/`TODO`/"handle edge cases"/"similar to". Every code step carries full content. ✓

**Type consistency:** `contractRecord{Type, Payload}`, `contractScan(t, path, substitute) (map[[2]string]bool, []string)`, `contractPlaceholderRE` — used consistently in Task 1. Fixture tool names `Bash` / `mcp__fs__read` match across the transcript (Task 2), the author-mode assertion (Task 2 Step 3), and `seam_expected_pass.json` (Task 3). The FAIL name `mcp__fs__write` is deliberately never called. ✓

**Known adjustment point:** if Task 1 Step 2 shows a *subset* failure (a fixture `(type,payload.type)` pair absent from testdata) rather than only version-sync failures, that is a genuine reference gap — add one representative real line for that pair to the matching `ingest/codex/testdata/*.jsonl` and re-run. (Verified at plan time: the current fixtures are already a subset, so only version-sync fails.)
