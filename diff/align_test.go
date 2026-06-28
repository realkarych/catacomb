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
	matched, ra, rb := alignItems(a, b)
	assert.Empty(t, matched)
	assert.Equal(t, []int{0, 1}, ra)
	assert.Equal(t, []int{0}, rb)
}
