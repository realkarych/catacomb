package stepkey

import (
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

func TestStepKeyEqualAcrossRunsDistinctPerStep(t *testing.T) {
	na, ea := pipeline("A:", 1000)
	nb, eb := pipeline("B:", 9000)
	ka := Compute(na, ea)
	kb := Compute(nb, eb)
	assert.Equal(t, ka["A:bash"].Key, kb["B:bash"].Key)
	assert.Equal(t, ka["A:read"].Key, kb["B:read"].Key)
	assert.NotEqual(t, ka["A:bash"].Key, ka["A:read"].Key)
	assert.Len(t, ka["A:bash"].Key, 32)
	assert.NotContains(t, ka["A:bash"].Key, "A:")
	assert.NotContains(t, ka["A:bash"].Key, "B:")
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

func TestSupersededSiblingDoesNotShiftLiveIndex(t *testing.T) {
	ts1 := time.Unix(1, 0).UTC()
	ts2 := time.Unix(2, 0).UTC()
	ts3 := time.Unix(3, 0).UTC()

	parent := &model.Node{ID: "parent", Type: model.NodeUserPrompt, Status: model.StatusOK, TStart: &ts1}
	dead := &model.Node{ID: "dead", Type: model.NodeToolCall, Name: "Edit", Status: model.StatusSuperseded, TStart: &ts2}
	dead.Payload = &model.Payload{Input: []byte(`{"file_path":"a.go"}`)}
	live1 := &model.Node{ID: "live1", Type: model.NodeToolCall, Name: "Edit", Status: model.StatusOK, TStart: &ts3}
	live1.Payload = &model.Payload{Input: []byte(`{"file_path":"a.go"}`)}

	nodesWithDead := []*model.Node{parent, dead, live1}
	nodesWithoutDead := []*model.Node{parent, live1}
	edges := []*model.Edge{edge("parent", "dead"), edge("parent", "live1")}
	edgesNoDead := []*model.Edge{edge("parent", "live1")}

	ka := Compute(nodesWithDead, edges)
	kb := Compute(nodesWithoutDead, edgesNoDead)
	assert.Equal(t, ka["live1"].Key, kb["live1"].Key)
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

func TestCycleGuardSelfLoop(t *testing.T) {
	n := tnode("t", model.NodeToolCall, "Bash", 1, `{"command":"ls"}`)
	selfLoop := &model.Edge{Type: model.EdgeParentChild, Src: "t", Dst: "t"}
	got := Compute([]*model.Node{n}, []*model.Edge{selfLoop})
	require.Contains(t, got, "t")
	assert.Len(t, got["t"].Key, 32)
	assert.NotEmpty(t, got["t"].Key)
}

func TestCycleGuardTwoCycle(t *testing.T) {
	n := tnode("t", model.NodeToolCall, "Bash", 1, `{"command":"ls"}`)
	p := tnode("p", model.NodeUserPrompt, "", 0, "")
	e1 := &model.Edge{Type: model.EdgeParentChild, Src: "p", Dst: "t"}
	e2 := &model.Edge{Type: model.EdgeParentChild, Src: "t", Dst: "p"}
	got := Compute([]*model.Node{n, p}, []*model.Edge{e1, e2})
	require.Contains(t, got, "t")
	assert.Len(t, got["t"].Key, 32)
	assert.NotEmpty(t, got["t"].Key)
}

func TestStepKeyGolden(t *testing.T) {
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
	assert.Len(t, gotA, 32)
	assert.Len(t, gotB, 32)
	assert.Len(t, gotC, 32)
}

func TestProjectInvalidJSON(t *testing.T) {
	assert.Equal(t, "", project([]byte("not-json"), "key"))
}

func TestProjectNonObject(t *testing.T) {
	assert.Equal(t, "", project([]byte(`"string"`), "key"))
}

func TestProjectMissingKey(t *testing.T) {
	assert.Equal(t, "", project([]byte(`{"other":"val"}`), "key"))
}

func TestCanonInvalidJSON(t *testing.T) {
	raw := []byte("not-json")
	assert.Equal(t, "not-json", canon(raw))
}

func TestNormTool(t *testing.T) {
	assert.Equal(t, "bash", normTool("  Bash  "))
	assert.Equal(t, "edit", normTool("Edit"))
}

func TestLiveFalseForSuperseded(t *testing.T) {
	n := &model.Node{Status: model.StatusSuperseded}
	assert.False(t, live(n))
}

func TestLiveFalseForAbandoned(t *testing.T) {
	n := &model.Node{Status: model.StatusAbandoned}
	assert.False(t, live(n))
}

func TestLiveTrueForOK(t *testing.T) {
	n := &model.Node{Status: model.StatusOK}
	assert.True(t, live(n))
}

func TestEligibleTypes(t *testing.T) {
	assert.True(t, eligible(model.NodeToolCall))
	assert.True(t, eligible(model.NodeMCPCall))
	assert.True(t, eligible(model.NodeSkill))
	assert.True(t, eligible(model.NodeSubagent))
	assert.False(t, eligible(model.NodeSession))
	assert.False(t, eligible(model.NodeUserPrompt))
	assert.False(t, eligible(model.NodeAssistantTurn))
}

func TestComputeNilEdges(t *testing.T) {
	n := tnode("t", model.NodeToolCall, "Bash", 1, `{"command":"ls"}`)
	got := Compute([]*model.Node{n}, nil)
	assert.Contains(t, got, "t")
	assert.Len(t, got["t"].Key, 32)
}

func TestComputeSkipsDeadNodes(t *testing.T) {
	n := &model.Node{ID: "n", Type: model.NodeToolCall, Name: "Bash", Status: model.StatusSuperseded}
	got := Compute([]*model.Node{n}, nil)
	assert.NotContains(t, got, "n")
}

func TestLessSiblingBothNilTimestamp(t *testing.T) {
	b := &builder{
		byID:     map[string]*model.Node{},
		parentOf: map[string]string{},
		children: map[string][]string{},
		terms:    map[string]string{},
	}
	a := &model.Node{ID: "a", Type: model.NodeToolCall, Name: "Bash"}
	c := &model.Node{ID: "c", Type: model.NodeToolCall, Name: "Read"}
	b.terms["a"] = "term-a"
	b.terms["c"] = "term-c"
	result := b.lessSibling(a, c)
	assert.True(t, result)
}

func TestLessSiblingANilTimestamp(t *testing.T) {
	b := &builder{
		byID:     map[string]*model.Node{},
		parentOf: map[string]string{},
		children: map[string][]string{},
		terms:    map[string]string{},
	}
	ts := time.Unix(1, 0).UTC()
	a := &model.Node{ID: "a", Type: model.NodeToolCall}
	c := &model.Node{ID: "c", Type: model.NodeToolCall, TStart: &ts}
	result := b.lessSibling(a, c)
	assert.False(t, result)
}

func TestLessSiblingCNilTimestamp(t *testing.T) {
	b := &builder{
		byID:     map[string]*model.Node{},
		parentOf: map[string]string{},
		children: map[string][]string{},
		terms:    map[string]string{},
	}
	ts := time.Unix(1, 0).UTC()
	a := &model.Node{ID: "a", Type: model.NodeToolCall, TStart: &ts}
	c := &model.Node{ID: "c", Type: model.NodeToolCall}
	result := b.lessSibling(a, c)
	assert.True(t, result)
}

func TestComputeEdgeNonParentChild(t *testing.T) {
	n := tnode("t", model.NodeToolCall, "Bash", 1, `{"command":"ls"}`)
	e := &model.Edge{Type: model.EdgeMarkerSpan, Src: "x", Dst: "t"}
	got := Compute([]*model.Node{n}, []*model.Edge{e})
	assert.Contains(t, got, "t")
}

func TestLiveIndexTargetNotFound(t *testing.T) {
	ts1 := time.Unix(1, 0).UTC()
	ts2 := time.Unix(2, 0).UTC()
	child1 := &model.Node{ID: "c1", Type: model.NodeToolCall, Status: model.StatusOK, TStart: &ts1}
	child2 := &model.Node{ID: "c2", Type: model.NodeToolCall, Status: model.StatusOK, TStart: &ts2}
	b := &builder{
		byID:     map[string]*model.Node{"c1": child1, "c2": child2},
		parentOf: map[string]string{},
		children: map[string][]string{"parent": {"c1", "c2"}},
		terms:    map[string]string{"c1": "t1", "c2": "t2"},
	}
	idx := b.liveIndex("parent", "missing")
	assert.Equal(t, 0, idx)
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

func TestMCPCallEligible(t *testing.T) {
	n := tnode("m", model.NodeMCPCall, "mcp__fs__read", 1, `{"file_path":"a.go"}`)
	got := Compute([]*model.Node{n}, nil)
	assert.Contains(t, got, "m")
	assert.Len(t, got["m"].Key, 32)
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
