package stepkey

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/redact"
)

const Method = "heuristic"

const scheme = "stepkey/v1"

type Key struct {
	Key    string
	Method string
}

func eligible(t model.NodeType) bool {
	switch t {
	case model.NodeToolCall, model.NodeMCPCall, model.NodeSubagent:
		return true
	default:
		return false
	}
}

func live(n *model.Node) bool {
	return n.Status != model.StatusSuperseded && n.Status != model.StatusAbandoned
}

type builder struct {
	byID     map[string]*model.Node
	parentOf map[string]string
	children map[string][]string
	terms    map[string]string
}

func Compute(nodes []*model.Node, edges []*model.Edge) map[string]Key {
	b := &builder{
		byID:     make(map[string]*model.Node, len(nodes)),
		parentOf: make(map[string]string, len(nodes)),
		children: map[string][]string{},
		terms:    make(map[string]string, len(nodes)),
	}
	for _, n := range nodes {
		b.byID[n.ID] = n
	}
	for _, e := range edges {
		if e.Type != model.EdgeParentChild {
			continue
		}
		b.parentOf[e.Dst] = e.Src
		b.children[e.Src] = append(b.children[e.Src], e.Dst)
	}
	for _, n := range nodes {
		b.terms[n.ID] = terminal(n)
	}
	out := map[string]Key{}
	for _, n := range nodes {
		if !eligible(n.Type) || !live(n) {
			continue
		}
		out[n.ID] = Key{Key: b.compute(n), Method: Method}
	}
	return out
}

func (b *builder) compute(n *model.Node) string {
	var levels []string
	cur := n.ID
	for {
		p, ok := b.parentOf[cur]
		if !ok {
			break
		}
		cn := b.byID[cur]
		if cn != nil && live(cn) {
			idx := b.liveIndex(p, cur)
			levels = append(levels, string(cn.Type)+"#"+strconv.Itoa(idx))
		}
		cur = p
	}
	slices.Reverse(levels)
	var sb strings.Builder
	sb.WriteString(scheme)
	sb.WriteByte('|')
	sb.WriteString(strings.Join(levels, "/"))
	sb.WriteByte('|')
	sb.WriteString(b.terms[n.ID])
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:16])
}

func (b *builder) liveIndex(parent, target string) int {
	sibs := make([]*model.Node, 0, len(b.children[parent]))
	for _, c := range b.children[parent] {
		cn := b.byID[c]
		if cn != nil && live(cn) {
			sibs = append(sibs, cn)
		}
	}
	sort.Slice(sibs, func(i, j int) bool {
		return b.lessSibling(sibs[i], sibs[j])
	})
	for i, s := range sibs {
		if s.ID == target {
			return i
		}
	}
	return 0
}

func (b *builder) lessSibling(a, c *model.Node) bool {
	at, ct := a.TStart, c.TStart
	switch {
	case at != nil && ct != nil && !at.Equal(*ct):
		return at.Before(*ct)
	case at == nil && ct != nil:
		return false
	case at != nil && ct == nil:
		return true
	}
	return b.sig(a) < b.sig(c)
}

func (b *builder) sig(n *model.Node) string {
	return string(n.Type) + "\x1f" + b.terms[n.ID]
}

func terminal(n *model.Node) string {
	if n.Type == model.NodeSubagent {
		return "agent=" + n.SubagentType
	}
	return "tool=" + n.Name + " in=" + salientHash(n)
}

func salientHash(n *model.Node) string {
	var raw []byte
	if n.Payload != nil {
		raw = n.Payload.Input
	}
	red := redact.Redact(raw).Data
	sum := sha256.Sum256([]byte(salient(n.Name, red)))
	return hex.EncodeToString(sum[:8])
}

func salient(name string, red []byte) string {
	switch normTool(name) {
	case "edit", "multiedit", "write":
		return project(red, "file_path")
	case "read":
		return project(red, "file_path", "path")
	case "bash":
		return project(red, "command")
	default:
		return canon(red)
	}
}

func normTool(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func project(red []byte, keys ...string) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(red, &obj); err != nil {
		return ""
	}
	for _, k := range keys {
		if raw, ok := obj[k]; ok {
			kb, _ := json.Marshal(k)
			return `{` + string(kb) + `:` + string(raw) + `}`
		}
	}
	return ""
}

func canon(red []byte) string {
	var v any
	if err := json.Unmarshal(red, &v); err != nil {
		return string(red)
	}
	out, _ := json.Marshal(v)
	return string(out)
}
