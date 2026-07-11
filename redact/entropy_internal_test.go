package redact

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShannonEntropy(t *testing.T) {
	require.Equal(t, 0.0, shannonEntropy(""))
	require.Equal(t, 0.0, shannonEntropy("aaaa"))
	require.InDelta(t, 1.0, shannonEntropy("abab"), 1e-9)
	require.Greater(t, shannonEntropy("9f86d081884c7d659a2feaa0c55ad015"), 3.2)
	require.Less(t, shannonEntropy("deadbeefcafe1234deadbeefcafe1234"), 3.2)
	require.Less(t, shannonEntropy("thisisalonglowercaseenglishlooking"), 4.0)
}
