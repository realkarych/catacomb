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
	c = c.Bump(ReasonUnknownSubtype)
	require.Equal(t, Counts{ReasonUnknownRecordType: 2, ReasonUnknownSubtype: 1}, c)
}

func TestMergeNilAndNonNil(t *testing.T) {
	var c Counts
	assert.Nil(t, c.Merge(nil))
	c = c.Merge(Counts{ReasonUnknownSpanName: 3})
	c = c.Bump(ReasonUnknownSpanName)
	merged := c.Merge(Counts{ReasonUnknownHookEvent: 1})
	assert.Equal(t, Counts{ReasonUnknownSpanName: 4, ReasonUnknownHookEvent: 1}, merged)
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.1.0", "2.1.0", 0},
		{"2.1", "2.1.0", 0},
		{"2.1.1", "2.1.0", 1},
		{"2.0.9", "2.1.0", -1},
		{"10.0", "9.9", 1},
		{"2.1.1-beta", "2.1.1", 0},
		{"beta", "0", 0},
		{"", "0.0.0", 0},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, CompareVersions(tc.a, tc.b), "%s vs %s", tc.a, tc.b)
	}
}

func TestNewerThanTested(t *testing.T) {
	assert.False(t, NewerThanTested(TestedClaudeCodeVersion))
	assert.False(t, NewerThanTested(""))
	assert.True(t, NewerThanTested("9999.0.0"))
}

func TestReasonConstants(t *testing.T) {
	assert.Equal(t, "unknown_hook_event", ReasonUnknownHookEvent)
	assert.Equal(t, "unknown_record_type", ReasonUnknownRecordType)
	assert.Equal(t, "unknown_subtype", ReasonUnknownSubtype)
	assert.Equal(t, "unknown_span_name", ReasonUnknownSpanName)
	assert.Equal(t, "unknown_content_block", ReasonUnknownContentBlock)
}

func TestContentBlockBump(t *testing.T) {
	var c Counts
	c = c.Bump(ReasonUnknownContentBlock)
	require.Equal(t, Counts{ReasonUnknownContentBlock: 1}, c)
}
