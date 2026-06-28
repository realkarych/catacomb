package diff

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLCSPairsNoMatch(t *testing.T) {
	assert.Nil(t, lcsPairs([]string{"x"}, []string{"y"}))
}

func TestLCSPairsSubsequence(t *testing.T) {
	got := lcsPairs([]string{"a", "b", "c"}, []string{"a", "c"})
	assert.Equal(t, [][2]int{{0, 0}, {2, 1}}, got)
}

func TestLCSPairsRepeatedContent(t *testing.T) {
	got := lcsPairs([]string{"a", "a"}, []string{"z", "a", "a"})
	assert.Equal(t, [][2]int{{0, 1}, {1, 2}}, got)
}

func TestLCSPairsTieRule(t *testing.T) {
	got := lcsPairs([]string{"a", "b"}, []string{"b", "a"})
	assert.Equal(t, [][2]int{{1, 0}}, got)
}

func TestLCSPairsEmptyA(t *testing.T) {
	assert.Nil(t, lcsPairs(nil, []string{"a"}))
}

func TestLCSPairsEmptyB(t *testing.T) {
	assert.Nil(t, lcsPairs([]string{"a"}, nil))
}
