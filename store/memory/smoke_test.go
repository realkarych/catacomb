package memory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func TestNewRoundTrip(t *testing.T) {
	s := New()
	require.NoError(t, s.Persist([]model.Observation{{ObsID: "o1", ExecutionID: "e", Seq: 1}}, nil, nil))
	got, err := s.ObservationsSince(0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, uint64(1), got[0].Seq)
	require.NoError(t, s.Close())
}
