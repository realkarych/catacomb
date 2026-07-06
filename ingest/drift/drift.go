package drift

const (
	ReasonUnknownRecordType   = "unknown_record_type"
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
