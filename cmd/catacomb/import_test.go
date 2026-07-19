package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/evidence"
	"github.com/realkarych/catacomb/ingest/drift"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
)

var (
	fixtureTranscriptStart = time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	fixtureTranscriptEnd   = time.Date(2026, 6, 20, 10, 0, 4, 0, time.UTC)
)

func writeImportBasket(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "basket.yaml")
	require.NoError(t, os.WriteFile(p, []byte(`basket: checkout
reps: 1
tasks:
  - id: add-item
    cmd: ["claude", "-p", "add an item", "--output-format", "stream-json"]
    checkpoints: ["phase:cart"]
variants:
  - id: trunk
  - id: patched
`), 0o600))
	return p
}

func TestImportRequiresSessionXorTranscript(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, runsDir: dir, projectsDir: dir,
	})
	require.ErrorIs(t, err, errImportInput)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
	assert.Empty(t, out.String())
}

func TestImportRejectsBothInputs(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, sessionID: "s1", transcript: "x.jsonl", runsDir: dir, projectsDir: dir,
	})
	require.ErrorIs(t, err, errImportInput)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
	assert.Empty(t, out.String())
}

func TestImportUnknownTask(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "nope", variant: "trunk", rep: 1, sessionID: "s1", runsDir: dir, projectsDir: dir,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task")
}

func TestImportUnknownVariant(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "nope", rep: 1, sessionID: "s1", runsDir: dir, projectsDir: dir,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "variant")
}

func TestImportBadBasket(t *testing.T) {
	dir := t.TempDir()
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, filepath.Join(dir, "missing.yaml"), importFlags{
		task: "add-item", variant: "trunk", rep: 1, sessionID: "s1", runsDir: dir, projectsDir: dir,
	})
	require.Error(t, err)
}

func TestImportCommandSessionNotFound(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"import", basket,
		"--task", "add-item", "--variant", "trunk", "--session-id", "s1",
		"--runs-dir", dir, "--projects-dir", dir,
	}, &stdout, &stderr)
	require.Equal(t, 2, code, stderr.String())
	assert.Contains(t, stderr.String(), "no transcript for session s1")
}

func stageTranscript(t *testing.T, projects, sid string) {
	t.Helper()
	dst := filepath.Join(projects, "proj", sid+".jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	data, err := os.ReadFile("testdata/session.jsonl")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(dst, data, 0o600))
}

func TestImportTranscriptsBySessionID(t *testing.T) {
	projects := t.TempDir()
	stageTranscript(t, projects, "sess-123")
	ts, sid, err := importTranscripts(drift.RuntimeClaudeCode, importFlags{sessionID: "sess-123", projectsDir: projects})
	require.NoError(t, err)
	assert.Equal(t, "sess-123", sid)
	assert.Equal(t, filepath.Join(projects, "proj", "sess-123.jsonl"), ts.Main)
	assert.Empty(t, ts.Subagents)
}

func TestImportTranscriptsByPathCollectsSubagentsInSortedOrder(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "sess-abc.jsonl")
	data, err := os.ReadFile("testdata/session.jsonl")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(main, data, 0o600))
	subDir := filepath.Join(dir, "sess-abc", "subagents")
	require.NoError(t, os.MkdirAll(subDir, 0o750))
	for _, name := range []string{"agent-003.jsonl", "agent-001.jsonl", "agent-002.jsonl", "notes.txt"} {
		require.NoError(t, os.WriteFile(filepath.Join(subDir, name), data, 0o600))
	}

	ts, sid, err := importTranscripts(drift.RuntimeClaudeCode, importFlags{transcript: main})
	require.NoError(t, err)
	assert.Equal(t, "sess-abc", sid)
	assert.Equal(t, []string{
		filepath.Join(subDir, "agent-001.jsonl"),
		filepath.Join(subDir, "agent-002.jsonl"),
		filepath.Join(subDir, "agent-003.jsonl"),
	}, ts.Subagents)
}

func TestImportTranscriptsBySessionIDNotFound(t *testing.T) {
	projects := t.TempDir()
	_, _, err := importTranscripts(drift.RuntimeClaudeCode, importFlags{sessionID: "missing", projectsDir: projects})
	require.Error(t, err)
}

func TestImportTranscriptsByPath(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "sess-abc.jsonl")
	data, err := os.ReadFile("testdata/session.jsonl")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(main, data, 0o600))
	ts, sid, err := importTranscripts(drift.RuntimeClaudeCode, importFlags{transcript: main})
	require.NoError(t, err)
	assert.Equal(t, "sess-abc", sid)
	assert.Equal(t, main, ts.Main)
}

func TestImportTranscriptsByPathMissing(t *testing.T) {
	_, _, err := importTranscripts(drift.RuntimeClaudeCode, importFlags{transcript: filepath.Join(t.TempDir(), "nope.jsonl")})
	require.Error(t, err)
}

func TestImportTranscriptsByPathBadSubagentGlob(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "bad[.jsonl")
	require.NoError(t, os.WriteFile(main, []byte("{}\n"), 0o600))
	_, _, err := importTranscripts(drift.RuntimeClaudeCode, importFlags{transcript: main})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subagents")
}

func TestImportWritesEvidence(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	projects := filepath.Join(dir, "projects")
	stageTranscript(t, projects, "sess-xyz")
	runs := filepath.Join(dir, "runs")
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, sessionID: "sess-xyz",
		projectsDir: projects, runsDir: runs,
	})
	require.NoError(t, err)
	metaPath := filepath.Join(runs, "import-checkout-add-item-trunk-r1", "meta.json")
	require.FileExists(t, metaPath)
	require.FileExists(t, filepath.Join(runs, "import-checkout-add-item-trunk-r1", "session.jsonl"))
}

func TestImportMetaShape(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	projects := filepath.Join(dir, "projects")
	stageTranscript(t, projects, "sess-xyz")
	runs := filepath.Join(dir, "runs")
	var out, errb bytes.Buffer
	require.NoError(t, runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "patched", rep: 2, sessionID: "sess-xyz",
		projectsDir: projects, runsDir: runs,
	}))
	m, err := evidence.ReadMeta(filepath.Join(runs, "import-checkout-add-item-patched-r2"))
	require.NoError(t, err)
	assert.Equal(t, "task:add-item", m.MarkerName)
	assert.Equal(t, "patched", m.Labels["variant"])
	assert.Equal(t, "checkout", m.Labels["basket"])
	assert.Equal(t, "2", m.Labels["rep"])
	assert.Nil(t, m.CostUSD)
	assert.Equal(t, "import-checkout-add-item-patched-r2", m.RunID)
	assert.Equal(t, "add-item", m.Task)
	assert.Equal(t, "patched", m.Variant)
	assert.Equal(t, 2, m.Rep)
	assert.Equal(t, "sess-xyz", m.SessionID)
	assert.Equal(t, 0, m.ExitCode)
	assert.True(t, m.MarkerStart.Equal(fixtureTranscriptStart), m.MarkerStart)
	assert.True(t, m.MarkerEnd.Equal(fixtureTranscriptEnd), m.MarkerEnd)
	assert.Equal(t, time.UTC, m.MarkerStart.Location())
	assert.Equal(t, time.UTC, m.MarkerEnd.Location())
}

func TestImportRunIDOverrideNamesTheDirAndTheMetaRunID(t *testing.T) {
	for _, id := range []string{"manual-1", "a.b", "run_2026-07-19", "..hidden", "x..y", "UPPER"} {
		t.Run(id, func(t *testing.T) {
			dir := t.TempDir()
			basket := writeImportBasket(t, dir)
			projects := filepath.Join(dir, "projects")
			stageTranscript(t, projects, "sess-xyz")
			runs := filepath.Join(dir, "runs")
			var out, errb bytes.Buffer
			require.NoError(t, runImport(context.Background(), &out, &errb, basket, importFlags{
				task: "add-item", variant: "trunk", rep: 1, sessionID: "sess-xyz", runID: id,
				projectsDir: projects, runsDir: runs,
			}))

			m, err := evidence.ReadMeta(filepath.Join(runs, id))
			require.NoError(t, err)
			assert.Equal(t, id, m.RunID)
			assert.Equal(t, []string{id}, runDirNames(t, runs))
			assert.Equal(t, fmt.Sprintf("import %s: %s\n", id, filepath.Join(runs, id)), out.String())
		})
	}
}

func TestImportRejectsNonLocalRunID(t *testing.T) {
	for _, id := range []string{"..", "../evil", ".", "./nested", "a/b", "not-clean/./x"} {
		t.Run(fmt.Sprintf("id %q", id), func(t *testing.T) {
			dir := t.TempDir()
			basket := writeImportBasket(t, dir)
			projects := filepath.Join(dir, "projects")
			stageTranscript(t, projects, "sess-xyz")
			runs := filepath.Join(dir, "runs")
			require.NoError(t, os.MkdirAll(runs, 0o755))
			sentinel := filepath.Join(dir, "catacomb.db")
			require.NoError(t, os.WriteFile(sentinel, []byte("baselines"), 0o600))

			var out, errb bytes.Buffer
			err := runImport(context.Background(), &out, &errb, basket, importFlags{
				task: "add-item", variant: "trunk", rep: 1, sessionID: "sess-xyz", runID: id,
				projectsDir: projects, runsDir: runs,
			})
			require.ErrorIs(t, err, errImportRunID)
			var opErr *operationalError
			require.ErrorAs(t, err, &opErr)

			require.FileExists(t, sentinel)
			require.DirExists(t, runs)
			assert.Empty(t, out.String())
		})
	}
}

func TestImportWarnsMissingCheckpoint(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	projects := filepath.Join(dir, "projects")
	stageTranscript(t, projects, "sess-xyz")
	runs := filepath.Join(dir, "runs")
	var out, errb bytes.Buffer
	require.NoError(t, runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, sessionID: "sess-xyz",
		projectsDir: projects, runsDir: runs,
	}))
	assert.Contains(t, errb.String(), "missing checkpoints")
	assert.Contains(t, errb.String(), "phase:cart")
}

func TestImportLabelsMergeAmbient(t *testing.T) {
	got := importLabels(importFlags{task: "t", variant: "v", rep: 3, labels: "env=ci,variant=SHOULD_LOSE"}, "b")
	assert.Equal(t, "v", got["variant"])
	assert.Equal(t, "ci", got["env"])
	assert.Equal(t, "3", got["rep"])
	assert.Equal(t, "b", got["basket"])
}

func TestImportParseError(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	notFile := filepath.Join(dir, "adir.jsonl")
	require.NoError(t, os.MkdirAll(notFile, 0o755))
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, transcript: notFile,
		runsDir: filepath.Join(dir, "runs"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "import")
}

func TestImportNoTimestamps(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	main := filepath.Join(dir, "empty.jsonl")
	require.NoError(t, os.WriteFile(main, nil, 0o600))
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, transcript: main,
		runsDir: filepath.Join(dir, "runs"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no timestamped records")
}

func writeCodexImportBasket(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "codex-basket.yaml")
	require.NoError(t, os.WriteFile(p, []byte(`basket: checkout
runtime: codex
reps: 1
tasks:
  - id: add-item
    cmd: ["codex", "exec", "add an item", "--json"]
    checkpoints: ["phase:cart"]
variants:
  - id: trunk
  - id: patched
`), 0o600))
	return p
}

func codexMarkPair(ts, callID, boundary string) string {
	begin := fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"mcp_tool_call_begin","call_id":%q,"invocation":{"server":"catacomb","tool":"mark","arguments":{"name":"phase:cart","boundary":%q}}}}`, ts, callID, boundary)
	end := fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"mcp_tool_call_end","call_id":%q,"invocation":{"server":"catacomb","tool":"mark","arguments":{"name":"phase:cart","boundary":%q}},"result":{"content":[{"type":"text","text":"ok"}],"is_error":false}}}`, ts, callID, boundary)
	return begin + "\n" + end + "\n"
}

func codexMainBody(version, threadID string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `{"timestamp":"2026-07-16T15:22:11.578Z","type":"session_meta","payload":{"session_id":%q,"id":%q,"cwd":"/work","originator":"codex_exec","cli_version":%q,"source":"exec"}}`+"\n", threadID, threadID, version)
	b.WriteString(`{"timestamp":"2026-07-16T15:22:12.000Z","type":"turn_context","payload":{"turn_id":"T1","cwd":"/work","model":"gpt-5.4-mini"}}` + "\n")
	b.WriteString(`{"timestamp":"2026-07-16T15:22:12.100Z","type":"event_msg","payload":{"type":"task_started","turn_id":"T1"}}` + "\n")
	b.WriteString(`{"timestamp":"2026-07-16T15:22:12.200Z","type":"event_msg","payload":{"type":"user_message","message":"add an item"}}` + "\n")
	b.WriteString(codexMarkPair("2026-07-16T15:22:13.000Z", "call_m1", "start"))
	b.WriteString(`{"timestamp":"2026-07-16T15:22:14.000Z","type":"event_msg","payload":{"type":"mcp_tool_call_begin","call_id":"call_n1","invocation":{"server":"notes","tool":"add","arguments":{"text":"cart"}}}}` + "\n")
	b.WriteString(`{"timestamp":"2026-07-16T15:22:14.100Z","type":"event_msg","payload":{"type":"mcp_tool_call_end","call_id":"call_n1","invocation":{"server":"notes","tool":"add","arguments":{"text":"cart"}},"result":{"content":[],"is_error":false}}}` + "\n")
	b.WriteString(codexMarkPair("2026-07-16T15:22:15.000Z", "call_m2", "end"))
	b.WriteString(`{"timestamp":"2026-07-16T15:22:16.000Z","type":"response_item","payload":{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"added"}],"internal_chat_message_metadata_passthrough":{"turn_id":"T1"}}}` + "\n")
	b.WriteString(`{"timestamp":"2026-07-16T15:22:16.100Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":100,"cached_input_tokens":10,"output_tokens":20}}}}` + "\n")
	b.WriteString(`{"timestamp":"2026-07-16T15:22:16.200Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"T1","last_agent_message":"added","duration_ms":4000}}` + "\n")
	return b.String()
}

func codexChildBody(version, childID, parentID string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `{"timestamp":"2026-07-16T15:23:00.000Z","type":"session_meta","payload":{"session_id":%q,"id":%q,"cwd":"/work","originator":"codex_exec","cli_version":%q,"parent_thread_id":%q,"agent_role":"explorer"}}`+"\n", childID, childID, version, parentID)
	b.WriteString(`{"timestamp":"2026-07-16T15:23:01.000Z","type":"turn_context","payload":{"turn_id":"CT1","cwd":"/work","model":"gpt-5.4-mini"}}` + "\n")
	b.WriteString(`{"timestamp":"2026-07-16T15:23:02.000Z","type":"response_item","payload":{"type":"message","id":"cm1","role":"assistant","content":[{"type":"output_text","text":"explored"}],"internal_chat_message_metadata_passthrough":{"turn_id":"CT1"}}}` + "\n")
	b.WriteString(`{"timestamp":"2026-07-16T15:23:03.000Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"CT1","last_agent_message":"explored","duration_ms":900}}` + "\n")
	return b.String()
}

func stageCodexSessionTree(t *testing.T, root, version string) string {
	t.Helper()
	return stageCodexSessionTreeIDs(t, root, version, codexMainThread, codexChildThread)
}

func stageCodexSessionTreeIDs(t *testing.T, root, version, mainID, childID string) string {
	t.Helper()
	day := codexDayDir(t, root)
	main := filepath.Join(day, codexRolloutName(mainID))
	require.NoError(t, os.WriteFile(main, []byte(codexMainBody(version, mainID)), 0o600))
	child := filepath.Join(day, codexRolloutName(childID))
	require.NoError(t, os.WriteFile(child, []byte(codexChildBody(version, childID, mainID)), 0o600))
	return main
}

func reduceCodexEvidence(t *testing.T, dir string) *reduce.Graph {
	t.Helper()
	subs, err := filepath.Glob(filepath.Join(dir, "subagents", "agent-*.jsonl"))
	require.NoError(t, err)
	obs, err := parseTranscriptsFor(drift.RuntimeCodex, filepath.Join(dir, "session.jsonl"), subs, codexMainThread, "exec-rr")
	require.NoError(t, err)
	return graphFromObservations(obs, "exec-rr", nil, nil)
}

func graphNodeTypes(g *reduce.Graph) map[model.NodeType]int {
	nodes, _ := g.Snapshot()
	out := map[model.NodeType]int{}
	for _, n := range nodes {
		out[n.Type]++
	}
	return out
}

func TestImportCodexBySessionID(t *testing.T) {
	resetDriftWarnings()
	errb := captureDriftOut(t)
	dir := t.TempDir()
	basket := writeCodexImportBasket(t, dir)
	sessions := filepath.Join(dir, "sessions")
	stageCodexSessionTree(t, sessions, "0.144.4")
	runs := filepath.Join(dir, "runs")
	var out, stderr bytes.Buffer
	require.NoError(t, runImport(context.Background(), &out, &stderr, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, sessionID: codexMainThread,
		sessionsDir: sessions, runsDir: runs,
	}))
	assert.Empty(t, stderr.String())
	assert.Empty(t, errb.String())

	evDir := filepath.Join(runs, "import-checkout-add-item-trunk-r1")
	m, err := evidence.ReadMeta(evDir)
	require.NoError(t, err)
	assert.Equal(t, "import-checkout-add-item-trunk-r1", m.RunID)
	assert.Equal(t, codexMainThread, m.SessionID)
	assert.Equal(t, "task:add-item", m.MarkerName)
	assert.Nil(t, m.CostUSD)
	assert.Equal(t, map[string]string{"basket": "checkout", "task": "add-item", "variant": "trunk", "rep": "1"}, m.Labels)
	require.NotNil(t, m.Env)
	assert.Equal(t, "codex", m.Env.AgentRuntime)
	assert.Equal(t, "0.144.4", m.Env.AgentVersion)
	assert.Empty(t, m.Env.ClaudeCodeVersion)
	assert.Equal(t, "gpt-5.4-mini", m.Env.ModelID)

	require.FileExists(t, filepath.Join(evDir, "session.jsonl"))
	require.FileExists(t, filepath.Join(evDir, "subagents", "agent-"+codexChildThread+".jsonl"))

	g := reduceCodexEvidence(t, evDir)
	types := graphNodeTypes(g)
	assert.Positive(t, types[model.NodeMCPCall])
	assert.Positive(t, types[model.NodeSubagent])
	_, marked := graphMarkerNames(g)["phase:cart"]
	assert.True(t, marked)
}

func TestImportCodexByTranscript(t *testing.T) {
	resetDriftWarnings()
	captureDriftOut(t)
	dir := t.TempDir()
	basket := writeCodexImportBasket(t, dir)
	sessions := filepath.Join(dir, "sessions")
	main := stageCodexSessionTree(t, sessions, "0.144.4")
	runs := filepath.Join(dir, "runs")
	var out, stderr bytes.Buffer
	require.NoError(t, runImport(context.Background(), &out, &stderr, basket, importFlags{
		task: "add-item", variant: "patched", rep: 2, transcript: main, runsDir: runs,
	}))
	evDir := filepath.Join(runs, "import-checkout-add-item-patched-r2")
	m, err := evidence.ReadMeta(evDir)
	require.NoError(t, err)
	assert.Equal(t, codexMainThread, m.SessionID)
	assert.Equal(t, "codex", m.Env.AgentRuntime)
	require.FileExists(t, filepath.Join(evDir, "subagents", "agent-"+codexChildThread+".jsonl"))
}

func TestImportCodexTranscriptUnderivableThreadID(t *testing.T) {
	dir := t.TempDir()
	basket := writeCodexImportBasket(t, dir)
	main := filepath.Join(dir, "session-abc.jsonl")
	require.NoError(t, os.WriteFile(main, []byte(codexMainBody("0.144.4", codexMainThread)), 0o600))
	var out, stderr bytes.Buffer
	err := runImport(context.Background(), &out, &stderr, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, transcript: main, runsDir: dir,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "thread id")
}

func TestImportCodexSessionNotFound(t *testing.T) {
	dir := t.TempDir()
	basket := writeCodexImportBasket(t, dir)
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"import", basket,
		"--task", "add-item", "--variant", "trunk", "--session-id", codexMainThread,
		"--runs-dir", dir, "--sessions-dir", filepath.Join(dir, "empty"),
	}, &stdout, &stderr)
	require.Equal(t, 2, code, stderr.String())
	assert.Contains(t, stderr.String(), "no transcript for session "+codexMainThread)
}

func TestImportCodexVersionWarningFiresOnce(t *testing.T) {
	resetDriftWarnings()
	buf := captureDriftOut(t)
	dir := t.TempDir()
	basket := writeCodexImportBasket(t, dir)
	sessions := filepath.Join(dir, "sessions")
	stageCodexSessionTree(t, sessions, "0.145.0")
	var out, stderr bytes.Buffer
	require.NoError(t, runImport(context.Background(), &out, &stderr, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, sessionID: codexMainThread,
		sessionsDir: sessions, runsDir: filepath.Join(dir, "runs"),
	}))
	warning := fmt.Sprintf("warning: transcript Codex version 0.145.0 is newer than tested %s", drift.TestedCodexVersion)
	assert.Equal(t, 1, strings.Count(buf.String(), warning), buf.String())
}

func TestImportCodexZstMainDecompressedEvidence(t *testing.T) {
	resetDriftWarnings()
	captureDriftOut(t)
	dir := t.TempDir()
	basket := writeCodexImportBasket(t, dir)
	sessions := filepath.Join(dir, "sessions")
	day := codexDayDir(t, sessions)
	writeZstFile(t, filepath.Join(day, codexRolloutName(codexMainThread)+".zst"), []byte(codexMainBody("0.144.4", codexMainThread)))
	child := filepath.Join(day, codexRolloutName(codexChildThread)+".zst")
	writeZstFile(t, child, []byte(codexChildBody("0.144.4", codexChildThread, codexMainThread)))
	runs := filepath.Join(dir, "runs")
	var out, stderr bytes.Buffer
	require.NoError(t, runImport(context.Background(), &out, &stderr, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, sessionID: codexMainThread,
		sessionsDir: sessions, runsDir: runs,
	}))
	evDir := filepath.Join(runs, "import-checkout-add-item-trunk-r1")
	data, err := os.ReadFile(filepath.Join(evDir, "session.jsonl"))
	require.NoError(t, err)
	assert.Contains(t, string(data), `"session_meta"`)
	sub, err := os.ReadFile(filepath.Join(evDir, "subagents", "agent-"+codexChildThread+".jsonl"))
	require.NoError(t, err)
	assert.Contains(t, string(sub), `"parent_thread_id"`)
	g := reduceCodexEvidence(t, evDir)
	assert.Positive(t, graphNodeTypes(g)[model.NodeSubagent])
}

func TestImportEvidenceWriteError(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	projects := filepath.Join(dir, "projects")
	stageTranscript(t, projects, "sess-xyz")
	blocker := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, sessionID: "sess-xyz",
		projectsDir: projects, runsDir: filepath.Join(blocker, "runs"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "evidence write")
}
