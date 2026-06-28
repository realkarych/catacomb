package diff

import "sort"

func alignItems(a, b []item) (matched [][2]int, ra []int, rb []int) {
	usedA := make([]bool, len(a))
	usedB := make([]bool, len(b))

	matchExact(a, b, usedA, usedB, &matched, func(it item) string { return it.step })
	matchUnique(a, b, usedA, usedB, &matched, func(it item) string { return it.content })
	matchLCS(a, b, usedA, usedB, &matched)
	matchExact(a, b, usedA, usedB, &matched, positionKey)

	for i := range a {
		if !usedA[i] {
			ra = append(ra, i)
		}
	}
	for i := range b {
		if !usedB[i] {
			rb = append(rb, i)
		}
	}
	return matched, ra, rb
}

func matchLCS(a, b []item, usedA, usedB []bool, matched *[][2]int) {
	var unusedA, unusedB []int
	var contentA, contentB []string
	for i, it := range a {
		if !usedA[i] {
			unusedA = append(unusedA, i)
			contentA = append(contentA, it.content)
		}
	}
	for j, it := range b {
		if !usedB[j] {
			unusedB = append(unusedB, j)
			contentB = append(contentB, it.content)
		}
	}
	pairs := lcsPairs(contentA, contentB)
	for _, p := range pairs {
		ai := unusedA[p[0]]
		bi := unusedB[p[1]]
		usedA[ai] = true
		usedB[bi] = true
		*matched = append(*matched, [2]int{ai, bi})
	}
}

func matchUnique(a, b []item, usedA, usedB []bool, matched *[][2]int, key func(item) string) {
	countA := countKey(a, usedA, key)
	countB := countKey(b, usedB, key)

	firstA := firstIndex(a, usedA, key)
	firstB := firstIndex(b, usedB, key)

	keys := make([]string, 0, len(firstA))
	for k := range firstA {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		idxA := firstA[k]
		if countA[k] != 1 || countB[k] != 1 {
			continue
		}
		idxB := firstB[k]
		usedA[idxA] = true
		usedB[idxB] = true
		*matched = append(*matched, [2]int{idxA, idxB})
	}
}

func countKey(items []item, used []bool, key func(item) string) map[string]int {
	m := map[string]int{}
	for i, it := range items {
		if !used[i] {
			m[key(it)]++
		}
	}
	return m
}

func firstIndex(items []item, used []bool, key func(item) string) map[string]int {
	m := map[string]int{}
	for i, it := range items {
		if used[i] {
			continue
		}
		k := key(it)
		if _, exists := m[k]; !exists {
			m[k] = i
		}
	}
	return m
}

func positionKey(it item) string {
	return it.pathKey + "\x1f" + string(it.node.Type) + "\x1f" + it.node.Name + "\x1f" + it.node.SubagentType
}

func matchExact(a, b []item, usedA, usedB []bool, matched *[][2]int, key func(item) string) {
	available := map[string][]int{}
	for j, it := range b {
		if !usedB[j] {
			k := key(it)
			available[k] = append(available[k], j)
		}
	}
	nextB := map[string]int{}
	for i, it := range a {
		if usedA[i] {
			continue
		}
		k := key(it)
		candidates := available[k]
		start := nextB[k]
		nextB[k] = start
		if start >= len(candidates) {
			continue
		}
		j := candidates[start]
		usedA[i] = true
		usedB[j] = true
		nextB[k] = start + 1
		*matched = append(*matched, [2]int{i, j})
	}
}
