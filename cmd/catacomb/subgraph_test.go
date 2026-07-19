package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/subgraph"
)

func executionPrefix(t *testing.T, ids []string) string {
	t.Helper()
	require.NotEmpty(t, ids)
	prefix, _, found := strings.Cut(ids[0], ":")
	require.True(t, found, "node id %q has no execution-id prefix", ids[0])
	for _, id := range ids {
		require.True(t, strings.HasPrefix(id, prefix+":"), "node id %q does not share execution %q", id, prefix)
	}
	return prefix
}

func TestSubgraphCommandJSONEmitsTheExactScopedNodeSequence(t *testing.T) {
	root := newRootCmd()
	var sb strings.Builder
	root.SetOut(&sb)
	root.SetArgs([]string{"subgraph", "--phase", "plan", "--json", "testdata/session_marked.jsonl"})
	require.NoError(t, root.Execute())

	var doc struct {
		Nodes []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"nodes"`
		Edges []struct {
			ID string `json:"id"`
		} `json:"edges"`
	}
	require.NoError(t, json.Unmarshal([]byte(sb.String()), &doc))

	ids := make([]string, 0, len(doc.Nodes))
	for _, n := range doc.Nodes {
		ids = append(ids, n.ID)
	}
	exec := executionPrefix(t, ids)
	assert.Equal(t, []string{
		exec + ":tool:toolu_2",
		exec + ":turn:msg_2",
		exec + ":turn:msg_3",
	}, ids)
	assert.Equal(t, []string{"tool_call", "assistant_turn", "assistant_turn"},
		[]string{doc.Nodes[0].Type, doc.Nodes[1].Type, doc.Nodes[2].Type})
	assert.Len(t, doc.Edges, 1)
}

func TestRunSubgraphIsDeterministicallySorted(t *testing.T) {
	const exec = "01JZZZZZZZZZZZZZZZZZZZZZZZ"
	a := subgraphArgs{input: writeMultiStepPhaseTranscript(t), phase: "plan"}

	nodes, edges, err := runSubgraphFor(a, exec)
	require.NoError(t, err)
	require.Greater(t, len(nodes), 1)
	require.Greater(t, len(edges), 1)

	want := snapshotIDs(nodes, edges)
	assert.True(t, sort.StringsAreSorted(want.nodes))
	assert.True(t, sort.StringsAreSorted(want.edges))

	for i := 0; i < 24; i++ {
		gotNodes, gotEdges, rerr := runSubgraphFor(a, exec)
		require.NoError(t, rerr)
		assert.Equal(t, want, snapshotIDs(gotNodes, gotEdges))
	}
}

func writeMultiStepPhaseTranscript(t *testing.T) string {
	t.Helper()
	lines := `{"type":"user","uuid":"u1","sessionId":"s1","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"go"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"s1","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"msg_1","model":"m","content":[{"type":"tool_use","id":"toolu_mark1","name":"mcp__catacomb__mark","input":{"name":"plan","boundary":"start"}}]}}
{"type":"user","uuid":"u2","parentUuid":"a1","sessionId":"s1","timestamp":"2026-06-20T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_mark1","content":"","is_error":false}]}}
{"type":"assistant","uuid":"a2","parentUuid":"u2","sessionId":"s1","timestamp":"2026-06-20T10:00:03Z","message":{"role":"assistant","id":"msg_2","model":"m","content":[{"type":"tool_use","id":"toolu_a","name":"Bash","input":{"command":"ls"}}]}}
{"type":"user","uuid":"u3","parentUuid":"a2","sessionId":"s1","timestamp":"2026-06-20T10:00:04Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_a","content":"ok","is_error":false}]}}
{"type":"assistant","uuid":"a3","parentUuid":"u3","sessionId":"s1","timestamp":"2026-06-20T10:00:05Z","message":{"role":"assistant","id":"msg_3","model":"m","content":[{"type":"tool_use","id":"toolu_b","name":"Read","input":{"file_path":"/x"}}]}}
{"type":"user","uuid":"u4","parentUuid":"a3","sessionId":"s1","timestamp":"2026-06-20T10:00:06Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_b","content":"ok","is_error":false}]}}
{"type":"assistant","uuid":"a4","parentUuid":"u4","sessionId":"s1","timestamp":"2026-06-20T10:00:07Z","message":{"role":"assistant","id":"msg_4","model":"m","content":[{"type":"tool_use","id":"toolu_c","name":"Grep","input":{"pattern":"y"}}]}}
{"type":"user","uuid":"u5","parentUuid":"a4","sessionId":"s1","timestamp":"2026-06-20T10:00:08Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_c","content":"ok","is_error":false}]}}
{"type":"assistant","uuid":"a5","parentUuid":"u5","sessionId":"s1","timestamp":"2026-06-20T10:00:09Z","message":{"role":"assistant","id":"msg_5","model":"m","content":[{"type":"tool_use","id":"toolu_mark2","name":"mcp__catacomb__mark","input":{"name":"plan","boundary":"end"}}]}}
{"type":"user","uuid":"u6","parentUuid":"a5","sessionId":"s1","timestamp":"2026-06-20T10:00:10Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_mark2","content":"","is_error":false}]}}
`
	path := filepath.Join(t.TempDir(), "phase.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(lines), 0o600))
	return path
}

type snapshotIDSet struct {
	nodes []string
	edges []string
}

func snapshotIDs(nodes []*model.Node, edges []*model.Edge) snapshotIDSet {
	out := snapshotIDSet{nodes: make([]string, 0, len(nodes)), edges: make([]string, 0, len(edges))}
	for _, n := range nodes {
		out.nodes = append(out.nodes, n.ID)
	}
	for _, e := range edges {
		out.edges = append(out.edges, e.ID)
	}
	return out
}

func TestSubgraphCommandHumanRendersHeadlineThenNodesInIDOrder(t *testing.T) {
	root := newRootCmd()
	var sb strings.Builder
	root.SetOut(&sb)
	root.SetArgs([]string{"subgraph", "--phase", "plan", "testdata/session_marked.jsonl"})
	require.NoError(t, root.Execute())

	lines := strings.Split(strings.TrimSuffix(sb.String(), "\n"), "\n")
	require.Len(t, lines, 4)
	assert.Equal(t, "nodes: 3  edges: 1", lines[0])
	ids := make([]string, 0, 3)
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		ids = append(ids, fields[len(fields)-1])
	}
	exec := executionPrefix(t, ids)
	assert.Equal(t, []string{
		"tool_call Bash " + exec + ":tool:toolu_2",
		"assistant_turn  " + exec + ":turn:msg_2",
		"assistant_turn  " + exec + ":turn:msg_3",
	}, lines[1:])
}

func TestSubgraphCommandPhaseNotFound(t *testing.T) {
	root := newRootCmd()
	root.SetOut(&strings.Builder{})
	root.SetArgs([]string{"subgraph", "--phase", "ghost", "testdata/session_marked.jsonl"})
	err := root.Execute()
	assert.ErrorIs(t, err, subgraph.ErrPhaseNotFound)
}

func TestSubgraphCommandInvalidSelector(t *testing.T) {
	root := newRootCmd()
	root.SetOut(&strings.Builder{})
	root.SetArgs([]string{"subgraph", "--from", "plan", "testdata/session_marked.jsonl"})
	err := root.Execute()
	assert.ErrorIs(t, err, subgraph.ErrInvalidSelector)
}

func TestRunSubgraphMissingFile(t *testing.T) {
	_, _, err := runSubgraph(subgraphArgs{input: "/nonexistent/file.jsonl", phase: "plan"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDiffInput)
}

func TestRunSubgraphEmptySpec(t *testing.T) {
	_, _, err := runSubgraph(subgraphArgs{input: "testdata/session_marked.jsonl"})
	assert.ErrorIs(t, err, subgraph.ErrInvalidSelector)
}

func TestSubgraphWarnsOnUnknownRecords(t *testing.T) {
	buf := captureDriftOut(t)
	drifty := writeDriftyCopy(t, "testdata/session_marked.jsonl")
	root := newRootCmd()
	root.SetOut(&strings.Builder{})
	root.SetArgs([]string{"subgraph", "--phase", "plan", drifty})
	require.NoError(t, root.Execute())
	assert.Contains(t, buf.String(), "unrecognized transcript record")
}

func TestSubgraphMissingInputIsOperational(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run([]string{"subgraph", "--phase", "plan", filepath.Join(t.TempDir(), "nope.jsonl")}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.NotEmpty(t, errBuf.String())
}

func TestSubgraphUnknownPhaseIsOperational(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run([]string{"subgraph", "--phase", "ghost", "testdata/session_marked.jsonl"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "phase not found")
}
