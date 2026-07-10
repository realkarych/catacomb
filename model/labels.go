package model

import (
	"regexp"
	"sort"
	"strings"
)

const (
	maxLabelPairs    = 32
	maxLabelValueLen = 256
)

var labelKeyRe = regexp.MustCompile(`^[a-z0-9_.-]{1,64}$`)

func ParseLabels(s string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if !labelKeyRe.MatchString(k) || len(v) > maxLabelValueLen {
			continue
		}
		if _, seen := out[k]; !seen && len(out) >= maxLabelPairs {
			continue
		}
		out[k] = v
	}
	return out
}

func FormatLabels(l map[string]string) string {
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+l[k])
	}
	return strings.Join(parts, ",")
}

func MergeLabels(dst, src map[string]string) map[string]string {
	if dst == nil {
		dst = map[string]string{}
	}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
