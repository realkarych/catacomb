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

func TestTestedClaudeCodeVersionCeiling(t *testing.T) {
	assert.Equal(t, "2.1.199", TestedClaudeCodeVersion)
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
	assert.False(t, NewerThanTested("1.0.0"))
	assert.True(t, NewerThanTested("9999.0.0"))
}

func TestRuntimeConstants(t *testing.T) {
	require.Equal(t, "0.144.5", TestedCodexVersion)
	require.Equal(t, "claude-code", RuntimeClaudeCode)
	require.Equal(t, "codex", RuntimeCodex)
}

func TestNewerThanTestedFor(t *testing.T) {
	cases := []struct {
		name, runtime, v string
		want             bool
	}{
		{"codex newer", RuntimeCodex, "0.145.0", true},
		{"codex equal", RuntimeCodex, TestedCodexVersion, false},
		{"codex older", RuntimeCodex, "0.133.0", false},
		{"claude delegates", RuntimeClaudeCode, "99.0.0", true},
		{"unknown runtime never warns", "gemini", "99.0.0", false},
		{"empty version", RuntimeCodex, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, NewerThanTestedFor(tc.runtime, tc.v))
		})
	}
}
