# Skill Nodes (`NodeSkill`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a first-class `NodeSkill` node type so skill invocations (`Skill` / `SlashCommand` tool calls) are detected, named after the actual skill, and displayed distinctly everywhere `mcp_call` already is.

**Architecture:** Ingestion is untouched — skills already arrive as `assistant_tool_use` observations with `attrs["name"] ∈ {"Skill","SlashCommand"}` and the tool input in `payload.Input`. All new logic lives in `reduce` (detection + name extraction), with parity edits mirroring the existing `NodeMCPCall` handling across step keys, four exporters, daemon stats, the web UI, and the TUI.

**Tech Stack:** Go (model/reduce/exports/daemon/tui), TypeScript + Svelte + Vitest (webui), CSS (oklch design tokens).

## Global Constraints

- **No comments in Go code** — none, not even doc comments. Only `//go:*` directives allowed. Enforced by `internal/codepolicy`.
- **100% test coverage, TDD-first.** Write the failing test before the implementation. Verified by `make cover` against `.testcoverage.yml`. The threshold never goes down.
- **Work stays in this worktree** (`.claude/worktrees/skill-nodes`, branch `worktree-skill-nodes`). Never edit the shared checkout.
- Go tests run with `-race`. Table-driven tests are the default; `testify/require` for fatal assertions, `testify/assert` otherwise.
- Verification commands: Go = `make cover`; no-comments = `go test ./internal/codepolicy/`; lint = `make lint`; web = `make web-test` and `make web-check`.

---

### Task 1: `NodeSkill` model type + reduce detection + name extraction

This is the keystone. Every later task depends on `model.NodeSkill` existing.

**Files:**
- Modify: `model/model.go` (NodeType const block, ~line 25)
- Modify: `reduce/reduce.go` (`applyTool`, ~lines 147-163)
- Create: `reduce/skill.go`
- Create: `reduce/skill_test.go`

**Interfaces:**
- Produces: `model.NodeSkill NodeType = "skill"`; `isSkill(name string) bool`; `extractSkillName(o model.Observation) string`; `toolDisplayName(o model.Observation, name string) string` (all in package `reduce`, except the const in `model`).
- Consumes: existing `ob(kind, toolUse string, ts time.Time) model.Observation` helper and `execID` const from `reduce/reduce_test.go`.

- [ ] **Step 1: Add the model const (no test needed for a const; covered transitively).**

In `model/model.go`, add `NodeSkill` right after `NodeMCPCall`:

```go
	NodeMCPCall       NodeType = "mcp_call"
	NodeSkill         NodeType = "skill"
	NodeHookEvent     NodeType = "hook_event"
```

- [ ] **Step 2: Write the failing tests** in new file `reduce/skill_test.go`:

```go
package reduce

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/realkarych/catacomb/model"
)

func TestSkillNodeType(t *testing.T) {
	use := ob("assistant_tool_use", "toolu_s1", time.Unix(0, 0).UTC())
	use.Attrs = map[string]any{"name": "Skill"}
	use.Payload = &model.Payload{Input: json.RawMessage(`{"skill":"superpowers:brainstorming"}`)}
	g := NewGraph()
	g.Apply(use)
	n := g.Nodes[model.ToolCallID(execID, "toolu_s1")]
	assert.Equal(t, model.NodeSkill, n.Type)
	assert.Equal(t, "superpowers:brainstorming", n.Name)
}

func TestSlashCommandNodeTypeAndCleanName(t *testing.T) {
	use := ob("assistant_tool_use", "toolu_s2", time.Unix(0, 0).UTC())
	use.Attrs = map[string]any{"name": "SlashCommand"}
	use.Payload = &model.Payload{Input: json.RawMessage(`{"command":"/code-review high"}`)}
	g := NewGraph()
	g.Apply(use)
	n := g.Nodes[model.ToolCallID(execID, "toolu_s2")]
	assert.Equal(t, model.NodeSkill, n.Type)
	assert.Equal(t, "code-review", n.Name)
}

func TestSkillNameFallbackNoPayload(t *testing.T) {
	use := ob("assistant_tool_use", "toolu_s3", time.Unix(0, 0).UTC())
	use.Attrs = map[string]any{"name": "Skill"}
	g := NewGraph()
	g.Apply(use)
	n := g.Nodes[model.ToolCallID(execID, "toolu_s3")]
	assert.Equal(t, model.NodeSkill, n.Type)
	assert.Equal(t, "Skill", n.Name)
}

func TestSkillNameFallbackBadJSON(t *testing.T) {
	use := ob("assistant_tool_use", "toolu_s4", time.Unix(0, 0).UTC())
	use.Attrs = map[string]any{"name": "Skill"}
	use.Payload = &model.Payload{Input: json.RawMessage(`{bad`)}
	g := NewGraph()
	g.Apply(use)
	n := g.Nodes[model.ToolCallID(execID, "toolu_s4")]
	assert.Equal(t, "Skill", n.Name)
}

func TestSkillTypeUpgradeReversedOrder(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 1, 0, time.UTC)
	t1 := t0.Add(time.Second)
	res := ob("tool_result", "toolu_s5", t0)
	res.Attrs = map[string]any{"status": string(model.StatusOK)}
	use := ob("assistant_tool_use", "toolu_s5", t1)
	use.Attrs = map[string]any{"name": "Skill"}
	use.Payload = &model.Payload{Input: json.RawMessage(`{"skill":"verify"}`)}
	g := NewGraph()
	g.ApplyAll([]model.Observation{res, use})
	n := g.Nodes[model.ToolCallID(execID, "toolu_s5")]
	assert.Equal(t, model.NodeSkill, n.Type)
	assert.Equal(t, "verify", n.Name)
}

func TestSlashCommandEmptyCommandFallback(t *testing.T) {
	use := ob("assistant_tool_use", "toolu_s6", time.Unix(0, 0).UTC())
	use.Attrs = map[string]any{"name": "SlashCommand"}
	use.Payload = &model.Payload{Input: json.RawMessage(`{"command":""}`)}
	g := NewGraph()
	g.Apply(use)
	n := g.Nodes[model.ToolCallID(execID, "toolu_s6")]
	assert.Equal(t, "SlashCommand", n.Name)
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./reduce/ -run 'Skill|SlashCommand' -race`
Expected: FAIL — `isSkill` / `extractSkillName` undefined, and `NodeSkill` not produced.

- [ ] **Step 4: Create `reduce/skill.go`** (no comments):

```go
package reduce

import (
	"encoding/json"
	"strings"

	"github.com/realkarych/catacomb/model"
)

func isSkill(name string) bool {
	return name == "Skill" || name == "SlashCommand"
}

type skillToolInput struct {
	Skill   string `json:"skill"`
	Command string `json:"command"`
}

func extractSkillName(o model.Observation) string {
	if o.Payload == nil || len(o.Payload.Input) == 0 {
		return ""
	}
	var in skillToolInput
	if err := json.Unmarshal(o.Payload.Input, &in); err != nil {
		return ""
	}
	if in.Skill != "" {
		return in.Skill
	}
	return cleanCommand(in.Command)
}

func cleanCommand(cmd string) string {
	cmd = strings.TrimPrefix(strings.TrimSpace(cmd), "/")
	if i := strings.IndexAny(cmd, " \t"); i >= 0 {
		cmd = cmd[:i]
	}
	return cmd
}

func toolDisplayName(o model.Observation, name string) string {
	if isSkill(name) {
		if sn := extractSkillName(o); sn != "" {
			return sn
		}
	}
	return name
}
```

- [ ] **Step 5: Wire detection + naming into `reduce/reduce.go` `applyTool`.**

Replace the node-type selection block (currently):

```go
	nodeType := model.NodeToolCall
	if isMCP(name) {
		nodeType = model.NodeMCPCall
	}
	n := g.node(id, o.RunID, nodeType)
	if n.Type == model.NodeToolCall && nodeType == model.NodeMCPCall {
		n.Type = model.NodeMCPCall
	}
```

with:

```go
	nodeType := model.NodeToolCall
	switch {
	case isMCP(name):
		nodeType = model.NodeMCPCall
	case isSkill(name):
		nodeType = model.NodeSkill
	}
	n := g.node(id, o.RunID, nodeType)
	if n.Type == model.NodeToolCall && nodeType != model.NodeToolCall {
		n.Type = nodeType
	}
```

Then replace the name-setting block (currently):

```go
	if name, ok := o.Attrs["name"].(string); ok {
		g.setName(n, o, name)
	}
```

with:

```go
	if dn := toolDisplayName(o, name); dn != "" {
		g.setName(n, o, dn)
	}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./reduce/ -run 'Skill|SlashCommand|MCP|ApplyTool' -race`
Expected: PASS (existing MCP/upgrade tests must still pass — the generalized guard preserves their behavior).

- [ ] **Step 7: Full package coverage check**

Run: `go test ./reduce/ ./model/ -race -coverprofile=/dev/null`
Then `make cover` to confirm the 100% gate still holds for the touched packages.
Expected: PASS, no coverage regression.

- [ ] **Step 8: Commit**

```bash
git add model/model.go reduce/reduce.go reduce/skill.go reduce/skill_test.go
git commit -m "feat(reduce): first-class NodeSkill for Skill/SlashCommand tool calls

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: Step-key eligibility for skills

**Files:**
- Modify: `stepkey/stepkey.go` (`eligible`, ~line 29)
- Modify: `stepkey/stepkey_test.go` (`TestEligibleTypes`, ~line 308)

**Interfaces:**
- Consumes: `model.NodeSkill` (Task 1).

- [ ] **Step 1: Extend the failing test** — add to `TestEligibleTypes`:

```go
	assert.True(t, eligible(model.NodeSkill))
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./stepkey/ -run TestEligibleTypes -race`
Expected: FAIL — `eligible(model.NodeSkill)` returns false.

- [ ] **Step 3: Add `NodeSkill` to `eligible`:**

```go
	case model.NodeToolCall, model.NodeMCPCall, model.NodeSkill, model.NodeSubagent:
		return true
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./stepkey/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add stepkey/stepkey.go stepkey/stepkey_test.go
git commit -m "feat(stepkey): skills are eligible for step-key matching

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: Exporter parity (neo4j, otlp, evalview, agentevals)

**Files:**
- Modify: `export/neo4j/export.go` (`nodeLabel`, ~line 104) + `export/neo4j/export_test.go` (`TestNodeLabelMapping`, ~line 112)
- Modify: `export/otlp/export.go` (`openInferenceKind`, ~line 179) + `export/otlp/export_test.go` (`TestOpenInferenceKindTable`, ~line 216)
- Modify: `export/evalview/export.go` (`spanType` ~line 131, `nodeToSpan` tool case ~line 115) + `export/evalview/export_test.go` (`TestSpanTypeMapping`, ~line 119)
- Modify: `export/agentevals/export.go` (both switch cases, ~lines 54 and 72) + `export/agentevals/export_test.go`

**Interfaces:**
- Consumes: `model.NodeSkill` (Task 1).

- [ ] **Step 1: Write failing exporter test rows.**

In `export/neo4j/export_test.go` `TestNodeLabelMapping`, add the skill case to the table (mirror the existing `{model.NodeMCPCall, "McpCall"}` entry):

```go
		{model.NodeSkill, "Skill"},
```

In `export/otlp/export_test.go` `TestOpenInferenceKindTable`, add (mirroring `{model.NodeMCPCall, "TOOL"}`):

```go
		{model.NodeSkill, "TOOL"},
```

In `export/evalview/export_test.go` `TestSpanTypeMapping`, add (mirroring `{model.NodeMCPCall, "mcp"}`):

```go
		{model.NodeSkill, "skill"},
```

In `export/agentevals/export_test.go`, locate the test that builds a node with `model.NodeMCPCall` and asserts a `"tool"` role message; add a sibling node with `model.NodeSkill` (distinct ID/parent) and assert it also yields a `Role: "tool"` message. Follow the exact construction already used for the mcp_call node in that test.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./export/neo4j/ ./export/otlp/ ./export/evalview/ ./export/agentevals/ -race`
Expected: FAIL — skill falls through to the default mappings ("Marker"/"CHAIN"/"agent") and agentevals drops the skill node.

- [ ] **Step 3: Implement neo4j label** — in `nodeLabel`, add before `default`:

```go
	case model.NodeSkill:
		return "Skill"
```

- [ ] **Step 4: Implement otlp kind** — add `NodeSkill` to the TOOL case:

```go
	case model.NodeToolCall, model.NodeMCPCall, model.NodeSkill:
		return "TOOL"
```

- [ ] **Step 5: Implement evalview** — in `spanType`, add before `default`:

```go
	case model.NodeSkill:
		return "skill"
```

and add `NodeSkill` to the tool case in `nodeToSpan`:

```go
	case model.NodeToolCall, model.NodeMCPCall, model.NodeSkill:
		s.Tool = &toolInfo{
			ToolName:        n.Name,
			ToolArgsBytes:   redactedLen(payloadInput(n)),
			ToolResultBytes: redactedLen(payloadOutput(n)),
			ToolSuccess:     n.Status == model.StatusOK,
		}
```

- [ ] **Step 6: Implement agentevals** — add `model.NodeSkill` to BOTH switch cases (the `topLevel` collection ~line 54 and the message-building switch ~line 72):

```go
	case model.NodeToolCall, model.NodeMCPCall, model.NodeSkill:
```

(apply to both occurrences).

- [ ] **Step 7: Run to verify they pass**

Run: `go test ./export/... -race`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add export/neo4j export/otlp export/evalview export/agentevals
git commit -m "feat(export): map NodeSkill across neo4j/otlp/evalview/agentevals

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: Daemon session stats count skills as tools

**Files:**
- Modify: `daemon/sessions.go` (both `ToolCount` sites, ~lines 195 and 321)
- Modify: `daemon/sessions_test.go` (mirror an existing `ToolCount` assertion, ~lines 77 / 912)

**Interfaces:**
- Consumes: `model.NodeSkill` (Task 1).

- [ ] **Step 1: Write the failing test.** In `daemon/sessions_test.go`, find the test that constructs a graph with a tool node and asserts `ToolCount`. Add a `model.NodeSkill` node to that graph fixture (unique ID, attached like the existing tool node) and bump the expected `ToolCount` by 1. If the simplest existing target is the summary test at ~line 912 (`assert.Equal(t, 1, sum.ToolCount)`), add one skill node and change the expectation to `2`. Mirror the exact node construction already used in that test.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./daemon/ -run ToolCount -race` (or the specific test name you edited)
Expected: FAIL — skill node is not counted, expectation off by one.

- [ ] **Step 3: Add `NodeSkill` to both `ToolCount` conditions** (~line 195 and ~line 321):

```go
			if n.Type == model.NodeToolCall || n.Type == model.NodeMCPCall || n.Type == model.NodeSkill {
				sum.ToolCount++
			}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./daemon/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add daemon/sessions.go daemon/sessions_test.go
git commit -m "feat(daemon): count skill nodes toward session ToolCount

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: TUI label for skills

**Files:**
- Modify: `tui/vocab.go` (`NodeTypeLabel`, ~line 15)
- Modify: `tui/vocab_test.go` (`TestNodeTypeLabel`, ~line 9)

**Interfaces:**
- Consumes: none beyond the string `"skill"` node type.

- [ ] **Step 1: Write the failing test** — add to `TestNodeTypeLabel` (mirror the `mcp_call` row):

```go
	assert.Equal(t, "skill", NodeTypeLabel("skill"))
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./tui/ -run TestNodeTypeLabel -race`
Expected: FAIL — `"skill"` falls through to `default` and returns `"marker"`.

- [ ] **Step 3: Add the case** in `NodeTypeLabel`, before `default`:

```go
	case "skill":
		return "skill"
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./tui/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add tui/vocab.go tui/vocab_test.go
git commit -m "feat(tui): label skill nodes

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: Web UI — legend, color token, outline label, conversation grouping

**Files:**
- Modify: `webui/web/src/lib/node-legend.ts` (`NODE_TYPE_MAP`, ~line 12)
- Modify: `webui/web/src/lib/node-legend.test.ts` (~lines 25, 72)
- Modify: `webui/web/style.css` (dark ~line 34, light ~line 101)
- Modify: `webui/web/src/lib/graph/outline.ts` (`outlineLabel`, ~line 76)
- Modify: `webui/web/src/lib/graph/outline.test.ts` (~line 207)
- Modify: `webui/web/src/lib/conversation.ts` (`isToolNode`, ~line 6)
- Modify: `webui/web/src/lib/conversation.test.ts` (`isToolNode` cases)

**Interfaces:**
- Produces (CSS): `--node-skill` token. Consumes: `'skill'` node type string.

- [ ] **Step 1: Write the failing tests.**

In `node-legend.test.ts`, add a `nodeTypeInfo` case (mirror the mcp_call one):

```ts
  it('returns correct token and label for skill', () => {
    expect(nodeTypeInfo('skill')).toEqual({ token: '--node-skill', label: 'skill' });
  });
```

and update the "all known types" test to include skill and expect nine:

```ts
  it('handles all nine known types without duplicates', () => {
    const all = ['session', 'user_prompt', 'assistant_turn', 'tool_call', 'subagent', 'mcp_call', 'skill', 'hook_event', 'marker'];
    const result = presentNodeTypes(all);
    expect(result).toHaveLength(9);
  });
```

In `outline.test.ts`, add (mirror the mcp_call block):

```ts
  it('skill: uses node.name or falls back to "skill"', () => {
    expect(outlineLabel(n('sk', 'skill', { name: 'verify' }))).toEqual({
      primary: 'verify',
      secondary: '',
    });
    expect(outlineLabel(n('sk', 'skill'))).toEqual({ primary: 'skill', secondary: '' });
  });
```

In `conversation.test.ts`, add an `isToolNode` assertion mirroring the existing mcp_call one:

```ts
  expect(isToolNode('skill')).toBe(true);
```

- [ ] **Step 2: Run to verify they fail**

Run: `make web-test`
Expected: FAIL — `nodeTypeInfo('skill')` returns the marker fallback; `outlineLabel` for skill hits the default; `isToolNode('skill')` is false.

- [ ] **Step 3: Add the legend entry** in `node-legend.ts`, after the `mcp_call` line:

```ts
  skill: { token: '--node-skill', label: 'skill' },
```

- [ ] **Step 4: Add the CSS tokens** in `style.css`. In the dark block, after `--node-mcp_call` (~line 34):

```css
  --node-skill: oklch(0.74 0.12 25);
```

In the light block, after `--node-mcp_call` (~line 101):

```css
    --node-skill: oklch(0.40 0.12 25);
```

- [ ] **Step 5: Add the outline case** in `outline.ts`, after the `mcp_call` case:

```ts
    case 'skill':
      return { primary: node.name || 'skill', secondary: '' };
```

- [ ] **Step 6: Extend `isToolNode`** in `conversation.ts`:

```ts
export function isToolNode(nodeType: string): boolean {
  return nodeType === 'tool_call' || nodeType === 'mcp_call' || nodeType === 'skill';
}
```

- [ ] **Step 7: Run to verify they pass + typecheck**

Run: `make web-test` then `make web-check`
Expected: PASS (tests green, no type/lint errors).

- [ ] **Step 8: Commit**

```bash
git add webui/web/src/lib/node-legend.ts webui/web/src/lib/node-legend.test.ts webui/web/style.css webui/web/src/lib/graph/outline.ts webui/web/src/lib/graph/outline.test.ts webui/web/src/lib/conversation.ts webui/web/src/lib/conversation.test.ts
git commit -m "feat(webui): first-class skill node — legend, color, outline, conversation

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 7: End-to-end JSONL fixture

Proves the full claim: a `Skill` tool call ingested from a transcript reduces to a `NodeSkill` named after the skill.

**Files:**
- Create: `ingest/jsonl/testdata/skill.jsonl`
- Modify: `ingest/jsonl/jsonl_test.go` (add one test, mirror `TestSubagentTranscriptBuildsNodeAndEdge` ~line 295)

**Interfaces:**
- Consumes: `ParseReader`, `reduce.NewGraph`, `model.NodeSkill`, `model.ToolCallID`.

- [ ] **Step 1: Create the fixture** `ingest/jsonl/testdata/skill.jsonl` (single line):

```json
{"type":"assistant","uuid":"a1","sessionId":"s1","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"msg_1","model":"claude-opus-4-8","content":[{"type":"tool_use","id":"toolu_sk","name":"Skill","input":{"skill":"superpowers:verify"}}]}}
```

- [ ] **Step 2: Write the failing test** in `ingest/jsonl/jsonl_test.go`:

```go
func TestSkillToolReducesToNodeSkill(t *testing.T) {
	f, err := os.Open("testdata/skill.jsonl")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	obs, err := ParseReader(f, "e1")
	require.NoError(t, err)
	g := reduce.NewGraph()
	g.ApplyAll(obs)
	n := g.Nodes[model.ToolCallID("e1", "toolu_sk")]
	require.NotNil(t, n)
	assert.Equal(t, model.NodeSkill, n.Type)
	assert.Equal(t, "superpowers:verify", n.Name)
}
```

- [ ] **Step 3: Run to verify it passes** (Task 1's reduce logic already makes this green; this guards the full pipeline against regression):

Run: `go test ./ingest/jsonl/ -run TestSkillToolReducesToNodeSkill -race`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add ingest/jsonl/testdata/skill.jsonl ingest/jsonl/jsonl_test.go
git commit -m "test(jsonl): end-to-end Skill tool call reduces to NodeSkill

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Final verification (after all tasks)

- [ ] Run the full Go gate: `make cover`
- [ ] Run the no-comments policy: `go test ./internal/codepolicy/`
- [ ] Run lint: `make lint`
- [ ] Run web tests + typecheck: `make web-test && make web-check`
- [ ] Confirm all green, then proceed to branch finish (PR) via the finishing-a-development-branch skill.

## Coverage notes

- New executable statements requiring fresh tests: `reduce/skill.go` (all funcs — covered by Task 1), the reduce `applyTool` switch/upgrade (Task 1), neo4j `case NodeSkill` (Task 3), evalview `spanType` `case NodeSkill` (Task 3), tui `case "skill"` (Task 5), the TS legend/outline/conversation branches (Task 6).
- Edits that add a label to an existing `case`/`||` (otlp, evalview `nodeToSpan`, agentevals, daemon, stepkey) create no new statements, so coverage cannot regress — the accompanying tests are for behavioral correctness, per TDD.
- CSS tokens are not under the coverage gate.
