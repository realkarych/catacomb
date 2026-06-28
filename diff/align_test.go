package diff

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func makeItem(id, step, content, pathKey string) item {
	return item{
		node:    &model.Node{ID: id, Status: model.StatusOK},
		step:    step,
		content: content,
		pathKey: pathKey,
	}
}

func TestAlignExactStepKeyMatches(t *testing.T) {
	a := []item{
		makeItem("a1", "sk1", "c1", "pk1"),
		makeItem("a2", "sk2", "c2", "pk2"),
	}
	b := []item{
		makeItem("b1", "sk1", "c1", "pk1"),
		makeItem("b2", "sk2", "c2", "pk2"),
	}
	matched, ra, rb := alignItems(a, b)
	assert.Len(t, matched, 2)
	assert.Empty(t, ra)
	assert.Empty(t, rb)
	assert.Equal(t, [2]int{0, 0}, matched[0])
	assert.Equal(t, [2]int{1, 1}, matched[1])
}

func TestAlignUnmatchedGoToResidual(t *testing.T) {
	a := []item{makeItem("a1", "sk1", "c1", "pk1")}
	b := []item{makeItem("b1", "sk2", "c2", "pk2")}
	matched, ra, rb := alignItems(a, b)
	assert.Empty(t, matched)
	assert.Equal(t, []int{0}, ra)
	assert.Equal(t, []int{0}, rb)
}

func TestAlignGreedyMultisetOrder(t *testing.T) {
	a := []item{
		makeItem("a1", "sk1", "c1", "pk1"),
		makeItem("a2", "sk1", "c1", "pk1"),
	}
	b := []item{
		makeItem("b1", "sk1", "c1", "pk1"),
		makeItem("b2", "sk1", "c1", "pk1"),
	}
	matched, ra, rb := alignItems(a, b)
	assert.Len(t, matched, 2)
	assert.Empty(t, ra)
	assert.Empty(t, rb)
}

func TestMatchExactSkipsPreUsedA(t *testing.T) {
	a := []item{makeItem("a1", "sk1", "c1", "pk1")}
	b := []item{makeItem("b1", "sk1", "c1", "pk1")}
	usedA := []bool{true}
	usedB := []bool{false}
	var matched [][2]int
	matchExact(a, b, usedA, usedB, &matched, func(it item) string { return it.step })
	assert.Empty(t, matched)
	assert.False(t, usedB[0])
}

func TestLessItemNilTStart(t *testing.T) {
	withTime := makeItem("a", "sk1", "c1", "pk1")
	ts1 := time.Unix(1, 0).UTC()
	withTime.node.TStart = &ts1
	noTime := makeItem("b", "sk1", "c1", "pk1")

	assert.True(t, lessItem(withTime, noTime))
	assert.False(t, lessItem(noTime, withTime))
}

func TestLessItemEqualTStartTieBreaks(t *testing.T) {
	ts1 := time.Unix(1, 0).UTC()

	sameTime := func(id, step, content string) item {
		it := makeItem(id, step, content, "pk1")
		it.node.TStart = &ts1
		return it
	}

	a := sameTime("a", "sk1", "c1")
	b := sameTime("b", "sk2", "c2")
	assert.True(t, lessItem(a, b))
	assert.False(t, lessItem(b, a))

	c := sameTime("c", "sk1", "c1")
	d := sameTime("d", "sk1", "c2")
	assert.True(t, lessItem(c, d))
	assert.False(t, lessItem(d, c))

	e := sameTime("a-id", "sk1", "c1")
	f := sameTime("b-id", "sk1", "c1")
	assert.True(t, lessItem(e, f))
	assert.False(t, lessItem(f, e))
}

func TestDriftAlignsCommonStepsByContent(t *testing.T) {
	turnA := &model.Node{ID: "turnA", Type: model.NodeAssistantTurn, Status: model.StatusOK}
	turnB := &model.Node{ID: "turnB", Type: model.NodeAssistantTurn, Status: model.StatusOK}

	ls := func(id string, sec int64) *model.Node {
		n := &model.Node{ID: id, Type: model.NodeToolCall, Name: "Bash", Status: model.StatusOK, TStart: ts(sec)}
		n.Payload = &model.Payload{Input: []byte(`{"command":"ls"}`)}
		return n
	}
	cat := func(id string, sec int64) *model.Node {
		n := &model.Node{ID: id, Type: model.NodeToolCall, Name: "Bash", Status: model.StatusOK, TStart: ts(sec)}
		n.Payload = &model.Payload{Input: []byte(`{"command":"cat x"}`)}
		return n
	}
	whoami := func(id string, sec int64) *model.Node {
		n := &model.Node{ID: id, Type: model.NodeToolCall, Name: "Bash", Status: model.StatusOK, TStart: ts(sec)}
		n.Payload = &model.Payload{Input: []byte(`{"command":"whoami"}`)}
		return n
	}

	nodesA := []*model.Node{turnA, ls("a_ls", 3), cat("a_cat", 4)}
	nodesB := []*model.Node{turnB, whoami("b_whoami", 2), ls("b_ls", 3), cat("b_cat", 4)}
	edgesA := []*model.Edge{
		{Type: model.EdgeParentChild, Src: "turnA", Dst: "a_ls"},
		{Type: model.EdgeParentChild, Src: "turnA", Dst: "a_cat"},
	}
	edgesB := []*model.Edge{
		{Type: model.EdgeParentChild, Src: "turnB", Dst: "b_whoami"},
		{Type: model.EdgeParentChild, Src: "turnB", Dst: "b_ls"},
		{Type: model.EdgeParentChild, Src: "turnB", Dst: "b_cat"},
	}

	result := DiffGraphs(nodesA, edgesA, nodesB, edgesB)

	require.Len(t, result.Added, 1, "whoami should be Added")
	assert.Equal(t, "Bash", result.Added[0].Tool, "wrong tool")
	assert.Empty(t, result.Removed)
	assert.Empty(t, result.Changed)
	require.Len(t, result.Unchanged, 2, "ls and cat should be Unchanged")

	for _, u := range result.Unchanged {
		assert.Equal(t, "content", u.Tier, "should be aligned by content key")
		assert.NotEqual(t, u.AStepKey, u.BStepKey, "step_keys must differ (drift visible)")
	}
}

func TestMatchUniqueSkipsNonUniqueContent(t *testing.T) {
	a := []item{
		makeItem("a1", "sk1", "same", "pk1"),
		makeItem("a2", "sk2", "same", "pk2"),
	}
	b := []item{
		makeItem("b1", "sk3", "same", "pk3"),
	}
	usedA := make([]bool, len(a))
	usedB := make([]bool, len(b))
	var matched [][2]int
	matchUnique(a, b, usedA, usedB, &matched, func(it item) string { return it.content })
	assert.Empty(t, matched)
	assert.Equal(t, []bool{false, false}, usedA)
	assert.Equal(t, []bool{false}, usedB)
}

func TestMatchLCSPairsResiduals(t *testing.T) {
	a := []item{
		makeItem("a1", "sk1", "same", "pk1"),
		makeItem("a2", "sk2", "same", "pk2"),
	}
	b := []item{
		makeItem("b1", "sk3", "same", "pk3"),
	}
	usedA := []bool{false, false}
	usedB := []bool{false}
	var matched [][2]int
	matchLCS(a, b, usedA, usedB, &matched)
	require.Len(t, matched, 1)
	assert.Equal(t, [2]int{0, 0}, matched[0])
	assert.True(t, usedA[0])
	assert.False(t, usedA[1])
	assert.True(t, usedB[0])
}

func TestChangedBashCommandIsChangedNotAddRemove(t *testing.T) {
	makeCmd := func(id, cmd string, sec int64, parent string) (*model.Node, *model.Edge) {
		n := &model.Node{ID: id, Type: model.NodeToolCall, Name: "Bash", Status: model.StatusOK, TStart: ts(sec)}
		n.Payload = &model.Payload{Input: []byte(`{"command":"` + cmd + `"}`)}
		e := &model.Edge{Type: model.EdgeParentChild, Src: parent, Dst: id}
		return n, e
	}

	turnA := &model.Node{ID: "tA", Type: model.NodeAssistantTurn, Status: model.StatusOK}
	turnB := &model.Node{ID: "tB", Type: model.NodeAssistantTurn, Status: model.StatusOK}

	lsA, eLA := makeCmd("a_ls", "ls -a", 1, "tA")
	catA, eCA := makeCmd("a_cat", "cat x", 2, "tA")
	lsB, eLB := makeCmd("b_ls", "ls -a", 1, "tB")
	catB, eCB := makeCmd("b_cat", "cat y", 2, "tB")

	nodesA := []*model.Node{turnA, lsA, catA}
	nodesB := []*model.Node{turnB, lsB, catB}
	edgesA := []*model.Edge{eLA, eCA}
	edgesB := []*model.Edge{eLB, eCB}

	result := DiffGraphs(nodesA, edgesA, nodesB, edgesB)

	assert.Empty(t, result.Added, "no Added expected")
	assert.Empty(t, result.Removed, "no Removed expected")
	require.Len(t, result.Unchanged, 1, "ls -a should be Unchanged")
	assert.Equal(t, "step_key", result.Unchanged[0].Tier)
	require.Len(t, result.Changed, 1, "cat should be Changed")
	assert.Equal(t, "position", result.Changed[0].Tier)
	require.NotNil(t, result.Changed[0].Deltas.Args)
}

func TestPositionDoesNotPairDifferentTools(t *testing.T) {
	turnA := &model.Node{ID: "tA", Type: model.NodeAssistantTurn, Status: model.StatusOK}
	turnB := &model.Node{ID: "tB", Type: model.NodeAssistantTurn, Status: model.StatusOK}

	bashA := &model.Node{ID: "a_bash", Type: model.NodeToolCall, Name: "Bash", Status: model.StatusOK, TStart: ts(1)}
	bashA.Payload = &model.Payload{Input: []byte(`{"command":"ls"}`)}
	readB := &model.Node{ID: "b_read", Type: model.NodeToolCall, Name: "Read", Status: model.StatusOK, TStart: ts(1)}
	readB.Payload = &model.Payload{Input: []byte(`{"file_path":"x"}`)}

	nodesA := []*model.Node{turnA, bashA}
	nodesB := []*model.Node{turnB, readB}
	edgesA := []*model.Edge{{Type: model.EdgeParentChild, Src: "tA", Dst: "a_bash"}}
	edgesB := []*model.Edge{{Type: model.EdgeParentChild, Src: "tB", Dst: "b_read"}}

	result := DiffGraphs(nodesA, edgesA, nodesB, edgesB)

	require.Len(t, result.Removed, 1, "Bash should be Removed")
	require.Len(t, result.Added, 1, "Read should be Added")
	assert.Empty(t, result.Changed, "no Changed expected: different tools")
	assert.Equal(t, "Bash", result.Removed[0].Tool)
	assert.Equal(t, "Read", result.Added[0].Tool)
}

func TestDriftWithRepeatedContentAlignsViaLCS(t *testing.T) {
	makeCmd := func(id, cmd string, sec int64) *model.Node {
		n := &model.Node{ID: id, Type: model.NodeToolCall, Name: "Bash", Status: model.StatusOK, TStart: ts(sec)}
		n.Payload = &model.Payload{Input: []byte(`{"command":"` + cmd + `"}`)}
		return n
	}

	turnA := &model.Node{ID: "tA", Type: model.NodeAssistantTurn, Status: model.StatusOK}
	turnB := &model.Node{ID: "tB", Type: model.NodeAssistantTurn, Status: model.StatusOK}

	nodesA := []*model.Node{
		turnA,
		makeCmd("a_ls1", "ls", 1),
		makeCmd("a_ls2", "ls", 2),
		makeCmd("a_cat", "cat x", 3),
	}
	nodesB := []*model.Node{
		turnB,
		makeCmd("b_echo", "echo hi", 1),
		makeCmd("b_ls1", "ls", 2),
		makeCmd("b_ls2", "ls", 3),
		makeCmd("b_cat", "cat x", 4),
	}
	edgesA := []*model.Edge{
		{Type: model.EdgeParentChild, Src: "tA", Dst: "a_ls1"},
		{Type: model.EdgeParentChild, Src: "tA", Dst: "a_ls2"},
		{Type: model.EdgeParentChild, Src: "tA", Dst: "a_cat"},
	}
	edgesB := []*model.Edge{
		{Type: model.EdgeParentChild, Src: "tB", Dst: "b_echo"},
		{Type: model.EdgeParentChild, Src: "tB", Dst: "b_ls1"},
		{Type: model.EdgeParentChild, Src: "tB", Dst: "b_ls2"},
		{Type: model.EdgeParentChild, Src: "tB", Dst: "b_cat"},
	}

	result := DiffGraphs(nodesA, edgesA, nodesB, edgesB)

	require.Len(t, result.Added, 1, "echo should be Added")
	assert.Empty(t, result.Removed)
	assert.Empty(t, result.Changed)
	require.Len(t, result.Unchanged, 3, "ls, ls, cat should be Unchanged via LCS")
}
