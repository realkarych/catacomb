package aggregate

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func cellFixtureGroup() []RunGraph {
	t0 := fixtureBase
	rb := RunGraph{
		Run: model.Run{ID: "cell-b", Labels: map[string]string{"arm": "baseline"}, StartedAt: tp(t0), EndedAt: tp(t0.Add(5 * time.Second))},
		Nodes: []*model.Node{
			{ID: "b-turn1", RunID: "cell-b", Type: model.NodeAssistantTurn, CostUSD: f64(1.5), TokensIn: i64(100), TokensOut: i64(40)},
			{ID: "b-tool1", RunID: "cell-b", Type: model.NodeToolCall, CostUSD: f64(0.5), TokensIn: i64(20), TokensOut: i64(10)},
		},
	}
	ra := RunGraph{
		Run: model.Run{ID: "cell-a", StartedAt: tp(t0)},
		Nodes: []*model.Node{
			{ID: "a-sess", RunID: "cell-a", Type: model.NodeSession},
			{ID: "a-turn1", RunID: "cell-a", Type: model.NodeAssistantTurn, CostUSD: f64(2), TokensIn: i64(200), TokensOut: i64(80)},
			{ID: "a-turn2", RunID: "cell-a", Type: model.NodeAssistantTurn},
			{ID: "a-turn3", RunID: "cell-a", Type: model.NodeAssistantTurn, TokensIn: i64(50)},
		},
	}
	return []RunGraph{rb, ra}
}

func TestCells(t *testing.T) {
	tests := []struct {
		name  string
		group []RunGraph
		want  []Cell
	}{
		{name: "nil group", group: nil, want: []Cell{}},
		{name: "empty group", group: []RunGraph{}, want: []Cell{}},
		{
			name:  "sums turns labels sort and unmeasured duration",
			group: cellFixtureGroup(),
			want: []Cell{
				{RunID: "cell-a", DurationMS: 0, CostUSD: 2, TokensIn: 250, TokensOut: 80, Turns: 3},
				{RunID: "cell-b", Labels: map[string]string{"arm": "baseline"}, DurationMS: 5000, CostUSD: 2, TokensIn: 120, TokensOut: 50, Turns: 1},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Cells(tc.group))
		})
	}
}

func TestCellJSONTags(t *testing.T) {
	tests := []struct {
		name string
		cell Cell
		want string
	}{
		{
			name: "all fields",
			cell: Cell{RunID: "r", Labels: map[string]string{"k": "v"}, DurationMS: 1, CostUSD: 2, TokensIn: 3, TokensOut: 4, Turns: 5},
			want: `{"run_id":"r","labels":{"k":"v"},"duration_ms":1,"cost_usd":2,"tokens_in":3,"tokens_out":4,"turns":5}`,
		},
		{
			name: "nil labels omitted",
			cell: Cell{RunID: "r2"},
			want: `{"run_id":"r2","duration_ms":0,"cost_usd":0,"tokens_in":0,"tokens_out":0,"turns":0}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.cell)
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(b))
		})
	}
}
