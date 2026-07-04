# PR-N: Known stream-json system subtypes (F10) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop the PR-K drift counter from flagging system subtypes that Claude Code 2.1.199 ships today. The V-1 dogfood run (`docs/reviews/2026-07-04-dogfood-calibration.md` §6 F10) logged `format drift: … reason=unknown_subtype count=1100` on correct graphs because `ingest/streamjson` recognizes only the `init` and `compact_boundary` system subtypes. Grounded live (one `claude -p "say hi" --output-format stream-json --verbose --model haiku` run, 2026-07-04, claude 2.1.199, hooks installed): the stream emits system subtypes `init`, `hook_started`, `hook_response`, `thinking_tokens`. The three non-`init` ones become known-ignored; a genuinely unknown subtype must still count as drift.

**Architecture:** one function in one file. `ingest/streamjson/streamjson.go` `build` case `"system"` (`:97-108`) gains a `knownIgnoredSystemSubtype` helper beside the existing `knownAssistantBlock`/`knownUserBlock` enumerations — per ADR-0025's consequences, each parser owns its known-shape enumeration and extending it is the deliberate maintenance point. No observations are emitted for the new subtypes: hook lifecycle events already arrive first-class via the hook path, and token counts arrive via assistant usage and `result`. No `ingest/jsonl` parity change — `ingest/jsonl/jsonl.go:167` (`knownLineType`) accepts `type:"system"` transcript lines wholesale without subtype discrimination, so these subtypes never counted as drift there. No daemon, reducer, or docs changes (the guide documents reason buckets, not subtype lists).

**Tech Stack:** Go 1.26, testify, table-driven tests. Repo rules: NO comments in Go (codepolicy), 100% coverage, TDD, gofumpt via `make fmt`, deterministic outputs, every task ends with a commit.

## Global Constraints

- Work in this worktree only; never touch the shared checkout.
- No comments in Go code — none (`internal/codepolicy` fails the build otherwise).
- 100% test coverage (`make cover`); TDD: failing test first, minimal implementation, refactor under green.
- `make lint` must pass; `make fmt` before committing.
- Reason labels stay the five ADR-0025 buckets; subtype names never leak into counter keys or metrics.
- Known-ignored means silently dropped with zero drift AND zero observations — mirror the existing `compact_boundary` behavior exactly.

---

### Task 1: known-ignored system subtypes in `ingest/streamjson`

**Files:**

- Modify: `ingest/streamjson/streamjson.go` (`build` case `"system"`, new `knownIgnoredSystemSubtype` helper)
- Test: `ingest/streamjson/streamjson_test.go`

**Interfaces:**

- No signature changes. `Parse` keeps `([]model.Observation, drift.Counts, error)`; the daemon wiring from PR-K is untouched.

- [ ] **Step 1: Re-ground the subtype list**

Run (in a throwaway dir, cheap single haiku call):

```bash
cd "$(mktemp -d)" && claude -p "say hi" --output-format stream-json --verbose --model haiku 2>/dev/null \
  | jq -r 'select(.type=="system") | .subtype' | sort -u
```

Expected: `hook_response`, `hook_started`, `init`, `thinking_tokens` (the 2026-07-04 grounding). If the output contains additional subtypes, add them to the test table and the helper in the steps below, and record the final observed list in the commit message. If `claude` is unavailable, proceed with the three grounded subtypes as-is.

- [ ] **Step 2: Write failing tests**

Add to `ingest/streamjson/streamjson_test.go`, next to `TestParseCompactBoundarySubtypeNoDrift` (reuse the existing `seq()` helper; imports are already in place):

```go
func TestParseKnownSystemSubtypesNoDrift(t *testing.T) {
	for _, subtype := range []string{"hook_started", "hook_response", "thinking_tokens"} {
		obs, dc, err := Parse([]byte(`{"type":"system","subtype":"`+subtype+`","session_id":"s1"}`), "exec-1", seq())
		require.NoError(t, err, subtype)
		assert.Empty(t, obs, subtype)
		assert.Empty(t, dc, subtype)
	}
}
```

The genuinely-unknown guard already exists and must keep passing untouched — `TestParseUnknownSystemSubtypeCountsDrift` (`streamjson_test.go:323-327`) asserts subtype `warmup` yields `drift.Counts{drift.ReasonUnknownSubtype: 1}`. Do not modify it; it is this PR's proof that the counter stays meaningful.

- [ ] **Step 3: Run tests, verify the new one fails**

Run: `go test ./ingest/streamjson/ -run 'TestParseKnownSystemSubtypesNoDrift|TestParseUnknownSystemSubtypeCountsDrift|TestParseCompactBoundarySubtypeNoDrift' -v`
Expected: `TestParseKnownSystemSubtypesNoDrift` FAILS (drift counts returned for all three subtypes); the other two PASS.

- [ ] **Step 4: Implement**

In `ingest/streamjson/streamjson.go`, replace the tail of the `case "system":` block (`build`, currently lines 105-108):

```go
		if knownIgnoredSystemSubtype(e.Subtype) {
			return nil, nil, nil
		}
		return nil, drift.Counts{drift.ReasonUnknownSubtype: 1}, nil
```

(the `if e.Subtype == "compact_boundary"` branch is deleted — it folds into the helper; the `init` branch above it is unchanged). Add the helper at the bottom of the file, beside `knownAssistantBlock`/`knownUserBlock`:

```go
func knownIgnoredSystemSubtype(t string) bool {
	switch t {
	case "compact_boundary", "hook_started", "hook_response", "thinking_tokens":
		return true
	default:
		return false
	}
}
```

Extend the case list with any additional subtypes observed in Step 1. Both helper branches are covered: `true` by `TestParseKnownSystemSubtypesNoDrift` + `TestParseCompactBoundarySubtypeNoDrift`, `false` by `TestParseUnknownSystemSubtypeCountsDrift`.

- [ ] **Step 5: Green + commit**

Run: `go test -race ./ingest/streamjson/ && go test -race ./ingest/streamjson/ -cover`
Expected: PASS, coverage 100.0%.

```bash
git add ingest/streamjson/
git commit -m "fix(ingest/streamjson): hook_started, hook_response, thinking_tokens are known system subtypes, not drift (F10)"
```

---

### Task 2: Full gates + live verify

**Files:** none new (verification only).

- [ ] **Step 1: Full gates**

Run: `make fmt && make lint && make cover && npx -y markdownlint-cli@0.49.0 'docs/**/*.md'`
Expected: fmt clean, lint 0 issues, coverage total/package/file 100%, markdownlint 0 errors (docs untouched; the plan files must still pass).

- [ ] **Step 2: Live verify against a throwaway daemon**

```bash
make build
TMP=$(mktemp -d)
bin/catacomb daemon --db "$TMP/cat.db" --discovery "$TMP/disc.json" > "$TMP/daemon.log" 2>&1 &
DPID=$!
until [ -f "$TMP/disc.json" ]; do sleep 0.2; done
ADDR=$(python3 -c "import json;print(json.load(open('$TMP/disc.json'))['addr'])")
TOKEN=$(python3 -c "import json;print(json.load(open('$TMP/disc.json'))['token'])")
printf '%s\n%s\n%s\n%s\n' \
  '{"type":"system","subtype":"hook_started","session_id":"subtype-live"}' \
  '{"type":"system","subtype":"hook_response","session_id":"subtype-live"}' \
  '{"type":"system","subtype":"thinking_tokens","session_id":"subtype-live"}' \
  '{"type":"system","subtype":"warmup","session_id":"subtype-live"}' \
  | curl -s -X POST "http://$ADDR/v1/stream-json" -H "Authorization: Bearer $TOKEN" --data-binary @-
curl -s "http://$ADDR/metrics"
grep -c "format drift" "$TMP/daemon.log"
kill $DPID
```

Expected:

- `/metrics` drift shows exactly `"stream_json/unknown_subtype":1` (the `warmup` line only) — no counts for the three shipped subtypes.
- `daemon.log` contains exactly 1 `format drift` warning.

Optional (network + credits): rerun the Step 1 grounding command through `bin/catacomb run --` with hooks installed and confirm the daemon log carries zero `unknown_subtype` warnings on real traffic.

Capture the `/metrics` body and the grep count in the PR description.

- [ ] **Step 3: Commit (if gates changed anything)**

`make fmt` output only; if the tree is dirty:

```bash
git add -A && git commit -m "chore: gofumpt after PR-N drift subtype fix"
```
