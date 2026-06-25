# W2 Task 12: TUI default-collapse parity — report

## What changed

### `tui/tree_model.go`

Added `collapsedByDefault(t string) bool` predicate: returns true iff `t == "assistant_turn" || t == "subagent"`.

Modified `seed()`: after building the graph, initialize `ts.expanded` by walking `BuildTree(g)` and pre-expanding every node that has children AND is not collapsed-by-default. `session` and `user_prompt` parents open by default; `assistant_turn` and `subagent` stay collapsed. This mirrors the web's `DEFAULT_COLLAPSE` policy exactly.

### `tui/tree_model_test.go`

Added two tests:

- `TestSeedDefaultExpandsSpineNotTurns` — verifies the new seed policy: spine nodes (`s`, `u`, `at`, `sub`) are visible; `t1` (child of `assistant_turn`) and `subc` (child of `subagent`) are hidden. This was the TDD red/green driver.
- `TestTreeEnterExpandsCollapsedByDefaultNode` — covers the Enter-on-collapsed-node path (lines 115–116 of tree_model.go) for a collapsed-by-default `assistant_turn` node. Added because the existing tests that previously exercised that branch (`TestTreeExpandRevealsChildren`) had to be updated to reflect the new default-open spine.

Updated existing tests that broke because `session` is now pre-expanded by `seed`:

| Test | Change | Justification |
|---|---|---|
| `TestTreeMoveAndClamp` | Assert cursor=1 after `j` (not 0); add second `j` to verify clamp | `session` pre-expanded → 2 visible rows; `j` moves to row 1 |
| `TestTreeExpandRevealsChildren` | Remove "not visible before Enter" assert; replace with h/l collapse-expand cycle | `tool_call` IS visible by default; Enter on pre-expanded `session` selects it instead of expanding |
| `TestTreeSpaceExpandCollapse` | Flip assertions: first Space collapses (hides), second expands (shows) | `session` starts expanded; Space toggles, so first press collapses |

### `tui/model_test.go`

Updated two tests that assumed Enter on `session` would expand it (old behavior):

| Test | Change | Justification |
|---|---|---|
| `TestTreeEnterSelectsNodeAndFocusesDetail` | Remove leading Enter; navigate via `j` then Enter | Enter on pre-expanded `session` selects it, shifts focus to detail, so subsequent `j` went to detail pane not tree |
| `TestTreeKeyDelegatedWhenFocusTree` | Remove Enter before `j` | Same root cause; `j` alone moves cursor to row 1 since `session` is pre-expanded |

## TDD red/green

- Red: `go test ./tui/ -run TestSeedDefaultExpandsSpineNotTurns` → `FAIL: spine not visible: map[s:true]`
- Implementation: added `collapsedByDefault` + seed loop
- Green: `go test ./tui/ -run TestSeedDefaultExpandsSpineNotTurns` → `PASS`

## Verification outputs

```
go test ./tui/ -v → PASS (all tests)
make cover → Total test coverage: 100% (3624/3624) — PASS
golangci-lint run --timeout=5m ./... → 0 issues
go build ./... → clean
internal/codepolicy → PASS (no comments added)
```
