package drift

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBumpAllocatesLazily(t *testing.T) {
	var c Counts
	c = c.Bump(ReasonUnknownRecordType)
	c = c.Bump(ReasonUnknownRecordType)
	c = c.Bump(ReasonUnknownContentBlock)
	require.Equal(t, Counts{ReasonUnknownRecordType: 2, ReasonUnknownContentBlock: 1}, c)
}

func TestMergeNilAndNonNil(t *testing.T) {
	var c Counts
	assert.Nil(t, c.Merge(nil))
	c = c.Merge(Counts{ReasonUnknownContentBlock: 3})
	c = c.Bump(ReasonUnknownContentBlock)
	merged := c.Merge(Counts{ReasonUnknownRecordType: 1})
	assert.Equal(t, Counts{ReasonUnknownContentBlock: 4, ReasonUnknownRecordType: 1}, merged)
}

func TestReasonConstants(t *testing.T) {
	assert.Equal(t, "unknown_record_type", ReasonUnknownRecordType)
	assert.Equal(t, "unknown_content_block", ReasonUnknownContentBlock)
}
