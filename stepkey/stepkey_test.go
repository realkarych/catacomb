package stepkey

import (
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func tnode(id string, typ model.NodeType, name string, startSec int64, input string) *model.Node {
	ts := time.Unix(startSec, 0).UTC()
	n := &model.Node{ID: id, Type: typ, Name: name, Status: model.StatusOK, TStart: &ts}
	if input != "" {
		n.Payload = &model.Payload{Input: []byte(input)}
	}
	return n
}

func edge(src, dst string) *model.Edge {
	return &model.Edge{Type: model.EdgeParentChild, Src: src, Dst: dst}
}

func pipeline(prefix string, baseSec int64) ([]*model.Node, []*model.Edge) {
	nodes := []*model.Node{
		tnode(prefix+"sess", model.NodeSession, "", baseSec, ""),
		tnode(prefix+"p", model.NodeUserPrompt, "", baseSec+1, ""),
		tnode(prefix+"turn", model.NodeAssistantTurn, "", baseSec+2, ""),
		tnode(prefix+"bash", model.NodeToolCall, "Bash", baseSec+3, `{"command":"ls"}`),
		tnode(prefix+"read", model.NodeToolCall, "Read", baseSec+4, `{"file_path":"x"}`),
	}
	edges := []*model.Edge{
		edge(prefix+"sess", prefix+"p"),
		edge(prefix+"p", prefix+"turn"),
		edge(prefix+"turn", prefix+"bash"),
		edge(prefix+"turn", prefix+"read"),
	}
	return nodes, edges
}

func TestComputeAssignsKeyToEligibleAndSkipsRest(t *testing.T) {
	nodes := []*model.Node{
		tnode("sess", model.NodeSession, "", 0, ""),
		tnode("p1", model.NodeUserPrompt, "", 1, ""),
		tnode("tool", model.NodeToolCall, "Bash", 2, `{"command":"ls"}`),
	}
	edges := []*model.Edge{edge("sess", "p1"), edge("p1", "tool")}
	got := Compute(nodes, edges)
	require.Contains(t, got, "tool")
	assert.Equal(t, Method, got["tool"].Method)
	assert.NotEmpty(t, got["tool"].Key)
	assert.NotContains(t, got, "sess")
	assert.NotContains(t, got, "p1")
}

func noisyPipeline(prefix string, baseSec int64) ([]*model.Node, []*model.Edge) {
	nodes := []*model.Node{
		tnode(prefix+"sess", model.NodeSession, "", baseSec, ""),
		tnode(prefix+"p", model.NodeUserPrompt, "", baseSec+1, ""),
		tnode(prefix+"turn", model.NodeAssistantTurn, "", baseSec+2, ""),
		tnode(prefix+"bash", model.NodeToolCall, "Bash", baseSec+3,
			"{  \"description\" : \"list files\" ,\n  \"command\"  :  \"ls\"  }"),
		tnode(prefix+"read", model.NodeToolCall, "Read", baseSec+4,
			"{ \"limit\": 40 ,\n \"file_path\" : \"x\" }"),
	}
	edges := []*model.Edge{
		edge(prefix+"sess", prefix+"p"),
		edge(prefix+"p", prefix+"turn"),
		edge(prefix+"turn", prefix+"bash"),
		edge(prefix+"turn", prefix+"read"),
	}
	return nodes, edges
}

func TestStepKeyIgnoresNodeIDsTimestampsArgOrderWhitespaceAndVolatileArgs(t *testing.T) {
	na, ea := pipeline("A:", 1000)
	nb, eb := noisyPipeline("B:", 9000)
	ka := Compute(na, ea)
	kb := Compute(nb, eb)

	require.Contains(t, ka, "A:bash")
	require.Contains(t, kb, "B:bash")
	assert.Equal(t, ka["A:bash"].Key, kb["B:bash"].Key,
		"same logical Bash step must key identically across runs")
	assert.Equal(t, ka["A:read"].Key, kb["B:read"].Key,
		"same logical Read step must key identically across runs")
	assert.Equal(t, ka["A:bash"].PathKey, kb["B:bash"].PathKey)
	assert.Equal(t, ka["A:bash"].Content, kb["B:bash"].Content)
}

func TestStepKeyDistinguishesToolIdentityArgumentsAndPosition(t *testing.T) {
	na, ea := pipeline("A:", 1000)
	ka := Compute(na, ea)

	differentTool := ka["A:read"].Key
	assert.NotEqual(t, ka["A:bash"].Key, differentTool,
		"different tools at different sibling slots must not share a key")

	changedArgs := Compute([]*model.Node{
		tnode("sess", model.NodeSession, "", 1000, ""),
		tnode("p", model.NodeUserPrompt, "", 1001, ""),
		tnode("turn", model.NodeAssistantTurn, "", 1002, ""),
		tnode("bash", model.NodeToolCall, "Bash", 1003, `{"command":"pwd"}`),
		tnode("read", model.NodeToolCall, "Read", 1004, `{"file_path":"x"}`),
	}, []*model.Edge{
		edge("sess", "p"), edge("p", "turn"), edge("turn", "bash"), edge("turn", "read"),
	})
	assert.NotEqual(t, ka["A:bash"].Key, changedArgs["bash"].Key,
		"a changed salient argument must change the step key")
	assert.Equal(t, ka["A:read"].Key, changedArgs["read"].Key,
		"an unrelated sibling must keep its key when another sibling's arguments change")
}

func one(name, input string) Key {
	n := tnode("t", model.NodeToolCall, name, 5, input)
	return Compute([]*model.Node{n}, nil)["t"]
}

func TestSalientInput(t *testing.T) {
	tests := []struct {
		nameA string
		inA   string
		nameB string
		inB   string
		equal bool
	}{
		{
			"Edit",
			`{"file_path":"a.go","old_string":"x","new_string":"y"}`,
			"Edit",
			`{"file_path":"a.go","old_string":"p","new_string":"q"}`,
			true,
		},
		{
			"Edit",
			`{"file_path":"a.go","old_string":"x"}`,
			"Edit",
			`{"file_path":"b.go","old_string":"x"}`,
			false,
		},
		{
			"Bash",
			`{"command":"go test"}`,
			"Bash",
			`{"command":"go test"}`,
			true,
		},
		{
			"Read",
			`{"file_path":"z"}`,
			"Read",
			`{"file_path":"z"}`,
			true,
		},
		{
			"mcp__fs__write",
			`{"a":1,"b":2}`,
			"mcp__fs__write",
			`{"b":2,"a":1}`,
			true,
		},
		{
			"Bash",
			`{"command":"curl -H 'Authorization: Bearer aaaaaaaaaaaaaaaa1'"}`,
			"Bash",
			`{"command":"curl -H 'Authorization: Bearer bbbbbbbbbbbbbbbb2'"}`,
			true,
		},
	}
	for _, tt := range tests {
		ka := one(tt.nameA, tt.inA)
		kb := one(tt.nameB, tt.inB)
		if tt.equal {
			assert.Equal(t, ka.Key, kb.Key, "expected equal: %s %s vs %s %s", tt.nameA, tt.inA, tt.nameB, tt.inB)
		} else {
			assert.NotEqual(t, ka.Key, kb.Key, "expected different: %s %s vs %s %s", tt.nameA, tt.inA, tt.nameB, tt.inB)
		}
	}
}

func TestSubagentKeyedOnAgentTypeNotPayload(t *testing.T) {
	n1 := tnode("a1", model.NodeSubagent, "", 5, "do task A")
	n1.SubagentType = "Explore"
	n2 := tnode("a2", model.NodeSubagent, "", 6, "do task B")
	n2.SubagentType = "Explore"
	n3 := tnode("a3", model.NodeSubagent, "", 7, "do task A")
	n3.SubagentType = "Plan"

	k1 := Compute([]*model.Node{n1}, nil)["a1"]
	k2 := Compute([]*model.Node{n2}, nil)["a2"]
	k3 := Compute([]*model.Node{n3}, nil)["a3"]

	assert.Equal(t, k1.Key, k2.Key)
	assert.NotEqual(t, k1.Key, k3.Key)
}

func TestComputeOrderInvariant(t *testing.T) {
	na, ea := pipeline("A:", 1000)

	rev := make([]*model.Node, len(na))
	copy(rev, na)
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	reve := make([]*model.Edge, len(ea))
	copy(reve, ea)
	for i, j := 0, len(reve)-1; i < j; i, j = i+1, j-1 {
		reve[i], reve[j] = reve[j], reve[i]
	}

	ka := Compute(na, ea)
	kb := Compute(rev, reve)
	assert.Equal(t, ka["A:bash"].Key, kb["A:bash"].Key)
}

func TestLessSiblingTiebreaksOnNodeIDWhenSignaturesTie(t *testing.T) {
	parent := tnode("parent", model.NodeUserPrompt, "", 0, "")
	a := tnode("sib-a", model.NodeToolCall, "Bash", 5, `{"command":"ls"}`)
	c := tnode("sib-c", model.NodeToolCall, "Bash", 5, `{"command":"ls"}`)

	nodes := []*model.Node{parent, a, c}
	edgesAC := []*model.Edge{edge("parent", "sib-a"), edge("parent", "sib-c")}
	edgesCA := []*model.Edge{edge("parent", "sib-c"), edge("parent", "sib-a")}
	nodesRev := []*model.Node{c, a, parent}

	kBase := Compute(nodes, edgesAC)
	kEdgeReversed := Compute(nodes, edgesCA)
	kBothReversed := Compute(nodesRev, edgesCA)

	require.Contains(t, kBase, "sib-a")
	require.Contains(t, kBase, "sib-c")
	require.Contains(t, kEdgeReversed, "sib-a")
	require.Contains(t, kEdgeReversed, "sib-c")
	require.Contains(t, kBothReversed, "sib-a")
	require.Contains(t, kBothReversed, "sib-c")

	assert.Equal(t, kBase["sib-a"].Key, kEdgeReversed["sib-a"].Key, "sib-a key must not depend on edge order")
	assert.Equal(t, kBase["sib-c"].Key, kEdgeReversed["sib-c"].Key, "sib-c key must not depend on edge order")
	assert.Equal(t, kBase["sib-a"].Key, kBothReversed["sib-a"].Key, "sib-a key must not depend on node+edge order")
	assert.Equal(t, kBase["sib-c"].Key, kBothReversed["sib-c"].Key, "sib-c key must not depend on node+edge order")

	assert.NotEqual(t, kBase["sib-a"].Key, kBase["sib-c"].Key, "distinct tied siblings must still get distinct keys")
}

func TestPromptOrdinalDifferentiatesIdenticalSteps(t *testing.T) {
	ts := func(s int64) *time.Time { t := time.Unix(s, 0).UTC(); return &t }

	sess := &model.Node{ID: "sess", Type: model.NodeSession, Status: model.StatusOK, TStart: ts(0)}
	p1 := &model.Node{ID: "p1", Type: model.NodeUserPrompt, Status: model.StatusOK, TStart: ts(1)}
	p2 := &model.Node{ID: "p2", Type: model.NodeUserPrompt, Status: model.StatusOK, TStart: ts(2)}
	tool1 := &model.Node{ID: "t1", Type: model.NodeToolCall, Name: "Bash", Status: model.StatusOK, TStart: ts(3)}
	tool1.Payload = &model.Payload{Input: []byte(`{"command":"ls"}`)}
	tool2 := &model.Node{ID: "t2", Type: model.NodeToolCall, Name: "Bash", Status: model.StatusOK, TStart: ts(4)}
	tool2.Payload = &model.Payload{Input: []byte(`{"command":"ls"}`)}

	nodes := []*model.Node{sess, p1, p2, tool1, tool2}
	edges := []*model.Edge{
		edge("sess", "p1"),
		edge("sess", "p2"),
		edge("p1", "t1"),
		edge("p2", "t2"),
	}

	got := Compute(nodes, edges)
	assert.NotEqual(t, got["t1"].Key, got["t2"].Key)
}

func TestKeyIndependentOfTier(t *testing.T) {
	na, ea := pipeline("A:", 1000)
	nb, eb := pipeline("B:", 1000)
	for _, n := range nb {
		n.Tier = "lean"
	}
	ka := Compute(na, ea)
	kb := Compute(nb, eb)
	assert.Equal(t, ka["A:bash"].Key, kb["B:bash"].Key)
}

func TestSelfLoopKeysAsIfTheNodeHadNoParent(t *testing.T) {
	n := tnode("t", model.NodeToolCall, "Bash", 1, `{"command":"ls"}`)
	selfLoop := &model.Edge{Type: model.EdgeParentChild, Src: "t", Dst: "t"}

	looped := Compute([]*model.Node{n}, []*model.Edge{selfLoop})
	rootless := Compute([]*model.Node{tnode("t", model.NodeToolCall, "Bash", 1, `{"command":"ls"}`)}, nil)

	require.Contains(t, looped, "t")
	assert.Equal(t, rootless["t"].Key, looped["t"].Key)
	assert.Equal(t, rootless["t"].PathKey, looped["t"].PathKey)
}

func TestTwoCycleKeysAsIfTheAncestorChainStoppedAtTheRepeatedNode(t *testing.T) {
	n := tnode("t", model.NodeToolCall, "Bash", 1, `{"command":"ls"}`)
	p := tnode("p", model.NodeUserPrompt, "", 0, "")
	cyclic := Compute(
		[]*model.Node{n, p},
		[]*model.Edge{edge("p", "t"), edge("t", "p")},
	)

	acyclic := Compute(
		[]*model.Node{
			tnode("t", model.NodeToolCall, "Bash", 1, `{"command":"ls"}`),
			tnode("p", model.NodeUserPrompt, "", 0, ""),
		},
		[]*model.Edge{edge("p", "t")},
	)

	require.Contains(t, cyclic, "t")
	assert.Equal(t, acyclic["t"].Key, cyclic["t"].Key)
	assert.Equal(t, acyclic["t"].PathKey, cyclic["t"].PathKey)
	assert.NotEqual(t, cyclic["t"].Key, Compute([]*model.Node{n}, nil)["t"].Key,
		"the one traversable ancestor level must still be in the key")
}

func TestStepKeyGoldenPinsSchemeV1WireValuesSoStoredBaselinesStayComparable(t *testing.T) {
	sess := tnode("sess", model.NodeSession, "", 0, "")
	prompt := tnode("p", model.NodeUserPrompt, "", 1, "")
	bash := tnode("bash", model.NodeToolCall, "Bash", 2, `{"command":"ls"}`)
	nodesA := []*model.Node{sess, prompt, bash}
	edgesA := []*model.Edge{edge("sess", "p"), edge("p", "bash")}
	gotA := Compute(nodesA, edgesA)["bash"].Key

	sub := tnode("s", model.NodeSubagent, "", 5, "")
	sub.SubagentType = "Explore"
	gotB := Compute([]*model.Node{sub}, nil)["s"].Key

	na, ea := pipeline("A:", 1000)
	gotC := Compute(na, ea)["A:bash"].Key

	assert.Equal(t, "f4486f779c383f76a7d29bea5db1d67c", gotA)
	assert.Equal(t, "75fe18f669fb537dd5dabb172bea6f76", gotB)
	assert.Equal(t, "208ef93d998f6095eb697f436432b036", gotC)
}

func TestToolNameMatchIgnoresCaseAndSurroundingWhitespace(t *testing.T) {
	spaced := one("  bAsH  ", `{"command":"ls","description":"first"}`)
	spacedOther := one("  bAsH  ", `{"command":"ls","description":"second"}`)
	assert.Equal(t, spaced.Key, spacedOther.Key,
		"a padded, mixed-case Bash must still project only the command")

	unmatched := one("Bash!", `{"command":"ls","description":"first"}`)
	unmatchedOther := one("Bash!", `{"command":"ls","description":"second"}`)
	assert.NotEqual(t, unmatched.Key, unmatchedOther.Key,
		"a name the salient table does not know must fall back to whole-input canonicalisation")
}

func TestOnlyToolMCPSkillAndSubagentNodesReceiveKeys(t *testing.T) {
	sub := tnode("subagent", model.NodeSubagent, "", 5, "")
	sub.SubagentType = "Explore"
	nodes := []*model.Node{
		tnode("sess", model.NodeSession, "", 0, ""),
		tnode("prompt", model.NodeUserPrompt, "", 1, ""),
		tnode("turn", model.NodeAssistantTurn, "", 2, ""),
		tnode("marker", model.NodeMarker, "plan", 3, ""),
		tnode("tool", model.NodeToolCall, "Bash", 4, `{"command":"ls"}`),
		sub,
		tnode("mcp", model.NodeMCPCall, "mcp__fs__read", 6, `{"file_path":"a.go"}`),
		tnode("skill", model.NodeSkill, "brainstorm", 7, `{"x":1}`),
	}
	edges := []*model.Edge{
		edge("sess", "prompt"), edge("prompt", "turn"),
		edge("sess", "marker"), edge("turn", "tool"),
		edge("turn", "subagent"), edge("turn", "mcp"), edge("turn", "skill"),
	}

	got := Compute(nodes, edges)

	keyed := make([]string, 0, len(got))
	for id := range got {
		keyed = append(keyed, id)
	}
	sort.Strings(keyed)
	assert.Equal(t, []string{"mcp", "skill", "subagent", "tool"}, keyed)

	distinct := map[string]string{}
	for id, k := range got {
		require.NotContains(t, distinct, k.Key, "keys must be distinct across node types")
		distinct[k.Key] = id
	}
}

func TestComputeWithoutEdgesKeysEveryEligibleNodeAtTheRoot(t *testing.T) {
	a := tnode("a", model.NodeToolCall, "Bash", 1, `{"command":"ls"}`)
	b := tnode("b", model.NodeToolCall, "Bash", 2, `{"command":"ls"}`)
	c := tnode("c", model.NodeToolCall, "Bash", 3, `{"command":"pwd"}`)

	got := Compute([]*model.Node{a, b, c}, nil)

	require.Len(t, got, 3)
	assert.Equal(t, got["a"].Key, got["b"].Key,
		"parentless nodes have an empty path, so identical content collapses to one key")
	assert.NotEqual(t, got["a"].Key, got["c"].Key)
	assert.Equal(t, got["a"].PathKey, got["c"].PathKey,
		"parentless nodes share the empty path key")
}

func untimedTool(id, name, input string) *model.Node {
	n := &model.Node{ID: id, Type: model.NodeToolCall, Name: name, Status: model.StatusOK}
	n.Payload = &model.Payload{Input: []byte(input)}
	return n
}

func TestSiblingsWithoutTimestampsAreOrderedDeterministicallyBySignatureThenID(t *testing.T) {
	parent := tnode("p", model.NodeUserPrompt, "", 0, "")
	bash := untimedTool("z-bash", "Bash", `{"command":"ls"}`)
	read := untimedTool("a-read", "Read", `{"file_path":"x"}`)

	forward := Compute(
		[]*model.Node{parent, bash, read},
		[]*model.Edge{edge("p", "z-bash"), edge("p", "a-read")},
	)
	reversed := Compute(
		[]*model.Node{read, bash, parent},
		[]*model.Edge{edge("p", "a-read"), edge("p", "z-bash")},
	)

	require.Contains(t, forward, "z-bash")
	require.Contains(t, forward, "a-read")
	assert.Equal(t, forward["z-bash"].Key, reversed["z-bash"].Key,
		"sibling order for timestamp-less nodes must not depend on input order")
	assert.Equal(t, forward["a-read"].Key, reversed["a-read"].Key)
	assert.NotEqual(t, forward["z-bash"].Key, forward["a-read"].Key)
	assert.NotEqual(t, forward["z-bash"].PathKey, forward["a-read"].PathKey,
		"tied siblings must occupy distinct sibling slots")
}

func TestTimestampedSiblingSortsBeforeSiblingWithoutTimestamp(t *testing.T) {
	parent := tnode("p", model.NodeUserPrompt, "", 0, "")
	timed := tnode("timed", model.NodeToolCall, "Bash", 5, `{"command":"ls"}`)
	untimed := untimedTool("untimed", "Read", `{"file_path":"x"}`)

	together := Compute(
		[]*model.Node{parent, timed, untimed},
		[]*model.Edge{edge("p", "timed"), edge("p", "untimed")},
	)
	togetherReversed := Compute(
		[]*model.Node{parent, untimed, timed},
		[]*model.Edge{edge("p", "untimed"), edge("p", "timed")},
	)
	timedAlone := Compute(
		[]*model.Node{tnode("p", model.NodeUserPrompt, "", 0, ""), timed},
		[]*model.Edge{edge("p", "timed")},
	)
	untimedAlone := Compute(
		[]*model.Node{tnode("p", model.NodeUserPrompt, "", 0, ""), untimed},
		[]*model.Edge{edge("p", "untimed")},
	)

	assert.Equal(t, timedAlone["timed"].PathKey, together["timed"].PathKey,
		"the timestamped sibling keeps slot 0 when an untimed sibling joins")
	assert.NotEqual(t, untimedAlone["untimed"].PathKey, together["untimed"].PathKey,
		"the untimed sibling must be pushed to slot 1 behind the timestamped one")

	assert.Equal(t, together["timed"].PathKey, togetherReversed["timed"].PathKey,
		"the timestamped sibling wins slot 0 regardless of which sibling is compared first")
	assert.Equal(t, together["untimed"].PathKey, togetherReversed["untimed"].PathKey)
}

func TestComputeEdgeNonParentChild(t *testing.T) {
	n := tnode("t", model.NodeToolCall, "Bash", 1, `{"command":"ls"}`)
	e := &model.Edge{Type: model.EdgeMarkerSpan, Src: "x", Dst: "t"}
	got := Compute([]*model.Node{n}, []*model.Edge{e})
	assert.Contains(t, got, "t")
}

func TestMultiEditSalient(t *testing.T) {
	ka := one("MultiEdit", `{"file_path":"a.go","edits":[]}`)
	kb := one("MultiEdit", `{"file_path":"a.go","other":"ignored"}`)
	assert.Equal(t, ka.Key, kb.Key)
}

func TestWriteSalient(t *testing.T) {
	ka := one("Write", `{"file_path":"a.go","content":"hello"}`)
	kb := one("Write", `{"file_path":"a.go","content":"world"}`)
	assert.Equal(t, ka.Key, kb.Key)
}

func TestReadWithPathKey(t *testing.T) {
	ka := one("Read", `{"path":"x"}`)
	kb := one("Read", `{"path":"x"}`)
	assert.Equal(t, ka.Key, kb.Key)
}

func TestComputeEmitsContentAndPathKeys(t *testing.T) {
	na, ea := pipeline("A:", 1000)
	nb, eb := pipeline("B:", 9000)
	ka := Compute(na, ea)
	kb := Compute(nb, eb)
	assert.Equal(t, ka["A:bash"].Content, kb["B:bash"].Content)
	assert.NotEqual(t, ka["A:bash"].Content, ka["A:read"].Content)
	assert.Equal(t, ka["A:bash"].PathKey, kb["B:bash"].PathKey)
	assert.NotEqual(t, ka["A:bash"].PathKey, ka["A:read"].PathKey)
	assert.Len(t, ka["A:bash"].PathKey, 32)
	assert.NotEqual(t, ka["A:bash"].PathKey, ka["A:bash"].Key)
}

func TestContentKeyIgnoresPosition(t *testing.T) {
	a := tnode("a", model.NodeToolCall, "Bash", 3, `{"command":"ls"}`)
	b := tnode("b", model.NodeToolCall, "Bash", 7, `{"command":"ls"}`)
	pa := tnode("pa", model.NodeUserPrompt, "", 0, "")
	pb := tnode("pb", model.NodeUserPrompt, "", 0, "")
	ka := Compute([]*model.Node{pa, a}, []*model.Edge{edge("pa", "a")})
	kb := Compute([]*model.Node{pb, b}, []*model.Edge{edge("pb", "b")})
	assert.Equal(t, ka["a"].Content, kb["b"].Content)
}

func TestClaudeSalientCharacterization(t *testing.T) {
	tests := []struct {
		name string
		tool string
		red  string
		want string
	}{
		{"bash projects command", "Bash", `{"command":"ls -la","description":"list"}`, `{"command":"ls -la"}`},
		{"bash invalid json", "Bash", `not-json`, ``},
		{"edit projects file_path", "Edit", `{"file_path":"a.go","old_string":"x","new_string":"y"}`, `{"file_path":"a.go"}`},
		{"multiedit projects file_path", "MultiEdit", `{"file_path":"m.go","edits":[{"old_string":"a","new_string":"b"}]}`, `{"file_path":"m.go"}`},
		{"write projects file_path", "Write", `{"file_path":"w.go","content":"body"}`, `{"file_path":"w.go"}`},
		{"read projects file_path", "Read", `{"file_path":"r.go","limit":10}`, `{"file_path":"r.go"}`},
		{"read falls back to path", "Read", `{"path":"alt.go"}`, `{"path":"alt.go"}`},
		{"read prefers file_path over path", "Read", `{"path":"p.go","file_path":"f.go"}`, `{"file_path":"f.go"}`},
		{"unknown tool canonicalizes", "Glob", `{"b":2,"a":1}`, `{"a":1,"b":2}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, salient(tt.tool, []byte(tt.red)))
		})
	}
}

func TestCodexSalientProjections(t *testing.T) {
	tests := []struct {
		name string
		tool string
		red  string
		want string
	}{
		{"exec_command projects cmd", "exec_command", `{"cmd":"echo probe-42","yield_time_ms":10000}`, `{"cmd":"echo probe-42"}`},
		{"exec_command missing cmd", "exec_command", `{"yield_time_ms":10000}`, ``},
		{
			"apply_patch string form add",
			"apply_patch",
			`"*** Begin Patch\n*** Add File: probe.txt\n+probe-42\n*** End Patch"`,
			`{"file":"probe.txt"}`,
		},
		{
			"apply_patch string form update",
			"apply_patch",
			`"*** Begin Patch\n*** Update File: pkg/a.go\n@@\n-x\n+y\n*** End Patch"`,
			`{"file":"pkg/a.go"}`,
		},
		{
			"apply_patch string form delete",
			"apply_patch",
			`"*** Begin Patch\n*** Delete File: gone.txt\n*** End Patch"`,
			`{"file":"gone.txt"}`,
		},
		{
			"apply_patch object form",
			"apply_patch",
			`{"input":"*** Begin Patch\n*** Update File: pkg/b.go\n@@\n-a\n+b\n*** End Patch"}`,
			`{"file":"pkg/b.go"}`,
		},
		{"apply_patch string without directive", "apply_patch", `"no directives here"`, `"no directives here"`},
		{"apply_patch object missing input", "apply_patch", `{"other":"x"}`, `{"other":"x"}`},
		{"apply_patch object non-string input", "apply_patch", `{"input":42}`, `{"input":42}`},
		{"apply_patch invalid json", "apply_patch", `not-json`, `not-json`},
		{"apply_patch number", "apply_patch", `123`, `123`},
		{"write_stdin projects session_id", "write_stdin", `{"session_id":"s1","chars":"ls\n","yield_time_ms":250}`, `{"session_id":"s1"}`},
		{"spawn_agent stays canon", "spawn_agent", `{"b":2,"a":1}`, `{"a":1,"b":2}`},
		{"update_plan stays canon", "update_plan", `{"plan":["x"],"a":1}`, `{"a":1,"plan":["x"]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, salient(tt.tool, []byte(tt.red)))
		})
	}
}

func TestCodexStepKeysIgnoreVolatileFields(t *testing.T) {
	ka := one("exec_command", `{"cmd":"echo probe-42","yield_time_ms":10000}`)
	kb := one("exec_command", `{"cmd":"echo probe-42","yield_time_ms":250,"workdir":"/tmp/x"}`)
	kc := one("exec_command", `{"cmd":"other"}`)
	assert.Equal(t, ka.Key, kb.Key)
	assert.NotEqual(t, ka.Key, kc.Key)

	wa := one("write_stdin", `{"session_id":"s1","chars":"ls\n"}`)
	wb := one("write_stdin", `{"session_id":"s1","chars":"pwd\n","yield_time_ms":100}`)
	wc := one("write_stdin", `{"session_id":"s2","chars":"ls\n"}`)
	assert.Equal(t, wa.Key, wb.Key)
	assert.NotEqual(t, wa.Key, wc.Key)
}

func TestCodexApplyPatchFormsConverge(t *testing.T) {
	str := one("apply_patch", `"*** Begin Patch\n*** Update File: probe.txt\n@@\n-a\n+b\n*** End Patch"`)
	obj := one("apply_patch", `{"input":"*** Begin Patch\n*** Update File: probe.txt\n@@\n-c\n+d\n*** End Patch"}`)
	other := one("apply_patch", `"*** Begin Patch\n*** Update File: other.txt\n@@\n-a\n+b\n*** End Patch"`)
	assert.Equal(t, str.Key, obj.Key)
	assert.NotEqual(t, str.Key, other.Key)
}

func TestSchemeExportedAndFeedsBothHashSites(t *testing.T) {
	assert.Equal(t, "stepkey/v1", Scheme)
	sess := tnode("sess", model.NodeSession, "", 0, "")
	tool := tnode("tool", model.NodeToolCall, "Bash", 1, `{"command":"ls"}`)
	got := Compute([]*model.Node{sess, tool}, []*model.Edge{edge("sess", "tool")})
	require.Contains(t, got, "tool")
	level := string(model.NodeToolCall) + "#0"
	assert.Equal(t, hash16(Scheme+"|"+level+"|"+got["tool"].Content), got["tool"].Key)
	assert.Equal(t, hash16(Scheme+"|path|"+level), got["tool"].PathKey)
}
