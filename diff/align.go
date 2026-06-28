package diff

func alignItems(a, b []item) (matched [][2]int, ra []int, rb []int) {
	usedA := make([]bool, len(a))
	usedB := make([]bool, len(b))

	matchExact(a, b, usedA, usedB, &matched, func(it item) string { return it.step })

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
