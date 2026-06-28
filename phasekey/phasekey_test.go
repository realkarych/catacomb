package phasekey

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPhaseKeyDistinctByOccurrenceAndName(t *testing.T) {
	a := Compute("step1", "review", 0)
	b := Compute("step1", "review", 1)
	c := Compute("step1", "build", 0)
	d := Compute("step2", "review", 0)
	assert.Len(t, a, 32)
	assert.NotEqual(t, a, b)
	assert.NotEqual(t, a, c)
	assert.NotEqual(t, a, d)
	assert.Equal(t, a, Compute("step1", "review", 0))
}
