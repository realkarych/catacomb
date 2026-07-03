package drift

import (
	"strconv"
	"strings"
)

const TestedClaudeCodeVersion = "2.1.199"

const (
	ReasonUnknownHookEvent    = "unknown_hook_event"
	ReasonUnknownRecordType   = "unknown_record_type"
	ReasonUnknownSubtype      = "unknown_subtype"
	ReasonUnknownSpanName     = "unknown_span_name"
	ReasonUnknownContentBlock = "unknown_content_block"
)

type Counts map[string]uint64

func (c Counts) Bump(reason string) Counts {
	if c == nil {
		c = Counts{}
	}
	c[reason]++
	return c
}

func (c Counts) Merge(other Counts) Counts {
	if len(other) == 0 {
		return c
	}
	if c == nil {
		c = Counts{}
	}
	for reason, n := range other {
		c[reason] += n
	}
	return c
}

func NewerThanTested(v string) bool {
	return CompareVersions(v, TestedClaudeCodeVersion) > 0
}

func CompareVersions(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		av, bv := segment(as, i), segment(bs, i)
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

func segment(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	s := parts[i]
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	v, err := strconv.Atoi(s[:end])
	if err != nil {
		return 0
	}
	return v
}
