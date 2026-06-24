package tui

import (
	"sort"
)

type TreeRow struct {
	Node    Node
	Depth   int
	HasKids bool
}

func BuildTree(g Graph) []TreeRow {
	childToParent := make(map[string]string)
	parentToChildren := make(map[string][]string)
	seqEdges := make(map[string][]string)

	for _, e := range g.Edges {
		switch e.Type {
		case "parent_child":
			childToParent[e.Dst] = e.Src
			parentToChildren[e.Src] = append(parentToChildren[e.Src], e.Dst)
		case "sequence":
			seqEdges[e.Src] = append(seqEdges[e.Src], e.Dst)
		}
	}

	var roots []Node
	for _, n := range g.Nodes {
		if _, hasParent := childToParent[n.ID]; !hasParent {
			roots = append(roots, n)
		}
	}

	sortNodes(roots)

	var rows []TreeRow
	var walk func(n Node, depth int)
	walk = func(n Node, depth int) {
		kids := parentToChildren[n.ID]
		childNodes := make([]Node, 0, len(kids))
		for _, id := range kids {
			if node, ok := g.Nodes[id]; ok {
				childNodes = append(childNodes, node)
			}
		}
		sortSiblings(childNodes, seqEdges)
		rows = append(rows, TreeRow{Node: n, Depth: depth, HasKids: len(childNodes) > 0})
		for _, child := range childNodes {
			walk(child, depth+1)
		}
	}

	for _, root := range roots {
		walk(root, 0)
	}
	return rows
}

func sortNodes(nodes []Node) {
	sort.SliceStable(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		aSession := a.Type == "session"
		bSession := b.Type == "session"
		if aSession != bSession {
			return aSession
		}
		switch {
		case a.TStart != nil && b.TStart != nil:
			if *a.TStart != *b.TStart {
				return *a.TStart < *b.TStart
			}
		case a.TStart != nil:
			return true
		case b.TStart != nil:
			return false
		}
		return a.ID < b.ID
	})
}

func sortSiblings(nodes []Node, seqEdges map[string][]string) {
	if len(nodes) == 0 {
		return
	}

	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}

	inDegree := make(map[string]int)
	for _, id := range ids {
		inDegree[id] = 0
	}

	sibSet := make(map[string]bool)
	for _, id := range ids {
		sibSet[id] = true
	}

	edges := make(map[string][]string)
	for _, src := range ids {
		for _, dst := range seqEdges[src] {
			if sibSet[dst] {
				edges[src] = append(edges[src], dst)
				inDegree[dst]++
			}
		}
	}

	hasSeq := false
	for _, v := range inDegree {
		if v > 0 {
			hasSeq = true
			break
		}
	}

	if !hasSeq {
		sortNodes(nodes)
		return
	}

	nodeMap := make(map[string]Node, len(nodes))
	for _, n := range nodes {
		nodeMap[n.ID] = n
	}

	var queue []string
	for _, id := range ids {
		if inDegree[id] == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)

	var ordered []string
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		ordered = append(ordered, cur)
		nexts := edges[cur]
		sort.Strings(nexts)
		for _, dst := range nexts {
			inDegree[dst]--
			if inDegree[dst] == 0 {
				queue = append(queue, dst)
			}
		}
	}

	seen := make(map[string]bool, len(ordered))
	for _, id := range ordered {
		seen[id] = true
	}
	for _, id := range ids {
		if !seen[id] {
			ordered = append(ordered, id)
		}
	}

	for i, id := range ordered {
		nodes[i] = nodeMap[id]
	}
}

func Flatten(g Graph, expanded map[string]bool) []TreeRow {
	full := BuildTree(g)
	lastAtDepth := make(map[int]string)
	var visible []TreeRow
	for _, row := range full {
		if row.Depth == 0 {
			visible = append(visible, row)
			lastAtDepth[0] = row.Node.ID
		} else {
			parentID := lastAtDepth[row.Depth-1]
			if expanded[parentID] {
				visible = append(visible, row)
				lastAtDepth[row.Depth] = row.Node.ID
			}
		}
	}
	return visible
}
