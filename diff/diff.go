package diff

import (
	"encoding/json"
	"sort"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/redact"
	"github.com/realkarych/catacomb/stepkey"
)

type Step struct {
	Type       string `json:"type"`
	Tool       string `json:"tool"`
	StepKey    string `json:"step_key"`
	ContentKey string `json:"content_key"`
}

type Match struct {
	Type        string `json:"type"`
	Tool        string `json:"tool"`
	AStepKey    string `json:"a_step_key"`
	BStepKey    string `json:"b_step_key"`
	AContentKey string `json:"a_content_key"`
	BContentKey string `json:"b_content_key"`
	Tier        string `json:"tier"`
}

type StringChange struct {
	Before string `json:"before"`
	After  string `json:"after"`
}

type FloatChange struct {
	Before float64 `json:"before"`
	After  float64 `json:"after"`
	Delta  float64 `json:"delta"`
}

type IntChange struct {
	Before int64 `json:"before"`
	After  int64 `json:"after"`
	Delta  int64 `json:"delta"`
}

type Deltas struct {
	Args       *StringChange `json:"args,omitempty"`
	Status     *StringChange `json:"status,omitempty"`
	CostUSD    *FloatChange  `json:"cost_usd,omitempty"`
	DurationMS *IntChange    `json:"duration_ms,omitempty"`
	TokensIn   *IntChange    `json:"tokens_in,omitempty"`
	TokensOut  *IntChange    `json:"tokens_out,omitempty"`
}

type ChangedStep struct {
	Match
	Deltas Deltas `json:"deltas"`
}

type DiffResult struct {
	Added     []Step        `json:"added"`
	Removed   []Step        `json:"removed"`
	Changed   []ChangedStep `json:"changed"`
	Unchanged []Match       `json:"unchanged"`
}

type item struct {
	node    *model.Node
	step    string
	content string
	pathKey string
}

func withoutMarkers(nodes []*model.Node, edges []*model.Edge) ([]*model.Node, []*model.Edge) {
	markers := make(map[string]bool)
	keptNodes := make([]*model.Node, 0, len(nodes))
	for _, n := range nodes {
		if n.Type == model.NodeMarker {
			markers[n.ID] = true
			continue
		}
		keptNodes = append(keptNodes, n)
	}
	keptEdges := make([]*model.Edge, 0, len(edges))
	for _, e := range edges {
		if markers[e.Src] || markers[e.Dst] {
			continue
		}
		keptEdges = append(keptEdges, e)
	}
	return keptNodes, keptEdges
}

func buildItems(nodes []*model.Node, edges []*model.Edge) []item {
	keys := stepkey.Compute(withoutMarkers(nodes, edges))
	items := make([]item, 0, len(keys))
	for _, n := range nodes {
		k, ok := keys[n.ID]
		if !ok {
			continue
		}
		items = append(items, item{
			node:    n,
			step:    k.Key,
			content: k.Content,
			pathKey: k.PathKey,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return lessItem(items[i], items[j])
	})
	return items
}

func lessItem(a, b item) bool {
	at, bt := a.node.TStart, b.node.TStart
	switch {
	case at != nil && bt != nil && !at.Equal(*bt):
		return at.Before(*bt)
	case at == nil && bt != nil:
		return false
	case at != nil && bt == nil:
		return true
	}
	if a.step != b.step {
		return a.step < b.step
	}
	if a.content != b.content {
		return a.content < b.content
	}
	return a.node.ID < b.node.ID
}

func DiffGraphs(
	an []*model.Node, ae []*model.Edge,
	bn []*model.Node, be []*model.Edge,
) DiffResult {
	a := buildItems(an, ae)
	b := buildItems(bn, be)

	matched, ra, rb := alignItems(a, b)

	result := DiffResult{
		Added:     make([]Step, 0),
		Removed:   make([]Step, 0),
		Changed:   make([]ChangedStep, 0),
		Unchanged: make([]Match, 0),
	}

	for _, pair := range matched {
		ai, bi := pair[0], pair[1]
		ia, ib := a[ai], b[bi]
		d := deltaOf(ia, ib)
		m := Match{
			Type:        string(ia.node.Type),
			Tool:        ia.node.Name,
			AStepKey:    ia.step,
			BStepKey:    ib.step,
			AContentKey: ia.content,
			BContentKey: ib.content,
			Tier:        tierOf(ia, ib),
		}
		if isZeroDeltas(d) {
			result.Unchanged = append(result.Unchanged, m)
		} else {
			result.Changed = append(result.Changed, ChangedStep{Match: m, Deltas: d})
		}
	}

	for _, i := range ra {
		result.Removed = append(result.Removed, stepOf(a[i]))
	}
	for _, i := range rb {
		result.Added = append(result.Added, stepOf(b[i]))
	}

	return result
}

func tierOf(ia, ib item) string {
	if ia.step == ib.step {
		return "step_key"
	}
	if ia.content == ib.content {
		return "content"
	}
	return "position"
}

func isZeroDeltas(d Deltas) bool {
	return d.Args == nil && d.Status == nil && d.CostUSD == nil &&
		d.DurationMS == nil && d.TokensIn == nil && d.TokensOut == nil
}

func stepOf(it item) Step {
	return Step{
		Type:       string(it.node.Type),
		Tool:       it.node.Name,
		StepKey:    it.step,
		ContentKey: it.content,
	}
}

func deltaOf(a, b item) Deltas {
	var d Deltas
	na, argB := normArgs(a.node), normArgs(b.node)
	if na != argB {
		d.Args = &StringChange{Before: na, After: argB}
	}
	if string(a.node.Status) != string(b.node.Status) {
		d.Status = &StringChange{Before: string(a.node.Status), After: string(b.node.Status)}
	}
	if fc := floatDelta(a.node.CostUSD, b.node.CostUSD); fc != nil {
		d.CostUSD = fc
	}
	if ic := intDelta(a.node.DurationMS, b.node.DurationMS); ic != nil {
		d.DurationMS = ic
	}
	if ic := intDelta(a.node.TokensIn, b.node.TokensIn); ic != nil {
		d.TokensIn = ic
	}
	if ic := intDelta(a.node.TokensOut, b.node.TokensOut); ic != nil {
		d.TokensOut = ic
	}
	return d
}

func floatDelta(a, b *float64) *FloatChange {
	av, bv := deref64(a), deref64(b)
	if av == bv {
		return nil
	}
	return &FloatChange{Before: av, After: bv, Delta: bv - av}
}

func deref64(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func intDelta(a, b *int64) *IntChange {
	av, bv := derefI(a), derefI(b)
	if av == bv {
		return nil
	}
	return &IntChange{Before: av, After: bv, Delta: bv - av}
}

func derefI(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func normArgs(n *model.Node) string {
	if n.Payload == nil {
		return ""
	}
	red := redact.Redact(n.Payload.Input).Data
	var v any
	if err := json.Unmarshal(red, &v); err != nil {
		return string(red)
	}
	out, _ := json.Marshal(v)
	return string(out)
}
